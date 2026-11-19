//go:build integration

package jobs

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func everySeconds(name string, n int) ScheduleInput {
	return ScheduleInput{Name: name, TaskType: "system.echo", TaskVersion: 1, Payload: json.RawMessage(`{"message":"tick"}`),
		IntervalSeconds: &n, Timezone: "UTC", Enabled: true}
}

// makeDue moves a schedule's next run into the past, as if its time had come.
func makeDue(t *testing.T, id uuid.UUID) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`UPDATE schedules SET next_run_at = now() - interval '1 second' WHERE id = $1`, id)
	require.NoError(t, err)
}

func TestScheduleFiresOnceAndAdvances(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)

	sc, created, err := s.PutSchedule(ctx, p, everySeconds("beat", 60))
	require.NoError(t, err)
	require.True(t, created)
	require.WithinDuration(t, time.Now().Add(time.Minute), sc.NextRunAt, 5*time.Second)

	again, created, err := s.PutSchedule(ctx, p, everySeconds("beat", 60))
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, sc.NextRunAt, again.NextRunAt, "re-syncing an unchanged schedule keeps its phase")

	n, err := s.FireDue(ctx, 10)
	require.NoError(t, err)
	require.Zero(t, n, "not due yet")

	makeDue(t, sc.ID)
	n, err = s.FireDue(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = s.FireDue(ctx, 10)
	require.NoError(t, err)
	require.Zero(t, n, "a due run fires exactly once")

	got, err := s.GetSchedule(ctx, p, "beat")
	require.NoError(t, err)
	require.NotNil(t, got.LastJobID)
	require.NotNil(t, got.LastRunAt)
	require.Nil(t, got.LastErrorCode)
	require.True(t, got.NextRunAt.After(time.Now()))

	job, err := s.Get(ctx, p, *got.LastJobID)
	require.NoError(t, err)
	require.Equal(t, StatusQueued, job.Status)
	require.Equal(t, "default", job.Queue)
	require.JSONEq(t, `{"message":"tick"}`, string(job.Payload))
}

func TestScheduleFiresOnceAcrossReplicas(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	in := everySeconds("shared", 60)
	in.AllowOverlap = true // so only SKIP LOCKED, not the overlap rule, prevents duplicates
	sc, _, err := s.PutSchedule(ctx, p, in)
	require.NoError(t, err)
	makeDue(t, sc.ID)

	var wg sync.WaitGroup
	for range 10 { // ten API replicas ticking at the same moment
		wg.Go(func() {
			_, err := s.FireDue(ctx, 10)
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	page, err := s.List(ctx, p, ListFilter{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
}

func TestScheduleSkipsWhilePreviousRunIsActive(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	sc, _, err := s.PutSchedule(ctx, p, everySeconds("heartbeat", 20))
	require.NoError(t, err)

	makeDue(t, sc.ID)
	_, err = s.FireDue(ctx, 10)
	require.NoError(t, err)
	first, err := s.GetSchedule(ctx, p, "heartbeat")
	require.NoError(t, err)

	makeDue(t, sc.ID)
	_, err = s.FireDue(ctx, 10)
	require.NoError(t, err)
	skipped, err := s.GetSchedule(ctx, p, "heartbeat")
	require.NoError(t, err)
	require.Equal(t, first.LastJobID, skipped.LastJobID, "no second job while the first is queued")
	require.Equal(t, "previous_run_active", *skipped.LastErrorCode)

	c, err := s.Claim(ctx, "w1", queues, supported)
	require.NoError(t, err)
	require.NotNil(t, c)
	require.NoError(t, s.Complete(ctx, c.Job.AttemptID, json.RawMessage(`{"message":"tick"}`)))

	makeDue(t, sc.ID)
	_, err = s.FireDue(ctx, 10)
	require.NoError(t, err)
	next, err := s.GetSchedule(ctx, p, "heartbeat")
	require.NoError(t, err)
	require.NotEqual(t, first.LastJobID, next.LastJobID, "fires again once the previous job finished")
	require.Nil(t, next.LastErrorCode)
}

func TestScheduleAllowOverlap(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	in := everySeconds("overlap", 20)
	in.AllowOverlap = true
	sc, _, err := s.PutSchedule(ctx, p, in)
	require.NoError(t, err)
	for range 2 {
		makeDue(t, sc.ID)
		_, err = s.FireDue(ctx, 10)
		require.NoError(t, err)
	}
	page, err := s.List(ctx, p, ListFilter{})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
}

func TestScheduleTimingChangeResetsNextRun(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	_, _, err := s.PutSchedule(ctx, p, everySeconds("digest", 60))
	require.NoError(t, err)

	in := everySeconds("digest", 3600)
	in.IntervalSeconds = nil
	in.Cron, in.Timezone = "0 18 * * *", "Asia/Kolkata"
	sc, created, err := s.PutSchedule(ctx, p, in)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, "0 18 * * *", *sc.Cron)
	require.Nil(t, sc.IntervalSeconds)
	ist, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	local := sc.NextRunAt.In(ist)
	require.Equal(t, []int{18, 0, 0}, []int{local.Hour(), local.Minute(), local.Second()})
}

func TestScheduleValidation(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)

	unknown := everySeconds("x", 60)
	unknown.TaskType = "nope.missing"
	_, _, err := s.PutSchedule(ctx, p, unknown)
	require.ErrorIs(t, err, ErrUnknownTaskType)

	badPayload := everySeconds("x", 60)
	badPayload.Payload = json.RawMessage(`{"other":1}`)
	_, _, err = s.PutSchedule(ctx, p, badPayload)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	require.Equal(t, CodeInvalidPayload, ve.Code)

	badCron := everySeconds("x", 60)
	badCron.IntervalSeconds, badCron.Cron = nil, "not a cron"
	_, _, err = s.PutSchedule(ctx, p, badCron)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, CodeInvalidSchedule, ve.Code)

	never := everySeconds("x", 60)
	never.IntervalSeconds, never.Cron = nil, "0 0 30 2 *"
	_, _, err = s.PutSchedule(ctx, p, never)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, CodeInvalidSchedule, ve.Code)

	badQueue := everySeconds("x", 60)
	badQueue.Queue = "Not A Queue"
	_, _, err = s.PutSchedule(ctx, p, badQueue)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, CodeInvalidOptions, ve.Code)

	list, err := s.ListSchedules(ctx, p)
	require.NoError(t, err)
	require.Empty(t, list, "rejected definitions are not stored")
}

func TestScheduleTenantIsolationAndDelete(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	a, b := newTenant(t), newTenant(t)
	_, _, err := s.PutSchedule(ctx, a, everySeconds("beat", 60))
	require.NoError(t, err)

	_, err = s.GetSchedule(ctx, b, "beat")
	require.ErrorIs(t, err, ErrScheduleNotFound)
	require.ErrorIs(t, s.DeleteSchedule(ctx, b, "beat"), ErrScheduleNotFound)
	list, err := s.ListSchedules(ctx, b)
	require.NoError(t, err)
	require.Empty(t, list)

	require.NoError(t, s.DeleteSchedule(ctx, a, "beat"))
	_, err = s.GetSchedule(ctx, a, "beat")
	require.ErrorIs(t, err, ErrScheduleNotFound)
}

func TestDisabledScheduleDoesNotFire(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	in := everySeconds("paused", 60)
	in.Enabled = false
	sc, _, err := s.PutSchedule(ctx, p, in)
	require.NoError(t, err)
	makeDue(t, sc.ID)
	n, err := s.FireDue(ctx, 10)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestScheduleOfRetiredTaskTypeRecordsError(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p := newTenant(t)
	sc, _, err := s.PutSchedule(ctx, p, everySeconds("orphan", 60))
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `DELETE FROM task_types WHERE task_type = 'system.echo'`)
	require.NoError(t, err)
	require.NoError(t, s.types.Refresh(ctx))

	makeDue(t, sc.ID)
	n, err := s.FireDue(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	got, err := s.GetSchedule(ctx, p, "orphan")
	require.NoError(t, err)
	require.Equal(t, "unknown_task_type", *got.LastErrorCode)
	require.Nil(t, got.LastJobID)
	require.True(t, got.NextRunAt.After(time.Now()), "a failed run still advances, so it doesn't fire every tick")
}
