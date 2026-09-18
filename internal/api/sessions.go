package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"project-router/internal/projects"
	"project-router/internal/runtime"
	"project-router/internal/session"
)

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": s.sessions.List(r.URL.Query().Get("projectId")),
	})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req session.StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "ungültiger Request-Body: "+err.Error())
		return
	}
	view, err := s.sessions.Start(req)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

// handleDeleteSession beendet eine laufende Session und entfernt sie anschließend.
// Ohne `stop=true` wird eine laufende Session nicht angetastet.
func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if r.URL.Query().Get("stop") == "true" {
		if err := s.sessions.Stop(id); err != nil {
			writeSessionError(w, err)
			return
		}
	}
	if err := s.sessions.Remove(id); err != nil {
		writeSessionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleStopSession beendet eine Session, lässt sie aber in der Liste stehen.
func (s *Server) handleStopSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.sessions.Stop(id); err != nil {
		writeSessionError(w, err)
		return
	}
	view, err := s.sessions.GetView(id)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleRestartSession startet eine frische Session mit denselben Eckdaten. Damit
// endet man beim Öffnen einer beendeten Session nicht in einer Sackgasse.
func (s *Server) handleRestartSession(w http.ResponseWriter, r *http.Request) {
	view, err := s.sessions.Restart(chi.URLParam(r, "id"))
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, runtime.ErrUnknown):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, runtime.ErrUnavailable):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, projects.ErrNotFound):
		writeError(w, http.StatusNotFound, "Projekt nicht gefunden")
	case errors.Is(err, projects.ErrUnavailable):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, session.ErrNotFound):
		writeError(w, http.StatusNotFound, "Session nicht gefunden")
	case errors.Is(err, session.ErrStillRunning):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
