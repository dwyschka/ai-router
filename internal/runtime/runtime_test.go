package runtime

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"project-router/internal/config"
)

// fakeBin legt ein ausführbares Skript in einem eigenen Verzeichnis an und liefert
// dessen Pfad, damit PATH-Manipulation testbar ist.
func fakeBin(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEingebauteRuntimes(t *testing.T) {
	cat, err := NewCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]Entry{}
	for _, e := range cat.List() {
		ids[e.ID] = e
	}
	claude, ok := ids["claude-code"]
	if !ok {
		t.Fatal("claude-code fehlt im Katalog")
	}
	if claude.DisplayName == "" || claude.Command != "claude" {
		t.Errorf("claude-code = %+v", claude)
	}
	if oc, ok := ids["opencode"]; !ok || oc.Command != "opencode" {
		t.Errorf("opencode fehlt oder ist falsch: %+v", oc)
	}
}

func TestVerfuegbarkeitFolgtDemPATH(t *testing.T) {
	bin := fakeBin(t, "claude", "echo hallo")
	cat, err := NewCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", bin)
	entry, err := cat.Get("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Available {
		t.Fatal("claude-code sollte mit präpariertem PATH verfügbar sein")
	}
	if entry.ResolvedPath != filepath.Join(bin, "claude") {
		t.Errorf("ResolvedPath = %q", entry.ResolvedPath)
	}

	// Kein Cache: nach dem Entfernen aus dem PATH schlägt der nächste Abruf um.
	t.Setenv("PATH", filepath.Join(bin, "leer"))
	entry, err = cat.Get("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Available {
		t.Error("Verfügbarkeit wurde gecacht")
	}
	if entry.Command != "claude" {
		t.Errorf("Eintrag nennt das gesuchte Kommando nicht: %+v", entry)
	}
}

func TestResolveNichtVerfuegbar(t *testing.T) {
	cat, err := NewCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(t.TempDir(), "leer"))
	_, err = cat.Resolve("claude-code")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Fehler = %v, erwartet ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("Fehler benennt das fehlende Kommando nicht: %v", err)
	}
}

func TestUnbekannteKennung(t *testing.T) {
	cat, err := NewCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Resolve("gibtesnicht"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Fehler = %v, erwartet ErrUnknown", err)
	}
}

func TestOverrideErgaenztKatalog(t *testing.T) {
	cat, err := NewCatalog([]config.RuntimeOverride{
		{ID: "eigen", DisplayName: "Eigenbau", Command: "echo", DefaultArgs: []string{"hallo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cat.Get("eigen")
	if err != nil {
		t.Fatalf("eigene Runtime fehlt: %v", err)
	}
	if entry.DisplayName != "Eigenbau" || entry.Command != "echo" {
		t.Errorf("Eintrag = %+v", entry)
	}
	if len(cat.List()) != 3 {
		t.Errorf("Katalog = %+v, erwartet drei Einträge", cat.List())
	}
}

func TestOverrideErsetztEingebaute(t *testing.T) {
	cat, err := NewCatalog([]config.RuntimeOverride{
		{ID: "claude-code", DisplayName: "Claude (angepasst)", Command: "mein-claude", DefaultArgs: []string{"--flag"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cat.Get("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Command != "mein-claude" || entry.DisplayName != "Claude (angepasst)" {
		t.Fatalf("Eintrag = %+v", entry)
	}
	if len(entry.DefaultArgs) != 1 || entry.DefaultArgs[0] != "--flag" {
		t.Errorf("DefaultArgs = %v, erwartet vollständige Ersetzung", entry.DefaultArgs)
	}
	if len(cat.List()) != 2 {
		t.Errorf("Katalog = %+v, erwartet zwei Einträge", cat.List())
	}
}

func TestOverrideOhneKommandoBrichtAb(t *testing.T) {
	_, err := NewCatalog([]config.RuntimeOverride{{ID: "kaputt", DisplayName: "Kaputt"}})
	if err == nil || !strings.Contains(err.Error(), "kaputt") {
		t.Fatalf("Fehler = %v, erwartet Abbruch mit Benennung der Runtime", err)
	}
}

func TestBuildCmdStartkontrakt(t *testing.T) {
	bin := fakeBin(t, "agent", `for a in "$@"; do echo "arg:$a"; done; echo "pwd:$(pwd)"; echo "term:$TERM"; echo "extra:$MEINE_VAR"`)
	t.Setenv("PATH", bin)
	cat, err := NewCatalog([]config.RuntimeOverride{{
		ID: "test", DisplayName: "Test", Command: "agent",
		DefaultArgs: []string{"--default"},
		Env:         map[string]string{"MEINE_VAR": "wert"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cat.Resolve("test")
	if err != nil {
		t.Fatal(err)
	}

	workdir := t.TempDir()
	// Sonderzeichen, die eine Shell interpretieren würde.
	sonderzeichen := `a b; rm -rf /; $(whoami) "quote" 'x' | & > <`
	cmd := entry.BuildCmd(workdir, []string{sonderzeichen})
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Ausführung: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "arg:--default\n") {
		t.Errorf("Standardargument fehlt: %s", text)
	}
	if !strings.Contains(text, "arg:"+sonderzeichen+"\n") {
		t.Errorf("Zusatzargument kam verändert an: %s", text)
	}
	if !strings.Contains(text, "extra:wert") {
		t.Errorf("Runtime-Variable fehlt: %s", text)
	}
	if !strings.Contains(text, "term:xterm") {
		t.Errorf("TERM fehlt oder ist untauglich: %s", text)
	}
	if !strings.Contains(text, "pwd:") {
		t.Fatalf("keine Ausgabe des Arbeitsverzeichnisses: %s", text)
	}
	real, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "pwd:"+real) && !strings.Contains(text, "pwd:"+workdir) {
		t.Errorf("Arbeitsverzeichnis = %s, erwartet %s", text, workdir)
	}
}

func TestBuildCmdNutztKeineShell(t *testing.T) {
	cat, err := NewCatalog([]config.RuntimeOverride{{ID: "test", Command: "echo"}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cat.Get("test")
	if err != nil {
		t.Fatal(err)
	}
	cmd := entry.BuildCmd(t.TempDir(), []string{"$HOME"})
	if len(cmd.Args) != 2 || cmd.Args[1] != "$HOME" {
		t.Fatalf("Argumentvektor = %v, erwartet unveränderte Übergabe", cmd.Args)
	}
	if filepath.Base(cmd.Path) != "echo" {
		t.Errorf("Pfad = %q, es sollte keine Shell dazwischenliegen", cmd.Path)
	}
	var _ *exec.Cmd = cmd
}

func TestTermWirdNichtUeberschriebenWennGesetzt(t *testing.T) {
	cat, err := NewCatalog([]config.RuntimeOverride{{ID: "test", Command: "echo", Env: map[string]string{"TERM": "eigen"}}})
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := cat.Get("test")
	cmd := entry.BuildCmd(t.TempDir(), nil)
	count := 0
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "TERM=") {
			count++
			if kv != "TERM=eigen" {
				t.Errorf("TERM = %q, erwartet den Wert der Runtime", kv)
			}
		}
	}
	if count != 1 {
		t.Errorf("TERM kommt %d-mal vor, erwartet genau einmal", count)
	}
}
