// Package api verdrahtet die HTTP- und WebSocket-Endpunkte des Routers.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"project-router/internal/auth"
	"project-router/internal/config"
	"project-router/internal/fsbrowse"
	"project-router/internal/pathsafe"
	"project-router/internal/projects"
	"project-router/internal/runtime"
	"project-router/internal/session"
	"project-router/internal/store"
)

// Server bündelt die Abhängigkeiten der Handler.
type Server struct {
	cfg      config.Config
	auth     *auth.Authenticator
	browser  *fsbrowse.Browser
	store    store.Store
	projects *projects.Registry
	runtimes *runtime.Catalog
	runtimeR *runtime.Registry
	sessions *session.Manager
}

// Options fasst die von main injizierten Bausteine zusammen.
type Options struct {
	Config   config.Config
	Auth     *auth.Authenticator
	Store    store.Store
	Runtimes *runtime.Catalog
	Sessions *session.Manager
}

func NewServer(opts Options) *Server {
	s := &Server{
		cfg:      opts.Config,
		auth:     opts.Auth,
		browser:  fsbrowse.New(opts.Config.Roots),
		store:    opts.Store,
		runtimes: opts.Runtimes,
		sessions: opts.Sessions,
	}
	if opts.Store != nil {
		s.projects = projects.New(opts.Store, opts.Config.Roots)
		if opts.Runtimes != nil {
			s.runtimeR = runtime.NewRegistry(opts.Store, opts.Runtimes)
		}
	}
	return s
}

// Routes liefert den API-Router. Die Auth-Middleware sitzt davor, sodass kein Handler
// ohne gültiges Token läuft.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.auth.Middleware)
	r.Get("/fs", s.handleFS)
	r.Get("/projects", s.handleListProjects)
	r.Post("/projects", s.handleCreateProject)
	r.Delete("/projects/{id}", s.handleDeleteProject)
	r.Get("/runtimes", s.handleListRuntimes)
	r.Post("/runtimes", s.handleSaveRuntime)
	r.Delete("/runtimes/{id}", s.handleDeleteRuntime)
	r.Get("/sessions", s.handleListSessions)
	r.Post("/sessions", s.handleCreateSession)
	r.Post("/sessions/{id}/stop", s.handleStopSession)
	r.Post("/sessions/{id}/restart", s.handleRestartSession)
	r.Delete("/sessions/{id}", s.handleDeleteSession)
	r.Get("/sessions/{id}/attach", s.handleAttach)
	return r
}

func (s *Server) handleListRuntimes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"runtimes": s.runtimes.List()})
}

// handleSaveRuntime hinterlegt eine eigene Runtime. Das Kommando kommt als ganze
// Zeile und wird ohne Shell in einen Argumentvektor zerlegt.
func (s *Server) handleSaveRuntime(w http.ResponseWriter, r *http.Request) {
	var req runtime.SaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "ungültiger Request-Body: "+err.Error())
		return
	}
	entry, err := s.runtimeR.Save(req)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleDeleteRuntime(w http.ResponseWriter, r *http.Request) {
	if err := s.runtimeR.Remove(chi.URLParam(r, "id")); err != nil {
		writeRuntimeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeRuntimeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, runtime.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, runtime.ErrNotCustom):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, runtime.ErrUnknown):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) handleFS(w http.ResponseWriter, r *http.Request) {
	listing, err := s.browser.List(r.URL.Query().Get("path"))
	if err != nil {
		writeFSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

// writeFSError bildet die Fehler des Browsers auf Statuscodes ab. Die Reihenfolge ist
// Teil der Spec: erst 403 (außerhalb der Roots), dann 404 (existiert nicht).
func writeFSError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pathsafe.ErrOutsideRoots):
		writeError(w, http.StatusForbidden, "Pfad liegt außerhalb der erlaubten Roots")
	case errors.Is(err, fsbrowse.ErrNotFound):
		writeError(w, http.StatusNotFound, "Verzeichnis existiert nicht")
	case errors.Is(err, fsbrowse.ErrNotDirectory):
		writeError(w, http.StatusBadRequest, "Pfad ist kein Verzeichnis")
	case errors.Is(err, fsbrowse.ErrPermission):
		writeError(w, http.StatusForbidden, "keine Leseberechtigung für das Verzeichnis")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}
