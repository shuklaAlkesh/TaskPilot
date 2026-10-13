// Package httpapi serves the public API (/v1) and the internal worker API (/internal/v1).
// Handlers decode, validate shape, call jobs, and encode. Contract: backend/api/openapi.yaml.
package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/jobs"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/tasktypes"
)

// Server holds the dependencies shared by both listeners.
type Server struct {
	jobs         *jobs.Service
	types        *tasktypes.Registry
	pool         *pgxpool.Pool
	workerTokens []string
	log          *slog.Logger
	shuttingDown atomic.Bool
}

// New builds a Server.
func New(svc *jobs.Service, types *tasktypes.Registry, pool *pgxpool.Pool, workerTokens []string, log *slog.Logger) *Server {
	return &Server{jobs: svc, types: types, pool: pool, workerTokens: workerTokens, log: log}
}

// SetShuttingDown makes /readyz fail so load balancers drain this instance.
func (s *Server) SetShuttingDown() { s.shuttingDown.Store(true) }

// PublicHandler routes the public listener.
func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("POST /v1/jobs", s.authed(auth.ScopeJobsWrite, s.submitJob))
	mux.HandleFunc("GET /v1/jobs", s.authed(auth.ScopeJobsRead, s.listJobs))
	mux.HandleFunc("GET /v1/jobs/{job_id}", s.authed(auth.ScopeJobsRead, s.getJob))
	mux.HandleFunc("GET /v1/jobs/{job_id}/attempts", s.authed(auth.ScopeJobsRead, s.listAttempts))
	mux.HandleFunc("POST /v1/jobs/{job_id}/cancel", s.authed(auth.ScopeJobsCancel, s.cancelJob))
	mux.HandleFunc("GET /v1/schedules", s.authed(auth.ScopeJobsRead, s.listSchedules))
	mux.HandleFunc("GET /v1/schedules/{name}", s.authed(auth.ScopeJobsRead, s.getSchedule))
	mux.HandleFunc("PUT /v1/schedules/{name}", s.authed(auth.ScopeJobsWrite, s.putSchedule))
	mux.HandleFunc("DELETE /v1/schedules/{name}", s.authed(auth.ScopeJobsWrite, s.deleteSchedule))
	mux.HandleFunc("GET /v1/task-types", s.authed(auth.ScopeJobsRead, s.listTaskTypes))
	mux.HandleFunc("GET /v1/stats", s.authed(auth.ScopeJobsRead, s.stats))
	mux.HandleFunc("GET /v1/me", s.authed("", s.me))
	return s.middleware(mux)
}

// WorkerHandler routes the internal worker listener. It must never be exposed publicly.
func (s *Server) WorkerHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /internal/v1/workers", s.worker(s.registerWorker))
	mux.HandleFunc("POST /internal/v1/claims", s.worker(s.claim))
	mux.HandleFunc("POST /internal/v1/attempts/{attempt_id}/heartbeat", s.worker(s.heartbeat))
	mux.HandleFunc("POST /internal/v1/attempts/{attempt_id}/complete", s.worker(s.complete))
	mux.HandleFunc("POST /internal/v1/attempts/{attempt_id}/fail", s.worker(s.fail))
	mux.HandleFunc("POST /internal/v1/attempts/{attempt_id}/cancelled", s.worker(s.cancelled))
	return s.middleware(mux)
}

type principalHandler func(w http.ResponseWriter, r *http.Request, p auth.Principal)

// authed authenticates the API key and checks scope ("" = any valid key).
func (s *Server) authed(scope string, h principalHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		p, err := auth.Authenticate(r.Context(), s.pool, key)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if scope != "" && !slices.Contains(p.Scopes, scope) {
			writeError(w, r, errInsufficientScope)
			return
		}
		h(w, r, p)
	}
}

func (s *Server) worker(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !auth.WorkerTokenValid(token, s.workerTokens) {
			writeError(w, r, auth.ErrUnauthenticated)
			return
		}
		h(w, r)
	}
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	dbOK := s.pool.Ping(ctx) == nil
	running := !s.shuttingDown.Load()
	state := map[bool]string{true: "ok", false: "failing"}
	status, code := "ready", http.StatusOK
	if !dbOK || !running {
		status, code = "not_ready", http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status": status,
		"checks": map[string]string{"database": state[dbOK], "shutting_down": state[running]},
	})
}

type ctxKey struct{}

var errPanic = errors.New("handler panicked")

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// statusRecorder captures the response status for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// middleware adds a request id, recovers panics, and writes one access-log line per request.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-ID")
		if !requestIDPattern.MatchString(id) {
			id = rand.Text()
		}
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, id))
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if v := recover(); v != nil {
				s.log.ErrorContext(r.Context(), "panic", "request_id", id, "panic", v)
				writeError(rec, r, errPanic)
			}
			s.log.InfoContext(r.Context(), "request", "request_id", id, "method", r.Method, "route", r.Pattern,
				"status", rec.status, "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(rec, r)
	})
}
