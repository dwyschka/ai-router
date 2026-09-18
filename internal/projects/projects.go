// Package projects verwaltet die Registry der Git-Repositories, unter denen Sessions laufen.
package projects

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"project-router/internal/fsbrowse"
	"project-router/internal/pathsafe"
	"project-router/internal/store"
)

// Fehlerfälle, die Handler auf Statuscodes abbilden.
var (
	// ErrNotGitRepo: Verzeichnis existiert, ist aber kein Repository (400).
	ErrNotGitRepo = errors.New("Verzeichnis ist kein Git-Repository")
	// ErrNotFound: unbekannte Projektkennung (404).
	ErrNotFound = errors.New("Projekt nicht gefunden")
	// ErrHasRunningSessions: Entfernen bei laufenden Sessions (409).
	ErrHasRunningSessions = errors.New("Projekt hat laufende Sessions")
	// ErrUnavailable: Projektpfad existiert nicht mehr (409).
	ErrUnavailable = errors.New("Projektpfad ist nicht mehr verfügbar")
	// ErrNotDirectory: Pfad zeigt auf eine reguläre Datei (400).
	ErrNotDirectory = errors.New("Pfad ist kein Verzeichnis")
)

// View ist ein Projekt so, wie die API es ausliefert.
type View struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Path            string    `json:"path"`
	CreatedAt       time.Time `json:"createdAt"`
	RunningSessions int       `json:"runningSessions"`
	Available       bool      `json:"available"`
	IsGitRepo       bool      `json:"isGitRepo"`
}

// Registry legt Projekte an, listet sie auf und entfernt sie.
type Registry struct {
	store store.Store
	roots []string
}

func New(st store.Store, roots []string) *Registry {
	return &Registry{store: st, roots: roots}
}

// CreateRequest beschreibt das Anlegen eines Projekts.
type CreateRequest struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
	// Init verlangt ausdrücklich `git init`. Ohne dieses Flag initialisiert der
	// Router niemals ein Repository.
	Init bool `json:"init,omitempty"`
}

// Create legt ein Projekt für einen bestehenden Pfad an. Ein bereits registrierter
// normalisierter Pfad liefert das bestehende Projekt statt eines Duplikats.
func (r *Registry) Create(req CreateRequest) (View, error) {
	resolved, err := pathsafe.ResolveWithin(r.roots, req.Path)
	if err != nil {
		return View{}, err
	}

	info, statErr := os.Stat(resolved)
	switch {
	case statErr == nil && !info.IsDir():
		return View{}, ErrNotDirectory
	case statErr == nil:
		if !fsbrowse.IsGitRepo(resolved) {
			if !req.Init {
				return View{}, fmt.Errorf("%w: %s", ErrNotGitRepo, resolved)
			}
			if err := gitInit(resolved); err != nil {
				return View{}, err
			}
		}
	case errors.Is(statErr, os.ErrNotExist):
		if !req.Init {
			return View{}, fmt.Errorf("%w: %s existiert nicht und die Initialisierung wurde nicht angefordert", ErrNotGitRepo, resolved)
		}
		// Der Elternpfad wurde durch ResolveWithin bereits mitgeprüft.
		if err := os.MkdirAll(resolved, 0o755); err != nil {
			return View{}, fmt.Errorf("Verzeichnis %s konnte nicht angelegt werden: %w", resolved, err)
		}
		if err := gitInit(resolved); err != nil {
			return View{}, err
		}
	default:
		return View{}, statErr
	}

	snapshot := r.store.Snapshot()
	for _, p := range snapshot.Projects {
		if p.Path == resolved {
			return r.view(p, snapshot.Sessions), nil
		}
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = filepath.Base(resolved)
	}
	project := store.Project{
		ID:        idFor(resolved),
		Name:      name,
		Path:      resolved,
		CreatedAt: time.Now().UTC(),
	}
	if err := r.store.Update(func(st *store.State) error {
		for _, p := range st.Projects {
			if p.Path == resolved {
				project = p
				return nil
			}
		}
		st.Projects = append(st.Projects, project)
		return nil
	}); err != nil {
		return View{}, err
	}
	return r.view(project, r.store.Snapshot().Sessions), nil
}

// gitInit führt `git init` aus und gibt dessen Fehlerausgabe unverändert durch.
func gitInit(dir string) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git init in %s fehlgeschlagen: %s", dir, msg)
	}
	return nil
}

// idFor leitet eine stabile Kennung aus dem normalisierten Pfad ab: derselbe Pfad
// ergibt über Neustarts hinweg dieselbe Kennung.
func idFor(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:8])
}

// List liefert alle registrierten Projekte, nach Name sortiert.
func (r *Registry) List() []View {
	snapshot := r.store.Snapshot()
	views := make([]View, 0, len(snapshot.Projects))
	for _, p := range snapshot.Projects {
		views = append(views, r.view(p, snapshot.Sessions))
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Name == views[j].Name {
			return views[i].Path < views[j].Path
		}
		return views[i].Name < views[j].Name
	})
	return views
}

// Get liefert ein einzelnes Projekt.
func (r *Registry) Get(id string) (View, error) {
	snapshot := r.store.Snapshot()
	for _, p := range snapshot.Projects {
		if p.ID == id {
			return r.view(p, snapshot.Sessions), nil
		}
	}
	return View{}, ErrNotFound
}

// Resolve liefert das Projekt für einen Session-Start und lehnt nicht verfügbare
// Projekte ab, bevor ein Prozess entsteht.
func (r *Registry) Resolve(id string) (View, error) {
	v, err := r.Get(id)
	if err != nil {
		return View{}, err
	}
	if !v.Available {
		return View{}, fmt.Errorf("%w: %s", ErrUnavailable, v.Path)
	}
	return v, nil
}

// Remove nimmt ein Projekt aus der Registry, ohne Dateien zu löschen. Laufende
// Sessions verhindern das Entfernen.
func (r *Registry) Remove(id string) error {
	return r.store.Update(func(st *store.State) error {
		idx := -1
		for i, p := range st.Projects {
			if p.ID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrNotFound
		}
		for _, s := range st.Sessions {
			if s.ProjectID == id && s.Status == store.StatusRunning {
				return fmt.Errorf("%w: zuerst die laufenden Sessions beenden", ErrHasRunningSessions)
			}
		}
		st.Projects = append(st.Projects[:idx:idx], st.Projects[idx+1:]...)
		// Metadaten beendeter Sessions des Projekts verschwinden mit ihm.
		kept := st.Sessions[:0]
		for _, s := range st.Sessions {
			if s.ProjectID != id {
				kept = append(kept, s)
			}
		}
		st.Sessions = append([]store.Session(nil), kept...)
		return nil
	})
}

func (r *Registry) view(p store.Project, sessions []store.Session) View {
	running := 0
	for _, s := range sessions {
		if s.ProjectID == p.ID && s.Status == store.StatusRunning {
			running++
		}
	}
	info, err := os.Stat(p.Path)
	available := err == nil && info.IsDir()
	return View{
		ID:              p.ID,
		Name:            p.Name,
		Path:            p.Path,
		CreatedAt:       p.CreatedAt,
		RunningSessions: running,
		Available:       available,
		IsGitRepo:       available && fsbrowse.IsGitRepo(p.Path),
	}
}
