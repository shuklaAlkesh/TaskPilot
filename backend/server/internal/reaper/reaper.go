// Package reaper runs the API's background loops: it recovers attempts whose lease or deadline
// expired (docs/05-JOB-LIFECYCLE.md §6) and submits the jobs of due schedules (§11).
package reaper

import (
	"context"
	"log/slog"
	"time"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/jobs"
)

const batchSize = 100

// Run reaps expired attempts on every tick until ctx is cancelled.
func Run(ctx context.Context, svc *jobs.Service, interval time.Duration, log *slog.Logger) error {
	return loop(ctx, interval, svc.Reap, "reaped expired attempts", "reap failed", log)
}

// RunSchedules fires due schedules on every tick until ctx is cancelled.
func RunSchedules(ctx context.Context, svc *jobs.Service, interval time.Duration, log *slog.Logger) error {
	return loop(ctx, interval, svc.FireDue, "fired schedules", "fire schedules failed", log)
}

// loop calls batch on every tick, again at once while it fills a whole batch. Errors are logged
// and retried on the next tick.
func loop(ctx context.Context, interval time.Duration, batch func(context.Context, int) (int, error),
	done, failed string, log *slog.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		for {
			n, err := batch(ctx, batchSize)
			if err != nil {
				if ctx.Err() == nil {
					log.ErrorContext(ctx, failed, "error", err)
				}
				break
			}
			if n > 0 {
				log.InfoContext(ctx, done, "count", n)
			}
			if n < batchSize {
				break
			}
		}
	}
}
