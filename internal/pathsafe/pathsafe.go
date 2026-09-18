// Package pathsafe enthält die einzige Stelle, an der ein vom Client übergebener Pfad
// gegen die Root-Allowlist geprüft wird: Abs → EvalSymlinks → Präfixvergleich auf
// Segmentgrenze.
package pathsafe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideRoots meldet einen Pfad außerhalb aller erlaubten Roots. Handler bilden
// ihn auf 403 ab — unabhängig davon, ob der Pfad existiert.
var ErrOutsideRoots = errors.New("Pfad liegt außerhalb der erlaubten Roots")

// ResolveWithin normalisiert input und prüft, ob das Ergebnis innerhalb eines der
// roots liegt. Existiert der Pfad noch nicht, wird über den nächstgelegenen
// existierenden Elternpfad aufgelöst, damit auch neu anzulegende Verzeichnisse
// geprüft werden können. Zurückgegeben wird der normalisierte absolute Pfad.
func ResolveWithin(roots []string, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("leerer Pfad")
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("Pfad nicht auflösbar: %w", err)
	}
	resolved, err := resolveExisting(abs)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		resolvedRoot, err := resolveExisting(filepath.Clean(root))
		if err != nil {
			continue
		}
		if within(resolvedRoot, resolved) {
			return resolved, nil
		}
	}
	return "", ErrOutsideRoots
}

// resolveExisting löst Symlinks auf. Für einen noch nicht existierenden Pfad wird der
// nächstgelegene existierende Elternpfad aufgelöst und der Rest wieder angehängt.
func resolveExisting(abs string) (string, error) {
	missing := make([]string, 0, 4)
	current := abs
	for {
		real, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				real = filepath.Join(real, missing[i])
			}
			return filepath.Clean(real), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("Pfad nicht auflösbar: %w", err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Bis zur Wurzel nichts gefunden: der bereinigte Pfad ist das Beste.
			return filepath.Clean(abs), nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// within vergleicht auf Segmentgrenze: /a/b erlaubt /a/b und /a/b/c, aber nicht /a/bc.
func within(root, candidate string) bool {
	if candidate == root {
		return true
	}
	if root == string(filepath.Separator) {
		return strings.HasPrefix(candidate, root)
	}
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}

// Within meldet, ob ein bereits normalisierter Pfad innerhalb eines der Roots liegt.
func Within(roots []string, resolved string) bool {
	for _, root := range roots {
		resolvedRoot, err := resolveExisting(filepath.Clean(root))
		if err != nil {
			continue
		}
		if within(resolvedRoot, resolved) {
			return true
		}
	}
	return false
}
