package httpapi

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/jobs"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/tasktypes"
)

var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func validQueues(queues []string) bool {
	return len(queues) >= 1 && len(queues) <= 50 && !slices.ContainsFunc(queues, func(q string) bool { return !tasktypes.ValidQueue(q) })
}

// registerWorker stores the worker's task type definitions (making them submittable) and its presence.
func (s *Server) registerWorker(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkerID    string                 `json:"worker_id"`
		Hostname    string                 `json:"hostname"`
		Version     string                 `json:"version"`
		Concurrency int                    `json:"concurrency"`
		Queues      []string               `json:"queues"`
		TaskTypes   []tasktypes.Definition `json:"task_types"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if len(req.WorkerID) < 1 || len(req.WorkerID) > 128 || len(req.Hostname) > 255 || len(req.Version) > 64 ||
		req.Concurrency < 1 || req.Concurrency > 1024 || !validQueues(req.Queues) || len(req.TaskTypes) < 1 || len(req.TaskTypes) > 500 {
		writeError(w, r, invalidRequest("worker_id, concurrency 1-1024, 1-50 valid queues, and 1-500 task types are required"))
		return
	}
	if err := s.types.Register(r.Context(), req.WorkerID, req.TaskTypes); err != nil {
		writeError(w, r, err)
		return
	}
	keys := make([]tasktypes.Key, len(req.TaskTypes))
	for i, d := range req.TaskTypes {
		keys[i] = d.Key
	}
	if err := s.jobs.RegisterWorker(r.Context(), jobs.Worker{
		WorkerID: req.WorkerID, Hostname: req.Hostname, Version: req.Version,
		Concurrency: req.Concurrency, Queues: req.Queues, TaskTypes: keys,
	}); err != nil {
		writeError(w, r, err)
		return
	}
	s.log.InfoContext(r.Context(), "worker registered", "worker_id", req.WorkerID, "queues", req.Queues, "task_types", len(keys))
	writeJSON(w, http.StatusOK, map[string]int{"registered": len(keys)})
}

func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkerID  string          `json:"worker_id"`
		Queues    []string        `json:"queues"`
		Supported []tasktypes.Key `json:"supported"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if len(req.WorkerID) < 1 || len(req.WorkerID) > 128 || !validQueues(req.Queues) || len(req.Supported) < 1 || len(req.Supported) > 500 {
		writeError(w, r, invalidRequest("worker_id (1-128 chars), 1-50 valid queues, and 1-500 supported task types are required"))
		return
	}
	claim, err := s.jobs.Claim(r.Context(), req.WorkerID, req.Queues, req.Supported)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if claim == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, claim)
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "attempt_id")
	if err == nil {
		err = decodeJSON(w, r, &struct{}{})
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	lease, err := s.jobs.Heartbeat(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, lease)
}

func (s *Server) complete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Result json.RawMessage `json:"result"`
	}
	id, err := pathUUID(r, "attempt_id")
	if err == nil {
		err = decodeJSON(w, r, &req)
	}
	if err == nil {
		err = s.jobs.Complete(r.Context(), id, req.Result)
	}
	respondNoContent(w, r, err)
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable *bool  `json:"retryable"`
		} `json:"error"`
	}
	id, err := pathUUID(r, "attempt_id")
	if err == nil {
		err = decodeJSON(w, r, &req)
	}
	if err == nil && (!errorCodePattern.MatchString(req.Error.Code) || req.Error.Retryable == nil) {
		err = invalidRequest("error.code must match ^[a-z][a-z0-9_]{0,63}$ and error.retryable is required")
	}
	if err == nil {
		err = s.jobs.Fail(r.Context(), id, jobs.AttemptError{
			Code: req.Error.Code, Message: req.Error.Message, Retryable: *req.Error.Retryable,
		})
	}
	respondNoContent(w, r, err)
}

func (s *Server) cancelled(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "attempt_id")
	if err == nil {
		err = decodeJSON(w, r, &struct{}{})
	}
	if err == nil {
		err = s.jobs.ConfirmCancelled(r.Context(), id)
	}
	respondNoContent(w, r, err)
}

func respondNoContent(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
