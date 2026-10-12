package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/jobs"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/tasktypes"
)

type errorInfo struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable *bool  `json:"retryable,omitempty"`
}

// jobView is the public Job representation (openapi.yaml #/components/schemas/Job).
type jobView struct {
	ID              uuid.UUID        `json:"id"`
	TaskType        string           `json:"task_type"`
	TaskVersion     int              `json:"task_version"`
	Queue           string           `json:"queue"`
	Status          jobs.Status      `json:"status"`
	CancelRequested bool             `json:"cancel_requested"`
	Payload         *json.RawMessage `json:"payload,omitempty"`
	Result          *json.RawMessage `json:"result,omitempty"`
	AttemptCount    int              `json:"attempt_count"`
	MaxAttempts     int              `json:"max_attempts"`
	TimeoutSeconds  int              `json:"timeout_seconds"`
	NextAttemptAt   *time.Time       `json:"next_attempt_at"`
	LastError       *errorInfo       `json:"last_error"`
	FailureReason   *string          `json:"failure_reason"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	StartedAt       *time.Time       `json:"started_at"`
	FinishedAt      *time.Time       `json:"finished_at"`
}

var jsonNull = json.RawMessage("null")

// newJobView builds the response; full includes payload and result (omitted in lists).
func newJobView(j jobs.Job, full bool) jobView {
	v := jobView{
		ID: j.ID, TaskType: j.TaskType, TaskVersion: j.TaskVersion, Queue: j.Queue, Status: j.Status,
		CancelRequested: j.CancelRequestedAt != nil, AttemptCount: j.AttemptCount, MaxAttempts: j.MaxAttempts,
		TimeoutSeconds: j.TimeoutSeconds, FailureReason: j.FailureReason, CreatedAt: j.CreatedAt,
		UpdatedAt: j.UpdatedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
	}
	if j.Status == jobs.StatusQueued {
		v.NextAttemptAt = &j.AvailableAt
	}
	if j.LastErrorCode != nil {
		v.LastError = &errorInfo{Code: *j.LastErrorCode, Message: deref(j.LastErrorMessage)}
	}
	if full {
		v.Payload, v.Result = &j.Payload, &jsonNull
		if j.Result != nil {
			v.Result = &j.Result
		}
	}
	return v
}

func (s *Server) submitJob(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		TaskType       string          `json:"task_type"`
		TaskVersion    int             `json:"task_version"`
		Payload        json.RawMessage `json:"payload"`
		MaxAttempts    *int            `json:"max_attempts"`
		TimeoutSeconds *int            `json:"timeout_seconds"`
		RunAt          *time.Time      `json:"run_at"`
		Queue          string          `json:"queue"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.TaskType == "" || req.TaskVersion < 1 || !bytes.HasPrefix(bytes.TrimSpace(req.Payload), []byte("{")) {
		writeError(w, r, invalidRequest("task_type, task_version >= 1 and an object payload are required"))
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) > 255 || strings.ContainsFunc(key, func(c rune) bool { return c < 0x21 || c > 0x7e }) {
		writeError(w, r, invalidRequest("Idempotency-Key must be 1-255 printable ASCII characters"))
		return
	}
	job, replayed, err := s.jobs.Submit(r.Context(), p, jobs.SubmitInput{
		TaskType: req.TaskType, TaskVersion: req.TaskVersion, Payload: req.Payload,
		MaxAttempts: req.MaxAttempts, TimeoutSeconds: req.TimeoutSeconds, RunAt: req.RunAt, Queue: req.Queue, IdempotencyKey: key,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	w.Header().Set("Location", "/v1/jobs/"+job.ID.String())
	writeJSON(w, http.StatusAccepted, newJobView(job, true))
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	q := r.URL.Query()
	f := jobs.ListFilter{TaskType: q.Get("task_type"), Queue: q.Get("queue"), Cursor: q.Get("cursor")}
	if f.Queue != "" && !tasktypes.ValidQueue(f.Queue) {
		writeError(w, r, invalidRequest("queue is not a valid queue name"))
		return
	}
	for _, st := range q["status"] {
		if !jobs.Status(st).Valid() {
			writeError(w, r, invalidRequest("unknown status "+strconv.Quote(st)))
			return
		}
		f.Statuses = append(f.Statuses, jobs.Status(st))
	}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > 100 {
			writeError(w, r, invalidRequest("limit must be 1-100"))
			return
		}
		f.Limit = n
	}
	page, err := s.jobs.List(r.Context(), p, f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]jobView, len(page.Items))
	for i, j := range page.Items {
		items[i] = newJobView(j, false)
	}
	var next *string
	if page.NextCursor != "" {
		next = &page.NextCursor
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, err := pathUUID(r, "job_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	job, err := s.jobs.Get(r.Context(), p, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newJobView(job, true))
}

func (s *Server) listAttempts(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, err := pathUUID(r, "job_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	attempts, err := s.jobs.Attempts(r.Context(), p, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	type attemptView struct {
		ID         uuid.UUID  `json:"id"`
		AttemptNo  int        `json:"attempt_no"`
		Status     string     `json:"status"`
		StartedAt  time.Time  `json:"started_at"`
		FinishedAt *time.Time `json:"finished_at"`
		Error      *errorInfo `json:"error"`
	}
	items := make([]attemptView, len(attempts))
	for i, a := range attempts {
		items[i] = attemptView{ID: a.ID, AttemptNo: a.AttemptNo, Status: a.Status, StartedAt: a.StartedAt, FinishedAt: a.FinishedAt}
		if a.ErrorCode != nil {
			items[i].Error = &errorInfo{Code: *a.ErrorCode, Message: deref(a.ErrorMessage), Retryable: a.ErrorRetryable}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, err := pathUUID(r, "job_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	job, err := s.jobs.Cancel(r.Context(), p, id)
	if errors.Is(err, jobs.ErrNotCancellable) {
		writeError(w, r, &httpError{http.StatusConflict, "job_not_cancellable", "Job already finished",
			"job status is " + string(job.Status)})
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	status := http.StatusOK
	if job.Status == jobs.StatusRunning {
		status = http.StatusAccepted // cancel requested; the worker stops the handler (§10)
	}
	writeJSON(w, status, newJobView(job, true))
}

func (s *Server) listTaskTypes(w http.ResponseWriter, r *http.Request, _ auth.Principal) {
	online, err := s.jobs.WorkersOnlineByTaskType(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	type view struct {
		TaskType              string          `json:"task_type"`
		TaskVersion           int             `json:"task_version"`
		Queue                 string          `json:"queue"`
		WorkersOnline         int             `json:"workers_online"`
		Title                 string          `json:"title"`
		Description           string          `json:"description"`
		PayloadSchema         json.RawMessage `json:"payload_schema"`
		Example               json.RawMessage `json:"example"`
		DefaultTimeoutSeconds int             `json:"default_timeout_seconds"`
		MaxTimeoutSeconds     int             `json:"max_timeout_seconds"`
		DefaultMaxAttempts    int             `json:"default_max_attempts"`
		Idempotent            bool            `json:"idempotent"`
		SideEffects           bool            `json:"side_effects"`
		TestOnly              bool            `json:"test_only"`
	}
	var items []view
	for _, t := range s.types.List() {
		example := t.Example
		if example == nil {
			example = json.RawMessage("null")
		}
		items = append(items, view{
			TaskType: t.TaskType, TaskVersion: t.TaskVersion, Queue: t.Queue, WorkersOnline: online[t.Key.String()],
			Title: t.Title, Description: t.Description, PayloadSchema: t.PayloadSchema, Example: example,
			DefaultTimeoutSeconds: t.DefaultTimeoutSeconds, MaxTimeoutSeconds: t.MaxTimeoutSeconds,
			DefaultMaxAttempts: t.DefaultMaxAttempts,
			Idempotent:         t.Idempotent, SideEffects: t.SideEffects, TestOnly: t.TestOnly,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "1h"
	}
	if _, ok := jobs.StatsWindows[window]; !ok {
		writeError(w, r, invalidRequest("window must be one of 15m, 1h, 6h, 24h"))
		return
	}
	stats, err := s.jobs.Stats(r.Context(), p, window)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) me(w http.ResponseWriter, _ *http.Request, p auth.Principal) {
	writeJSON(w, http.StatusOK, p)
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.UUID{}, invalidRequest(name + " must be a UUID")
	}
	return id, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
