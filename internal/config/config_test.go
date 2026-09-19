package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestDisableAuthErlaubtNetworkedOhneToken(t *testing.T) {
	path := writeConfig(t, "mode: networked\nbind: 0.0.0.0\ndisableAuth: true\nroots:\n  - /tmp\n")
	cfg, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.DisableAuth {
		t.Fatal("disableAuth wurde nicht übernommen")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.AuthToken() != "" {
		t.Errorf("AuthToken = %q, erwartet leer", cfg.AuthToken())
	}
}

func TestDisableAuthAusEnv(t *testing.T) {
	path := writeConfig(t, "mode: networked\nroots:\n  - /tmp\n")
	for _, wert := range []string{"1", "true", "yes", "on"} {
		env := func(k string) string {
			if k == "ROUTER_DISABLE_AUTH" {
				return wert
			}
			return ""
		}
		cfg, err := Load(path, env)
		if err != nil {
			t.Fatalf("%s: %v", wert, err)
		}
		if !cfg.DisableAuth {
			t.Errorf("ROUTER_DISABLE_AUTH=%s wurde nicht übernommen", wert)
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("%s: Validate: %v", wert, err)
		}
	}
}

func TestDisableAuthAusEnvAbschaltbar(t *testing.T) {
	// Die Datei schaltet ab, die Umgebung schaltet wieder ein — Env hat Vorrang.
	path := writeConfig(t, "mode: networked\ndisableAuth: true\nroots:\n  - /tmp\n")
	env := func(k string) string {
		if k == "ROUTER_DISABLE_AUTH" {
			return "false"
		}
		return ""
	}
	cfg, err := Load(path, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DisableAuth {
		t.Fatal("ROUTER_DISABLE_AUTH=false sollte die Datei überstimmen")
	}
	if err := cfg.Validate(); err == nil {
		t.Error("ohne Token und ohne disableAuth muss der Start abbrechen")
	}
}

func TestDisableAuthUnbrauchbarerWert(t *testing.T) {
	env := func(k string) string {
		if k == "ROUTER_DISABLE_AUTH" {
			return "vielleicht"
		}
		return ""
	}
	if _, err := Load("", env); err == nil {
		t.Fatal("erwartet Fehler bei unbrauchbarem Wahrheitswert")
	}
}

func TestAuthTokenOhneDisableAuth(t *testing.T) {
	cfg := Defaults()
	cfg.Token = "geheim"
	if cfg.AuthToken() != "geheim" {
		t.Errorf("AuthToken = %q", cfg.AuthToken())
	}
}

func TestWarnungen(t *testing.T) {
	tests := []struct {
		name     string
		cfg      func() Config
		enthaelt string
		leerwenn bool
	}{
		{
			name: "networked ohne Token-Prüfung",
			cfg: func() Config {
				c := Defaults()
				c.Mode = ModeNetworked
				c.Bind = "0.0.0.0"
				c.DisableAuth = true
				return c
			},
			enthaelt: "abgeschaltet",
		},
		{
			name: "Token wird ignoriert",
			cfg: func() Config {
				c := Defaults()
				c.DisableAuth = true
				c.Token = "geheim"
				return c
			},
			enthaelt: "ignoriert",
		},
		{
			name: "local ohne Token",
			cfg: func() Config {
				return Defaults()
			},
			enthaelt: "ohne Authentifizierung",
		},
		{
			name: "Token gesetzt",
			cfg: func() Config {
				c := Defaults()
				c.Token = "geheim"
				return c
			},
			leerwenn: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			warnungen := tc.cfg().Warnings()
			if tc.leerwenn {
				if len(warnungen) != 0 {
					t.Fatalf("Warnungen = %v, erwartet keine", warnungen)
				}
				return
			}
			gefunden := false
			for _, w := range warnungen {
				if strings.Contains(w, tc.enthaelt) {
					gefunden = true
				}
			}
			if !gefunden {
				t.Errorf("Warnungen = %v, erwartet Hinweis mit %q", warnungen, tc.enthaelt)
			}
		})
	}
}

func TestNameAusDatei(t *testing.T) {
	path := writeConfig(t, "name: Homelab-Router\nroots:\n  - /tmp\n")
	cfg, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Name != "Homelab-Router" {
		t.Errorf("Name = %q, erwartet den konfigurierten Namen", cfg.Name)
	}
}

func TestNameDefaultUndEnv(t *testing.T) {
	cfg, err := Load("", noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Name != DefaultName {
		t.Errorf("Name = %q, erwartet %q", cfg.Name, DefaultName)
	}

	// Ein leerer Name in der Datei fällt auf den Default zurück, statt eine
	// namenlose Überschrift zu erzeugen.
	path := writeConfig(t, "name: \"   \"\nroots:\n  - /tmp\n")
	cfg, err = Load(path, noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Name != DefaultName {
		t.Errorf("Name = %q, erwartet den Default bei leerer Angabe", cfg.Name)
	}

	env := func(k string) string {
		if k == "ROUTER_NAME" {
			return "aus-der-Umgebung"
		}
		return ""
	}
	cfg, err = Load(path, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Name != "aus-der-Umgebung" {
		t.Errorf("Name = %q, erwartet den Wert aus ROUTER_NAME", cfg.Name)
	}
}

func TestNotifyDefaults(t *testing.T) {
	cfg, err := Load("", noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Notify.Enabled {
		t.Error("Notify.Enabled = false, erwartet: Erkennung ist voreingestellt an")
	}
	if cfg.Notify.IdleAfter.Duration() != 4*time.Second {
		t.Errorf("IdleAfter = %s, erwartet 4s", cfg.Notify.IdleAfter)
	}
	if cfg.Notify.Webhook.Configured() {
		t.Error("ohne Angabe darf kein Webhook konfiguriert sein")
	}
	if cfg.Notify.Webhook.Method != "POST" {
		t.Errorf("Methode = %q, erwartet POST", cfg.Notify.Webhook.Method)
	}
}

func TestNotifyAusDatei(t *testing.T) {
	path := writeConfig(t, `roots:
  - /tmp
notify:
  enabled: true
  idleAfter: 90s
  patterns:
    - "braucht deine Freigabe"
  webhook:
    url: https://ntfy.sh/mein-topic
    contentType: text/plain
    headers:
      Title: Router
`)
	cfg, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Notify.IdleAfter.Duration() != 90*time.Second {
		t.Errorf("IdleAfter = %s, erwartet 90s", cfg.Notify.IdleAfter)
	}
	if cfg.Notify.Webhook.URL != "https://ntfy.sh/mein-topic" {
		t.Errorf("Webhook-URL = %q", cfg.Notify.Webhook.URL)
	}
	if cfg.Notify.Webhook.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, erwartet text/plain", cfg.Notify.Webhook.ContentType)
	}
	if cfg.Notify.Webhook.Headers["Title"] != "Router" {
		t.Errorf("Header Title = %q", cfg.Notify.Webhook.Headers["Title"])
	}
	if len(cfg.Notify.Patterns) != 1 {
		t.Errorf("Patterns = %v, erwartet genau eines", cfg.Notify.Patterns)
	}
}

func TestNotifyEnvUeberschreibtDatei(t *testing.T) {
	path := writeConfig(t, "roots:\n  - /tmp\nnotify:\n  webhook:\n    url: https://alt.example/hook\n")
	env := func(k string) string {
		switch k {
		case "ROUTER_NOTIFY_WEBHOOK":
			return "https://neu.example/hook"
		case "ROUTER_NOTIFY_IDLE_AFTER":
			return "12s"
		}
		return ""
	}
	cfg, err := Load(path, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Notify.Webhook.URL != "https://neu.example/hook" {
		t.Errorf("Webhook-URL = %q, erwartet den Wert aus der Umgebung", cfg.Notify.Webhook.URL)
	}
	if cfg.Notify.IdleAfter.Duration() != 12*time.Second {
		t.Errorf("IdleAfter = %s, erwartet 12s", cfg.Notify.IdleAfter)
	}
}

func TestNotifyAbschaltenPerEnv(t *testing.T) {
	env := func(k string) string {
		if k == "ROUTER_NOTIFY" {
			return "0"
		}
		return ""
	}
	cfg, err := Load("", env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Notify.Enabled {
		t.Error("Notify.Enabled = true, erwartet abgeschaltet über ROUTER_NOTIFY=0")
	}
}

func TestUnlesbareZeitspanneMeldetFehler(t *testing.T) {
	path := writeConfig(t, "roots:\n  - /tmp\nnotify:\n  idleAfter: bald\n")
	if _, err := Load(path, noEnv); err == nil {
		t.Fatal("erwartet ein Fehler für eine unlesbare Zeitspanne")
	}
}

func TestNotifyValidierung(t *testing.T) {
	faelle := []struct {
		name string
		yaml string
	}{
		{name: "kaputtes Muster", yaml: "roots:\n  - /tmp\nnotify:\n  patterns:\n    - \"(\"\n"},
		{name: "Webhook ohne Schema", yaml: "roots:\n  - /tmp\nnotify:\n  webhook:\n    url: ntfy.sh/topic\n"},
		{name: "fremdes Schema", yaml: "roots:\n  - /tmp\nnotify:\n  webhook:\n    url: ftp://example.org/hook\n"},
		{name: "ersetzen ohne Muster", yaml: "roots:\n  - /tmp\nnotify:\n  replacePatterns: true\n"},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, f.yaml), noEnv)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if err := cfg.Validate(); err == nil {
				t.Fatal("erwartet ein Validierungsfehler")
			}
		})
	}
}
