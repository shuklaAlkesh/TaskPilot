package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/robfig/cron/v3"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/store"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/tasktypes"
)

// Schedule limits (Provisional).
const (
	MinIntervalSeconds = 5
	MaxIntervalSeconds = 365 * 24 * 60 * 60
	maxSchedules       = 1000
)

var scheduleNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,99}$`)

// ValidScheduleName reports whether name can identify a schedule.
func ValidScheduleName(name string) bool { return scheduleNamePattern.MatchString(name) }

// Schedule is one row of the schedules table.
type Schedule struct {
	ID               uuid.UUID       `db:"id"`
	TenantID         uuid.UUID       `db:"tenant_id"`
	Name             string          `db:"name"`
	TaskType         string          `db:"task_type"`
	TaskVersion      int             `db:"task_version"`
	Queue            *string         `db:"queue"`
	Payload          json.RawMessage `db:"payload"`
	IntervalSeconds  *int            `db:"interval_seconds"`
	Cron             *string         `db:"cron"`
	Timezone         string          `db:"timezone"`
	Enabled          bool            `db:"enabled"`
	AllowOverlap     bool            `db:"allow_overlap"`
	MaxAttempts      *int            `db:"max_attempts"`
	TimeoutSeconds   *int            `db:"timeout_seconds"`
	NextRunAt        time.Time       `db:"next_run_at"`
	LastRunAt        *time.Time      `db:"last_run_at"`
	LastJobID        *uuid.UUID      `db:"last_job_id"`
	LastErrorCode    *string         `db:"last_error_code"`
	LastErrorMessage *string         `db:"last_error_message"`
	CreatedAt        time.Time       `db:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
}

const scheduleColumns = `id, tenant_id, name, task_type, task_version, queue, payload, interval_seconds, cron, timezone,
	enabled, allow_overlap, max_attempts, timeout_seconds, next_run_at, last_run_at, last_job_id, last_error_code,
	last_error_message, created_at, updated_at`

// ScheduleInput is a shape-checked PUT /v1/schedules/{name} request.
type ScheduleInput struct {
	Name            string
	TaskType        string
	TaskVersion     int
	Payload         json.RawMessage
	Queue           string // overrides the task type's queue when set
	IntervalSeconds *int   // exactly one of IntervalSeconds and Cron
	Cron            string
	Timezone        string // IANA name the cron expression is evaluated in
	Enabled         bool
	AllowOverlap    bool
	MaxAttempts     *int
	TimeoutSeconds  *int
}

// timing computes when a schedule runs: every `every`, or when `spec` matches in `loc`.
type timing struct {
	every time.Duration
	spec  cron.Schedule
	loc   *time.Location
}

// cronParser takes standard 5-field expressions and descriptors such as @daily (no seconds field).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func newTiming(intervalSeconds *int, expr, tz string) (timing, []tasktypes.FieldError) {
	var t timing
	var fe []tasktypes.FieldError
	add := func(field, msg string) { fe = append(fe, tasktypes.FieldError{Field: field, Message: msg}) }
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" || tz == "Local" {
		add("/timezone", "must be an IANA time zone such as UTC or Asia/Kolkata")
	}
	t.loc = loc
	switch {
	case intervalSeconds == nil && expr == "":
		add("/interval_seconds", "set interval_seconds or cron")
	case intervalSeconds != nil && expr != "":
		add("/cron", "set interval_seconds or cron, not both")
	case intervalSeconds != nil:
		if n := *intervalSeconds; n < MinIntervalSeconds || n > MaxIntervalSeconds {
			add("/interval_seconds", fmt.Sprintf("must be between %d and %d", MinIntervalSeconds, MaxIntervalSeconds))
		}
		t.every = time.Duration(*intervalSeconds) * time.Second
	case len(expr) > 100 || strings.HasPrefix(expr, "TZ=") || strings.HasPrefix(expr, "CRON_TZ=") || strings.HasPrefix(expr, "@every"):
		add("/cron", "must be at most 100 characters; put the time zone in timezone and intervals in interval_seconds")
	default:
		if t.spec, err = cronParser.Parse(expr); err != nil {
			add("/cron", `must be a 5-field cron expression such as "0 18 * * *" or a descriptor such as @daily`)
		}
	}
	return t, fe
}

// next is the first run time after now, given the run that was due at prev. Interval schedules keep
// their phase (prev + k*every). Runs missed while no API replica was up are skipped, not replayed.
func (t timing) next(prev, now time.Time) time.Time {
	if t.spec != nil {
		return t.spec.Next(now.In(t.loc))
	}
	return prev.Add((now.Sub(prev)/t.every + 1) * t.every)
}

// PutSchedule creates or replaces the tenant's schedule in.Name and reports whether it was created.
// Re-sending an unchanged timing keeps the next run time, so callers can sync schedules from code
// on every deploy without shifting them.
func (s *Service) PutSchedule(ctx context.Context, p auth.Principal, in ScheduleInput) (Schedule, bool, error) {
	if _, _, _, err := s.prepare(SubmitInput{TaskType: in.TaskType, TaskVersion: in.TaskVersion, Payload: in.Payload,
		MaxAttempts: in.MaxAttempts, TimeoutSeconds: in.TimeoutSeconds, Queue: in.Queue}); err != nil {
		return Schedule{}, false, err
	}
	t, fields := newTiming(in.IntervalSeconds, in.Cron, in.Timezone)
	if len(fields) > 0 {
		return Schedule{}, false, &ValidationError{Code: CodeInvalidSchedule, Fields: fields}
	}
	type upserted struct {
		Schedule
		Created bool `db:"created"`
	}
	var out upserted
	err := store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
			return fmt.Errorf("read clock: %w", err)
		}
		next := t.next(now, now)
		if next.IsZero() { // e.g. "0 0 30 2 *"
			return &ValidationError{Code: CodeInvalidSchedule, Fields: []tasktypes.FieldError{{Field: "/cron", Message: "never matches a date"}}}
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("new schedule id: %w", err)
		}
		rows, err := tx.Query(ctx, `
			INSERT INTO schedules (id, tenant_id, name, task_type, task_version, queue, payload, interval_seconds, cron,
				timezone, enabled, allow_overlap, max_attempts, timeout_seconds, next_run_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (tenant_id, name) DO UPDATE SET
				task_type = EXCLUDED.task_type, task_version = EXCLUDED.task_version, queue = EXCLUDED.queue,
				payload = EXCLUDED.payload, interval_seconds = EXCLUDED.interval_seconds, cron = EXCLUDED.cron,
				timezone = EXCLUDED.timezone, enabled = EXCLUDED.enabled, allow_overlap = EXCLUDED.allow_overlap,
				max_attempts = EXCLUDED.max_attempts, timeout_seconds = EXCLUDED.timeout_seconds,
				next_run_at = CASE
					WHEN schedules.interval_seconds IS DISTINCT FROM EXCLUDED.interval_seconds
						OR schedules.cron IS DISTINCT FROM EXCLUDED.cron
						OR schedules.timezone <> EXCLUDED.timezone
						OR (EXCLUDED.enabled AND NOT schedules.enabled)
					THEN EXCLUDED.next_run_at ELSE schedules.next_run_at END,
				updated_at = now()
			RETURNING `+scheduleColumns+`, xmax = 0 AS created`,
			id, p.TenantID, in.Name, in.TaskType, in.TaskVersion, nullable(in.Queue), in.Payload, in.IntervalSeconds,
			nullable(in.Cron), in.Timezone, in.Enabled, in.AllowOverlap, in.MaxAttempts, in.TimeoutSeconds, next)
		if err != nil {
			return fmt.Errorf("upsert schedule: %w", err)
		}
		if out, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[upserted]); err != nil {
			return fmt.Errorf("upsert schedule: %w", err)
		}
		return auditSchedule(ctx, tx, p, "schedule.put", in.Name)
	})
	return out.Schedule, out.Created, err
}

// GetSchedule returns one of the tenant's schedules.
func (s *Service) GetSchedule(ctx context.Context, p auth.Principal, name string) (Schedule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE tenant_id = $1 AND name = $2`,
		p.TenantID, name)
	if err != nil {
		return Schedule{}, fmt.Errorf("get schedule: %w", err)
	}
	sc, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Schedule])
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, ErrScheduleNotFound
	}
	if err != nil {
		return Schedule{}, fmt.Errorf("get schedule: %w", err)
	}
	return sc, nil
}

// ListSchedules returns the tenant's schedules ordered by name.
func (s *Service) ListSchedules(ctx context.Context, p auth.Principal) ([]Schedule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE tenant_id = $1 ORDER BY name LIMIT $2`,
		p.TenantID, maxSchedules)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[Schedule])
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	return items, nil
}

// DeleteSchedule removes one of the tenant's schedules. Jobs it already submitted are unaffected.
func (s *Service) DeleteSchedule(ctx context.Context, p auth.Principal, name string) error {
	return store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM schedules WHERE tenant_id = $1 AND name = $2`, p.TenantID, name)
		if err != nil {
			return fmt.Errorf("delete schedule: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrScheduleNotFound
		}
		return auditSchedule(ctx, tx, p, "schedule.deleted", name)
	})
}

// FireDue submits the job of every enabled schedule whose run is due and moves it to its next run.
// FOR UPDATE SKIP LOCKED lets every API replica call it: each due run is taken by exactly one.
// It returns how many schedules were processed.
func (s *Service) FireDue(ctx context.Context, limit int) (int, error) {
	n := 0
	err := store.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
			return fmt.Errorf("read clock: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT `+scheduleColumns+` FROM schedules s
			WHERE enabled AND next_run_at <= $1
			  AND EXISTS (SELECT 1 FROM tenants t WHERE t.id = s.tenant_id AND t.disabled_at IS NULL)
			ORDER BY next_run_at
			LIMIT $2
			FOR UPDATE OF s SKIP LOCKED`, now, limit)
		if err != nil {
			return fmt.Errorf("select due schedules: %w", err)
		}
		due, err := pgx.CollectRows(rows, pgx.RowToStructByName[Schedule])
		if err != nil {
			return fmt.Errorf("select due schedules: %w", err)
		}
		for _, sc := range due {
			if err := s.fire(ctx, tx, now, sc); err != nil {
				return fmt.Errorf("fire schedule %s: %w", sc.ID, err)
			}
		}
		n = len(due)
		return nil
	})
	return n, err
}

// fire submits one due schedule's job, unless its previous job is still active, and records the run.
// A job that cannot be submitted (task type retired, schema changed) is recorded as last_error.
func (s *Service) fire(ctx context.Context, tx pgx.Tx, now time.Time, sc Schedule) error {
	var expr string
	if sc.Cron != nil {
		expr = *sc.Cron
	}
	t, fields := newTiming(sc.IntervalSeconds, expr, sc.Timezone)
	if len(fields) > 0 { // stored rows were validated; only a server without the zone's tzdata gets here
		_, err := tx.Exec(ctx, `UPDATE schedules SET enabled = false, last_error_code = 'invalid_schedule',
			last_error_message = $2, updated_at = now() WHERE id = $1`, sc.ID, fields[0].Field+" "+fields[0].Message)
		return err
	}
	var jobID *uuid.UUID
	var code, msg string
	active, err := previousRunActive(ctx, tx, sc)
	switch {
	case err != nil:
		return err
	case active:
		code, msg = "previous_run_active", "run skipped: the previous job is still queued or running"
	default:
		id, err := s.submitScheduled(ctx, tx, sc)
		var ve *ValidationError
		switch {
		case errors.Is(err, ErrUnknownTaskType):
			code, msg = "unknown_task_type", "task type is not registered or not enabled"
		case errors.As(err, &ve):
			code, msg = ve.Code, "the payload or options no longer match the registered task type"
		case err != nil:
			return err
		default:
			jobID = &id
		}
	}
	_, err = tx.Exec(ctx, `
		UPDATE schedules SET next_run_at = $2,
			last_run_at = CASE WHEN $3::uuid IS NULL THEN last_run_at ELSE now() END,
			last_job_id = COALESCE($3, last_job_id),
			last_error_code = $4, last_error_message = $5, updated_at = now()
		WHERE id = $1`, sc.ID, t.next(sc.NextRunAt, now), jobID, nullable(code), nullable(msg))
	if err != nil {
		return fmt.Errorf("advance schedule: %w", err)
	}
	return nil
}

func previousRunActive(ctx context.Context, tx pgx.Tx, sc Schedule) (bool, error) {
	if sc.AllowOverlap || sc.LastJobID == nil {
		return false, nil
	}
	var active bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM jobs WHERE id = $1 AND status IN ('queued', 'running'))`,
		*sc.LastJobID).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("check previous run: %w", err)
	}
	return active, nil
}

// submitScheduled applies T1 for a schedule: the same validation and defaults as Submit.
func (s *Service) submitScheduled(ctx context.Context, tx pgx.Tx, sc Schedule) (uuid.UUID, error) {
	in := SubmitInput{TaskType: sc.TaskType, TaskVersion: sc.TaskVersion, Payload: sc.Payload,
		MaxAttempts: sc.MaxAttempts, TimeoutSeconds: sc.TimeoutSeconds}
	if sc.Queue != nil {
		in.Queue = *sc.Queue
	}
	maxAttempts, timeout, queue, err := s.prepare(in)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("new job id: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO jobs (id, tenant_id, task_type, task_version, payload, status, max_attempts, timeout_seconds,
			available_at, queue)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6, $7, now(), $8)`,
		id, sc.TenantID, sc.TaskType, sc.TaskVersion, sc.Payload, maxAttempts, timeout, queue); err != nil {
		return uuid.Nil, fmt.Errorf("insert scheduled job: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, action, job_id, metadata)
		VALUES ($1, 'system', 'scheduler', 'job.submitted', $2, jsonb_build_object('schedule', $3::text))`,
		sc.TenantID, id, sc.Name); err != nil {
		return uuid.Nil, fmt.Errorf("audit scheduled job: %w", err)
	}
	return id, nil
}

func auditSchedule(ctx context.Context, tx pgx.Tx, p auth.Principal, action, name string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, action, metadata)
		VALUES ($1, 'api_key', $2, $3, jsonb_build_object('schedule', $4::text))`, p.TenantID, p.KeyID.String(), action, name)
	if err != nil {
		return fmt.Errorf("audit %s: %w", action, err)
	}
	return nil
}
