// Package store persistiert Projekte und Session-Metadaten als eine JSON-Datei.
// Geschrieben wird immer als Temp-Datei plus Rename, damit ein abgebrochener
// Schreibvorgang die alte Datei intakt lässt.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionStatus ist der Lebenszyklus-Status einer Session.
type SessionStatus string

const (
	StatusRunning SessionStatus = "running"
	StatusExited  SessionStatus = "exited"
	StatusFailed  SessionStatus = "failed"
)

// Project ist ein registriertes Git-Repository.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
}

// Session sind die persistierten Metadaten einer Agent-Session. Der Scrollback liegt
// im Speicher und wird bewusst nicht persistiert (siehe design.md — Non-Goals).
type Session struct {
	ID        string        `json:"id"`
	ProjectID string        `json:"projectId"`
	RuntimeID string        `json:"runtimeId"`
	Args      []string      `json:"args,omitempty"`
	Status    SessionStatus `json:"status"`
	StartedAt time.Time     `json:"startedAt"`
	EndedAt   *time.Time    `json:"endedAt,omitempty"`
	ExitCode  *int          `json:"exitCode,omitempty"`
	Error     string        `json:"error,omitempty"`
	LogPath   string        `json:"logPath,omitempty"`
}

// Runtime ist eine in der WebUI hinterlegte Agent-Runtime. Sie ergänzt den Katalog
// oder ersetzt — bei gleicher Kennung — eine ausgelieferte Definition.
type Runtime struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"displayName"`
	Command     string            `json:"command"`
	DefaultArgs []string          `json:"defaultArgs,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
}

// State ist der gesamte persistierte Zustand.
type State struct {
	Projects []Project `json:"projects"`
	Sessions []Session `json:"sessions"`
	Runtimes []Runtime `json:"runtimes,omitempty"`
}

// Store ist das schmale Interface, hinter dem die Persistenz liegt — ein späterer
// Wechsel auf SQLite bliebe damit lokal.
type Store interface {
	Snapshot() State
	Update(func(*State) error) error
	StateDir() string
	SessionLogPath(sessionID string) string
}

// FileStore hält den Zustand im Speicher und schreibt ihn nach jeder Änderung.
type FileStore struct {
	mu       sync.RWMutex
	state    State
	path     string
	stateDir string
}

// Open legt das State-Verzeichnis samt sessions/-Unterordner an und lädt die Datei.
// Eine unparsbare Datei bricht mit Pfadangabe ab und wird nicht überschrieben.
func Open(stateDir string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Join(stateDir, "sessions"), 0o700); err != nil {
		return nil, fmt.Errorf("State-Verzeichnis %s: %w", stateDir, err)
	}
	fs := &FileStore{
		path:     filepath.Join(stateDir, "state.json"),
		stateDir: stateDir,
	}
	if err := fs.load(); err != nil {
		return nil, err
	}
	return fs, nil
}

func (s *FileStore) load() error {
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.state = State{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("Zustandsdatei %s nicht lesbar: %w", s.path, err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("Zustandsdatei %s ist beschädigt und wurde nicht überschrieben: %w", s.path, err)
	}
	// Der Prozess, der diese Sessions hielt, existiert nicht mehr.
	now := time.Now()
	for i := range state.Sessions {
		if state.Sessions[i].Status == StatusRunning {
			state.Sessions[i].Status = StatusExited
			if state.Sessions[i].EndedAt == nil {
				ended := now
				state.Sessions[i].EndedAt = &ended
			}
		}
	}
	s.state = state
	return nil
}

// Snapshot liefert eine Kopie des Zustands.
func (s *FileStore) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return State{
		Projects: append([]Project(nil), s.state.Projects...),
		Sessions: append([]Session(nil), s.state.Sessions...),
		Runtimes: append([]Runtime(nil), s.state.Runtimes...),
	}
}

// Update wendet fn auf den Zustand an und schreibt ihn atomar. Schlägt fn fehl,
// bleibt der Zustand unverändert.
func (s *FileStore) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	draft := State{
		Projects: append([]Project(nil), s.state.Projects...),
		Sessions: append([]Session(nil), s.state.Sessions...),
		Runtimes: append([]Runtime(nil), s.state.Runtimes...),
	}
	if err := fn(&draft); err != nil {
		return err
	}
	if err := writeAtomic(s.path, draft); err != nil {
		return err
	}
	s.state = draft
	return nil
}

func (s *FileStore) StateDir() string { return s.stateDir }

// SessionLogPath ist der Pfad des append-only Logs einer Session.
func (s *FileStore) SessionLogPath(sessionID string) string {
	return filepath.Join(s.stateDir, "sessions", sessionID+".log")
}

// writeAtomic schreibt in eine Temp-Datei im selben Verzeichnis und benennt sie um.
func writeAtomic(path string, state State) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.json")
	if err != nil {
		return fmt.Errorf("Zustandsdatei %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op nach erfolgreichem Rename

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("Zustandsdatei %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("Zustandsdatei %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("Zustandsdatei %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
