package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenLegtVerzeichnisseAn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, "sessions")); err != nil || !info.IsDir() {
		t.Fatalf("sessions/-Unterordner fehlt: %v", err)
	}
	if got := s.SessionLogPath("abc"); got != filepath.Join(dir, "sessions", "abc.log") {
		t.Errorf("SessionLogPath = %q", got)
	}
}

func TestUpdateUndSnapshot(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = s.Update(func(st *State) error {
		st.Projects = append(st.Projects, Project{ID: "p1", Name: "alpha", Path: "/tmp/alpha"})
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	snap := s.Snapshot()
	if len(snap.Projects) != 1 || snap.Projects[0].ID != "p1" {
		t.Fatalf("Snapshot = %+v", snap)
	}
	// Der Snapshot ist eine Kopie: Änderungen daran dürfen den Store nicht berühren.
	snap.Projects[0].Name = "geändert"
	if s.Snapshot().Projects[0].Name != "alpha" {
		t.Error("Snapshot teilt sich den Speicher mit dem Store")
	}
}

func TestUpdateFehlerLaesstZustandUnveraendert(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(st *State) error {
		st.Projects = append(st.Projects, Project{ID: "p1"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("abgebrochen")
	err = s.Update(func(st *State) error {
		st.Projects = append(st.Projects, Project{ID: "p2"})
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Fehler = %v, erwartet sentinel", err)
	}
	if len(s.Snapshot().Projects) != 1 {
		t.Errorf("abgebrochenes Update hat den Zustand verändert: %+v", s.Snapshot())
	}
}

func TestAbgebrochenerSchreibvorgangLaesstDateiIntakt(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(st *State) error {
		st.Projects = []Project{{ID: "p1", Name: "alpha"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	vorher, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Ein Schreibvorgang, der in fn abbricht, darf die Datei nicht anfassen.
	_ = s.Update(func(st *State) error {
		st.Projects = nil
		return errors.New("Abbruch")
	})

	nachher, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(vorher) != string(nachher) {
		t.Fatalf("Datei wurde verändert:\nvorher: %s\nnachher: %s", vorher, nachher)
	}
	// Und es bleibt keine Temp-Datei liegen.
	items, _ := os.ReadDir(dir)
	for _, item := range items {
		if strings.HasPrefix(item.Name(), ".state-") {
			t.Errorf("Temp-Datei %s blieb liegen", item.Name())
		}
	}
}

func TestLadenBeimStart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(st *State) error {
		st.Projects = []Project{{ID: "p1", Name: "alpha", Path: "/tmp/alpha"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	wieder, err := Open(dir)
	if err != nil {
		t.Fatalf("erneutes Open: %v", err)
	}
	if len(wieder.Snapshot().Projects) != 1 {
		t.Fatalf("Projekte nach Neustart = %+v", wieder.Snapshot().Projects)
	}
}

func TestKaputteDateiBrichtStartAb(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	kaputt := []byte("{ das ist kein json")
	if err := os.WriteFile(path, kaputt, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(dir)
	if err == nil {
		t.Fatal("erwartet Fehler bei beschädigter Datei")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("Fehler nennt den Pfad nicht: %v", err)
	}
	nachher, _ := os.ReadFile(path)
	if string(nachher) != string(kaputt) {
		t.Error("beschädigte Datei wurde überschrieben")
	}
}

func TestRunningSessionsWerdenBeimLadenBeendet(t *testing.T) {
	dir := t.TempDir()
	start := time.Now().Add(-time.Hour)
	vorbereitet := State{
		Sessions: []Session{
			{ID: "s1", Status: StatusRunning, StartedAt: start},
			{ID: "s2", Status: StatusExited, StartedAt: start},
		},
	}
	raw, err := json.Marshal(vorbereitet)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sessions := s.Snapshot().Sessions
	if len(sessions) != 2 {
		t.Fatalf("Sessions = %+v", sessions)
	}
	if sessions[0].Status != StatusExited {
		t.Errorf("Status von s1 = %q, erwartet exited", sessions[0].Status)
	}
	if sessions[0].EndedAt == nil {
		t.Error("Endzeit von s1 fehlt")
	}
	if sessions[1].Status != StatusExited {
		t.Errorf("Status von s2 wurde verändert: %q", sessions[1].Status)
	}
}
