package jobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/tasktypes"
)

// WorkerOnlineSeconds: a worker counts as online if it claimed or heartbeated this recently.
// Idle workers claim every poll interval (~1 s); busy ones heartbeat every 10 s.
const WorkerOnlineSeconds = 30

// Worker is a worker process as it announces itself on startup.
type Worker struct {
	WorkerID    string
	Hostname    string
	Version     string
	Concurrency int
	Queues      []string
	TaskTypes   []tasktypes.Key
}

// RegisterWorker records (or refreshes) a worker and marks it online.
func (s *Service) RegisterWorker(ctx context.Context, w Worker) error {
	keys := make([]string, len(w.TaskTypes))
	for i, k := range w.TaskTypes {
		keys[i] = k.String()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (worker_id, hostname, version, concurrency, queues, task_types)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (worker_id) DO UPDATE SET hostname = EXCLUDED.hostname, version = EXCLUDED.version,
			concurrency = EXCLUDED.concurrency, queues = EXCLUDED.queues, task_types = EXCLUDED.task_types,
			last_seen_at = now()`,
		w.WorkerID, w.Hostname, w.Version, w.Concurrency, w.Queues, keys)
	if err != nil {
		return fmt.Errorf("register worker: %w", err)
	}
	return nil
}

// WorkersOnlineByTaskType counts online workers able to run each "task_type@version".
// Platform-level and aggregated: no worker identities reach tenants.
func (s *Service) WorkersOnlineByTaskType(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t, count(*) FROM workers, unnest(task_types) AS t
		WHERE last_seen_at > now() - make_interval(secs => $1)
		GROUP BY t`, WorkerOnlineSeconds)
	if err != nil {
		return nil, fmt.Errorf("workers online: %w", err)
	}
	out := map[string]int{}
	var key string
	var n int
	_, err = pgx.ForEachRow(rows, []any{&key, &n}, func() error {
		out[key] = n
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("workers online: %w", err)
	}
	return out, nil
}
