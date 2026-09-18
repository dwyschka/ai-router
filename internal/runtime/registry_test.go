package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"project-router/internal/config"
	"project-router/internal/store"
)

func registry(t *testing.T, overrides []config.RuntimeOverride) (*Registry, *Catalog, *store.FileStore) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cat, err := NewCatalog(overrides)
	if err != nil {
		t.Fatal(err)
	}
	return NewRegistry(st, cat), cat, st
}

func TestEigeneRuntimeHinterlegen(t *testing.T) {
	bin := fakeBin(t, "ollama", "echo hallo")
	t.Setenv("PATH", bin)
	reg, cat, _ := registry(t, nil)

	entry, err := reg.Save(SaveRequest{
		DisplayName: "Claude über Ollama",
		ID:          "claude-ollama",
		CommandLine: "ollama launch claude",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if entry.Command != "ollama" {
		t.Errorf("Kommando = %q, erwartet ollama", entry.Command)
	}
	if len(entry.DefaultArgs) != 2 || entry.DefaultArgs[0] != "launch" || entry.DefaultArgs[1] != "claude" {
		t.Errorf("Standardargumente = %v, erwartet [launch claude]", entry.DefaultArgs)
	}
	if !entry.Available {
		t.Error("Runtime sollte verfügbar sein, ollama liegt im PATH")
	}
	if entry.Source != SourceCustom {
		t.Errorf("Source = %q, erwartet custom", entry.Source)
	}
	if entry.CommandLine != "ollama launch claude" {
		t.Errorf("CommandLine = %q", entry.CommandLine)
	}

	// Sie taucht sofort im Katalog auf und ist startbar.
	if _, err := cat.Resolve("claude-ollama"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	gefunden := false
	for _, e := range cat.List() {
		if e.ID == "claude-ollama" {
			gefunden = true
		}
	}
	if !gefunden {
		t.Error("Runtime fehlt im Katalog")
	}
}

func TestHinterlegteRuntimeUeberlebtNeustart(t *testing.T) {
	reg, _, st := registry(t, nil)
	if _, err := reg.Save(SaveRequest{ID: "eigen", DisplayName: "Eigen", CommandLine: "echo hallo"}); err != nil {
		t.Fatal(err)
	}

	wieder, err := store.Open(st.StateDir())
	if err != nil {
		t.Fatal(err)
	}
	cat, err := NewCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	NewRegistry(wieder, cat)
	entry, err := cat.Get("eigen")
	if err != nil {
		t.Fatalf("nach Neustart: %v", err)
	}
	if entry.Command != "echo" || len(entry.DefaultArgs) != 1 {
		t.Errorf("Eintrag nach Neustart = %+v", entry)
	}
}

func TestHinterlegteRuntimeUeberschreibtAusgelieferte(t *testing.T) {
	reg, cat, _ := registry(t, nil)
	if _, err := reg.Save(SaveRequest{ID: "claude-code", DisplayName: "Claude via Ollama", CommandLine: "ollama launch claude"}); err != nil {
		t.Fatal(err)
	}
	entry, err := cat.Get("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Command != "ollama" || entry.Source != SourceCustom {
		t.Fatalf("Eintrag = %+v, erwartet die hinterlegte Definition", entry)
	}

	// Nach dem Entfernen lebt die ausgelieferte Definition wieder auf.
	if err := reg.Remove("claude-code"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	entry, err = cat.Get("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Command != "claude" || entry.Source != SourceBuiltin {
		t.Errorf("Eintrag = %+v, erwartet die ausgelieferte Definition", entry)
	}
}

func TestAusgelieferteRuntimeNichtEntfernbar(t *testing.T) {
	reg, _, _ := registry(t, nil)
	err := reg.Remove("claude-code")
	if !errors.Is(err, ErrNotCustom) {
		t.Fatalf("Fehler = %v, erwartet ErrNotCustom", err)
	}
}

func TestUnbekannteRuntimeEntfernen(t *testing.T) {
	reg, _, _ := registry(t, nil)
	if err := reg.Remove("gibtesnicht"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Fehler = %v, erwartet ErrUnknown", err)
	}
}

func TestUngueltigeDefinitionen(t *testing.T) {
	reg, _, _ := registry(t, nil)
	tests := []struct {
		name string
		req  SaveRequest
	}{
		{"leeres Kommando", SaveRequest{ID: "leer", CommandLine: "   "}},
		{"kaputte Anführungszeichen", SaveRequest{ID: "quote", CommandLine: `tool "offen`}},
		{"unbrauchbare Kennung", SaveRequest{ID: "Groß Geschrieben", CommandLine: "echo"}},
		{"Kennung nicht ableitbar", SaveRequest{DisplayName: "???", CommandLine: "echo"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := reg.Save(tc.req); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Fehler = %v, erwartet ErrInvalid", err)
			}
		})
	}
}

func TestKennungWirdAusNamenAbgeleitet(t *testing.T) {
	reg, _, _ := registry(t, nil)
	tests := []struct{ name, want string }{
		{"Claude über Ollama", "claude-ueber-ollama"},
		{"Größe  Test", "groesse-test"},
		{"Mein Tool 2", "mein-tool-2"},
	}
	for _, tc := range tests {
		entry, err := reg.Save(SaveRequest{DisplayName: tc.name, CommandLine: "echo"})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if entry.ID != tc.want {
			t.Errorf("Kennung für %q = %q, erwartet %q", tc.name, entry.ID, tc.want)
		}
	}
}

func TestEnvWirdUebernommen(t *testing.T) {
	bin := fakeBin(t, "tool", `echo "$MEINE_VAR"`)
	t.Setenv("PATH", bin)
	reg, cat, _ := registry(t, nil)
	if _, err := reg.Save(SaveRequest{
		ID: "mit-env", CommandLine: "tool", Env: map[string]string{"MEINE_VAR": "wert", "  ": "leer"},
	}); err != nil {
		t.Fatal(err)
	}
	entry, err := cat.Resolve("mit-env")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Env["MEINE_VAR"] != "wert" {
		t.Errorf("Env = %v", entry.Env)
	}
	if _, leer := entry.Env["  "]; leer {
		t.Error("leerer Variablenname sollte verworfen werden")
	}

	out, err := entry.BuildCmd(t.TempDir(), nil).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "wert" {
		t.Errorf("Ausgabe = %q, erwartet wert", out)
	}
}

func TestAktualisierenErsetztVollstaendig(t *testing.T) {
	reg, cat, _ := registry(t, nil)
	if _, err := reg.Save(SaveRequest{ID: "eigen", CommandLine: "tool --alt", Env: map[string]string{"A": "1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Save(SaveRequest{ID: "eigen", CommandLine: "anderes-tool"}); err != nil {
		t.Fatal(err)
	}
	entry, err := cat.Get("eigen")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Command != "anderes-tool" || len(entry.DefaultArgs) != 0 || len(entry.Env) != 0 {
		t.Errorf("Eintrag = %+v, erwartet vollständige Ersetzung", entry)
	}
}

func TestKonfigurierteRuntimeBleibtSichtbar(t *testing.T) {
	_, cat, _ := registry(t, []config.RuntimeOverride{{ID: "aus-config", DisplayName: "Aus Config", Command: "echo"}})
	entry, err := cat.Get("aus-config")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Source != SourceConfig {
		t.Errorf("Source = %q, erwartet config", entry.Source)
	}
}

func TestStateDateiBleibtLesbar(t *testing.T) {
	reg, _, st := registry(t, nil)
	if _, err := reg.Save(SaveRequest{ID: "eigen", CommandLine: "ollama launch claude"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(st.StateDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ollama") {
		t.Errorf("state.json enthält die Runtime nicht: %s", raw)
	}
}
