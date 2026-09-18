package projects

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"project-router/internal/pathsafe"
	"project-router/internal/store"
)

func setup(t *testing.T) (*Registry, *store.FileStore, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "projects")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	return New(st, []string{root}), st, root
}

func repo(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCreateGueltigesRepository(t *testing.T) {
	reg, _, root := setup(t)
	dir := repo(t, root, "alpha")

	v, err := reg.Create(CreateRequest{Path: dir})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if v.ID == "" || v.Name != "alpha" || v.Path != dir {
		t.Fatalf("Projekt = %+v", v)
	}
	list := reg.List()
	if len(list) != 1 || list[0].ID != v.ID {
		t.Fatalf("Liste = %+v", list)
	}
}

func TestCreateKeinGitRepository(t *testing.T) {
	reg, st, root := setup(t)
	dir := filepath.Join(root, "ohne-git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := reg.Create(CreateRequest{Path: dir})
	if !errors.Is(err, ErrNotGitRepo) {
		t.Fatalf("Fehler = %v, erwartet ErrNotGitRepo", err)
	}
	if len(st.Snapshot().Projects) != 0 {
		t.Error("es wurde trotz Fehler ein Projekt gespeichert")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		t.Error("ohne ausdrückliche Anforderung darf kein Repository initialisiert werden")
	}
}

func TestCreateAusserhalbDerRoots(t *testing.T) {
	reg, _, _ := setup(t)
	if _, err := reg.Create(CreateRequest{Path: "/etc"}); !errors.Is(err, pathsafe.ErrOutsideRoots) {
		t.Fatalf("Fehler = %v, erwartet ErrOutsideRoots", err)
	}
}

func TestCreateDoppelterPfadLiefertBestehendesProjekt(t *testing.T) {
	reg, st, root := setup(t)
	dir := repo(t, root, "alpha")

	erst, err := reg.Create(CreateRequest{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	// Derselbe Pfad, nur anders geschrieben.
	zweit, err := reg.Create(CreateRequest{Path: filepath.Join(root, ".", "alpha") + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if zweit.ID != erst.ID {
		t.Errorf("zweite Registrierung ergab neue Kennung: %q vs %q", zweit.ID, erst.ID)
	}
	if len(st.Snapshot().Projects) != 1 {
		t.Errorf("Duplikat gespeichert: %+v", st.Snapshot().Projects)
	}
}

func TestInitialisierungAngefordert(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git nicht im PATH")
	}
	reg, _, root := setup(t)
	dir := filepath.Join(root, "neu")

	v, err := reg.Create(CreateRequest{Path: dir, Init: true})
	if err != nil {
		t.Fatalf("Create mit Init: %v", err)
	}
	if !v.IsGitRepo {
		t.Error("Projekt sollte als Git-Repository ausgewiesen sein")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf(".git fehlt: %v", err)
	}
}

func TestInitialisierungElternpfadAusserhalbDerRoots(t *testing.T) {
	reg, _, _ := setup(t)
	_, err := reg.Create(CreateRequest{Path: "/etc/gibtesnicht/neu", Init: true})
	if !errors.Is(err, pathsafe.ErrOutsideRoots) {
		t.Fatalf("Fehler = %v, erwartet ErrOutsideRoots (403)", err)
	}
}

func TestInitialisierungSchlaegtFehl(t *testing.T) {
	reg, st, root := setup(t)
	dir := filepath.Join(root, "neu")
	// git aus dem PATH nehmen, damit `git init` scheitert.
	t.Setenv("PATH", filepath.Join(root, "leerer-pfad"))

	_, err := reg.Create(CreateRequest{Path: dir, Init: true})
	if err == nil {
		t.Fatal("erwartet Fehler, wenn git nicht auffindbar ist")
	}
	if !strings.Contains(err.Error(), "git init") {
		t.Errorf("Fehler gibt die git-Ausgabe nicht durch: %v", err)
	}
	if len(st.Snapshot().Projects) != 0 {
		t.Error("trotz gescheitertem git init wurde ein Projekt angelegt")
	}
}

func TestListeMitLaufendenSessions(t *testing.T) {
	reg, st, root := setup(t)
	v, err := reg.Create(CreateRequest{Path: repo(t, root, "alpha")})
	if err != nil {
		t.Fatal(err)
	}
	err = st.Update(func(s *store.State) error {
		s.Sessions = append(s.Sessions,
			store.Session{ID: "s1", ProjectID: v.ID, Status: store.StatusRunning, StartedAt: time.Now()},
			store.Session{ID: "s2", ProjectID: v.ID, Status: store.StatusRunning, StartedAt: time.Now()},
			store.Session{ID: "s3", ProjectID: v.ID, Status: store.StatusExited, StartedAt: time.Now()},
		)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	list := reg.List()
	if list[0].RunningSessions != 2 {
		t.Errorf("laufende Sessions = %d, erwartet 2", list[0].RunningSessions)
	}
}

func TestVerschwundenerPfad(t *testing.T) {
	reg, _, root := setup(t)
	dir := repo(t, root, "alpha")
	v, err := reg.Create(CreateRequest{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("Projekt sollte gelistet bleiben: %+v", list)
	}
	if list[0].Available {
		t.Error("Projekt sollte als nicht verfügbar markiert sein")
	}
	if _, err := reg.Resolve(v.ID); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Resolve = %v, erwartet ErrUnavailable", err)
	}
}

func TestRemoveOhneSessions(t *testing.T) {
	reg, _, root := setup(t)
	dir := repo(t, root, "alpha")
	v, err := reg.Create(CreateRequest{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Remove(v.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(reg.List()) != 0 {
		t.Error("Projekt ist noch gelistet")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("Remove hat Dateien gelöscht: %v", err)
	}
}

func TestRemoveMitLaufendenSessions(t *testing.T) {
	reg, st, root := setup(t)
	v, err := reg.Create(CreateRequest{Path: repo(t, root, "alpha")})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(func(s *store.State) error {
		s.Sessions = append(s.Sessions, store.Session{ID: "s1", ProjectID: v.ID, Status: store.StatusRunning})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err = reg.Remove(v.ID)
	if !errors.Is(err, ErrHasRunningSessions) {
		t.Fatalf("Fehler = %v, erwartet ErrHasRunningSessions", err)
	}
	if !strings.Contains(err.Error(), "beenden") {
		t.Errorf("Fehler nennt den nötigen Schritt nicht: %v", err)
	}
	if len(reg.List()) != 1 {
		t.Error("Projekt wurde trotz laufender Session entfernt")
	}
}

func TestRemoveUnbekannt(t *testing.T) {
	reg, _, _ := setup(t)
	if err := reg.Remove("gibtesnicht"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Fehler = %v, erwartet ErrNotFound", err)
	}
}

func TestKennungUeberlebtNeustart(t *testing.T) {
	reg, st, root := setup(t)
	dir := repo(t, root, "alpha")
	v, err := reg.Create(CreateRequest{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	wieder, err := store.Open(st.StateDir())
	if err != nil {
		t.Fatal(err)
	}
	neu := New(wieder, []string{root})
	list := neu.List()
	if len(list) != 1 || list[0].ID != v.ID {
		t.Fatalf("Projekt nach Neustart = %+v", list)
	}
}
