package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"project-router/internal/projects"
	"project-router/internal/store"
)

func TestProjectsAnlegenUndAuflisten(t *testing.T) {
	e := newEnv(t)
	dir := e.repo("alpha")

	rec := e.do(http.MethodPost, "/api/projects", map[string]any{"path": dir})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Status = %d: %s", rec.Code, rec.Body)
	}
	created := decode[projects.View](t, rec)
	if created.Name != "alpha" || created.Path != dir || !created.Available {
		t.Fatalf("Projekt = %+v", created)
	}

	list := decode[struct {
		Projects []projects.View `json:"projects"`
	}](t, e.do(http.MethodGet, "/api/projects", nil))
	if len(list.Projects) != 1 || list.Projects[0].ID != created.ID {
		t.Fatalf("Liste = %+v", list.Projects)
	}
}

func TestProjectsKeinRepository(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.Root, "ohne-git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := e.do(http.MethodPost, "/api/projects", map[string]any{"path": dir})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Status = %d, erwartet 400: %s", rec.Code, rec.Body)
	}
	if len(e.Store.Snapshot().Projects) != 0 {
		t.Error("Projekt wurde trotz Fehler gespeichert")
	}
}

func TestProjectsAusserhalbDerRoots(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, "/api/projects", map[string]any{"path": "/etc/neu", "init": true})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Status = %d, erwartet 403: %s", rec.Code, rec.Body)
	}
}

func TestProjectsDoppelterPfad(t *testing.T) {
	e := newEnv(t)
	dir := e.repo("alpha")
	erst := decode[projects.View](t, e.do(http.MethodPost, "/api/projects", map[string]any{"path": dir}))
	rec := e.do(http.MethodPost, "/api/projects", map[string]any{"path": dir})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Status = %d: %s", rec.Code, rec.Body)
	}
	if decode[projects.View](t, rec).ID != erst.ID {
		t.Error("zweiter Aufruf lieferte ein anderes Projekt")
	}
	if len(e.Store.Snapshot().Projects) != 1 {
		t.Error("Duplikat angelegt")
	}
}

func TestProjectsInitialisierung(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git nicht im PATH")
	}
	e := newEnv(t)
	dir := filepath.Join(e.Root, "frisch")
	rec := e.do(http.MethodPost, "/api/projects", map[string]any{"path": dir, "init": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Status = %d: %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf(".git fehlt: %v", err)
	}
}

func TestProjectsEntfernen(t *testing.T) {
	e := newEnv(t)
	dir := e.repo("alpha")
	created := decode[projects.View](t, e.do(http.MethodPost, "/api/projects", map[string]any{"path": dir}))

	rec := e.do(http.MethodDelete, "/api/projects/"+created.ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Status = %d: %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("Repository auf der Platte wurde angetastet: %v", err)
	}
	if rec := e.do(http.MethodDelete, "/api/projects/"+created.ID, nil); rec.Code != http.StatusNotFound {
		t.Errorf("zweites Entfernen = %d, erwartet 404", rec.Code)
	}
}

func TestProjectsEntfernenMitLaufenderSession(t *testing.T) {
	e := newEnv(t)
	created := decode[projects.View](t, e.do(http.MethodPost, "/api/projects", map[string]any{"path": e.repo("alpha")}))
	if err := e.Store.Update(func(st *store.State) error {
		st.Sessions = append(st.Sessions, store.Session{
			ID: "s1", ProjectID: created.ID, Status: store.StatusRunning, StartedAt: time.Now(),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := e.do(http.MethodDelete, "/api/projects/"+created.ID, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("Status = %d, erwartet 409: %s", rec.Code, rec.Body)
	}
}

func TestProjectsVerschwundenerPfad(t *testing.T) {
	e := newEnv(t)
	dir := e.repo("alpha")
	created := decode[projects.View](t, e.do(http.MethodPost, "/api/projects", map[string]any{"path": dir}))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	list := decode[struct {
		Projects []projects.View `json:"projects"`
	}](t, e.do(http.MethodGet, "/api/projects", nil))
	if len(list.Projects) != 1 || list.Projects[0].Available {
		t.Fatalf("Projekt sollte gelistet, aber nicht verfügbar sein: %+v", list.Projects)
	}
	_ = created
}
