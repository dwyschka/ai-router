package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"project-router/internal/runtime"
	"project-router/internal/session"
)

func TestRuntimeKatalogAbrufen(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodGet, "/api/runtimes", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d: %s", rec.Code, rec.Body)
	}
	body := decode[struct {
		Runtimes []runtime.Entry `json:"runtimes"`
	}](t, rec)

	seen := map[string]runtime.Entry{}
	for _, r := range body.Runtimes {
		seen[r.ID] = r
	}
	for _, id := range []string{"claude-code", "opencode"} {
		entry, ok := seen[id]
		if !ok {
			t.Fatalf("%s fehlt im Katalog: %+v", id, body.Runtimes)
		}
		if entry.DisplayName == "" || entry.Command == "" {
			t.Errorf("%s ohne Anzeigename oder Kommando: %+v", id, entry)
		}
	}
}

func TestRuntimeKatalogVerfuegbarkeit(t *testing.T) {
	e := newEnv(t)
	t.Setenv("PATH", filepath.Join(e.Base, "leerer-pfad"))
	body := decode[struct {
		Runtimes []runtime.Entry `json:"runtimes"`
	}](t, e.do(http.MethodGet, "/api/runtimes", nil))
	for _, r := range body.Runtimes {
		if r.Available {
			t.Errorf("%s sollte bei leerem PATH nicht verfügbar sein", r.ID)
		}
	}
}

func TestEigeneRuntimeAnlegenUndStarten(t *testing.T) {
	e := newEnv(t)
	// Ein Skript namens ollama im PATH, damit die Verfügbarkeitsprüfung greift.
	bin := filepath.Join(e.Base, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'ollama %s\\n' \"$*\"; sleep 0.3\n"
	if err := os.WriteFile(filepath.Join(bin, "ollama"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	rec := e.do(http.MethodPost, "/api/runtimes", map[string]any{
		"id":          "claude-ollama",
		"displayName": "Claude über Ollama",
		"commandLine": "ollama launch claude",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d: %s", rec.Code, rec.Body)
	}
	entry := decode[runtime.Entry](t, rec)
	if entry.Command != "ollama" || !entry.Available || entry.Source != runtime.SourceCustom {
		t.Fatalf("Eintrag = %+v", entry)
	}

	// Sie steht im Katalog …
	katalog := decode[struct {
		Runtimes []runtime.Entry `json:"runtimes"`
	}](t, e.do(http.MethodGet, "/api/runtimes", nil))
	var gefunden *runtime.Entry
	for i := range katalog.Runtimes {
		if katalog.Runtimes[i].ID == "claude-ollama" {
			gefunden = &katalog.Runtimes[i]
		}
	}
	if gefunden == nil {
		t.Fatalf("Runtime fehlt im Katalog: %+v", katalog.Runtimes)
	}
	if gefunden.CommandLine != "ollama launch claude" {
		t.Errorf("CommandLine = %q", gefunden.CommandLine)
	}

	// … und eine Session damit startet den Prozess mit dem zerlegten Argumentvektor.
	p := e.neuesProjekt("alpha")
	start := e.do(http.MethodPost, "/api/sessions", map[string]any{
		"projectId": p.ID, "runtimeId": "claude-ollama", "cols": 80, "rows": 24,
	})
	if start.Code != http.StatusCreated {
		t.Fatalf("Session-Start = %d: %s", start.Code, start.Body)
	}
	sess, ok := e.Sessions.Get(decode[session.View](t, start).ID)
	if !ok {
		t.Fatal("Session nicht im Speicher")
	}
	sub := sess.Attach(session.Size{Cols: 80, Rows: 24})
	ausgabe := string(sub.Snapshot)
	for f := range sub.Frames {
		ausgabe += string(f.Data)
		if strings.Contains(ausgabe, "ollama launch claude") {
			break
		}
	}
	if !strings.Contains(ausgabe, "ollama launch claude") {
		t.Errorf("Ausgabe = %q, erwartet die zerlegten Argumente", ausgabe)
	}
}

func TestEigeneRuntimeUngueltig(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, "/api/runtimes", map[string]any{"id": "leer", "commandLine": "  "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Status = %d, erwartet 400: %s", rec.Code, rec.Body)
	}
}

func TestEigeneRuntimeEntfernen(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(http.MethodPost, "/api/runtimes", map[string]any{
		"id": "eigen", "commandLine": "echo hallo",
	}); rec.Code != http.StatusOK {
		t.Fatalf("Anlegen = %d: %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodDelete, "/api/runtimes/eigen", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Entfernen = %d", rec.Code)
	}
	if rec := e.do(http.MethodDelete, "/api/runtimes/eigen", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("zweites Entfernen = %d, erwartet 404", rec.Code)
	}
}

func TestAusgelieferteRuntimeNichtEntfernbarUeberAPI(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodDelete, "/api/runtimes/claude-code", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("Status = %d, erwartet 409: %s", rec.Code, rec.Body)
	}
}

func TestUeberschreibenUndZuruecksetzen(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(http.MethodPost, "/api/runtimes", map[string]any{
		"id": "claude-code", "displayName": "Claude via Ollama", "commandLine": "ollama launch claude",
	}); rec.Code != http.StatusOK {
		t.Fatalf("Überschreiben = %d: %s", rec.Code, rec.Body)
	}
	katalog := func() runtime.Entry {
		t.Helper()
		body := decode[struct {
			Runtimes []runtime.Entry `json:"runtimes"`
		}](t, e.do(http.MethodGet, "/api/runtimes", nil))
		for _, r := range body.Runtimes {
			if r.ID == "claude-code" {
				return r
			}
		}
		t.Fatal("claude-code fehlt im Katalog")
		return runtime.Entry{}
	}
	if got := katalog(); got.Command != "ollama" || got.Source != runtime.SourceCustom {
		t.Fatalf("nach dem Überschreiben: %+v", got)
	}
	if rec := e.do(http.MethodDelete, "/api/runtimes/claude-code", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Zurücksetzen = %d", rec.Code)
	}
	if got := katalog(); got.Command != "claude" || got.Source != runtime.SourceBuiltin {
		t.Fatalf("nach dem Zurücksetzen: %+v", got)
	}
}

func TestOverridesStaticWirdGemeldet(t *testing.T) {
	e := newEnv(t)
	eigen := decode[runtime.Entry](t, e.do(http.MethodPost, "/api/runtimes", map[string]any{
		"id": "nur-eigen", "commandLine": "echo",
	}))
	if eigen.OverridesStatic {
		t.Error("eine rein eigene Runtime überschreibt nichts")
	}
	ueber := decode[runtime.Entry](t, e.do(http.MethodPost, "/api/runtimes", map[string]any{
		"id": "claude-code", "commandLine": "ollama launch claude",
	}))
	if !ueber.OverridesStatic {
		t.Error("claude-code überschreibt die ausgelieferte Definition")
	}
}
