package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/accounts"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
)

// appHeader names the application a dashboard session acts on when calling the job endpoints.
const appHeader = "X-TaskPilot-App"

type userHandler func(w http.ResponseWriter, r *http.Request, u accounts.User, token string)

// signedIn requires a dashboard session token (API keys are not accepted here).
func (s *Server) signedIn(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		u, err := s.accounts.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, r, err)
			return
		}
		h(w, r, u, token)
	}
}

// sessionPrincipal turns a session plus the X-TaskPilot-App header into a principal for that application.
func (s *Server) sessionPrincipal(r *http.Request, token string) (auth.Principal, error) {
	u, err := s.accounts.Authenticate(r.Context(), token)
	if err != nil {
		return auth.Principal{}, err
	}
	appID, err := uuid.Parse(r.Header.Get(appHeader))
	if err != nil {
		return auth.Principal{}, invalidRequest(appHeader + " header must be the application id")
	}
	return s.accounts.Principal(r.Context(), u, appID)
}

// requestLoginCode always answers 202 for a valid address, so it never reveals who is invited.
func (s *Server) requestLoginCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.accounts.RequestCode(r.Context(), req.Email); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "sent_if_invited"})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	session, err := s.accounts.VerifyCode(r.Context(), req.Email, strings.TrimSpace(req.Code))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request, _ accounts.User, token string) {
	respondNoContent(w, r, s.accounts.SignOut(r.Context(), token))
}

func (s *Server) listApps(w http.ResponseWriter, r *http.Request, u accounts.User, _ string) {
	apps, err := s.accounts.Apps(r.Context(), u)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"email": u.Email, "items": apps})
}

func (s *Server) createApp(w http.ResponseWriter, r *http.Request, u accounts.User, _ string) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	app, key, err := s.accounts.CreateApp(r.Context(), u, req.Name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"app": app, "api_key": key})
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request, u accounts.User, _ string) {
	appID, err := pathUUID(r, "app_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	keys, err := s.accounts.Keys(r.Context(), u, appID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request, u accounts.User, _ string) {
	appID, err := pathUUID(r, "app_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	key, err := s.accounts.CreateKey(r.Context(), u, appID, req.Name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, key)
}
