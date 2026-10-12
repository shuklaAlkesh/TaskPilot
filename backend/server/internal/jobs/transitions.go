package jobs

import (
	"time"
)

// Status is a job state. Authoritative definitions: docs/05-JOB-LIFECYCLE.md §2.
type Status string

// Job states.
const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Valid reports whether s is a known job status.
func (s Status) Valid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// Failure reasons for StatusFailed.
const (
	ReasonPermanentError      = "permanent_error"
	ReasonMaxAttemptsExceeded = "max_attempts_exceeded"
)

// OutcomeKind says who ended an attempt and how.
type OutcomeKind int

// Outcome kinds: worker reports (Failed, Cancelled) and reaper findings (TimedOut, LeaseExpired).
const (
	OutcomeFailed OutcomeKind = iota
	OutcomeCancelled
	OutcomeTimedOut
	OutcomeLeaseExpired
)

// Outcome is any non-success end of an attempt.
type Outcome struct {
	Kind      OutcomeKind
	Code      string
	Message   string
	Retryable bool // only read for OutcomeFailed
}

// AttemptStatus is the terminal job_attempts.status for this outcome (docs/05-JOB-LIFECYCLE.md §4).
func (o Outcome) AttemptStatus() string {
	switch {
	case o.Kind == OutcomeCancelled:
		return "cancelled"
	case o.Kind == OutcomeLeaseExpired:
		return "lease_expired"
	case o.Kind == OutcomeTimedOut, o.Code == "timeout":
		return "timed_out"
	}
	return "failed"
}

func (o Outcome) retryable() bool {
	switch o.Kind {
	case OutcomeFailed:
		return o.Retryable
	case OutcomeCancelled:
		return false
	}
	return true // timeouts and lease expiry are always retryable (§5.2)
}

// RetryPolicy configures exponential backoff with full jitter.
type RetryPolicy struct {
	Base time.Duration
	Cap  time.Duration
}

// Decision is the job's next state after a non-success outcome.
type Decision struct {
	Status        Status
	FailureReason string        // set when Status is failed
	RetryAfter    time.Duration // set when Status is queued
}

// Decide applies transitions T7, T8, and T9 (docs/05-JOB-LIFECYCLE.md §3).
// attemptCount already includes the attempt that just ended. randN returns a value in [0, n).
func Decide(attemptCount, maxAttempts int, cancelRequested bool, o Outcome, p RetryPolicy, randN func(int64) int64) Decision {
	switch {
	case o.Kind == OutcomeCancelled || cancelRequested:
		return Decision{Status: StatusCancelled}
	case !o.retryable():
		return Decision{Status: StatusFailed, FailureReason: ReasonPermanentError}
	case attemptCount >= maxAttempts:
		return Decision{Status: StatusFailed, FailureReason: ReasonMaxAttemptsExceeded}
	}
	return Decision{Status: StatusQueued, RetryAfter: Backoff(attemptCount, p, randN)}
}

// Backoff returns a full-jitter delay: uniform in [0, min(cap, base·2^(n−1))], at least 1 s (§5.3).
func Backoff(attemptNo int, p RetryPolicy, randN func(int64) int64) time.Duration {
	shift := min(max(attemptNo-1, 0), 20) // 20 doublings exceed any sane cap and cannot overflow
	ceiling := min(p.Cap, p.Base<<shift)
	return max(time.Second, time.Duration(randN(int64(ceiling)+1)))
}

// CancelAction is what a client cancel request does to a job.
type CancelAction int

// Cancel actions (docs/05-JOB-LIFECYCLE.md §10).
const (
	CancelNoop    CancelAction = iota // already cancelled or already requested
	CancelNow                         // T3: queued → cancelled
	CancelRequest                     // T5: running, set cancel_requested_at
)

// DecideCancel returns the cancel action for a job, or ErrNotCancellable.
func DecideCancel(s Status, cancelRequested bool) (CancelAction, error) {
	switch s {
	case StatusQueued:
		return CancelNow, nil
	case StatusRunning:
		if cancelRequested {
			return CancelNoop, nil
		}
		return CancelRequest, nil
	case StatusCancelled:
		return CancelNoop, nil
	}
	return CancelNoop, ErrNotCancellable
}
