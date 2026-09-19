// Package config lädt die Router-Konfiguration aus einer YAML-Datei, überlagert sie
// mit Umgebungsvariablen und validiert sie, bevor irgendein Port geöffnet wird.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Mode unterscheidet den lokalen Betrieb vom erreichbaren Dienst.
type Mode string

const (
	ModeLocal     Mode = "local"
	ModeNetworked Mode = "networked"
)

// Duration ist eine Zeitspanne in der Schreibweise von time.ParseDuration ("4s",
// "500ms"). YAML kennt keinen Zeitspannen-Typ, deshalb steht sie als Zeichenkette in
// der Datei und wird beim Laden geprüft — nicht erst beim ersten Gebrauch.
type Duration time.Duration

// UnmarshalYAML nimmt die Zeichenkette entgegen und meldet eine unlesbare Angabe als
// Konfigurationsfehler.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("Zeitspanne %q muss als Zeichenkette angegeben werden, etwa \"4s\"", node.Value)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("Zeitspanne %q ist ungültig (erwartet etwa \"4s\" oder \"500ms\")", raw)
	}
	*d = Duration(parsed)
	return nil
}

// Duration liefert die Zeitspanne als time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// String macht die Zeitspanne wieder lesbar, etwa für Startmeldungen.
func (d Duration) String() string { return time.Duration(d).String() }

// WebhookConfig beschreibt den HTTP-Aufruf, mit dem eine Rückfrage nach außen
// gemeldet wird — etwa an ntfy, Gotify oder einen Chat-Webhook.
type WebhookConfig struct {
	URL         string            `yaml:"url"`
	Method      string            `yaml:"method"`
	ContentType string            `yaml:"contentType"`
	Headers     map[string]string `yaml:"headers"`
	// Template ist ein Go-Template für den Body. Leer heißt: der eingebaute Body,
	// passend zum ContentType.
	Template string   `yaml:"template"`
	Timeout  Duration `yaml:"timeout"`
}

// Configured meldet, ob überhaupt ein Ziel hinterlegt ist.
func (w WebhookConfig) Configured() bool { return strings.TrimSpace(w.URL) != "" }

// NotifyConfig steuert die Erkennung und Meldung von Rückfragen: wartet ein Agent auf
// eine Entscheidung, soll das nicht erst beim nächsten Blick in den Browser auffallen.
type NotifyConfig struct {
	// Enabled schaltet Erkennung und Meldung insgesamt ab.
	Enabled bool `yaml:"enabled"`
	// IdleAfter ist die Stille, die auf eine erkannte Rückfrage folgen muss, bevor
	// gemeldet wird. Ein arbeitender Agent schreibt laufend; ein wartender nicht.
	IdleAfter Duration `yaml:"idleAfter"`
	// Patterns sind zusätzliche Muster (RE2, ohne Rücksicht auf Groß-/Kleinschreibung).
	Patterns []string `yaml:"patterns"`
	// ReplacePatterns ersetzt die eingebauten Muster, statt sie zu ergänzen.
	ReplacePatterns bool          `yaml:"replacePatterns"`
	Webhook         WebhookConfig `yaml:"webhook"`
}

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
	// Name ist der Anzeigename dieser Instanz: Überschrift der WebUI, Fenstertitel
	// und Absender der Benachrichtigungen. Wer mehrere Router betreibt, unterscheidet
	// sie hier.
	Name  string `yaml:"name"`
	Mode  Mode   `yaml:"mode"`
	Bind  string `yaml:"bind"`
	Port  int    `yaml:"port"`
	Token string `yaml:"token"`
	// DisableAuth schaltet die Token-Prüfung ausdrücklich ab — auch im Modus
	// networked, wo sonst ein Token Startbedingung ist.
	DisableAuth bool              `yaml:"disableAuth"`
	Roots       []string          `yaml:"roots"`
	StateDir    string            `yaml:"stateDir"`
	BufferBytes int               `yaml:"bufferBytes"`
	BaseURL     string            `yaml:"baseURL"`
	Runtimes    []RuntimeOverride `yaml:"runtimes"`
	Notify      NotifyConfig      `yaml:"notify"`
}

// DefaultName ist der Anzeigename ohne eigene Angabe.
const DefaultName = "project-router"

// Defaults liefert die Konfiguration eines Starts ohne Datei und ohne Umgebungsvariablen.
func Defaults() Config {
	return Config{
		Name:        DefaultName,
		Mode:        ModeLocal,
		Bind:        "127.0.0.1",
		Port:        7777,
		BufferBytes: 1 << 20,
		StateDir:    defaultStateDir(),
		Notify: NotifyConfig{
			Enabled:   true,
			IdleAfter: Duration(4 * time.Second),
			Webhook: WebhookConfig{
				Method:      "POST",
				ContentType: "application/json",
				Timeout:     Duration(10 * time.Second),
			},
		},
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
	if v := env("ROUTER_NAME"); v != "" {
		cfg.Name = v
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
	if v := env("ROUTER_DISABLE_AUTH"); v != "" {
		an, err := parseBool(v)
		if err != nil {
			return fmt.Errorf("ROUTER_DISABLE_AUTH ist kein Wahrheitswert: %q", v)
		}
		cfg.DisableAuth = an
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
	if v := env("ROUTER_NOTIFY"); v != "" {
		an, err := parseBool(v)
		if err != nil {
			return fmt.Errorf("ROUTER_NOTIFY ist kein Wahrheitswert: %q", v)
		}
		cfg.Notify.Enabled = an
	}
	if v := env("ROUTER_NOTIFY_IDLE_AFTER"); v != "" {
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("ROUTER_NOTIFY_IDLE_AFTER ist keine Zeitspanne wie \"4s\": %q", v)
		}
		cfg.Notify.IdleAfter = Duration(d)
	}
	if v := env("ROUTER_NOTIFY_WEBHOOK"); v != "" {
		cfg.Notify.Webhook.URL = v
	}
	return nil
}

// parseBool nimmt die üblichen Schreibweisen an, damit ROUTER_DISABLE_AUTH=1 und
// ROUTER_DISABLE_AUTH=yes dasselbe bedeuten.
func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	}
	return false, fmt.Errorf("unbekannter Wert %q", v)
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
	// Ein leerer oder nur aus Leerraum bestehender Name fällt auf den Default zurück:
	// eine namenlose Überschrift hilft niemandem.
	if cfg.Name = strings.TrimSpace(cfg.Name); cfg.Name == "" {
		cfg.Name = DefaultName
	}
	cfg.Notify.Webhook.URL = strings.TrimSpace(cfg.Notify.Webhook.URL)
	if cfg.Notify.Webhook.Method = strings.ToUpper(strings.TrimSpace(cfg.Notify.Webhook.Method)); cfg.Notify.Webhook.Method == "" {
		cfg.Notify.Webhook.Method = "POST"
	}
	if cfg.Notify.Webhook.ContentType == "" {
		cfg.Notify.Webhook.ContentType = "application/json"
	}
	if cfg.Notify.IdleAfter <= 0 {
		cfg.Notify.IdleAfter = Duration(4 * time.Second)
	}
	if cfg.Notify.Webhook.Timeout <= 0 {
		cfg.Notify.Webhook.Timeout = Duration(10 * time.Second)
	}

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
	if c.Mode == ModeNetworked && c.Token == "" && !c.DisableAuth {
		return fmt.Errorf("Modus 'networked' erfordert ein Auth-Token: 'token' in der Konfiguration oder ROUTER_TOKEN setzen — oder die Prüfung mit 'disableAuth: true' ausdrücklich abschalten")
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
	return c.Notify.validate()
}

// validate prüft die Benachrichtigung beim Start: ein unlesbares Muster oder eine
// kaputte Webhook-URL soll auffallen, bevor die erste Rückfolge darauf läuft.
func (n NotifyConfig) validate() error {
	for _, muster := range n.Patterns {
		if _, err := regexp.Compile("(?i)" + muster); err != nil {
			return fmt.Errorf("notify.patterns: %q ist kein gültiger regulärer Ausdruck: %w", muster, err)
		}
	}
	if n.ReplacePatterns && len(n.Patterns) == 0 {
		return fmt.Errorf("notify.replacePatterns ist gesetzt, aber unter notify.patterns steht kein Muster — so wird nie etwas erkannt")
	}
	if !n.Webhook.Configured() {
		return nil
	}
	parsed, err := url.Parse(n.Webhook.URL)
	if err != nil {
		return fmt.Errorf("notify.webhook.url ist keine gültige URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("notify.webhook.url braucht das Schema http oder https, ist %q", n.Webhook.URL)
	}
	if parsed.Host == "" {
		return fmt.Errorf("notify.webhook.url ohne Host: %q", n.Webhook.URL)
	}
	return nil
}

// AuthToken ist das tatsächlich geprüfte Token. Ist die Prüfung abgeschaltet, ist es
// leer und jeder Request kommt durch.
func (c Config) AuthToken() string {
	if c.DisableAuth {
		return ""
	}
	return c.Token
}

// Warnings sind Hinweise, die beim Start auf stderr gehören: Konstellationen, die
// zulässig, aber riskant oder überraschend sind.
func (c Config) Warnings() []string {
	var out []string
	if c.DisableAuth {
		if c.Mode == ModeNetworked {
			out = append(out, fmt.Sprintf(
				"Token-Prüfung ist abgeschaltet (disableAuth), der Router lauscht dabei auf %s — jeder, der diese Adresse erreicht, kann Prozesse starten. Nur hinter VPN oder in einem vertrauenswürdigen Netz betreiben.",
				c.Addr()))
		} else {
			out = append(out, "Token-Prüfung ist abgeschaltet (disableAuth).")
		}
		if c.Token != "" {
			out = append(out, "Das konfigurierte Token wird wegen disableAuth ignoriert.")
		}
	} else if c.Token == "" {
		out = append(out, "Kein Token konfiguriert — die API ist ohne Authentifizierung erreichbar.")
	}
	return out
}

// Addr ist die Bind-Adresse für den Listener.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Bind, strconv.Itoa(c.Port))
}
