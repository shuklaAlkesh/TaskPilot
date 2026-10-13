// Package jobs holds the job state rules and the transactional operations that apply them.
// Authoritative rules: docs/05-JOB-LIFECYCLE.md. SQL guards: docs/04-DATA-MODEL.md §4.
package jobs

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/store"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/tasktypes"
)

// Size limits (Provisional, Q-SEC-3).
const (
	MaxPayloadBytes  = 64 << 10
	MaxResultBytes   = 64 << 10
	maxErrorMessage  = 1000
	reaperLockID     = 0x7461736b70696c6f // "taskpilo"
	envelopeVersion  = 1
	maxListLimit     = 100
	defaultListLimit = 20
)

// Job is one row of the jobs table.
type Job struct {
	ID                uuid.UUID       `db:"id"`
	TenantID          uuid.UUID       `db:"tenant_id"`
	TaskType          string          `db:"task_type"`
	Queue             string          `db:"queue"`
	TaskVersion       int             `db:"task_version"`
	Payload           json.RawMessage `db:"payload"`
	Status            Status          `db:"status"`
	AttemptCount      int             `db:"attempt_count"`
	MaxAttempts       int             `db:"max_attempts"`
	TimeoutSeconds    int             `db:"timeout_seconds"`
	AvailableAt       time.Time       `db:"available_at"`
	CurrentAttemptID  *uuid.UUID      `db:"current_attempt_id"`
	LeaseExpiresAt    *time.Time      `db:"lease_expires_at"`
	CancelRequestedAt *time.Time      `db:"cancel_requested_at"`
	Result            json.RawMessage `db:"result"`
	LastErrorCode     *string         `db:"last_error_code"`
	LastErrorMessage  *string         `db:"last_error_message"`
	FailureReason     *string         `db:"failure_reason"`
	CreatedAt         time.Time       `db:"created_at"`
	UpdatedAt         time.Time       `db:"updated_at"`
	StartedAt         *time.Time      `db:"started_at"`
	FinishedAt        *time.Time      `db:"finished_at"`
}

// Attempt is one row of job_attempts.
type Attempt struct {
	ID              uuid.UUID  `db:"id"`
	AttemptNo       int        `db:"attempt_no"`
	WorkerID        string     `db:"worker_id"`
	Status          string     `db:"status"`
	StartedAt       time.Time  `db:"started_at"`
	DeadlineAt      time.Time  `db:"deadline_at"`
	LastHeartbeatAt *time.Time `db:"last_heartbeat_at"`
	FinishedAt      *time.Time `db:"finished_at"`
	ErrorCode       *string    `db:"error_code"`
	ErrorMessage    *string    `db:"error_message"`
	ErrorRetryable  *bool      `db:"error_retryable"`
}

const jobColumns = `id, tenant_id, task_type, queue, task_version, payload, status, attempt_count, max_attempts,
	timeout_seconds, available_at, current_attempt_id, lease_expires_at, cancel_requested_at, result,
	last_error_code, last_error_message, failure_reason, created_at, updated_at, started_at, finished_at`

// listColumns omits payload and result to keep list responses small (docs/06-API-SPECIFICATION.md §3.2).
var listColumns = strings.NewReplacer("payload,", "NULL::jsonb AS payload,", "result,", "NULL::jsonb AS result,").
	Replace(jobColumns)

// Settings are the tunables from config.
type Settings struct {
	Retry              RetryPolicy
	DefaultMaxAttempts int
	Lease              time.Duration
	HeartbeatInterval  time.Duration
	DeadlineGrace      time.Duration
	MaxScheduleAhead   time.Duration
	IdempotencyTTL     time.Duration
}

// Service runs job operations inside transactions.
type Service struct {
	pool  *pgxpool.Pool
	types *tasktypes.Registry
	cfg   Settings
	randN func(int64) int64
	now   func() time.Time // for request validation only; leases always use DB now()
}

// NewService wires a Service.
func NewService(pool *pgxpool.Pool, types *tasktypes.Registry, cfg Settings) *Service {
	return &Service{pool: pool, types: types, cfg: cfg, randN: rand.Int64N, now: time.Now}
}

// SubmitInput is a validated-shape submit request.
type SubmitInput struct {
	TaskType       string
	TaskVersion    int
	Payload        json.RawMessage
	MaxAttempts    *int
	TimeoutSeconds *int
	RunAt          *time.Time
	Queue          string // overrides the task type's queue when set
	IdempotencyKey string
}

// prepare validates a job request against its registered task type and resolves the defaults.
// Submit and scheduled runs both use it.
func (s *Service) prepare(in SubmitInput) (maxAttempts, timeout int, queue string, err error) {
	t, ok := s.types.Lookup(tasktypes.Key{TaskType: in.TaskType, TaskVersion: in.TaskVersion})
	if !ok {
		return 0, 0, "", ErrUnknownTaskType
	}
	if len(in.Payload) > MaxPayloadBytes {
		return 0, 0, "", ErrPayloadTooLarge
	}
	fields, err := t.Validate(in.Payload)
	if err != nil {
		return 0, 0, "", err
	}
	if len(fields) > 0 {
		return 0, 0, "", &ValidationError{Code: CodeInvalidPayload, Fields: fields}
	}
	return s.resolveOptions(t, in)
}

// Submit applies T1. It returns the job and whether it is an idempotent replay.
func (s *Service) Submit(ctx context.Context, p auth.Principal, in SubmitInput) (Job, bool, error) {
	maxAttempts, timeout, queue, err := s.prepare(in)
	if err != nil {
		return Job{}, false, err
	}

	var key *string
	var hash []byte
	if in.IdempotencyKey != "" {
		key = &in.IdempotencyKey
		if hash, err = fingerprint(in, maxAttempts, timeout, queue); err != nil {
			return Job{}, false, err
		}
	}

	var job Job
	replayed := false
	err = store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if key != nil {
			// An expired key no longer matches (docs/05-JOB-LIFECYCLE.md §9).
			if _, err := tx.Exec(ctx, `
				UPDATE jobs SET idempotency_key = NULL, request_hash = NULL, idempotency_expires_at = NULL
				WHERE tenant_id = $1 AND idempotency_key = $2 AND idempotency_expires_at <= now()`,
				p.TenantID, *key); err != nil {
				return fmt.Errorf("expire idempotency key: %w", err)
			}
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("new job id: %w", err)
		}
		job, err = queryJob(ctx, tx, `
			INSERT INTO jobs (id, tenant_id, task_type, task_version, payload, status, max_attempts,
				timeout_seconds, available_at, idempotency_key, request_hash, idempotency_expires_at, queue)
			VALUES ($1, $2, $3, $4, $5, 'queued', $6, $7, GREATEST(COALESCE($8::timestamptz, now()), now()),
				$9, $10, CASE WHEN $9::text IS NULL THEN NULL ELSE now() + make_interval(secs => $11) END, $12)
			ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
			RETURNING `+jobColumns,
			id, p.TenantID, in.TaskType, in.TaskVersion, in.Payload, maxAttempts, timeout, in.RunAt,
			key, hash, s.cfg.IdempotencyTTL.Seconds(), queue)
		if errors.Is(err, ErrNotFound) && key != nil { // ON CONFLICT DO NOTHING returned no row
			return s.replay(ctx, tx, p, *key, hash, &job, &replayed)
		}
		if err != nil {
			return fmt.Errorf("insert job: %w", err)
		}
		return audit(ctx, tx, p, "job.submitted", job.ID)
	})
	return job, replayed, err
}

// replay loads the job that already owns the idempotency key and checks the request matches.
func (s *Service) replay(ctx context.Context, tx pgx.Tx, p auth.Principal, key string, hash []byte, job *Job, replayed *bool) error {
	var stored []byte
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id, request_hash FROM jobs WHERE tenant_id = $1 AND idempotency_key = $2`,
		p.TenantID, key).Scan(&id, &stored)
	if err != nil {
		return fmt.Errorf("load idempotent job: %w", err)
	}
	if !bytes.Equal(stored, hash) {
		return ErrIdempotencyConflict
	}
	*job, err = queryJob(ctx, tx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1`, id)
	*replayed = true
	return err
}

func (s *Service) resolveOptions(t *tasktypes.TaskType, in SubmitInput) (maxAttempts, timeout int, queue string, err error) {
	maxAttempts, timeout, queue = t.DefaultMaxAttempts, t.DefaultTimeoutSeconds, cmp.Or(in.Queue, t.Queue)
	var fields []tasktypes.FieldError
	if !tasktypes.ValidQueue(queue) {
		fields = append(fields, tasktypes.FieldError{Field: "/queue", Message: "must match ^[a-z][a-z0-9_.-]{0,62}$"})
	}
	if in.MaxAttempts != nil {
		maxAttempts = *in.MaxAttempts
		if maxAttempts < 1 || maxAttempts > 10 {
			fields = append(fields, tasktypes.FieldError{Field: "/max_attempts", Message: "must be between 1 and 10"})
		}
	}
	if in.TimeoutSeconds != nil {
		timeout = *in.TimeoutSeconds
		if timeout < 1 || timeout > t.MaxTimeoutSeconds {
			fields = append(fields, tasktypes.FieldError{Field: "/timeout_seconds",
				Message: fmt.Sprintf("must be between 1 and %d", t.MaxTimeoutSeconds)})
		}
	}
	if in.RunAt != nil && in.RunAt.After(s.now().Add(s.cfg.MaxScheduleAhead)) {
		fields = append(fields, tasktypes.FieldError{Field: "/run_at", Message: "is too far in the future"})
	}
	if len(fields) > 0 {
		return 0, 0, "", &ValidationError{Code: CodeInvalidOptions, Fields: fields}
	}
	return maxAttempts, timeout, queue, nil
}

// fingerprint is SHA-256 of the canonical request (encoding/json sorts map keys).
func fingerprint(in SubmitInput, maxAttempts, timeout int, queue string) ([]byte, error) {
	var payload any
	if err := json.Unmarshal(in.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var runAt *time.Time
	if in.RunAt != nil {
		u := in.RunAt.UTC()
		runAt = &u
	}
	b, err := json.Marshal(map[string]any{
		"task_type": in.TaskType, "task_version": in.TaskVersion, "payload": payload,
		"max_attempts": maxAttempts, "timeout_seconds": timeout, "run_at": runAt, "queue": queue,
	})
	if err != nil {
		return nil, fmt.Errorf("encode fingerprint: %w", err)
	}
	sum := sha256.Sum256(b)
	return sum[:], nil
}

// Get returns one of the tenant's jobs.
func (s *Service) Get(ctx context.Context, p auth.Principal, id uuid.UUID) (Job, error) {
	return queryJob(ctx, s.pool, `SELECT `+jobColumns+` FROM jobs WHERE id = $1 AND tenant_id = $2`, id, p.TenantID)
}

// ListFilter selects jobs for List.
type ListFilter struct {
	Statuses []Status
	TaskType string
	Queue    string
	Limit    int
	Cursor   string
}

// Page is one page of jobs, newest first.
type Page struct {
	Items      []Job
	NextCursor string
}

// List returns the tenant's jobs with keyset pagination (docs/04-DATA-MODEL.md §4.2).
func (s *Service) List(ctx context.Context, p auth.Principal, f ListFilter) (Page, error) {
	limit := f.Limit
	if limit == 0 {
		limit = defaultListLimit
	}
	limit = min(max(limit, 1), maxListLimit)
	var afterTime *time.Time
	var afterID uuid.UUID
	if f.Cursor != "" {
		t, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return Page{}, err
		}
		afterTime, afterID = &t, id
	}
	statuses := make([]string, len(f.Statuses))
	for i, st := range f.Statuses {
		statuses[i] = string(st)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+listColumns+` FROM jobs
		WHERE tenant_id = $1
		  AND (cardinality($2::text[]) = 0 OR status = ANY($2))
		  AND ($3 = '' OR task_type = $3)
		  AND ($7 = '' OR queue = $7)
		  AND ($4::timestamptz IS NULL OR (created_at, id) < ($4, $5))
		ORDER BY created_at DESC, id DESC
		LIMIT $6`,
		p.TenantID, statuses, f.TaskType, afterTime, afterID, limit+1, f.Queue)
	if err != nil {
		return Page{}, fmt.Errorf("list jobs: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[Job])
	if err != nil {
		return Page{}, fmt.Errorf("list jobs: %w", err)
	}
	page := Page{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

func encodeCursor(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeCursor(c string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, uuid.UUID{}, ErrInvalidCursor
	}
	ts, idStr, ok := strings.Cut(string(raw), "|")
	t, terr := time.Parse(time.RFC3339Nano, ts)
	id, ierr := uuid.Parse(idStr)
	if !ok || terr != nil || ierr != nil {
		return time.Time{}, uuid.UUID{}, ErrInvalidCursor
	}
	return t, id, nil
}

// Attempts lists a tenant job's attempts, oldest first.
func (s *Service) Attempts(ctx context.Context, p auth.Principal, jobID uuid.UUID) ([]Attempt, error) {
	if _, err := s.Get(ctx, p, jobID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, attempt_no, worker_id, status, started_at, deadline_at, last_heartbeat_at, finished_at,
			error_code, error_message, error_retryable
		FROM job_attempts WHERE job_id = $1 AND tenant_id = $2 ORDER BY attempt_no`, jobID, p.TenantID)
	if err != nil {
		return nil, fmt.Errorf("list attempts: %w", err)
	}
	attempts, err := pgx.CollectRows(rows, pgx.RowToStructByName[Attempt])
	if err != nil {
		return nil, fmt.Errorf("list attempts: %w", err)
	}
	return attempts, nil
}

// Cancel applies T3 or T5. On ErrNotCancellable the returned job carries the current status.
func (s *Service) Cancel(ctx context.Context, p auth.Principal, id uuid.UUID) (Job, error) {
	var job Job
	var cancelErr error
	err := store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		job, err = queryJob(ctx, tx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
			id, p.TenantID)
		if err != nil {
			return err
		}
		action, err := DecideCancel(job.Status, job.CancelRequestedAt != nil)
		if err != nil {
			cancelErr = err
			return nil
		}
		var set, event string
		switch action {
		case CancelNoop:
			return nil
		case CancelNow:
			set, event = `status = 'cancelled', finished_at = now()`, "job.cancelled"
		case CancelRequest:
			set, event = `cancel_requested_at = now()`, "job.cancel_requested"
		}
		if job, err = queryJob(ctx, tx, `UPDATE jobs SET `+set+`, updated_at = now() WHERE id = $1 RETURNING `+jobColumns, id); err != nil {
			return fmt.Errorf("cancel job: %w", err)
		}
		return audit(ctx, tx, p, event, id)
	})
	if err != nil {
		return Job{}, err
	}
	return job, cancelErr
}

// Envelope is the job handed to a worker (backend/contracts/job.schema.json).
type Envelope struct {
	SchemaVersion  int             `json:"schema_version"`
	JobID          uuid.UUID       `json:"job_id"`
	AttemptID      uuid.UUID       `json:"attempt_id"`
	AttemptNo      int             `json:"attempt_no"`
	MaxAttempts    int             `json:"max_attempts"`
	TaskType       string          `json:"task_type"`
	TaskVersion    int             `json:"task_version"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"created_at"`
}

// Claim is a leased attempt.
type Claim struct {
	LeaseSeconds             int      `json:"lease_seconds"`
	HeartbeatIntervalSeconds int      `json:"heartbeat_interval_seconds"`
	Job                      Envelope `json:"job"`
}

// Claim applies T2 to the oldest available job in one of the worker's queues that it supports.
// It returns nil when none is ready. Every claim also marks the worker as seen.
func (s *Service) Claim(ctx context.Context, workerID string, queues []string, supported []tasktypes.Key) (*Claim, error) {
	if _, err := s.pool.Exec(ctx, `UPDATE workers SET last_seen_at = now() WHERE worker_id = $1`, workerID); err != nil {
		return nil, fmt.Errorf("mark worker seen: %w", err)
	}
	names := make([]string, len(supported))
	versions := make([]int, len(supported))
	for i, k := range supported {
		names[i], versions[i] = k.TaskType, k.TaskVersion
	}
	var claim *Claim
	err := store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var e Envelope
		err := tx.QueryRow(ctx, `
			SELECT id, task_type, task_version, payload, attempt_count + 1, max_attempts, timeout_seconds, created_at
			FROM jobs
			WHERE status = 'queued' AND queue = ANY($3::text[]) AND available_at <= now()
			  AND (task_type, task_version) IN (SELECT t, v FROM unnest($1::text[], $2::int[]) AS s(t, v))
			ORDER BY available_at, id
			LIMIT 1
			FOR UPDATE SKIP LOCKED`, names, versions, queues,
		).Scan(&e.JobID, &e.TaskType, &e.TaskVersion, &e.Payload, &e.AttemptNo, &e.MaxAttempts, &e.TimeoutSeconds, &e.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("select claimable job: %w", err)
		}
		if e.AttemptID, err = uuid.NewV7(); err != nil {
			return fmt.Errorf("new attempt id: %w", err)
		}
		deadline := (time.Duration(e.TimeoutSeconds)*time.Second + s.cfg.DeadlineGrace).Seconds()
		if _, err := tx.Exec(ctx, `
			INSERT INTO job_attempts (id, job_id, tenant_id, attempt_no, worker_id, status, deadline_at)
			SELECT $1, id, tenant_id, $2, $3, 'running', now() + make_interval(secs => $4) FROM jobs WHERE id = $5`,
			e.AttemptID, e.AttemptNo, workerID, deadline, e.JobID); err != nil {
			return fmt.Errorf("insert attempt: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE jobs SET status = 'running', attempt_count = attempt_count + 1, current_attempt_id = $2,
				lease_expires_at = now() + make_interval(secs => $3), started_at = COALESCE(started_at, now()),
				updated_at = now()
			WHERE id = $1 AND status = 'queued'`, e.JobID, e.AttemptID, s.cfg.Lease.Seconds()); err != nil {
			return fmt.Errorf("mark job running: %w", err)
		}
		e.SchemaVersion = envelopeVersion
		claim = &Claim{
			LeaseSeconds:             int(s.cfg.Lease.Seconds()),
			HeartbeatIntervalSeconds: int(s.cfg.HeartbeatInterval.Seconds()),
			Job:                      e,
		}
		return nil
	})
	return claim, err
}

// Lease is the heartbeat response.
type Lease struct {
	LeaseSeconds    int  `json:"lease_seconds"`
	CancelRequested bool `json:"cancel_requested"`
}

// Heartbeat applies T4: it extends the lease of the current attempt.
func (s *Service) Heartbeat(ctx context.Context, attemptID uuid.UUID) (Lease, error) {
	lease := Lease{LeaseSeconds: int(s.cfg.Lease.Seconds())}
	err := store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE jobs SET lease_expires_at = now() + make_interval(secs => $2), updated_at = now()
			WHERE current_attempt_id = $1 AND status = 'running'
			RETURNING cancel_requested_at IS NOT NULL`, attemptID, s.cfg.Lease.Seconds()).Scan(&lease.CancelRequested)
		if errors.Is(err, pgx.ErrNoRows) {
			return resolveReport(ctx, tx, attemptID, "")
		}
		if err != nil {
			return fmt.Errorf("extend lease: %w", err)
		}
		if _, err = tx.Exec(ctx, `UPDATE job_attempts SET last_heartbeat_at = now() WHERE id = $1`, attemptID); err != nil {
			return fmt.Errorf("record heartbeat: %w", err)
		}
		// A worker busy with long jobs doesn't claim, so its heartbeats keep it marked online.
		_, err = tx.Exec(ctx, `UPDATE workers SET last_seen_at = now()
			WHERE worker_id = (SELECT worker_id FROM job_attempts WHERE id = $1)`, attemptID)
		return err
	})
	return lease, err
}

// Complete applies T6. A repeated identical report is a no-op.
func (s *Service) Complete(ctx context.Context, attemptID uuid.UUID, result json.RawMessage) error {
	if len(result) > MaxResultBytes {
		return ErrResultTooLarge
	}
	if !json.Valid(result) || !bytes.HasPrefix(bytes.TrimSpace(result), []byte("{")) {
		return ErrInvalidResult
	}
	return store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE jobs SET status = 'succeeded', result = $2, current_attempt_id = NULL, lease_expires_at = NULL,
				finished_at = now(), updated_at = now()
			WHERE current_attempt_id = $1 AND status = 'running'`, attemptID, result)
		if err != nil {
			return fmt.Errorf("complete job: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return resolveReport(ctx, tx, attemptID, "succeeded")
		}
		_, err = tx.Exec(ctx, `UPDATE job_attempts SET status = 'succeeded', finished_at = now()
			WHERE id = $1 AND status = 'running'`, attemptID)
		return err
	})
}

// AttemptError is a worker's failure report.
type AttemptError struct {
	Code      string
	Message   string
	Retryable bool
}

// Fail applies T7, T8, or T9 for a worker failure report.
func (s *Service) Fail(ctx context.Context, attemptID uuid.UUID, e AttemptError) error {
	return s.finish(ctx, attemptID, Outcome{Kind: OutcomeFailed, Code: e.Code, Message: e.Message, Retryable: e.Retryable})
}

// ConfirmCancelled applies T9 after the worker stopped the handler.
func (s *Service) ConfirmCancelled(ctx context.Context, attemptID uuid.UUID) error {
	return s.finish(ctx, attemptID, Outcome{Kind: OutcomeCancelled})
}

// runningJob is the locked state needed to decide an outcome.
type runningJob struct {
	ID              uuid.UUID
	AttemptID       uuid.UUID
	AttemptCount    int
	MaxAttempts     int
	CancelRequested bool
}

func (s *Service) finish(ctx context.Context, attemptID uuid.UUID, o Outcome) error {
	return store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		r := runningJob{AttemptID: attemptID}
		err := tx.QueryRow(ctx, `
			SELECT id, attempt_count, max_attempts, cancel_requested_at IS NOT NULL
			FROM jobs WHERE current_attempt_id = $1 AND status = 'running' FOR UPDATE`, attemptID,
		).Scan(&r.ID, &r.AttemptCount, &r.MaxAttempts, &r.CancelRequested)
		if errors.Is(err, pgx.ErrNoRows) {
			return resolveReport(ctx, tx, attemptID, o.AttemptStatus())
		}
		if err != nil {
			return fmt.Errorf("lock running job: %w", err)
		}
		return s.apply(ctx, tx, r, o)
	})
}

// apply writes a Decide result for a job already locked in tx.
func (s *Service) apply(ctx context.Context, tx pgx.Tx, r runningJob, o Outcome) error {
	d := Decide(r.AttemptCount, r.MaxAttempts, r.CancelRequested, o, s.cfg.Retry, s.randN)
	code, msg := nullable(o.Code), nullable(truncate(o.Message, maxErrorMessage))
	var retryable *bool
	if o.Kind != OutcomeCancelled {
		v := o.retryable()
		retryable = &v
	}
	if _, err := tx.Exec(ctx, `
		UPDATE job_attempts SET status = $2, finished_at = now(), error_code = $3, error_message = $4,
			error_retryable = $5
		WHERE id = $1 AND status = 'running'`, r.AttemptID, o.AttemptStatus(), code, msg, retryable); err != nil {
		return fmt.Errorf("finish attempt: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs SET status = $2::text,
			available_at = CASE WHEN $2::text = 'queued' THEN now() + make_interval(secs => $3) ELSE available_at END,
			failure_reason = $4,
			last_error_code = COALESCE($5, last_error_code),
			last_error_message = CASE WHEN $5::text IS NULL THEN last_error_message ELSE $6 END,
			current_attempt_id = NULL, lease_expires_at = NULL,
			finished_at = CASE WHEN $2::text = 'queued' THEN NULL ELSE now() END,
			updated_at = now()
		WHERE id = $1`, r.ID, d.Status, d.RetryAfter.Seconds(), nullable(d.FailureReason), code, msg); err != nil {
		return fmt.Errorf("apply %s: %w", d.Status, err)
	}
	return nil
}

// resolveReport explains why a fenced write matched no row: unknown attempt, an identical
// repeated report (no-op), or a stale attempt (docs/05-JOB-LIFECYCLE.md §6).
func resolveReport(ctx context.Context, tx pgx.Tx, attemptID uuid.UUID, want string) error {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM job_attempts WHERE id = $1`, attemptID).Scan(&status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("load attempt: %w", err)
	case want != "" && status == want:
		return nil
	}
	return ErrAttemptNotCurrent
}

// Reap expires running jobs whose lease or attempt deadline has passed. Only one replica
// reaps at a time (advisory lock). It returns how many jobs were processed.
func (s *Service) Reap(ctx context.Context, limit int) (int, error) {
	n := 0
	err := store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var locked bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, int64(reaperLockID)).Scan(&locked); err != nil {
			return fmt.Errorf("reaper lock: %w", err)
		}
		if !locked {
			return nil
		}
		rows, err := tx.Query(ctx, `
			SELECT j.id, j.current_attempt_id, j.attempt_count, j.max_attempts,
				j.cancel_requested_at IS NOT NULL, a.deadline_at < now()
			FROM jobs j JOIN job_attempts a ON a.id = j.current_attempt_id
			WHERE j.status = 'running' AND (j.lease_expires_at < now() OR a.deadline_at < now())
			ORDER BY j.lease_expires_at
			LIMIT $1
			FOR UPDATE OF j SKIP LOCKED`, limit)
		if err != nil {
			return fmt.Errorf("select expired jobs: %w", err)
		}
		type expired struct {
			runningJob
			DeadlineExceeded bool
		}
		found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (expired, error) {
			var e expired
			err := row.Scan(&e.ID, &e.AttemptID, &e.AttemptCount, &e.MaxAttempts, &e.CancelRequested, &e.DeadlineExceeded)
			return e, err
		})
		if err != nil {
			return fmt.Errorf("select expired jobs: %w", err)
		}
		for _, e := range found {
			o := Outcome{Kind: OutcomeLeaseExpired, Code: "lease_expired", Message: "worker stopped renewing the lease"}
			if e.DeadlineExceeded {
				o = Outcome{Kind: OutcomeTimedOut, Code: "deadline_exceeded", Message: "attempt passed its server deadline"}
			}
			if err := s.apply(ctx, tx, e.runningJob, o); err != nil {
				return err
			}
		}
		n = len(found)
		return nil
	})
	return n, err
}

// querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// queryJob runs a query returning one job row; no row becomes ErrNotFound.
func queryJob(ctx context.Context, q querier, sql string, args ...any) (Job, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return Job{}, fmt.Errorf("query job: %w", err)
	}
	job, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Job])
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("scan job: %w", err)
	}
	return job, nil
}

func audit(ctx context.Context, tx pgx.Tx, p auth.Principal, action string, jobID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, action, job_id)
		VALUES ($1, 'api_key', $2, $3, $4)`, p.TenantID, p.KeyID.String(), action, jobID)
	if err != nil {
		return fmt.Errorf("audit %s: %w", action, err)
	}
	return nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
