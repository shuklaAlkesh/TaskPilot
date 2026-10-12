// Package reaper recovers attempts whose lease or deadline expired (docs/05-JOB-LIFECYCLE.md §6).
package reaper

import (
	"context"
	"log/slog"
	"time"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/jobs"
)

const batchSize = 100

// Run reaps on every tick until ctx is cancelled. Errors are logged and retried next tick.
func Run(ctx context.Context, svc *jobs.Service, interval time.Duration, log *slog.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		for {
			n, err := svc.Reap(ctx, batchSize)
			if err != nil {
				if ctx.Err() == nil {
					log.ErrorContext(ctx, "reap failed", "error", err)
				}
				break
			}
			if n > 0 {
				log.InfoContext(ctx, "reaped expired attempts", "count", n)
			}
			if n < batchSize {
				break
			}
		}
	}
}
