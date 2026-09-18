package fsbrowse

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"project-router/internal/pathsafe"
)

// gerüst legt ein temporäres Verzeichnisgerüst an und liefert den Root-Pfad.
func gerüst(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(base, "projects", "beta", ".git"), 0o755))
	must(os.MkdirAll(filepath.Join(base, "projects", "alpha"), 0o755))
	must(os.MkdirAll(filepath.Join(base, "projects", "leer"), 0o755))
	must(os.WriteFile(filepath.Join(base, "projects", "notiz.txt"), []byte("hallo"), 0o644))
	return base
}

func TestRootsAlsEinstieg(t *testing.T) {
	base := gerüst(t)
	b := New([]string{filepath.Join(base, "projects"), filepath.Join(base, "zweit")})
	if err := os.MkdirAll(filepath.Join(base, "zweit"), 0o755); err != nil {
		t.Fatal(err)
	}
	listing, err := b.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !listing.IsRoots {
		t.Error("IsRoots sollte gesetzt sein")
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("Einträge = %v, erwartet zwei Roots", listing.Entries)
	}
	if listing.Entries[0].Name != "projects" || listing.Entries[1].Name != "zweit" {
		t.Errorf("Roots nicht alphabetisch sortiert: %v", listing.Entries)
	}
}

func TestUnterverzeichnisAuflisten(t *testing.T) {
	base := gerüst(t)
	root := filepath.Join(base, "projects")
	b := New([]string{root})
	listing, err := b.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var names []string
	for _, e := range listing.Entries {
		names = append(names, e.Name)
	}
	if len(names) != 3 || names[0] != "alpha" || names[1] != "beta" || names[2] != "leer" {
		t.Fatalf("Einträge = %v, erwartet [alpha beta leer] ohne notiz.txt", names)
	}
	if listing.Parent != "" {
		t.Errorf("Parent = %q, erwartet leer (Elternpfad liegt außerhalb der Roots)", listing.Parent)
	}
}

func TestElternpfadInnerhalbDerRoots(t *testing.T) {
	base := gerüst(t)
	b := New([]string{base})
	listing, err := b.List(filepath.Join(base, "projects"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Parent != base {
		t.Errorf("Parent = %q, erwartet %q", listing.Parent, base)
	}
}

func TestGitRepoMarkierung(t *testing.T) {
	base := gerüst(t)
	root := filepath.Join(base, "projects")
	listing, err := New([]string{root}).List(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range listing.Entries {
		if e.Name == "beta" && !e.IsGitRepo {
			t.Error("beta enthält .git und sollte markiert sein")
		}
		if e.Name == "alpha" && e.IsGitRepo {
			t.Error("alpha enthält kein .git und darf nicht markiert sein")
		}
	}
}

func TestLeeresVerzeichnis(t *testing.T) {
	base := gerüst(t)
	root := filepath.Join(base, "projects")
	listing, err := New([]string{root}).List(filepath.Join(root, "leer"))
	if err != nil {
		t.Fatalf("leeres Verzeichnis darf kein Fehler sein: %v", err)
	}
	if len(listing.Entries) != 0 {
		t.Errorf("Einträge = %v, erwartet leere Liste", listing.Entries)
	}
}

func TestPfadZeigtAufDatei(t *testing.T) {
	base := gerüst(t)
	root := filepath.Join(base, "projects")
	_, err := New([]string{root}).List(filepath.Join(root, "notiz.txt"))
	if !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("Fehler = %v, erwartet ErrNotDirectory", err)
	}
}

func TestPfadExistiertNicht(t *testing.T) {
	base := gerüst(t)
	root := filepath.Join(base, "projects")
	_, err := New([]string{root}).List(filepath.Join(root, "gibtesnicht"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Fehler = %v, erwartet ErrNotFound", err)
	}
}

func TestPfadAusserhalbDerRoots(t *testing.T) {
	base := gerüst(t)
	root := filepath.Join(base, "projects")
	b := New([]string{root})
	if _, err := b.List("/etc"); !errors.Is(err, pathsafe.ErrOutsideRoots) {
		t.Fatalf("Fehler = %v, erwartet ErrOutsideRoots", err)
	}
	// Nicht existierender Pfad außerhalb: Root-Prüfung schlägt zuerst zu.
	if _, err := b.List(filepath.Join(base, "gibtesnicht", "auchnicht")); !errors.Is(err, pathsafe.ErrOutsideRoots) {
		t.Fatalf("Fehler = %v, erwartet ErrOutsideRoots statt ErrNotFound", err)
	}
}

func TestKeineLeseberechtigung(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("als root greifen Dateirechte nicht")
	}
	base := gerüst(t)
	gesperrt := filepath.Join(base, "gesperrt")
	if err := os.MkdirAll(gesperrt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(gesperrt, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(gesperrt, 0o755) })

	_, err := New([]string{base}).List(gesperrt)
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("Fehler = %v, erwartet ErrPermission statt leerer Liste", err)
	}
}
