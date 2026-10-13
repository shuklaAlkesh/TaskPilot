// Command taskpilot-api serves the public API (:8080), the internal worker API (:8081), and runs the reaper.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // schedule time zones must resolve on hosts and images without zoneinfo

	"golang.org/x/sync/errgroup"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/config"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/httpapi"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/jobs"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/reaper"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/store"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/tasktypes"
)

func main() {
	if err := run(); err != nil {
		slog.Error("taskpilot-api stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Open(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.CheckSchema(ctx, pool); err != nil {
		return err
	}
	// Task types are registered by workers at startup and stored in PostgreSQL (ADR-0009).
	types := tasktypes.NewRegistry(pool, cfg.EnableTestTasks)
	if err := types.Refresh(ctx); err != nil {
		return fmt.Errorf("task types: %w", err)
	}

	svc := jobs.NewService(pool, types, jobs.Settings{
		Retry:              jobs.RetryPolicy{Base: cfg.RetryBase, Cap: cfg.RetryCap},
		DefaultMaxAttempts: cfg.DefaultMaxAttempts,
		Lease:              cfg.LeaseDuration,
		HeartbeatInterval:  cfg.HeartbeatInterval,
		DeadlineGrace:      cfg.AttemptDeadlineGrace,
		MaxScheduleAhead:   cfg.MaxScheduleAhead,
		IdempotencyTTL:     cfg.IdempotencyTTL,
	})
	api := httpapi.New(svc, types, pool, cfg.WorkerTokens, log)
	public := newServer(cfg.HTTPAddr, api.PublicHandler())
	internal := newServer(cfg.WorkerAPIAddr, api.WorkerHandler())

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return serve(public) })
	g.Go(func() error { return serve(internal) })
	g.Go(func() error { return reaper.Run(gctx, svc, cfg.ReaperInterval, log) })
	g.Go(func() error { return reaper.RunSchedules(gctx, svc, cfg.SchedulerInterval, log) })
	g.Go(func() error {
		return types.Run(gctx, registryRefresh, func(err error) { log.ErrorContext(gctx, "refresh task types", "error", err) })
	})
	g.Go(func() error {
		<-gctx.Done()
		// Fail readiness first so load balancers stop routing, then drain (docs/03-TECHNICAL-DESIGN.md §4.1).
		api.SetShuttingDown()
		if ctx.Err() != nil {
			time.Sleep(cfg.ShutdownReadyDelay)
		}
		sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return errors.Join(public.Shutdown(sctx), internal.Shutdown(sctx))
	})
	log.Info("taskpilot-api started", "config", cfg, "task_types", len(types.List()))
	return g.Wait()
}

// registryRefresh is how quickly a registration made through one API replica reaches the others.
const registryRefresh = 10 * time.Second

func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func serve(s *http.Server) error {
	if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen %s: %w", s.Addr, err)
	}
	return nil
}
