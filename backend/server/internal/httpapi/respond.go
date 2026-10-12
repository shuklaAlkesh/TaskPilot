package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/jobs"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/tasktypes"
)

const maxBodyBytes = 128 << 10

// problem is an RFC 9457 response body (docs/06-API-SPECIFICATION.md §4).
type problem struct {
	Type      string                 `json:"type"`
	Title     string                 `json:"title"`
	Status    int                    `json:"status"`
	Code      string                 `json:"code"`
	Detail    string                 `json:"detail,omitempty"`
	RequestID string                 `json:"request_id"`
	Errors    []tasktypes.FieldError `json:"errors,omitempty"`
	JobStatus jobs.Status            `json:"job_status,omitempty"`
}

// httpError is a request-level failure decided by this package.
type httpError struct {
	status int
	code   string
	title  string
	detail string
}

func (e *httpError) Error() string { return e.code }

func invalidRequest(detail string) error {
	return &httpError{http.StatusBadRequest, "invalid_request", "Invalid request", detail}
}

var (
	errUnsupportedMedia  = &httpError{http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", ""}
	errBodyTooLarge      = &httpError{http.StatusRequestEntityTooLarge, "payload_too_large", "Request body too large", ""}
	errInsufficientScope = &httpError{http.StatusForbidden, "insufficient_scope", "API key lacks the required scope", ""}
)

// decodeJSON reads exactly one JSON object, rejecting unknown fields and oversized bodies.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return errUnsupportedMedia
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errBodyTooLarge
		}
		return invalidRequest(err.Error())
	}
	if dec.More() {
		return invalidRequest("body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // client went away; nothing left to do
}

// writeError is the single mapping from errors to problem responses.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	p := problem{Status: http.StatusInternalServerError, Code: "internal_error", Title: "Internal error"}
	var he *httpError
	var ve *jobs.ValidationError
	var de *tasktypes.DefinitionError
	switch {
	case errors.As(err, &he):
		p.Status, p.Code, p.Title, p.Detail = he.status, he.code, he.title, he.detail
	case errors.As(err, &ve):
		p.Status, p.Code, p.Title, p.Errors = http.StatusUnprocessableEntity, ve.Code, "Validation failed", ve.Fields
	case errors.As(err, &de):
		p.Status, p.Code, p.Title, p.Detail, p.Errors = http.StatusUnprocessableEntity, "invalid_task_type",
			"Task type definition rejected", de.Key.String(), de.Fields
	case errors.Is(err, tasktypes.ErrSchemaConflict):
		p.Status, p.Code, p.Title, p.Detail = http.StatusConflict, "task_type_conflict",
			"Task type version already registered with a different schema", err.Error()
	case errors.Is(err, auth.ErrUnauthenticated):
		p.Status, p.Code, p.Title = http.StatusUnauthorized, "unauthenticated", "Missing or invalid credentials"
		w.Header().Set("WWW-Authenticate", "Bearer")
	case errors.Is(err, jobs.ErrNotFound):
		p.Status, p.Code, p.Title = http.StatusNotFound, "job_not_found", "Not found"
	case errors.Is(err, jobs.ErrIdempotencyConflict):
		p.Status, p.Code, p.Title = http.StatusConflict, "idempotency_key_conflict", "Idempotency key reused with a different request"
	case errors.Is(err, jobs.ErrNotCancellable):
		p.Status, p.Code, p.Title = http.StatusConflict, "job_not_cancellable", "Job already finished"
	case errors.Is(err, jobs.ErrAttemptNotCurrent):
		p.Status, p.Code, p.Title = http.StatusConflict, "attempt_not_current", "Attempt is no longer current"
	case errors.Is(err, jobs.ErrPayloadTooLarge), errors.Is(err, jobs.ErrResultTooLarge):
		p.Status, p.Code, p.Title = http.StatusRequestEntityTooLarge, "payload_too_large", "Payload or result exceeds 64 KiB"
	case errors.Is(err, jobs.ErrUnknownTaskType):
		p.Status, p.Code, p.Title = http.StatusUnprocessableEntity, "unknown_task_type", "Task type is not registered or not enabled"
	case errors.Is(err, jobs.ErrInvalidResult):
		p.Status, p.Code, p.Title = http.StatusUnprocessableEntity, "invalid_result", "Result must be a JSON object"
	case errors.Is(err, jobs.ErrInvalidCursor):
		p.Status, p.Code, p.Title = http.StatusBadRequest, "invalid_cursor", "Cursor is invalid"
	case errors.Is(err, context.DeadlineExceeded):
		p.Status, p.Code, p.Title = http.StatusServiceUnavailable, "unavailable", "Service temporarily unavailable"
		w.Header().Set("Retry-After", "5")
	default:
		slog.ErrorContext(r.Context(), "request failed", "request_id", requestID(r.Context()), "error", err)
	}
	p.Type = "https://taskpilot.dev/problems/" + p.Code
	p.RequestID = requestID(r.Context())
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p) // client went away; nothing left to do
}
