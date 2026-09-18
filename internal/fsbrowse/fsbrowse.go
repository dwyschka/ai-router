// Package fsbrowse listet Verzeichnisstruktur innerhalb der erlaubten Roots auf.
// Dateiinhalte und reguläre Dateien verlässt diese Capability nie.
package fsbrowse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"project-router/internal/pathsafe"
)

// Fehlerfälle, die Handler auf Statuscodes abbilden.
var (
	// ErrNotFound: Pfad innerhalb eines Roots, existiert aber nicht (404).
	ErrNotFound = errors.New("Pfad existiert nicht")
	// ErrNotDirectory: Pfad zeigt auf eine reguläre Datei (400).
	ErrNotDirectory = errors.New("Pfad ist kein Verzeichnis")
	// ErrPermission: Betriebssystem verweigert den Lesezugriff (403).
	ErrPermission = errors.New("keine Leseberechtigung für das Verzeichnis")
)

// Entry ist ein navigierbarer Verzeichniseintrag.
type Entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsGitRepo bool   `json:"isGitRepo"`
}

// Listing ist die Antwort auf eine Auflistung.
type Listing struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent,omitempty"`
	IsRoots bool    `json:"isRoots"`
	Entries []Entry `json:"entries"`
}

// Browser listet innerhalb einer festen Root-Allowlist auf.
type Browser struct {
	roots []string
}

func New(roots []string) *Browser { return &Browser{roots: roots} }

// List liefert ohne Pfadangabe die konfigurierten Roots als Einstiegspunkte und
// sonst die direkten Unterverzeichnisse des Pfads, alphabetisch sortiert.
func (b *Browser) List(input string) (Listing, error) {
	if input == "" {
		return b.rootListing(), nil
	}

	// Root-Prüfung vor Existenzprüfung: sonst verrät der Statuscode, welche Pfade
	// außerhalb der Roots existieren.
	resolved, err := pathsafe.ResolveWithin(b.roots, input)
	if err != nil {
		return Listing{}, err
	}

	info, err := os.Stat(resolved)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return Listing{}, fmt.Errorf("%w: %s", ErrNotFound, filepath.Base(resolved))
	case errors.Is(err, os.ErrPermission):
		return Listing{}, ErrPermission
	case err != nil:
		return Listing{}, err
	}
	if !info.IsDir() {
		return Listing{}, ErrNotDirectory
	}

	items, err := os.ReadDir(resolved)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return Listing{}, ErrPermission
		}
		return Listing{}, err
	}

	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		if !isDir(resolved, item) {
			continue // nur Verzeichnisse, niemals Dateien
		}
		child := filepath.Join(resolved, item.Name())
		entries = append(entries, Entry{
			Name:      item.Name(),
			Path:      child,
			IsGitRepo: IsGitRepo(child),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	listing := Listing{Path: resolved, Entries: entries}
	if parent := filepath.Dir(resolved); parent != resolved && pathsafe.Within(b.roots, parent) {
		listing.Parent = parent
	}
	return listing, nil
}

// rootListing liefert die konfigurierten Roots als navigierbare Einträge.
func (b *Browser) rootListing() Listing {
	entries := make([]Entry, 0, len(b.roots))
	for _, root := range b.roots {
		entries = append(entries, Entry{
			Name:      filepath.Base(root),
			Path:      root,
			IsGitRepo: IsGitRepo(root),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return Listing{IsRoots: true, Entries: entries}
}

// isDir behandelt Symlinks: ein Symlink auf ein Verzeichnis bleibt navigierbar, die
// Root-Prüfung greift beim nächsten Aufruf.
func isDir(parent string, item os.DirEntry) bool {
	if item.IsDir() {
		return true
	}
	if item.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(parent, item.Name()))
	return err == nil && info.IsDir()
}

// IsGitRepo meldet, ob das Verzeichnis ein `.git` enthält.
func IsGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
