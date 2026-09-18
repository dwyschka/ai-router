package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"project-router/internal/pathsafe"
	"project-router/internal/projects"
)

func (s *Server) handleListProjects(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"projects": s.projects.List()})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req projects.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "ungültiger Request-Body: "+err.Error())
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "Pfad fehlt")
		return
	}
	view, err := s.projects.Create(req)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.projects.Remove(chi.URLParam(r, "id")); err != nil {
		writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeProjectError bildet die Registry-Fehler auf Statuscodes ab. Die Root-Prüfung
// (403) geht auch hier jeder Aussage über Existenz voraus.
func writeProjectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pathsafe.ErrOutsideRoots):
		writeError(w, http.StatusForbidden, "Pfad liegt außerhalb der erlaubten Roots")
	case errors.Is(err, projects.ErrNotFound):
		writeError(w, http.StatusNotFound, "Projekt nicht gefunden")
	case errors.Is(err, projects.ErrHasRunningSessions):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, projects.ErrUnavailable):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, projects.ErrNotGitRepo), errors.Is(err, projects.ErrNotDirectory):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
