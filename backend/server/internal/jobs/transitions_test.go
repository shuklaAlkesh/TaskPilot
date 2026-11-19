package jobs

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var policy = RetryPolicy{Base: 5 * time.Second, Cap: 10 * time.Minute}

func seeded() func(int64) int64 { return rand.New(rand.NewPCG(1, 2)).Int64N }

func TestDecide(t *testing.T) {
	t.Parallel()
	retryable := Outcome{Kind: OutcomeFailed, Code: "handler_error", Retryable: true}
	permanent := Outcome{Kind: OutcomeFailed, Code: "csv_parse_error"}
	tests := []struct {
		name            string
		attempts, max   int
		cancelRequested bool
		outcome         Outcome
		want            Status
		reason          string
	}{
		{"T7 retryable with attempts left", 1, 3, false, retryable, StatusQueued, ""},
		{"T7 lease expired with attempts left", 2, 3, false, Outcome{Kind: OutcomeLeaseExpired}, StatusQueued, ""},
		{"T7 timeout with attempts left", 1, 3, false, Outcome{Kind: OutcomeTimedOut}, StatusQueued, ""},
		{"T8 permanent on first attempt", 1, 3, false, permanent, StatusFailed, ReasonPermanentError},
		{"T8 retryable but exhausted", 3, 3, false, retryable, StatusFailed, ReasonMaxAttemptsExceeded},
		{"T8 lease expired and exhausted", 1, 1, false, Outcome{Kind: OutcomeLeaseExpired}, StatusFailed, ReasonMaxAttemptsExceeded},
		{"T9 cancelled report", 1, 3, false, Outcome{Kind: OutcomeCancelled}, StatusCancelled, ""},
		{"T9 failure while cancel requested", 1, 3, true, retryable, StatusCancelled, ""},
		{"T9 lease expired while cancel requested (F11)", 1, 3, true, Outcome{Kind: OutcomeLeaseExpired}, StatusCancelled, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := Decide(tt.attempts, tt.max, tt.cancelRequested, tt.outcome, policy, seeded())
			require.Equal(t, tt.want, d.Status)
			require.Equal(t, tt.reason, d.FailureReason)
			if tt.want == StatusQueued {
				require.GreaterOrEqual(t, d.RetryAfter, time.Second)
			} else {
				require.Zero(t, d.RetryAfter)
			}
		})
	}
}

func TestOutcome_AttemptStatus(t *testing.T) {
	t.Parallel()
	require.Equal(t, "failed", Outcome{Kind: OutcomeFailed, Code: "handler_error"}.AttemptStatus())
	require.Equal(t, "timed_out", Outcome{Kind: OutcomeFailed, Code: "timeout"}.AttemptStatus())
	require.Equal(t, "timed_out", Outcome{Kind: OutcomeTimedOut}.AttemptStatus())
	require.Equal(t, "lease_expired", Outcome{Kind: OutcomeLeaseExpired}.AttemptStatus())
	require.Equal(t, "cancelled", Outcome{Kind: OutcomeCancelled}.AttemptStatus())
}

func TestBackoff_Bounds(t *testing.T) {
	t.Parallel()
	randN := seeded()
	for n := 1; n <= 10; n++ {
		ceiling := min(policy.Cap, policy.Base<<(n-1))
		for range 200 {
			d := Backoff(n, policy, randN)
			require.GreaterOrEqual(t, d, time.Second, "attempt %d", n)
			require.LessOrEqual(t, d, ceiling, "attempt %d", n)
		}
	}
	require.Equal(t, time.Second, Backoff(0, policy, func(int64) int64 { return 0 }))
	require.Equal(t, policy.Cap, Backoff(1000, policy, func(n int64) int64 { return n - 1 }))
}

func TestDecideCancel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status    Status
		requested bool
		want      CancelAction
		err       error
	}{
		{StatusQueued, false, CancelNow, nil},
		{StatusRunning, false, CancelRequest, nil},
		{StatusRunning, true, CancelNoop, nil},
		{StatusCancelled, false, CancelNoop, nil},
		{StatusSucceeded, false, CancelNoop, ErrNotCancellable},
		{StatusFailed, false, CancelNoop, ErrNotCancellable},
	}
	for _, tt := range tests {
		got, err := DecideCancel(tt.status, tt.requested)
		require.Equal(t, tt.want, got, tt.status)
		require.ErrorIs(t, err, tt.err)
	}
}

func TestCursor_RoundTrip(t *testing.T) {
	t.Parallel()
	_, _, err := decodeCursor("not-base64!")
	require.ErrorIs(t, err, ErrInvalidCursor)
	_, _, err = decodeCursor(encodeCursor(time.Now(), [16]byte{})[:10])
	require.ErrorIs(t, err, ErrInvalidCursor)
}
