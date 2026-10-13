package httpapi

import (
	"bytes"
	"cmp"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/jobs"
)

// scheduleView is the public Schedule representation (openapi.yaml #/components/schemas/Schedule).
type scheduleView struct {
	Name            string          `json:"name"`
	TaskType        string          `json:"task_type"`
	TaskVersion     int             `json:"task_version"`
	Queue           *string         `json:"queue"`
	Payload         json.RawMessage `json:"payload"`
	IntervalSeconds *int            `json:"interval_seconds"`
	Cron            *string         `json:"cron"`
	Timezone        string          `json:"timezone"`
	Enabled         bool            `json:"enabled"`
	AllowOverlap    bool            `json:"allow_overlap"`
	MaxAttempts     *int            `json:"max_attempts"`
	TimeoutSeconds  *int            `json:"timeout_seconds"`
	NextRunAt       *time.Time      `json:"next_run_at"`
	LastRunAt       *time.Time      `json:"last_run_at"`
	LastJobID       *uuid.UUID      `json:"last_job_id"`
	LastError       *errorInfo      `json:"last_error"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func newScheduleView(sc jobs.Schedule) scheduleView {
	v := scheduleView{
		Name: sc.Name, TaskType: sc.TaskType, TaskVersion: sc.TaskVersion, Queue: sc.Queue, Payload: sc.Payload,
		IntervalSeconds: sc.IntervalSeconds, Cron: sc.Cron, Timezone: sc.Timezone, Enabled: sc.Enabled,
		AllowOverlap: sc.AllowOverlap, MaxAttempts: sc.MaxAttempts, TimeoutSeconds: sc.TimeoutSeconds,
		LastRunAt: sc.LastRunAt, LastJobID: sc.LastJobID, CreatedAt: sc.CreatedAt, UpdatedAt: sc.UpdatedAt,
	}
	if sc.Enabled {
		v.NextRunAt = &sc.NextRunAt
	}
	if sc.LastErrorCode != nil {
		v.LastError = &errorInfo{Code: *sc.LastErrorCode, Message: deref(sc.LastErrorMessage)}
	}
	return v
}

func scheduleName(r *http.Request) (string, error) {
	name := r.PathValue("name")
	if !jobs.ValidScheduleName(name) {
		return "", invalidRequest("name must match ^[a-z0-9][a-z0-9_.-]{0,99}$")
	}
	return name, nil
}

func (s *Server) putSchedule(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	name, err := scheduleName(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req struct {
		TaskType        string          `json:"task_type"`
		TaskVersion     int             `json:"task_version"`
		Payload         json.RawMessage `json:"payload"`
		Queue           string          `json:"queue"`
		IntervalSeconds *int            `json:"interval_seconds"`
		Cron            string          `json:"cron"`
		Timezone        string          `json:"timezone"`
		Enabled         *bool           `json:"enabled"`
		AllowOverlap    bool            `json:"allow_overlap"`
		MaxAttempts     *int            `json:"max_attempts"`
		TimeoutSeconds  *int            `json:"timeout_seconds"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.TaskType == "" || req.TaskVersion < 1 || !bytes.HasPrefix(bytes.TrimSpace(req.Payload), []byte("{")) {
		writeError(w, r, invalidRequest("task_type, task_version >= 1 and an object payload are required"))
		return
	}
	sc, created, err := s.jobs.PutSchedule(r.Context(), p, jobs.ScheduleInput{
		Name: name, TaskType: req.TaskType, TaskVersion: req.TaskVersion, Payload: req.Payload, Queue: req.Queue,
		IntervalSeconds: req.IntervalSeconds, Cron: req.Cron, Timezone: cmp.Or(req.Timezone, "UTC"),
		Enabled: req.Enabled == nil || *req.Enabled, AllowOverlap: req.AllowOverlap,
		MaxAttempts: req.MaxAttempts, TimeoutSeconds: req.TimeoutSeconds,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		w.Header().Set("Location", "/v1/schedules/"+name)
	}
	writeJSON(w, status, newScheduleView(sc))
}

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	items, err := s.jobs.ListSchedules(r.Context(), p)
	if err != nil {
		writeError(w, r, err)
		return
	}
	views := make([]scheduleView, len(items))
	for i, sc := range items {
		views[i] = newScheduleView(sc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	name, err := scheduleName(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	sc, err := s.jobs.GetSchedule(r.Context(), p, name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newScheduleView(sc))
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	name, err := scheduleName(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.jobs.DeleteSchedule(r.Context(), p, name); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
