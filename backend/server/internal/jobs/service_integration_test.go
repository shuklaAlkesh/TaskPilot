//go:build integration

package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/store"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/tasktypes"
)

var testPool *pgxpool.Pool

// TestMain creates a throwaway database from TASKPILOT_TEST_DATABASE_URL (an owner URL),
// migrates it, runs the tests, and drops it.
func TestMain(m *testing.M) {
	os.Exit(func() int {
		base := os.Getenv("TASKPILOT_TEST_DATABASE_URL")
		if base == "" {
			fmt.Println("TASKPILOT_TEST_DATABASE_URL not set")
			return 1
		}
		ctx := context.Background()
		admin, err := store.Open(ctx, base, 2)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		defer admin.Close()
		name := fmt.Sprintf("test_jobs_%d", rand.Uint32())
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			fmt.Println(err)
			return 1
		}
		defer admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)") //nolint:errcheck // best-effort cleanup

		u, _ := url.Parse(base)
		u.Path = "/" + name
		testPool, err = store.Open(ctx, u.String(), 60)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		defer testPool.Close()
		if _, err := store.Migrate(ctx, testPool); err != nil {
			fmt.Println(err)
			return 1
		}
		return m.Run()
	}())
}

var (
	supported = []tasktypes.Key{{TaskType: "system.echo", TaskVersion: 1}, {TaskType: "system.sleep", TaskVersion: 1}}
	queues    = []string{"default"}
)

// testDefinitions are what a worker would register for the two test task types.
func testDefinitions() []tasktypes.Definition {
	schema := func(prop string) json.RawMessage {
		return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["` + prop + `"],
			"properties":{"` + prop + `":{"type":["string","number"],"minLength":1}}}`)
	}
	return []tasktypes.Definition{
		{Key: tasktypes.Key{TaskType: "system.echo", TaskVersion: 1}, Queue: "default", Title: "Echo",
			PayloadSchema: schema("message"), DefaultTimeoutSeconds: 10, MaxTimeoutSeconds: 30, DefaultMaxAttempts: 3, Idempotent: true},
		{Key: tasktypes.Key{TaskType: "system.sleep", TaskVersion: 1}, Queue: "default", Title: "Sleep",
			PayloadSchema: schema("seconds"), DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 900, DefaultMaxAttempts: 3, Idempotent: true, TestOnly: true},
	}
}

// newService resets job tables so claims in one test never see another test's jobs.
func newService(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	_, err := testPool.Exec(ctx, `TRUNCATE jobs, job_attempts, audit_events, task_types, workers, schedules CASCADE`)
	require.NoError(t, err)
	types := tasktypes.NewRegistry(testPool, true)
	require.NoError(t, types.Register(ctx, "test-worker", testDefinitions()))
	t.Cleanup(func() { checkInvariants(t) })
	return NewService(testPool, types, Settings{
		Retry:              RetryPolicy{Base: 5 * time.Second, Cap: time.Minute},
		DefaultMaxAttempts: 3,
		Lease:              30 * time.Second,
		HeartbeatInterval:  10 * time.Second,
		DeadlineGrace:      30 * time.Second,
		MaxScheduleAhead:   time.Hour,
		IdempotencyTTL:     24 * time.Hour,
	})
}

func newTenant(t *testing.T) auth.Principal {
	t.Helper()
	p := auth.Principal{TenantID: uuid.Must(uuid.NewV7()), KeyID: uuid.Must(uuid.NewV7())}
	_, err := testPool.Exec(context.Background(), `INSERT INTO tenants (id, name) VALUES ($1, $2)`,
		p.TenantID, "tenant-"+p.TenantID.String())
	require.NoError(t, err)
	return p
}

func echo(msg string) SubmitInput {
	return SubmitInput{TaskType: "system.echo", TaskVersion: 1, Payload: json.RawMessage(`{"message":"` + msg + `"}`)}
}

// checkInvariants is the SQL invariant checker from docs/09-TESTING-STRATEGY.md §4.1.
func checkInvariants(t *testing.T) {
	t.Helper()
	var bad int
	err := testPool.QueryRow(context.Background(), `
		SELECT
		  (SELECT count(*) FROM jobs j WHERE j.status = 'running' AND NOT EXISTS (
		     SELECT 1 FROM job_attempts a WHERE a.id = j.current_attempt_id AND a.status = 'running'))
		+ (SELECT count(*) FROM (SELECT job_id FROM job_attempts WHERE status = 'running'
		     GROUP BY job_id HAVING count(*) > 1) x)
		+ (SELECT count(*) FROM jobs j WHERE attempt_count <> (SELECT count(*) FROM job_attempts a WHERE a.job_id = j.id))
		+ (SELECT count(*) FROM jobs j JOIN job_attempts a ON a.job_id = j.id
		     WHERE j.status IN ('succeeded','failed','cancelled') AND a.status = 'running')`).Scan(&bad)
	require.NoError(t, err)
	require.Zero(t, bad, "job invariants violated")
}

func TestSubmitClaimComplete(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)

	job, replayed, err := s.Submit(ctx, p, echo("hi"))
	require.NoError(t, err)
	require.False(t, replayed)
	require.Equal(t, StatusQueued, job.Status)
	require.Equal(t, 3, job.MaxAttempts)
	require.Equal(t, 10, job.TimeoutSeconds) // system.echo default

	c, err := s.Claim(ctx, "w1", queues, supported)
	require.NoError(t, err)
	require.NotNil(t, c)
	require.Equal(t, job.ID, c.Job.JobID)
	require.Equal(t, 1, c.Job.AttemptNo)
	require.JSONEq(t, `{"message":"hi"}`, string(c.Job.Payload))

	none, err := s.Claim(ctx, "w2", queues, supported)
	require.NoError(t, err)
	require.Nil(t, none, "a running job must not be claimed twice")

	lease, err := s.Heartbeat(ctx, c.Job.AttemptID)
	require.NoError(t, err)
	require.False(t, lease.CancelRequested)

	require.NoError(t, s.Complete(ctx, c.Job.AttemptID, json.RawMessage(`{"message":"hi"}`)))
	require.NoError(t, s.Complete(ctx, c.Job.AttemptID, json.RawMessage(`{"message":"hi"}`)), "repeat is a no-op")
	require.ErrorIs(t, s.Fail(ctx, c.Job.AttemptID, AttemptError{Code: "x", Retryable: true}), ErrAttemptNotCurrent)

	got, err := s.Get(ctx, p, job.ID)
	require.NoError(t, err)
	require.Equal(t, StatusSucceeded, got.Status)
	require.JSONEq(t, `{"message":"hi"}`, string(got.Result))

	_, err = s.Get(ctx, newTenant(t), job.ID)
	require.ErrorIs(t, err, ErrNotFound, "another tenant's job looks missing")
}

func TestSubmit_Validation(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)

	_, _, err := s.Submit(ctx, p, SubmitInput{TaskType: "no.such", TaskVersion: 1, Payload: json.RawMessage(`{}`)})
	require.ErrorIs(t, err, ErrUnknownTaskType)

	var ve *ValidationError
	_, _, err = s.Submit(ctx, p, SubmitInput{TaskType: "system.echo", TaskVersion: 1, Payload: json.RawMessage(`{}`)})
	require.ErrorAs(t, err, &ve)
	require.Equal(t, CodeInvalidPayload, ve.Code)

	in := echo("x")
	in.MaxAttempts = new(11)
	_, _, err = s.Submit(ctx, p, in)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, CodeInvalidOptions, ve.Code)
}

func TestIdempotency(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)

	in := echo("a")
	in.IdempotencyKey = "order-1"
	first, replayed, err := s.Submit(ctx, p, in)
	require.NoError(t, err)
	require.False(t, replayed)

	again, replayed, err := s.Submit(ctx, p, in)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, first.ID, again.ID)

	other := echo("b")
	other.IdempotencyKey = "order-1"
	_, _, err = s.Submit(ctx, p, other)
	require.ErrorIs(t, err, ErrIdempotencyConflict)

	separate, replayed, err := s.Submit(ctx, newTenant(t), in)
	require.NoError(t, err)
	require.False(t, replayed, "keys are scoped per tenant")
	require.NotEqual(t, first.ID, separate.ID)

	_, err = testPool.Exec(ctx, `UPDATE jobs SET idempotency_expires_at = now() - interval '1 second' WHERE id = $1`, first.ID)
	require.NoError(t, err)
	expired, replayed, err := s.Submit(ctx, p, in)
	require.NoError(t, err)
	require.False(t, replayed, "an expired key creates a new job (T-IDEM-3)")
	require.NotEqual(t, first.ID, expired.ID)
}

func TestIdempotency_ConcurrentSubmits(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	in := echo("same")
	in.IdempotencyKey = "concurrent"

	ids := make([]uuid.UUID, 20)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() {
			job, _, err := s.Submit(ctx, p, in)
			require.NoError(t, err)
			ids[i] = job.ID
		})
	}
	wg.Wait()
	for _, id := range ids {
		require.Equal(t, ids[0], id)
	}
	var n int
	require.NoError(t, testPool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE tenant_id = $1`, p.TenantID).Scan(&n))
	require.Equal(t, 1, n)
}

func TestClaim_ConcurrentWorkers(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	for i := range 20 {
		_, _, err := s.Submit(ctx, p, echo(fmt.Sprint(i)))
		require.NoError(t, err)
	}

	var mu sync.Mutex
	claimed := map[uuid.UUID]int{}
	empty := 0
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			c, err := s.Claim(ctx, fmt.Sprint("w", i), queues, supported)
			require.NoError(t, err)
			mu.Lock()
			defer mu.Unlock()
			if c == nil {
				empty++
				return
			}
			claimed[c.Job.JobID]++
		})
	}
	wg.Wait()
	require.Len(t, claimed, 20)
	require.Equal(t, 30, empty)
	for id, n := range claimed {
		require.Equal(t, 1, n, "job %s claimed twice", id)
	}
}

func TestStaleAttempt_IsFenced(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	job, _, err := s.Submit(ctx, p, echo("x"))
	require.NoError(t, err)

	old, err := s.Claim(ctx, "w1", queues, supported)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `UPDATE jobs SET lease_expires_at = now() - interval '1 second' WHERE id = $1`, job.ID)
	require.NoError(t, err)
	n, err := s.Reap(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	reaped, err := s.Get(ctx, p, job.ID)
	require.NoError(t, err)
	require.Equal(t, StatusQueued, reaped.Status)
	require.Equal(t, "lease_expired", *reaped.LastErrorCode)
	require.True(t, reaped.AvailableAt.After(time.Now()), "retry is delayed by backoff")

	_, err = testPool.Exec(ctx, `UPDATE jobs SET available_at = now() WHERE id = $1`, job.ID)
	require.NoError(t, err)
	fresh, err := s.Claim(ctx, "w2", queues, supported)
	require.NoError(t, err)
	require.Equal(t, 2, fresh.Job.AttemptNo)

	require.ErrorIs(t, s.Complete(ctx, old.Job.AttemptID, json.RawMessage(`{}`)), ErrAttemptNotCurrent)
	_, err = s.Heartbeat(ctx, old.Job.AttemptID)
	require.ErrorIs(t, err, ErrAttemptNotCurrent)
	require.NoError(t, s.Complete(ctx, fresh.Job.AttemptID, json.RawMessage(`{"message":"x"}`)))

	attempts, err := s.Attempts(ctx, p, job.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"lease_expired", "succeeded"}, []string{attempts[0].Status, attempts[1].Status})
}

func TestFail_RetryThenExhaust(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	in := echo("x")
	in.MaxAttempts = new(2)
	job, _, err := s.Submit(ctx, p, in)
	require.NoError(t, err)

	for attempt := 1; attempt <= 2; attempt++ {
		_, err = testPool.Exec(ctx, `UPDATE jobs SET available_at = now() WHERE id = $1`, job.ID)
		require.NoError(t, err)
		c, err := s.Claim(ctx, "w", queues, supported)
		require.NoError(t, err)
		require.NoError(t, s.Fail(ctx, c.Job.AttemptID, AttemptError{Code: "handler_error", Message: "boom", Retryable: true}))
	}
	got, err := s.Get(ctx, p, job.ID)
	require.NoError(t, err)
	require.Equal(t, StatusFailed, got.Status)
	require.Equal(t, ReasonMaxAttemptsExceeded, *got.FailureReason)
	require.Equal(t, "handler_error", *got.LastErrorCode)
}

func TestFail_Permanent(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	job, _, err := s.Submit(ctx, p, echo("x"))
	require.NoError(t, err)
	c, err := s.Claim(ctx, "w", queues, supported)
	require.NoError(t, err)
	e := AttemptError{Code: "csv_parse_error", Message: "bad", Retryable: false}
	require.NoError(t, s.Fail(ctx, c.Job.AttemptID, e))
	require.NoError(t, s.Fail(ctx, c.Job.AttemptID, e), "repeat is a no-op")

	got, err := s.Get(ctx, p, job.ID)
	require.NoError(t, err)
	require.Equal(t, StatusFailed, got.Status)
	require.Equal(t, ReasonPermanentError, *got.FailureReason)
	require.Equal(t, 1, got.AttemptCount)
}

func TestCancel(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)

	queued, _, err := s.Submit(ctx, p, echo("q"))
	require.NoError(t, err)
	got, err := s.Cancel(ctx, p, queued.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCancelled, got.Status)
	got, err = s.Cancel(ctx, p, queued.ID)
	require.NoError(t, err, "cancel is idempotent")
	require.Equal(t, StatusCancelled, got.Status)

	running, _, err := s.Submit(ctx, p, echo("r"))
	require.NoError(t, err)
	c, err := s.Claim(ctx, "w", queues, supported)
	require.NoError(t, err)
	got, err = s.Cancel(ctx, p, running.ID)
	require.NoError(t, err)
	require.Equal(t, StatusRunning, got.Status)
	require.NotNil(t, got.CancelRequestedAt)

	lease, err := s.Heartbeat(ctx, c.Job.AttemptID)
	require.NoError(t, err)
	require.True(t, lease.CancelRequested)
	require.NoError(t, s.ConfirmCancelled(ctx, c.Job.AttemptID))
	got, err = s.Get(ctx, p, running.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCancelled, got.Status)

	done, _, err := s.Submit(ctx, p, echo("d"))
	require.NoError(t, err)
	c, err = s.Claim(ctx, "w", queues, supported)
	require.NoError(t, err)
	require.NoError(t, s.Complete(ctx, c.Job.AttemptID, json.RawMessage(`{}`)))
	got, err = s.Cancel(ctx, p, done.ID)
	require.ErrorIs(t, err, ErrNotCancellable)
	require.Equal(t, StatusSucceeded, got.Status)

	_, err = s.Cancel(ctx, newTenant(t), done.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestReap_DeadlineExceeded(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	in := echo("x")
	in.MaxAttempts = new(1)
	job, _, err := s.Submit(ctx, p, in)
	require.NoError(t, err)
	c, err := s.Claim(ctx, "w", queues, supported)
	require.NoError(t, err)
	// Heartbeats keep the lease alive, but the server-side deadline still wins (T-TIME-2).
	_, err = testPool.Exec(ctx, `UPDATE job_attempts SET deadline_at = now() - interval '1 second' WHERE id = $1`, c.Job.AttemptID)
	require.NoError(t, err)
	_, err = s.Heartbeat(ctx, c.Job.AttemptID)
	require.NoError(t, err)

	n, err := s.Reap(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	got, err := s.Get(ctx, p, job.ID)
	require.NoError(t, err)
	require.Equal(t, StatusFailed, got.Status)
	require.Equal(t, "deadline_exceeded", *got.LastErrorCode)
	_, err = s.Heartbeat(ctx, c.Job.AttemptID)
	require.ErrorIs(t, err, ErrAttemptNotCurrent)
}

func TestList_Pagination(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	for i := range 5 {
		_, _, err := s.Submit(ctx, p, echo(fmt.Sprint(i)))
		require.NoError(t, err)
	}
	seen := map[uuid.UUID]bool{}
	cursor := ""
	for {
		page, err := s.List(ctx, p, ListFilter{Limit: 2, Cursor: cursor})
		require.NoError(t, err)
		for _, j := range page.Items {
			require.Nil(t, j.Payload, "list omits payload")
			seen[j.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	require.Len(t, seen, 5)

	page, err := s.List(ctx, p, ListFilter{Statuses: []Status{StatusSucceeded}})
	require.NoError(t, err)
	require.Empty(t, page.Items)
}

func TestStats_CountsEveryState(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	retryable := AttemptError{Code: "handler_error", Message: "boom", Retryable: true}
	submitAndClaim := func(worker string) *Claim {
		_, _, err := s.Submit(ctx, p, echo(worker))
		require.NoError(t, err)
		c, err := s.Claim(ctx, worker, queues, supported)
		require.NoError(t, err)
		return c
	}
	makeAvailable := func(id uuid.UUID) {
		_, err := testPool.Exec(ctx, `UPDATE jobs SET available_at = now() WHERE id = $1`, id)
		require.NoError(t, err)
	}

	a := submitAndClaim("w-a") // succeeded first time
	require.NoError(t, s.Complete(ctx, a.Job.AttemptID, json.RawMessage(`{}`)))
	b := submitAndClaim("w-b") // fails once, succeeds on retry
	require.NoError(t, s.Fail(ctx, b.Job.AttemptID, retryable))
	makeAvailable(b.Job.JobID)
	b2, err := s.Claim(ctx, "w-b", queues, supported)
	require.NoError(t, err)
	require.NoError(t, s.Complete(ctx, b2.Job.AttemptID, json.RawMessage(`{}`)))
	c := submitAndClaim("w-c") // dead letter
	require.NoError(t, s.Fail(ctx, c.Job.AttemptID, AttemptError{Code: "csv_parse_error", Retryable: false}))
	g := submitAndClaim("w-g") // waiting for a retry (backoff)
	require.NoError(t, s.Fail(ctx, g.Job.AttemptID, retryable))
	submitAndClaim("w-d") // still running
	h, _, err := s.Submit(ctx, p, echo("h"))
	require.NoError(t, err)
	_, err = s.Cancel(ctx, p, h.ID) // cancelled while queued
	require.NoError(t, err)
	_, _, err = s.Submit(ctx, p, echo("e")) // ready
	require.NoError(t, err)
	later := echo("f")
	runAt := time.Now().Add(30 * time.Minute)
	later.RunAt = &runAt
	_, _, err = s.Submit(ctx, p, later) // scheduled ahead
	require.NoError(t, err)

	other := newTenant(t) // must not leak into p's numbers
	_, _, err = s.Submit(ctx, other, echo("other"))
	require.NoError(t, err)

	st, err := s.Stats(ctx, p, "1h")
	require.NoError(t, err)
	require.Equal(t, StatsTotals{
		Submitted: 8, Delayed: 2, RetryWaiting: 1, Ready: 1, Running: 1, Succeeded: 2, Failed: 1,
		Cancelled: 1, Retries: 1, JobsRetried: 1, ActiveWorkers: 1,
	}, st.Totals)
	require.InDelta(t, 2.0/3.0, *st.SuccessRate, 1e-9)
	require.Equal(t, LastMinute{Submitted: 8, Finished: 4}, st.LastMinute)
	require.NotNil(t, st.QueueLagMs, "one job is ready")
	require.GreaterOrEqual(t, *st.QueueLagMs, 0.0)
	require.Equal(t, map[string]int{"succeeded": 2, "failed": 3}, st.Attempts)
	require.Equal(t, []CodeCount{{"handler_error", 2}, {"csv_parse_error", 1}}, st.Errors)
	require.Len(t, st.TaskTypes, 1)
	require.Equal(t, 8, st.TaskTypes[0].Total)
	require.NotNil(t, st.Latency.RunTimeP50)
	require.NotNil(t, st.Latency.CompleteP95)

	var sum ThroughputBucket
	for _, b := range st.Throughput {
		sum.Submitted += b.Submitted
		sum.Succeeded += b.Succeeded
		sum.Failed += b.Failed
		sum.Cancelled += b.Cancelled
		sum.Retried += b.Retried
	}
	require.Equal(t, ThroughputBucket{Submitted: 8, Succeeded: 2, Failed: 1, Cancelled: 1, Retried: 1}, sum)
	require.GreaterOrEqual(t, len(st.Throughput), 60)

	empty, err := s.Stats(ctx, newTenant(t), "15m")
	require.NoError(t, err)
	require.Zero(t, empty.Totals)
	require.Nil(t, empty.SuccessRate, "no finished jobs means no rate, not 0%")
	require.Nil(t, empty.QueueLagMs, "nothing ready means no lag")
	_, err = s.Stats(ctx, p, "7d")
	require.Error(t, err)
}

func TestQueues_RouteJobsToSubscribedWorkers(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	report := tasktypes.Definition{
		Key: tasktypes.Key{TaskType: "reports.tasks.send_digest", TaskVersion: 1}, Queue: "notifications", Title: "Digest",
		PayloadSchema:         json.RawMessage(`{"type":"object","properties":{"to":{"type":"string"}},"required":["to"]}`),
		DefaultTimeoutSeconds: 3600, MaxTimeoutSeconds: 86400, DefaultMaxAttempts: 1, SideEffects: true,
	}
	require.NoError(t, s.types.Register(ctx, "notifier", []tasktypes.Definition{report}))
	all := append(slices.Clone(supported), report.Key)

	job, _, err := s.Submit(ctx, p, SubmitInput{TaskType: report.TaskType, TaskVersion: 1, Payload: json.RawMessage(`{"to":"ops"}`)})
	require.NoError(t, err)
	require.Equal(t, "notifications", job.Queue, "routed by the task type's queue")
	require.Equal(t, 3600, job.TimeoutSeconds, "long default timeout from registration")

	none, err := s.Claim(ctx, "default-worker", []string{"default"}, all)
	require.NoError(t, err)
	require.Nil(t, none, "a worker on another queue never gets the job")
	c, err := s.Claim(ctx, "notifier", []string{"notifications"}, all)
	require.NoError(t, err)
	require.Equal(t, job.ID, c.Job.JobID)

	in := echo("override")
	in.Queue = "urgent"
	moved, _, err := s.Submit(ctx, p, in)
	require.NoError(t, err)
	require.Equal(t, "urgent", moved.Queue, "submit can override the queue")
	in.Queue = "Not Valid"
	var ve *ValidationError
	_, _, err = s.Submit(ctx, p, in)
	require.ErrorAs(t, err, &ve)
}

func TestWorkers_PresenceAndQueueStats(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	require.NoError(t, s.RegisterWorker(ctx, Worker{WorkerID: "w-1", Hostname: "h", Version: "1", Concurrency: 4,
		Queues: []string{"default"}, TaskTypes: supported}))
	require.NoError(t, s.RegisterWorker(ctx, Worker{WorkerID: "w-old", Hostname: "h", Version: "1", Concurrency: 8,
		Queues: []string{"default"}, TaskTypes: supported}))
	_, err := testPool.Exec(ctx, `UPDATE workers SET last_seen_at = now() - interval '5 minutes' WHERE worker_id = 'w-old'`)
	require.NoError(t, err)

	online, err := s.WorkersOnlineByTaskType(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, online["system.echo@1"], "a worker silent for 5 minutes is offline")

	_, _, err = s.Submit(ctx, p, echo("waiting"))
	require.NoError(t, err)
	st, err := s.Stats(ctx, p, "1h")
	require.NoError(t, err)
	require.Len(t, st.Queues, 1)
	q := st.Queues[0]
	require.Equal(t, "default", q.Queue)
	require.Equal(t, 1, q.Ready)
	require.Equal(t, 1, q.WorkersOnline)
	require.Equal(t, 4, q.Slots)
	require.Equal(t, 2, q.TaskTypes)
	require.NotNil(t, q.LagMs)
}

func TestRegister_SchemaChangeRules(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	defs := testDefinitions()
	changed := slices.Clone(defs)
	changed[0].PayloadSchema = json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"},"loud":{"type":"boolean"}},"required":["message"]}`)

	require.NoError(t, s.types.Register(ctx, "w", defs), "re-registering the same schema is a no-op")
	job, _, err := s.Submit(ctx, p, echo("pending"))
	require.NoError(t, err)
	require.ErrorIs(t, s.types.Register(ctx, "w", changed), tasktypes.ErrSchemaConflict, "a queued job still uses v1")

	_, err = s.Cancel(ctx, p, job.ID)
	require.NoError(t, err)
	require.NoError(t, s.types.Register(ctx, "w", changed), "allowed once no job is waiting or running")
	tt, ok := s.types.Lookup(defs[0].Key)
	require.True(t, ok)
	fields, err := tt.Validate(json.RawMessage(`{"message":"x","loud":true}`))
	require.NoError(t, err)
	require.Empty(t, fields, "the new schema is live")

	defs[0].Title = "Echo (renamed)"
	defs[0].PayloadSchema = changed[0].PayloadSchema
	require.NoError(t, s.types.Register(ctx, "w", defs[:1]), "metadata may always change")
	tt, _ = s.types.Lookup(defs[0].Key)
	require.Equal(t, "Echo (renamed)", tt.Title)
}
