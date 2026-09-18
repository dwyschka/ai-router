package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"project-router/internal/config"
	"project-router/internal/projects"
	"project-router/internal/runtime"
	"project-router/internal/session"
	"project-router/internal/store"
)

// dummyRuntime hinterlegt eine Runtime, die ein präpariertes Skript startet.
func (e *env) dummyRuntime(t *testing.T, id, script string) {
	t.Helper()
	bin := filepath.Join(e.Base, "bin-"+id)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, id), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := config.Defaults()
	cfg.Roots = e.Server.cfg.Roots
	cfg.StateDir = e.Store.StateDir()
	cfg.Runtimes = []config.RuntimeOverride{{ID: id, DisplayName: id, Command: id}}
	cat, err := runtime.NewCatalog(cfg.Runtimes)
	if err != nil {
		t.Fatal(err)
	}
	reg := projects.New(e.Store, cfg.Roots)
	mgr := session.NewManager(e.Store, reg, cat, cfg.BufferBytes)
	mgr.SetGrace(300 * time.Millisecond)
	e.Sessions = mgr
	e.Server.runtimes = cat
	e.Server.sessions = mgr
	t.Cleanup(mgr.StopAll)
}

// neuesProjekt registriert ein Repository und liefert dessen Kennung.
func (e *env) neuesProjekt(name string) projects.View {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/projects", map[string]any{"path": e.repo(name)})
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("Projekt anlegen: Status %d: %s", rec.Code, rec.Body)
	}
	return decode[projects.View](e.t, rec)
}

func TestSessionStartUndListe(t *testing.T) {
	e := newEnv(t)
	e.dummyRuntime(t, "dummy", "sleep 2")
	p := e.neuesProjekt("alpha")

	rec := e.do(http.MethodPost, "/api/sessions", map[string]any{
		"projectId": p.ID, "runtimeId": "dummy", "cols": 100, "rows": 30,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Status = %d: %s", rec.Code, rec.Body)
	}
	created := decode[session.View](t, rec)
	if created.Status != store.StatusRunning || created.ProjectID != p.ID {
		t.Fatalf("Session = %+v", created)
	}

	list := decode[struct {
		Sessions []session.View `json:"sessions"`
	}](t, e.do(http.MethodGet, "/api/sessions", nil))
	if len(list.Sessions) != 1 || list.Sessions[0].ID != created.ID {
		t.Fatalf("Liste = %+v", list.Sessions)
	}
	if list.Sessions[0].ProjectName != "alpha" {
		t.Errorf("Projektname = %q", list.Sessions[0].ProjectName)
	}

	// Das Projekt weist die laufende Session aus.
	pl := decode[struct {
		Projects []projects.View `json:"projects"`
	}](t, e.do(http.MethodGet, "/api/projects", nil))
	if pl.Projects[0].RunningSessions != 1 {
		t.Errorf("laufende Sessions am Projekt = %d", pl.Projects[0].RunningSessions)
	}
}

func TestSessionFilterNachProjekt(t *testing.T) {
	e := newEnv(t)
	e.dummyRuntime(t, "dummy", "sleep 2")
	a := e.neuesProjekt("alpha")
	b := e.neuesProjekt("beta")

	for _, p := range []projects.View{a, a, b} {
		if rec := e.do(http.MethodPost, "/api/sessions", map[string]any{"projectId": p.ID, "runtimeId": "dummy"}); rec.Code != http.StatusCreated {
			t.Fatalf("Start: %d %s", rec.Code, rec.Body)
		}
	}
	list := decode[struct {
		Sessions []session.View `json:"sessions"`
	}](t, e.do(http.MethodGet, "/api/sessions?projectId="+a.ID, nil))
	if len(list.Sessions) != 2 {
		t.Fatalf("gefilterte Liste = %+v, erwartet zwei Sessions", list.Sessions)
	}
	alle := decode[struct {
		Sessions []session.View `json:"sessions"`
	}](t, e.do(http.MethodGet, "/api/sessions", nil))
	if len(alle.Sessions) != 3 {
		t.Fatalf("Gesamtliste = %+v", alle.Sessions)
	}
}

func TestSessionUnbekannteRuntime(t *testing.T) {
	e := newEnv(t)
	p := e.neuesProjekt("alpha")
	rec := e.do(http.MethodPost, "/api/sessions", map[string]any{"projectId": p.ID, "runtimeId": "gibtesnicht"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Status = %d, erwartet 400: %s", rec.Code, rec.Body)
	}
	if len(e.Store.Snapshot().Sessions) != 0 {
		t.Error("es entstand trotzdem eine Session")
	}
}

func TestSessionNichtVerfuegbareRuntime(t *testing.T) {
	e := newEnv(t)
	p := e.neuesProjekt("alpha")
	t.Setenv("PATH", filepath.Join(e.Base, "leerer-pfad"))

	rec := e.do(http.MethodPost, "/api/sessions", map[string]any{"projectId": p.ID, "runtimeId": "claude-code"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Status = %d, erwartet 400: %s", rec.Code, rec.Body)
	}
	body := decode[errorBody](t, rec)
	if body.Error == "" {
		t.Error("Fehler benennt das fehlende Kommando nicht")
	}
	if len(e.Store.Snapshot().Sessions) != 0 {
		t.Error("es entstand ein Prozess trotz nicht verfügbarer Runtime")
	}
}

func TestSessionProjektNichtVerfuegbar(t *testing.T) {
	e := newEnv(t)
	e.dummyRuntime(t, "dummy", "sleep 1")
	p := e.neuesProjekt("alpha")
	if err := os.RemoveAll(p.Path); err != nil {
		t.Fatal(err)
	}
	rec := e.do(http.MethodPost, "/api/sessions", map[string]any{"projectId": p.ID, "runtimeId": "dummy"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("Status = %d, erwartet 409: %s", rec.Code, rec.Body)
	}
	if len(e.Store.Snapshot().Sessions) != 0 {
		t.Error("es entstand ein Prozess für ein nicht verfügbares Projekt")
	}
}

func TestSessionBeendenUndEntfernen(t *testing.T) {
	e := newEnv(t)
	e.dummyRuntime(t, "dummy", "sleep 30")
	p := e.neuesProjekt("alpha")
	created := decode[session.View](t, e.do(http.MethodPost, "/api/sessions", map[string]any{
		"projectId": p.ID, "runtimeId": "dummy",
	}))

	// Laufende Session lässt sich nicht einfach entfernen.
	if rec := e.do(http.MethodDelete, "/api/sessions/"+created.ID, nil); rec.Code != http.StatusConflict {
		t.Fatalf("Entfernen einer laufenden Session = %d, erwartet 409", rec.Code)
	}

	stopped := decode[session.View](t, e.do(http.MethodPost, "/api/sessions/"+created.ID+"/stop", nil))
	if stopped.Status != store.StatusExited {
		t.Fatalf("Status nach Stop = %q", stopped.Status)
	}

	// Beendete Session bleibt sichtbar, bis sie ausdrücklich entfernt wird.
	list := decode[struct {
		Sessions []session.View `json:"sessions"`
	}](t, e.do(http.MethodGet, "/api/sessions", nil))
	if len(list.Sessions) != 1 || list.Sessions[0].Status != store.StatusExited {
		t.Fatalf("Liste = %+v, erwartet die beendete Session", list.Sessions)
	}
	if list.Sessions[0].EndedAt == nil || list.Sessions[0].ExitCode == nil {
		t.Errorf("Endzeit oder Exit-Code fehlen: %+v", list.Sessions[0])
	}

	if rec := e.do(http.MethodDelete, "/api/sessions/"+created.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Entfernen = %d", rec.Code)
	}
	nach := decode[struct {
		Sessions []session.View `json:"sessions"`
	}](t, e.do(http.MethodGet, "/api/sessions", nil))
	if len(nach.Sessions) != 0 {
		t.Errorf("Session ist noch gelistet: %+v", nach.Sessions)
	}
}

func TestEntfernenGibtRingpufferFrei(t *testing.T) {
	e := newEnv(t)
	e.dummyRuntime(t, "dummy", "printf 'ausgabe\\n'; exit 0")
	p := e.neuesProjekt("alpha")
	created := decode[session.View](t, e.do(http.MethodPost, "/api/sessions", map[string]any{
		"projectId": p.ID, "runtimeId": "dummy",
	}))
	sess, ok := e.Sessions.Get(created.ID)
	if !ok {
		t.Fatal("Session nicht im Speicher")
	}
	sess.Wait()

	if rec := e.do(http.MethodDelete, "/api/sessions/"+created.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Entfernen = %d", rec.Code)
	}
	if _, ok := e.Sessions.Get(created.ID); ok {
		t.Error("Session ist noch im Speicher")
	}
	sub := sess.Attach(session.Size{Cols: 80, Rows: 24})
	if len(sub.Snapshot) != 0 {
		t.Errorf("Ringpuffer wurde nicht freigegeben: %q", sub.Snapshot)
	}
}

func TestSessionsUeberlebenNeustartAlsExited(t *testing.T) {
	e := newEnv(t)
	e.dummyRuntime(t, "dummy", "sleep 30")
	p := e.neuesProjekt("alpha")
	created := decode[session.View](t, e.do(http.MethodPost, "/api/sessions", map[string]any{
		"projectId": p.ID, "runtimeId": "dummy",
	}))

	// Neustart des Routers: derselbe State-Ordner wird erneut geladen.
	wieder, err := store.Open(e.Store.StateDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, meta := range wieder.Snapshot().Sessions {
		if meta.ID == created.ID && meta.Status != store.StatusExited {
			t.Fatalf("Status nach Neustart = %q, erwartet exited", meta.Status)
		}
		if meta.ID == created.ID && meta.LogPath == "" {
			t.Error("Logpfad fehlt nach dem Neustart")
		}
	}
}

func TestSessionNeustartLiefertFrischeSession(t *testing.T) {
	e := newEnv(t)
	e.dummyRuntime(t, "dummy", "printf 'lauf %s\\n' \"$1\"; sleep 0.2")
	p := e.neuesProjekt("alpha")

	erste := decode[session.View](t, e.do(http.MethodPost, "/api/sessions", map[string]any{
		"projectId": p.ID, "runtimeId": "dummy", "args": []string{"eins"}, "cols": 90, "rows": 40,
	}))
	sess, ok := e.Sessions.Get(erste.ID)
	if !ok {
		t.Fatal("Session nicht im Speicher")
	}
	sess.Wait()

	rec := e.do(http.MethodPost, "/api/sessions/"+erste.ID+"/restart", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Status = %d, erwartet 201: %s", rec.Code, rec.Body)
	}
	neu := decode[session.View](t, rec)

	if neu.ID == erste.ID {
		t.Error("Neustart sollte eine eigene Kennung bekommen")
	}
	if neu.Status != store.StatusRunning {
		t.Errorf("Status = %q, erwartet running", neu.Status)
	}
	if neu.ProjectID != erste.ProjectID || neu.RuntimeID != erste.RuntimeID {
		t.Errorf("Eckdaten weichen ab: %+v vs %+v", neu, erste)
	}
	if len(neu.Args) != 1 || neu.Args[0] != "eins" {
		t.Errorf("Argumente = %v, erwartet [eins]", neu.Args)
	}

	// Der Scrollback der neuen Session beginnt leer und füllt sich frisch.
	neueSess, ok := e.Sessions.Get(neu.ID)
	if !ok {
		t.Fatal("neue Session nicht im Speicher")
	}
	sub := neueSess.Attach(session.Size{Cols: 90, Rows: 40})
	ausgabe := string(sub.Snapshot)
	for f := range sub.Frames {
		ausgabe += string(f.Data)
	}
	if !strings.Contains(ausgabe, "lauf eins") {
		t.Errorf("Ausgabe = %q, erwartet den Lauf mit denselben Argumenten", ausgabe)
	}
	// Frischer Scrollback: der Verlauf der alten Session steckt nicht darin.
	if n := strings.Count(ausgabe, "lauf eins"); n != 1 {
		t.Errorf("Ausgabe enthält den Lauf %d-mal, erwartet genau einmal: %q", n, ausgabe)
	}
	if sub.Truncated {
		t.Error("eine frische Session hat keinen gekürzten Verlauf")
	}

	// Die alte Session bleibt mit ihrem Verlauf in der Liste stehen.
	liste := decode[struct {
		Sessions []session.View `json:"sessions"`
	}](t, e.do(http.MethodGet, "/api/sessions", nil))
	if len(liste.Sessions) != 2 {
		t.Fatalf("Liste = %+v, erwartet alte und neue Session", liste.Sessions)
	}
}

func TestNeustartEinerLaufendenSessionAbgelehnt(t *testing.T) {
	e := newEnv(t)
	e.dummyRuntime(t, "dummy", "sleep 5")
	p := e.neuesProjekt("alpha")
	laufend := decode[session.View](t, e.do(http.MethodPost, "/api/sessions", map[string]any{
		"projectId": p.ID, "runtimeId": "dummy",
	}))
	rec := e.do(http.MethodPost, "/api/sessions/"+laufend.ID+"/restart", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("Status = %d, erwartet 409: %s", rec.Code, rec.Body)
	}
}

func TestNeustartUnbekannterSession(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(http.MethodPost, "/api/sessions/gibtesnicht/restart", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("Status = %d, erwartet 404", rec.Code)
	}
}

func TestNeustartNachRouterNeustart(t *testing.T) {
	e := newEnv(t)
	e.dummyRuntime(t, "dummy", "sleep 5")
	p := e.neuesProjekt("alpha")
	alt := decode[session.View](t, e.do(http.MethodPost, "/api/sessions", map[string]any{
		"projectId": p.ID, "runtimeId": "dummy", "cols": 80, "rows": 24,
	}))

	// Router-Neustart: der Prozess ist weg, die Metadaten stehen auf exited.
	e.Sessions.StopAll()
	if err := e.Store.Update(func(st *store.State) error {
		for i := range st.Sessions {
			if st.Sessions[i].ID == alt.ID {
				st.Sessions[i].Status = store.StatusExited
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rec := e.do(http.MethodPost, "/api/sessions/"+alt.ID+"/restart", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Status = %d, erwartet 201: %s", rec.Code, rec.Body)
	}
	if decode[session.View](t, rec).Status != store.StatusRunning {
		t.Error("die neue Session sollte laufen")
	}
}

func TestNeustartMitVerschwundenemProjekt(t *testing.T) {
	e := newEnv(t)
	e.dummyRuntime(t, "dummy", "exit 0")
	p := e.neuesProjekt("alpha")
	alt := decode[session.View](t, e.do(http.MethodPost, "/api/sessions", map[string]any{
		"projectId": p.ID, "runtimeId": "dummy",
	}))
	if sess, ok := e.Sessions.Get(alt.ID); ok {
		sess.Wait()
	}
	if err := os.RemoveAll(p.Path); err != nil {
		t.Fatal(err)
	}

	rec := e.do(http.MethodPost, "/api/sessions/"+alt.ID+"/restart", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("Status = %d, erwartet 409: %s", rec.Code, rec.Body)
	}
}
