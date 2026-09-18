package runtime

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"project-router/internal/store"
)

// Fehlerfälle beim Hinterlegen eigener Runtimes.
var (
	// ErrInvalid: unvollständige oder unbrauchbare Definition (400).
	ErrInvalid = errors.New("Runtime-Definition ist unvollständig")
	// ErrNotCustom: Kennung gehört zu einer ausgelieferten oder konfigurierten
	// Runtime und ist nicht zur Laufzeit entfernbar (409).
	ErrNotCustom = errors.New("Runtime ist nicht in der Oberfläche hinterlegt")
)

// idPattern hält Kennungen kurz und URL-tauglich.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Registry legt eigene Runtimes an und entfernt sie wieder. Sie liegen im selben
// Zustandsspeicher wie Projekte und Sessions und überleben damit Neustarts.
type Registry struct {
	store   store.Store
	catalog *Catalog
}

func NewRegistry(st store.Store, catalog *Catalog) *Registry {
	reg := &Registry{store: st, catalog: catalog}
	catalog.SetCustomSource(reg.custom)
	return reg
}

// custom liest die hinterlegten Runtimes aus dem Zustandsspeicher.
func (r *Registry) custom() []Runtime {
	state := r.store.Snapshot()
	out := make([]Runtime, 0, len(state.Runtimes))
	for _, rt := range state.Runtimes {
		out = append(out, Runtime{
			ID:          rt.ID,
			DisplayName: rt.DisplayName,
			Command:     rt.Command,
			DefaultArgs: rt.DefaultArgs,
			Env:         rt.Env,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SaveRequest beschreibt eine hinterlegte Runtime. Das Kommando kommt als ganze
// Zeile (`ollama launch claude`) und wird ohne Shell in Argumentvektor zerlegt.
type SaveRequest struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"displayName"`
	CommandLine string            `json:"commandLine"`
	Env         map[string]string `json:"env,omitempty"`
}

// Save legt eine Runtime an oder ersetzt eine bereits hinterlegte mit derselben
// Kennung vollständig.
func (r *Registry) Save(req SaveRequest) (Entry, error) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = ableitenID(req.DisplayName)
	}
	if !idPattern.MatchString(id) {
		return Entry{}, fmt.Errorf("%w: Kennung %q darf nur Kleinbuchstaben, Ziffern, - und _ enthalten", ErrInvalid, id)
	}

	command, args, err := ParseCommandLine(req.CommandLine)
	if err != nil {
		return Entry{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		name = id
	}
	env := map[string]string{}
	for k, v := range req.Env {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		env[k] = v
	}
	if len(env) == 0 {
		env = nil
	}

	eintrag := store.Runtime{
		ID:          id,
		DisplayName: name,
		Command:     command,
		DefaultArgs: args,
		Env:         env,
		CreatedAt:   time.Now().UTC(),
	}
	err = r.store.Update(func(st *store.State) error {
		for i := range st.Runtimes {
			if st.Runtimes[i].ID == id {
				eintrag.CreatedAt = st.Runtimes[i].CreatedAt
				st.Runtimes[i] = eintrag
				return nil
			}
		}
		st.Runtimes = append(st.Runtimes, eintrag)
		return nil
	})
	if err != nil {
		return Entry{}, err
	}
	return r.catalog.Get(id)
}

// Remove entfernt eine hinterlegte Runtime. Überschrieb sie eine ausgelieferte
// Definition, lebt diese danach wieder auf.
func (r *Registry) Remove(id string) error {
	return r.store.Update(func(st *store.State) error {
		for i, rt := range st.Runtimes {
			if rt.ID != id {
				continue
			}
			st.Runtimes = append(st.Runtimes[:i:i], st.Runtimes[i+1:]...)
			return nil
		}
		if _, ok := r.catalog.Static(id); ok {
			return fmt.Errorf("%w: %s stammt aus dem Binary oder der Konfigurationsdatei", ErrNotCustom, id)
		}
		return fmt.Errorf("%w: %s", ErrUnknown, id)
	})
}

// umlaute hält die Kennung lesbar, statt Sonderzeichen ersatzlos zu schlucken.
var umlaute = strings.NewReplacer(
	"ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss",
	"á", "a", "à", "a", "â", "a", "é", "e", "è", "e", "ê", "e",
	"í", "i", "ì", "i", "ó", "o", "ò", "o", "ô", "o", "ú", "u", "ù", "u", "ñ", "n", "ç", "c",
)

// ableitenID baut aus einem Anzeigenamen eine brauchbare Kennung.
func ableitenID(name string) string {
	var b strings.Builder
	vorher := '-'
	for _, r := range umlaute.Replace(strings.ToLower(strings.TrimSpace(name))) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			vorher = r
		case r == ' ' || r == '-' || r == '_' || r == '.':
			if vorher != '-' {
				b.WriteRune('-')
				vorher = '-'
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
