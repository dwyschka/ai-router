// Package runtime beschreibt Agent-Runtimes als Daten, nicht als Code: eine Runtime ist
// eine Struktur aus Kennung, Kommando, Standardargumenten und Umgebungsvariablen.
package runtime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"project-router/internal/config"
)

// Fehlerfälle, die Handler auf Statuscodes abbilden.
var (
	// ErrUnknown: Kennung existiert im Katalog nicht (400).
	ErrUnknown = errors.New("unbekannte Runtime-Kennung")
	// ErrUnavailable: Kommando liegt nicht im PATH (400).
	ErrUnavailable = errors.New("Runtime ist nicht verfügbar")
)

// Runtime ist die Definition eines startbaren Agent-Tools.
type Runtime struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"displayName"`
	Command     string            `json:"command"`
	DefaultArgs []string          `json:"defaultArgs"`
	Env         map[string]string `json:"env,omitempty"`
}

// Entry ist ein Katalogeintrag inklusive Verfügbarkeit.
type Entry struct {
	Runtime
	Available bool `json:"available"`
	// ResolvedPath ist der gefundene Pfad des Kommandos, leer wenn nicht verfügbar.
	ResolvedPath string `json:"resolvedPath,omitempty"`
	// Source sagt, woher die Definition stammt — nur hinterlegte sind änderbar.
	Source Source `json:"source,omitempty"`
	// OverridesStatic meldet, dass hinter einer hinterlegten Definition eine
	// ausgelieferte oder konfigurierte liegt, die beim Entfernen wieder auflebt.
	OverridesStatic bool `json:"overridesStatic,omitempty"`
	// CommandLine ist die anzeigbare Kommandozeile aus Kommando und Standardargumenten.
	CommandLine string `json:"commandLine,omitempty"`
}

// builtins sind die ausgelieferten Runtimes. Konfigurationseinträge ersetzen sie per ID.
func builtins() []Runtime {
	return []Runtime{
		{
			ID:          "claude-code",
			DisplayName: "Claude Code",
			Command:     "claude",
			DefaultArgs: nil,
		},
		{
			ID:          "opencode",
			DisplayName: "OpenCode",
			Command:     "opencode",
			DefaultArgs: nil,
		},
	}
}

// Source benennt, woher eine Runtime-Definition stammt.
type Source string

const (
	SourceBuiltin Source = "builtin" // im Binary ausgeliefert
	SourceConfig  Source = "config"  // aus der Konfigurationsdatei
	SourceCustom  Source = "custom"  // in der WebUI hinterlegt
)

// Catalog hält die bekannten Runtimes. Die Verfügbarkeit wird bei jedem Abruf neu
// geprüft und bewusst nicht gecacht.
type Catalog struct {
	runtimes []Runtime
	sources  map[string]Source
	lookPath func(string) (string, error)
	// custom liefert die zur Laufzeit hinterlegten Runtimes. Sie werden bei jedem
	// Abruf frisch gelesen, damit eine neu angelegte Runtime sofort startbar ist.
	custom func() []Runtime
}

// NewCatalog baut den Katalog aus den eingebauten Definitionen plus den Overrides der
// Konfiguration. Eine Definition ohne Kommando bricht mit Benennung der Runtime ab.
func NewCatalog(overrides []config.RuntimeOverride) (*Catalog, error) {
	byID := map[string]int{}
	list := builtins()
	for i, rt := range list {
		byID[rt.ID] = i
	}
	for _, o := range overrides {
		id := strings.TrimSpace(o.ID)
		if id == "" {
			return nil, fmt.Errorf("Runtime-Definition ohne Kennung in der Konfiguration")
		}
		if strings.TrimSpace(o.Command) == "" {
			return nil, fmt.Errorf("Runtime %q definiert kein Kommando", id)
		}
		rt := Runtime{
			ID:          id,
			DisplayName: o.DisplayName,
			Command:     o.Command,
			DefaultArgs: o.DefaultArgs,
			Env:         o.Env,
		}
		if rt.DisplayName == "" {
			rt.DisplayName = id
		}
		if idx, ok := byID[id]; ok {
			list[idx] = rt // bekannte Kennung ersetzt die eingebaute Definition vollständig
			continue
		}
		byID[id] = len(list)
		list = append(list, rt)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })

	sources := map[string]Source{}
	for _, rt := range builtins() {
		sources[rt.ID] = SourceBuiltin
	}
	for _, o := range overrides {
		sources[strings.TrimSpace(o.ID)] = SourceConfig
	}
	return &Catalog{runtimes: list, sources: sources, lookPath: exec.LookPath}, nil
}

// SetCustomSource hinterlegt die Quelle der zur Laufzeit angelegten Runtimes.
func (c *Catalog) SetCustomSource(fn func() []Runtime) { c.custom = fn }

// all führt die statischen Definitionen mit den hinterlegten zusammen. Eine
// hinterlegte Runtime mit bekannter Kennung ersetzt die Definition vollständig —
// dieselbe Regel wie bei der Konfiguration, nur zur Laufzeit änderbar.
func (c *Catalog) all() ([]Runtime, map[string]Source) {
	list := append([]Runtime(nil), c.runtimes...)
	sources := map[string]Source{}
	for id, src := range c.sources {
		sources[id] = src
	}
	if c.custom == nil {
		return list, sources
	}
	for _, rt := range c.custom() {
		sources[rt.ID] = SourceCustom
		ersetzt := false
		for i := range list {
			if list[i].ID == rt.ID {
				list[i] = rt
				ersetzt = true
				break
			}
		}
		if !ersetzt {
			list = append(list, rt)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list, sources
}

// List liefert den Katalog mit frisch geprüfter Verfügbarkeit.
func (c *Catalog) List() []Entry {
	list, sources := c.all()
	entries := make([]Entry, 0, len(list))
	for _, rt := range list {
		path, err := c.lookPath(rt.Command)
		entries = append(entries, Entry{
			Runtime:         rt,
			Available:       err == nil,
			ResolvedPath:    path,
			Source:          sources[rt.ID],
			CommandLine:     FormatCommandLine(rt.Command, rt.DefaultArgs),
			OverridesStatic: sources[rt.ID] == SourceCustom && c.hatStatisch(rt.ID),
		})
	}
	return entries
}

// Get liefert einen Eintrag samt Verfügbarkeit.
func (c *Catalog) Get(id string) (Entry, error) {
	list, sources := c.all()
	for _, rt := range list {
		if rt.ID != id {
			continue
		}
		path, err := c.lookPath(rt.Command)
		return Entry{
			Runtime:         rt,
			Available:       err == nil,
			ResolvedPath:    path,
			Source:          sources[rt.ID],
			CommandLine:     FormatCommandLine(rt.Command, rt.DefaultArgs),
			OverridesStatic: sources[rt.ID] == SourceCustom && c.hatStatisch(rt.ID),
		}, nil
	}
	return Entry{}, fmt.Errorf("%w: %s", ErrUnknown, id)
}

// hatStatisch meldet, ob es zu einer Kennung eine statische Definition gibt.
func (c *Catalog) hatStatisch(id string) bool {
	_, ok := c.Static(id)
	return ok
}

// Static meldet, ob eine Kennung aus dem Binary oder der Konfiguration stammt. Eine
// hinterlegte Runtime mit dieser Kennung überschreibt sie nur, statt sie zu ersetzen —
// beim Entfernen lebt die ursprüngliche Definition wieder auf.
func (c *Catalog) Static(id string) (Runtime, bool) {
	for _, rt := range c.runtimes {
		if rt.ID == id {
			return rt, true
		}
	}
	return Runtime{}, false
}

// Resolve liefert eine startbare Runtime und lehnt nicht verfügbare ab, bevor ein
// Prozess erzeugt wird.
func (c *Catalog) Resolve(id string) (Entry, error) {
	entry, err := c.Get(id)
	if err != nil {
		return Entry{}, err
	}
	if !entry.Available {
		return Entry{}, fmt.Errorf("%w: Kommando %q nicht im PATH gefunden", ErrUnavailable, entry.Command)
	}
	return entry, nil
}

// BuildCmd baut den Prozess für eine Session: Argumentvektor ohne Shell,
// Arbeitsverzeichnis gleich Repo-Pfad, Umgebung gleich Router-Umgebung plus
// Runtime-Variablen plus TERM.
func (e Entry) BuildCmd(workdir string, extraArgs []string) *exec.Cmd {
	args := make([]string, 0, len(e.DefaultArgs)+len(extraArgs))
	args = append(args, e.DefaultArgs...)
	args = append(args, extraArgs...)

	cmd := exec.Command(e.Command, args...)
	cmd.Dir = workdir
	cmd.Env = buildEnv(os.Environ(), e.Env)
	return cmd
}

// buildEnv überlagert die Router-Umgebung mit den Runtime-Variablen und stellt eine
// für interaktive TUIs geeignete TERM-Variable sicher.
func buildEnv(base []string, extra map[string]string) []string {
	merged := make([]string, 0, len(base)+len(extra)+1)
	overridden := map[string]bool{}
	for k := range extra {
		overridden[k] = true
	}
	hasTerm := false
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if overridden[key] {
			continue
		}
		if key == "TERM" {
			hasTerm = true
		}
		merged = append(merged, kv)
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "TERM" {
			hasTerm = true
		}
		merged = append(merged, k+"="+extra[k])
	}
	if !hasTerm {
		merged = append(merged, "TERM=xterm-256color")
	}
	return merged
}
