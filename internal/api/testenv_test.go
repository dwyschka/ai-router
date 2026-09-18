package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"project-router/internal/auth"
	"project-router/internal/config"
	"project-router/internal/projects"
	"project-router/internal/runtime"
	"project-router/internal/session"
	"project-router/internal/store"
)

// env ist die gemeinsame Testumgebung der Handler-Tests: ein temporäres
// Verzeichnisgerüst, ein Store und ein Server ohne Token.
type env struct {
	t        *testing.T
	Server   *Server
	Store    *store.FileStore
	Base     string
	Root     string
	Sessions *session.Manager
}

func newEnv(t *testing.T) *env {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "projects")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Roots = []string{root}
	cfg.StateDir = st.StateDir()
	cat, err := runtime.NewCatalog(cfg.Runtimes)
	if err != nil {
		t.Fatal(err)
	}
	reg := projects.New(st, cfg.Roots)
	mgr := session.NewManager(st, reg, cat, cfg.BufferBytes)
	mgr.SetGrace(300 * time.Millisecond)
	srv := NewServer(Options{Config: cfg, Auth: auth.New("", ""), Store: st, Runtimes: cat, Sessions: mgr})
	t.Cleanup(mgr.StopAll)
	return &env{t: t, Server: srv, Store: st, Base: base, Root: root, Sessions: mgr}
}

// repo legt ein Verzeichnis mit .git unterhalb des Roots an.
func (e *env) repo(name string) string {
	e.t.Helper()
	dir := filepath.Join(e.Root, name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		e.t.Fatal(err)
	}
	return dir
}

// do schickt einen Request an den API-Router.
func (e *env) do(method, target string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	http.StripPrefix("/api", e.Server.Routes()).ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("Antwort nicht lesbar (%s): %v", rec.Body, err)
	}
	return out
}
