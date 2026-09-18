// Package config lädt die Router-Konfiguration aus einer YAML-Datei, überlagert sie
// mit Umgebungsvariablen und validiert sie, bevor irgendein Port geöffnet wird.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mode unterscheidet den lokalen Betrieb vom erreichbaren Dienst.
type Mode string

const (
	ModeLocal     Mode = "local"
	ModeNetworked Mode = "networked"
)

// RuntimeOverride beschreibt eine Runtime aus der Konfigurationsdatei. Eine bekannte
// Kennung ersetzt die eingebaute Definition vollständig.
type RuntimeOverride struct {
	ID          string            `yaml:"id"`
	DisplayName string            `yaml:"displayName"`
	Command     string            `yaml:"command"`
	DefaultArgs []string          `yaml:"defaultArgs"`
	Env         map[string]string `yaml:"env"`
}

// Config ist die vollständige Laufzeitkonfiguration des Routers.
type Config struct {
	Mode        Mode              `yaml:"mode"`
	Bind        string            `yaml:"bind"`
	Port        int               `yaml:"port"`
	Token       string            `yaml:"token"`
	Roots       []string          `yaml:"roots"`
	StateDir    string            `yaml:"stateDir"`
	BufferBytes int               `yaml:"bufferBytes"`
	BaseURL     string            `yaml:"baseURL"`
	Runtimes    []RuntimeOverride `yaml:"runtimes"`
}

// Defaults liefert die Konfiguration eines Starts ohne Datei und ohne Umgebungsvariablen.
func Defaults() Config {
	return Config{
		Mode:        ModeLocal,
		Bind:        "127.0.0.1",
		Port:        7777,
		BufferBytes: 1 << 20,
		StateDir:    defaultStateDir(),
	}
}

func defaultStateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "project-router")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "project-router")
	}
	return filepath.Join(home, ".local", "state", "project-router")
}

// Load liest die Konfiguration: Defaults, darüber die Datei (sofern angegeben),
// darüber die Umgebungsvariablen. Ein leerer Pfad überspringt die Datei.
func Load(path string, env func(string) string) (Config, error) {
	cfg := Defaults()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("Konfigurationsdatei %s: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("Konfigurationsdatei %s: %w", path, err)
		}
	}
	if err := applyEnv(&cfg, env); err != nil {
		return Config{}, err
	}
	normalize(&cfg)
	return cfg, nil
}

// applyEnv überlagert die Datei-Werte; gesetzte Umgebungsvariablen haben Vorrang.
func applyEnv(cfg *Config, env func(string) string) error {
	if env == nil {
		env = os.Getenv
	}
	if v := env("ROUTER_MODE"); v != "" {
		cfg.Mode = Mode(v)
	}
	if v := env("ROUTER_BIND"); v != "" {
		cfg.Bind = v
	}
	if v := env("ROUTER_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("ROUTER_PORT ist keine Zahl: %q", v)
		}
		cfg.Port = port
	}
	if v := env("ROUTER_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := env("ROUTER_ROOTS"); v != "" {
		cfg.Roots = splitList(v)
	}
	if v := env("ROUTER_STATE_DIR"); v != "" {
		cfg.StateDir = v
	}
	if v := env("ROUTER_BUFFER_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("ROUTER_BUFFER_BYTES ist keine Zahl: %q", v)
		}
		cfg.BufferBytes = n
	}
	if v := env("ROUTER_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	return nil
}

func splitList(v string) []string {
	parts := strings.Split(v, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalize(cfg *Config) {
	roots := make([]string, 0, len(cfg.Roots))
	for _, r := range cfg.Roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if expanded, err := expandHome(r); err == nil {
			r = expanded
		}
		if abs, err := filepath.Abs(r); err == nil {
			r = abs
		}
		roots = append(roots, filepath.Clean(r))
	}
	cfg.Roots = roots
	if expanded, err := expandHome(cfg.StateDir); err == nil {
		cfg.StateDir = expanded
	}
	if abs, err := filepath.Abs(cfg.StateDir); err == nil {
		cfg.StateDir = abs
	}
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p, err
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/")), nil
}

// Validate prüft die Startbedingungen. Schlägt sie fehl, wird kein Port geöffnet.
func (c Config) Validate() error {
	switch c.Mode {
	case ModeLocal, ModeNetworked:
	default:
		return fmt.Errorf("unbekannter Modus %q (erlaubt: local, networked)", c.Mode)
	}
	if len(c.Roots) == 0 {
		return fmt.Errorf("keine Projekt-Roots konfiguriert: mindestens ein erlaubtes Wurzelverzeichnis in 'roots' oder ROUTER_ROOTS angeben")
	}
	if c.Mode == ModeNetworked && c.Token == "" {
		return fmt.Errorf("Modus 'networked' erfordert ein Auth-Token: 'token' in der Konfiguration oder ROUTER_TOKEN setzen")
	}
	if c.BufferBytes <= 0 {
		return fmt.Errorf("bufferBytes muss größer als 0 sein, ist %d", c.BufferBytes)
	}
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("Port %d liegt außerhalb des gültigen Bereichs", c.Port)
	}
	for _, rt := range c.Runtimes {
		if strings.TrimSpace(rt.ID) == "" {
			return fmt.Errorf("Runtime-Definition ohne Kennung in der Konfiguration")
		}
		if strings.TrimSpace(rt.Command) == "" {
			return fmt.Errorf("Runtime %q definiert kein Kommando", rt.ID)
		}
	}
	return nil
}

// Addr ist die Bind-Adresse für den Listener.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Bind, strconv.Itoa(c.Port))
}
