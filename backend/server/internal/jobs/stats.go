package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
)

// StatsWindow is a supported analytics time range and its chart bucket size.
type StatsWindow struct {
	Span   time.Duration
	Bucket time.Duration
}

// StatsWindows lists the ranges GET /v1/stats accepts. Bucket counts stay between 30 and 72.
var StatsWindows = map[string]StatsWindow{
	"15m": {15 * time.Minute, 30 * time.Second},
	"1h":  {time.Hour, time.Minute},
	"6h":  {6 * time.Hour, 5 * time.Minute},
	"24h": {24 * time.Hour, 30 * time.Minute},
}

// StatsTotals are all-time counts for the tenant's jobs that retention has not deleted yet.
type StatsTotals struct {
	Submitted     int `json:"submitted"`
	Delayed       int `json:"delayed"`       // queued, available_at in the future (run_at or retry backoff)
	RetryWaiting  int `json:"retry_waiting"` // the part of Delayed that is waiting for a retry
	Ready         int `json:"ready"`         // queued and claimable now
	Running       int `json:"running"`
	Succeeded     int `json:"succeeded"`
	Failed        int `json:"failed"` // dead letter (docs/05-JOB-LIFECYCLE.md §2)
	Cancelled     int `json:"cancelled"`
	Retries       int `json:"retries"`      // retry attempts started (attempts after the first)
	JobsRetried   int `json:"jobs_retried"` // jobs that needed more than one attempt
	ActiveWorkers int `json:"active_workers"`
}

// StatsLatency holds p50/p95 durations in milliseconds over the window; nil when there is no data.
type StatsLatency struct {
	TimeToStartP50 *float64 `json:"time_to_start_p50_ms"`
	TimeToStartP95 *float64 `json:"time_to_start_p95_ms"`
	RunTimeP50     *float64 `json:"run_time_p50_ms"`
	RunTimeP95     *float64 `json:"run_time_p95_ms"`
	CompleteP50    *float64 `json:"time_to_complete_p50_ms"`
	CompleteP95    *float64 `json:"time_to_complete_p95_ms"`
}

// ThroughputBucket counts events whose timestamp falls in [Start, Start+bucket).
type ThroughputBucket struct {
	Start     time.Time `json:"start"`
	Submitted int       `json:"submitted"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Cancelled int       `json:"cancelled"`
	Retried   int       `json:"retried"`
}

// TaskTypeStats breaks the totals down by task type; run times cover the window.
type TaskTypeStats struct {
	TaskType  string   `json:"task_type"`
	Total     int      `json:"total"`
	Queued    int      `json:"queued"`
	Running   int      `json:"running"`
	Succeeded int      `json:"succeeded"`
	Failed    int      `json:"failed"`
	Cancelled int      `json:"cancelled"`
	Retries   int      `json:"retries"`
	RunP50    *float64 `json:"run_time_p50_ms"`
	RunP95    *float64 `json:"run_time_p95_ms"`
}

// CodeCount is how often an attempt error code occurred in the window.
type CodeCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// LastMinute is a live rate: events in the 60 seconds before the snapshot.
type LastMinute struct {
	Submitted int `json:"submitted"`
	Finished  int `json:"finished"`
}

// QueueStats is one named queue: the tenant's jobs in it, and the workers serving it.
type QueueStats struct {
	Queue         string   `json:"queue"`
	Ready         int      `json:"ready"`
	Delayed       int      `json:"delayed"`
	Running       int      `json:"running"`
	LagMs         *float64 `json:"lag_ms"`
	WorkersOnline int      `json:"workers_online"`
	Slots         int      `json:"slots"` // total concurrency of online workers subscribed to this queue
	TaskTypes     int      `json:"task_types"`
}

// Stats is the GET /v1/stats response. Every number comes from one database snapshot.
type Stats struct {
	GeneratedAt   time.Time          `json:"generated_at"`
	Window        string             `json:"window"`
	BucketSeconds int                `json:"bucket_seconds"`
	Totals        StatsTotals        `json:"totals"`
	SuccessRate   *float64           `json:"success_rate"` // succeeded / (succeeded + failed); cancelled excluded
	QueueLagMs    *float64           `json:"queue_lag_ms"` // how long the oldest ready job has been claimable; nil when none is ready
	LastMinute    LastMinute         `json:"last_minute"`
	Latency       StatsLatency       `json:"latency"`
	Throughput    []ThroughputBucket `json:"throughput"`
	TaskTypes     []TaskTypeStats    `json:"task_types"`
	Errors        []CodeCount        `json:"errors"`
	Attempts      map[string]int     `json:"attempts"` // attempt outcomes finished in the window
	Queues        []QueueStats       `json:"queues"`
}

// Stats aggregates the tenant's jobs. All queries share one REPEATABLE READ snapshot and one now(),
// so the numbers agree with each other.
func (s *Service) Stats(ctx context.Context, p auth.Principal, window string) (Stats, error) {
	w, ok := StatsWindows[window]
	if !ok {
		return Stats{}, fmt.Errorf("unknown window %q", window)
	}
	st := Stats{Window: window, BucketSeconds: int(w.Bucket.Seconds()), Attempts: map[string]int{}}
	span, bucket := w.Span.Seconds(), w.Bucket.Seconds()
	opts := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	err := pgx.BeginTxFunc(ctx, s.pool, opts, func(tx pgx.Tx) error {
		t := &st.Totals
		if err := tx.QueryRow(ctx, `
			SELECT now(), count(*),
				count(*) FILTER (WHERE status = 'queued' AND available_at > now()),
				count(*) FILTER (WHERE status = 'queued' AND available_at > now() AND attempt_count > 0),
				count(*) FILTER (WHERE status = 'queued' AND available_at <= now()),
				count(*) FILTER (WHERE status = 'running'),
				count(*) FILTER (WHERE status = 'succeeded'),
				count(*) FILTER (WHERE status = 'failed'),
				count(*) FILTER (WHERE status = 'cancelled'),
				COALESCE(sum(GREATEST(attempt_count - 1, 0)), 0),
				count(*) FILTER (WHERE attempt_count > 1),
				(SELECT count(DISTINCT worker_id) FROM job_attempts WHERE tenant_id = $1 AND status = 'running')
			FROM jobs WHERE tenant_id = $1`, p.TenantID,
		).Scan(&st.GeneratedAt, &t.Submitted, &t.Delayed, &t.RetryWaiting, &t.Ready, &t.Running,
			&t.Succeeded, &t.Failed, &t.Cancelled, &t.Retries, &t.JobsRetried, &t.ActiveWorkers); err != nil {
			return fmt.Errorf("stats totals: %w", err)
		}
		if done := t.Succeeded + t.Failed; done > 0 {
			rate := float64(t.Succeeded) / float64(done)
			st.SuccessRate = &rate
		}

		if err := tx.QueryRow(ctx, `
			SELECT
				(SELECT EXTRACT(EPOCH FROM now() - min(available_at)) * 1000 FROM jobs
				 WHERE tenant_id = $1 AND status = 'queued' AND available_at <= now()),
				count(*) FILTER (WHERE created_at >= now() - interval '1 minute'),
				count(*) FILTER (WHERE finished_at >= now() - interval '1 minute')
			FROM jobs WHERE tenant_id = $1`, p.TenantID,
		).Scan(&st.QueueLagMs, &st.LastMinute.Submitted, &st.LastMinute.Finished); err != nil {
			return fmt.Errorf("stats live: %w", err)
		}

		l := &st.Latency
		if err := tx.QueryRow(ctx, `
			WITH first_attempts AS (
				SELECT EXTRACT(EPOCH FROM a.started_at - j.created_at) * 1000 AS ms
				FROM job_attempts a JOIN jobs j ON j.id = a.job_id
				WHERE a.tenant_id = $1 AND a.attempt_no = 1 AND a.started_at >= now() - make_interval(secs => $2)
			), runs AS (
				SELECT EXTRACT(EPOCH FROM finished_at - started_at) * 1000 AS ms
				FROM job_attempts
				WHERE tenant_id = $1 AND status = 'succeeded' AND finished_at >= now() - make_interval(secs => $2)
			), completes AS (
				SELECT EXTRACT(EPOCH FROM finished_at - created_at) * 1000 AS ms
				FROM jobs
				WHERE tenant_id = $1 AND status = 'succeeded' AND finished_at >= now() - make_interval(secs => $2)
			)
			SELECT
				(SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY ms) FROM first_attempts),
				(SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY ms) FROM first_attempts),
				(SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY ms) FROM runs),
				(SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY ms) FROM runs),
				(SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY ms) FROM completes),
				(SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY ms) FROM completes)`,
			p.TenantID, span,
		).Scan(&l.TimeToStartP50, &l.TimeToStartP95, &l.RunTimeP50, &l.RunTimeP95, &l.CompleteP50, &l.CompleteP95); err != nil {
			return fmt.Errorf("stats latency: %w", err)
		}

		rows, err := tx.Query(ctx, `
			WITH params AS (
				SELECT make_interval(secs => $2) AS b, now() - make_interval(secs => $3) AS since
			), buckets AS (
				SELECT generate_series(date_bin(b, since, TIMESTAMPTZ '2000-01-01'), now(), b) AS t FROM params
			), submitted AS (
				SELECT date_bin(b, created_at, TIMESTAMPTZ '2000-01-01') AS t, count(*) AS n
				FROM jobs, params WHERE tenant_id = $1 AND created_at >= since - b GROUP BY 1
			), finished AS (
				SELECT date_bin(b, finished_at, TIMESTAMPTZ '2000-01-01') AS t,
					count(*) FILTER (WHERE status = 'succeeded') AS ok,
					count(*) FILTER (WHERE status = 'failed') AS failed,
					count(*) FILTER (WHERE status = 'cancelled') AS cancelled
				FROM jobs, params WHERE tenant_id = $1 AND finished_at >= since - b GROUP BY 1
			), retried AS (
				SELECT date_bin(b, started_at, TIMESTAMPTZ '2000-01-01') AS t, count(*) AS n
				FROM job_attempts, params WHERE tenant_id = $1 AND attempt_no > 1 AND started_at >= since - b GROUP BY 1
			)
			SELECT buckets.t, COALESCE(submitted.n, 0), COALESCE(finished.ok, 0), COALESCE(finished.failed, 0),
				COALESCE(finished.cancelled, 0), COALESCE(retried.n, 0)
			FROM buckets
			LEFT JOIN submitted USING (t) LEFT JOIN finished USING (t) LEFT JOIN retried USING (t)
			ORDER BY buckets.t`, p.TenantID, bucket, span)
		if err != nil {
			return fmt.Errorf("stats throughput: %w", err)
		}
		if st.Throughput, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ThroughputBucket, error) {
			var b ThroughputBucket
			return b, r.Scan(&b.Start, &b.Submitted, &b.Succeeded, &b.Failed, &b.Cancelled, &b.Retried)
		}); err != nil {
			return fmt.Errorf("stats throughput: %w", err)
		}

		rows, err = tx.Query(ctx, `
			WITH runs AS (
				SELECT j.task_type,
					percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM a.finished_at - a.started_at) * 1000) AS p50,
					percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM a.finished_at - a.started_at) * 1000) AS p95
				FROM job_attempts a JOIN jobs j ON j.id = a.job_id
				WHERE a.tenant_id = $1 AND a.status = 'succeeded' AND a.finished_at >= now() - make_interval(secs => $2)
				GROUP BY j.task_type
			)
			SELECT j.task_type, count(*),
				count(*) FILTER (WHERE status = 'queued'), count(*) FILTER (WHERE status = 'running'),
				count(*) FILTER (WHERE status = 'succeeded'), count(*) FILTER (WHERE status = 'failed'),
				count(*) FILTER (WHERE status = 'cancelled'), COALESCE(sum(GREATEST(attempt_count - 1, 0)), 0),
				runs.p50, runs.p95
			FROM jobs j LEFT JOIN runs USING (task_type)
			WHERE j.tenant_id = $1
			GROUP BY j.task_type, runs.p50, runs.p95
			ORDER BY count(*) DESC, j.task_type`, p.TenantID, span)
		if err != nil {
			return fmt.Errorf("stats task types: %w", err)
		}
		if st.TaskTypes, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (TaskTypeStats, error) {
			var t TaskTypeStats
			return t, r.Scan(&t.TaskType, &t.Total, &t.Queued, &t.Running, &t.Succeeded, &t.Failed, &t.Cancelled,
				&t.Retries, &t.RunP50, &t.RunP95)
		}); err != nil {
			return fmt.Errorf("stats task types: %w", err)
		}

		rows, err = tx.Query(ctx, `
			SELECT status, count(*) FROM job_attempts
			WHERE tenant_id = $1 AND finished_at >= now() - make_interval(secs => $2)
			GROUP BY status`, p.TenantID, span)
		if err != nil {
			return fmt.Errorf("stats attempts: %w", err)
		}
		var status string
		var n int
		if _, err := pgx.ForEachRow(rows, []any{&status, &n}, func() error {
			st.Attempts[status] = n
			return nil
		}); err != nil {
			return fmt.Errorf("stats attempts: %w", err)
		}

		rows, err = tx.Query(ctx, `
			SELECT error_code, count(*) FROM job_attempts
			WHERE tenant_id = $1 AND error_code IS NOT NULL AND finished_at >= now() - make_interval(secs => $2)
			GROUP BY error_code ORDER BY count(*) DESC, error_code LIMIT 10`, p.TenantID, span)
		if err != nil {
			return fmt.Errorf("stats errors: %w", err)
		}
		st.Errors, err = pgx.CollectRows(rows, pgx.RowToStructByPos[CodeCount])
		if err != nil {
			return fmt.Errorf("stats errors: %w", err)
		}

		rows, err = tx.Query(ctx, `
			WITH online AS (
				SELECT unnest(queues) AS queue, concurrency FROM workers
				WHERE last_seen_at > now() - make_interval(secs => $2)
			), w AS (
				SELECT queue, count(*) AS workers, sum(concurrency) AS slots FROM online GROUP BY queue
			), j AS (
				SELECT queue,
					count(*) FILTER (WHERE status = 'queued' AND available_at <= now()) AS ready,
					count(*) FILTER (WHERE status = 'queued' AND available_at > now()) AS delayed,
					count(*) FILTER (WHERE status = 'running') AS running,
					EXTRACT(EPOCH FROM now() - min(available_at) FILTER (WHERE status = 'queued' AND available_at <= now())) * 1000 AS lag
				FROM jobs WHERE tenant_id = $1 AND status IN ('queued', 'running') GROUP BY queue
			), t AS (
				SELECT queue, count(*) AS n FROM task_types GROUP BY queue
			), q AS (
				SELECT queue FROM w UNION SELECT queue FROM j UNION SELECT queue FROM t
			)
			SELECT q.queue, COALESCE(j.ready, 0), COALESCE(j.delayed, 0), COALESCE(j.running, 0), j.lag,
				COALESCE(w.workers, 0), COALESCE(w.slots, 0), COALESCE(t.n, 0)
			FROM q LEFT JOIN j USING (queue) LEFT JOIN w USING (queue) LEFT JOIN t USING (queue)
			ORDER BY q.queue`, p.TenantID, WorkerOnlineSeconds)
		if err != nil {
			return fmt.Errorf("stats queues: %w", err)
		}
		if st.Queues, err = pgx.CollectRows(rows, pgx.RowToStructByPos[QueueStats]); err != nil {
			return fmt.Errorf("stats queues: %w", err)
		}
		return nil
	})
	return st, err
}
