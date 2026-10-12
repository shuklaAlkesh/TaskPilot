package jobs

import (
	"errors"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/tasktypes"
)

// Domain errors. httpapi maps each one to a problem response (docs/06-API-SPECIFICATION.md §4).
var (
	ErrNotFound            = errors.New("job not found")
	ErrNotCancellable      = errors.New("job not cancellable")
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
	ErrAttemptNotCurrent   = errors.New("attempt not current")
	ErrUnknownTaskType     = errors.New("unknown task type")
	ErrPayloadTooLarge     = errors.New("payload too large")
	ErrInvalidResult       = errors.New("invalid result")
	ErrResultTooLarge      = errors.New("result too large")
	ErrInvalidCursor       = errors.New("invalid cursor")
)

// Validation codes carried by ValidationError.
const (
	CodeInvalidPayload = "invalid_payload"
	CodeInvalidOptions = "invalid_options"
)

// ValidationError lists field problems in a submit request.
type ValidationError struct {
	Code   string // CodeInvalidPayload or CodeInvalidOptions
	Fields []tasktypes.FieldError
}

func (e *ValidationError) Error() string { return e.Code }
