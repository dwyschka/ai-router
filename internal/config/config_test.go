package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func TestDefaultsOhneDateiUndEnv(t *testing.T) {
	cfg, err := Load("", noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Mode != ModeLocal {
		t.Errorf("Mode = %q, erwartet %q", cfg.Mode, ModeLocal)
	}
	if cfg.Bind != "127.0.0.1" {
		t.Errorf("Bind = %q, erwartet Loopback", cfg.Bind)
	}
	if cfg.Port != 7777 {
		t.Errorf("Port = %d, erwartet 7777", cfg.Port)
	}
	if cfg.BufferBytes != 1<<20 {
		t.Errorf("BufferBytes = %d, erwartet 1 MiB", cfg.BufferBytes)
	}
	if cfg.Token != "" {
		t.Errorf("Token = %q, erwartet leer", cfg.Token)
	}
	if !strings.HasSuffix(cfg.StateDir, filepath.Join("project-router")) {
		t.Errorf("StateDir = %q, erwartet Default unterhalb project-router", cfg.StateDir)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "router.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnvUeberschreibtDatei(t *testing.T) {
	path := writeConfig(t, "port: 8080\nroots:\n  - /tmp\n")
	env := func(k string) string {
		if k == "ROUTER_PORT" {
			return "9000"
		}
		return ""
	}
	cfg, err := Load(path, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 9000 {
		t.Errorf("Port = %d, erwartet 9000 (Env vor Datei)", cfg.Port)
	}
}

func TestDateiwerteOhneEnv(t *testing.T) {
	path := writeConfig(t, "mode: networked\nbind: 0.0.0.0\nport: 8080\ntoken: geheim\nroots:\n  - /tmp\n")
	cfg, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Mode != ModeNetworked || cfg.Bind != "0.0.0.0" || cfg.Port != 8080 || cfg.Token != "geheim" {
		t.Fatalf("Datei nicht übernommen: %+v", cfg)
	}
	if cfg.Addr() != "0.0.0.0:8080" {
		t.Errorf("Addr = %q", cfg.Addr())
	}
}

func TestValidateLeereRootsBrichtAb(t *testing.T) {
	cfg := Defaults()
	err := cfg.Validate()
	if err == nil {
		t.Fatal("erwartet Fehler bei leerer Root-Liste")
	}
	if !strings.Contains(err.Error(), "Root") {
		t.Errorf("Fehler benennt die Roots nicht: %v", err)
	}
}

func TestValidateNetworkedOhneToken(t *testing.T) {
	cfg := Defaults()
	cfg.Mode = ModeNetworked
	cfg.Roots = []string{"/tmp"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("erwartet Fehler bei networked ohne Token")
	}
	if !strings.Contains(err.Error(), "Token") {
		t.Errorf("Fehler benennt das fehlende Token nicht: %v", err)
	}
}

func TestValidateNetworkedMitToken(t *testing.T) {
	cfg := Defaults()
	cfg.Mode = ModeNetworked
	cfg.Token = "geheim"
	cfg.Roots = []string{"/tmp"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
}

func TestValidateRuntimeOhneKommando(t *testing.T) {
	cfg := Defaults()
	cfg.Roots = []string{"/tmp"}
	cfg.Runtimes = []RuntimeOverride{{ID: "kaputt", DisplayName: "Kaputt"}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "kaputt") {
		t.Fatalf("erwartet Fehler, der die Runtime benennt, bekam: %v", err)
	}
}

func TestRootsWerdenNormalisiert(t *testing.T) {
	path := writeConfig(t, "roots:\n  - /tmp/./a/../a\n")
	cfg, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Roots) != 1 || cfg.Roots[0] != "/tmp/a" {
		t.Errorf("Roots = %v, erwartet [/tmp/a]", cfg.Roots)
	}
}

func TestRootsAusEnv(t *testing.T) {
	env := func(k string) string {
		if k == "ROUTER_ROOTS" {
			return "/tmp/a" + string(os.PathListSeparator) + "/tmp/b"
		}
		return ""
	}
	cfg, err := Load("", env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Roots) != 2 {
		t.Fatalf("Roots = %v, erwartet zwei Einträge", cfg.Roots)
	}
}
