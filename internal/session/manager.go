package session

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"crypto/rand"
	"encoding/hex"

	"project-router/internal/projects"
	"project-router/internal/runtime"
	"project-router/internal/store"
)

// Fehlerfälle des Managers.
var (
	// ErrNotFound: unbekannte Session-Kennung (404).
	ErrNotFound = errors.New("Session nicht gefunden")
	// ErrStillRunning: Entfernen einer laufenden Session (409).
	ErrStillRunning = errors.New("Session läuft noch")
)

// DefaultGrace ist die Karenzzeit zwischen Terminierungssignal und hartem Beenden.
const DefaultGrace = 5 * time.Second

// View ist eine Session so, wie die API sie ausliefert.
type View struct {
	ID          string              `json:"id"`
	ProjectID   string              `json:"projectId"`
	ProjectName string              `json:"projectName,omitempty"`
	RuntimeID   string              `json:"runtimeId"`
	Args        []string            `json:"args,omitempty"`
	Status      store.SessionStatus `json:"status"`
	StartedAt   time.Time           `json:"startedAt"`
	EndedAt     *time.Time          `json:"endedAt,omitempty"`
	ExitCode    *int                `json:"exitCode,omitempty"`
	Error       string              `json:"error,omitempty"`
	// Attention ist die offene Rückfrage dieser Session; leer, solange keine ansteht.
	// Sie wird nicht persistiert: eine Rückfrage überlebt den Prozess nicht.
	Attention string `json:"attention,omitempty"`
}

// Manager startet Sessions und hält die laufenden im Speicher.
type Manager struct {
	store    store.Store
	projects *projects.Registry
	runtimes *runtime.Catalog
	bufBytes int
	grace    time.Duration
	// watch erzeugt den Beobachter für Rückfragen; nil heißt: keine Erkennung.
	watch WatchFunc

	mu     sync.RWMutex
	active map[string]*Session
	// lastSize ist die zuletzt von einem Client gemeldete Startgröße; sie dient als
	// Vorgabe, wenn ein Neustart ohne Größenangabe angefordert wird.
	lastSize Size
}

func NewManager(st store.Store, reg *projects.Registry, cat *runtime.Catalog, bufBytes int) *Manager {
	return &Manager{
		store:    st,
		projects: reg,
		runtimes: cat,
		bufBytes: bufBytes,
		grace:    DefaultGrace,
		active:   map[string]*Session{},
	}
}

// SetGrace setzt die Karenzzeit (für Tests).
func (m *Manager) SetGrace(d time.Duration) { m.grace = d }

// SetWatch hinterlegt die Erkennung von Rückfragen. Ohne Aufruf bleibt sie aus.
// Gilt ab der nächsten gestarteten Session.
func (m *Manager) SetWatch(fn WatchFunc) { m.watch = fn }

// StartRequest beschreibt den Start einer Session.
type StartRequest struct {
	ProjectID string   `json:"projectId"`
	RuntimeID string   `json:"runtimeId"`
	Args      []string `json:"args,omitempty"`
	Cols      uint16   `json:"cols,omitempty"`
	Rows      uint16   `json:"rows,omitempty"`
}

// Start prüft Projekt und Runtime, bevor ein Prozess entsteht, und startet dann die
// Session im Repo-Verzeichnis des Projekts.
func (m *Manager) Start(req StartRequest) (View, error) {
	project, err := m.projects.Resolve(req.ProjectID)
	if err != nil {
		return View{}, err
	}
	entry, err := m.runtimes.Resolve(req.RuntimeID)
	if err != nil {
		return View{}, err
	}

	id, err := newID()
	if err != nil {
		return View{}, err
	}
	if req.Cols > 0 && req.Rows > 0 {
		m.mu.Lock()
		m.lastSize = Size{Cols: req.Cols, Rows: req.Rows}
		m.mu.Unlock()
	}
	cmd := entry.BuildCmd(project.Path, req.Args)

	sess := Start(Config{
		ID:          id,
		ProjectID:   project.ID,
		RuntimeID:   entry.ID,
		Args:        req.Args,
		Cmd:         cmd,
		InitialSize: Size{Cols: req.Cols, Rows: req.Rows},
		BufferBytes: m.bufBytes,
		LogPath:     m.store.SessionLogPath(id),
		Persist:     m.persist,
		Watch:       m.watch,
		Meta: WatchMeta{
			SessionID:   id,
			ProjectID:   project.ID,
			ProjectName: project.Name,
			RuntimeID:   entry.ID,
		},
	})

	m.mu.Lock()
	m.active[id] = sess
	m.mu.Unlock()

	m.persist(sess.Meta())
	return m.viewOf(sess.Meta()), nil
}

func newID() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("Session-Kennung konnte nicht erzeugt werden: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// persist schreibt die Metadaten einer Session in den Store.
func (m *Manager) persist(meta store.Session) {
	_ = m.store.Update(func(st *store.State) error {
		for i := range st.Sessions {
			if st.Sessions[i].ID == meta.ID {
				st.Sessions[i] = meta
				return nil
			}
		}
		st.Sessions = append(st.Sessions, meta)
		return nil
	})
}

// Get liefert eine laufende Session aus dem Speicher.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.active[id]
	return sess, ok
}

// List liefert alle Sessions — laufende wie beendete —, optional gefiltert nach
// Projekt, jüngste zuerst.
func (m *Manager) List(projectID string) []View {
	snapshot := m.store.Snapshot()
	views := make([]View, 0, len(snapshot.Sessions))
	for _, meta := range snapshot.Sessions {
		if projectID != "" && meta.ProjectID != projectID {
			continue
		}
		views = append(views, m.viewOf(meta))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].StartedAt.After(views[j].StartedAt) })
	return views
}

// GetView liefert die Metadaten einer einzelnen Session.
func (m *Manager) GetView(id string) (View, error) {
	for _, meta := range m.store.Snapshot().Sessions {
		if meta.ID == id {
			return m.viewOf(meta), nil
		}
	}
	return View{}, ErrNotFound
}

func (m *Manager) viewOf(meta store.Session) View {
	v := View{
		ID:        meta.ID,
		ProjectID: meta.ProjectID,
		RuntimeID: meta.RuntimeID,
		Args:      meta.Args,
		Status:    meta.Status,
		StartedAt: meta.StartedAt,
		EndedAt:   meta.EndedAt,
		ExitCode:  meta.ExitCode,
		Error:     meta.Error,
	}
	if p, err := m.projects.Get(meta.ProjectID); err == nil {
		v.ProjectName = p.Name
	}
	// Die offene Rückfrage steht nur in der laufenden Session, nicht im Store —
	// deshalb kommt sie hier dazu, damit auch die Übersicht sie zeigen kann.
	if sess, ok := m.Get(meta.ID); ok {
		v.Attention = sess.Attention()
	}
	return v
}

// Restart startet eine frische Session mit denselben Eckdaten — Projekt, Runtime und
// Zusatzargumente — wie die angegebene. Die alte bleibt mit ihrem Verlauf in der Liste
// stehen; die neue bekommt eine eigene Kennung, ein eigenes Log und einen leeren
// Scrollback.
func (m *Manager) Restart(id string) (View, error) {
	var vorlage store.Session
	gefunden := false
	for _, meta := range m.store.Snapshot().Sessions {
		if meta.ID == id {
			vorlage = meta
			gefunden = true
			break
		}
	}
	if !gefunden {
		return View{}, ErrNotFound
	}
	if sess, ok := m.Get(id); ok && sess.Status() == store.StatusRunning {
		return View{}, fmt.Errorf("%w: sie muss nicht neu gestartet werden", ErrStillRunning)
	}

	return m.Start(StartRequest{
		ProjectID: vorlage.ProjectID,
		RuntimeID: vorlage.RuntimeID,
		Args:      vorlage.Args,
		Cols:      m.lastSize.Cols,
		Rows:      m.lastSize.Rows,
	})
}

// Stop beendet eine laufende Session: Signal, Karenzzeit, hartes Beenden.
func (m *Manager) Stop(id string) error {
	sess, ok := m.Get(id)
	if !ok {
		if _, err := m.GetView(id); err != nil {
			return ErrNotFound
		}
		return nil // bereits beendet
	}
	return sess.Stop(m.grace)
}

// Remove entfernt eine beendete Session aus der Liste und gibt ihren Ringpuffer frei.
// Eine laufende Session wird zuvor nicht beendet, sondern abgelehnt.
func (m *Manager) Remove(id string) error {
	sess, ok := m.Get(id)
	if ok && sess.Status() == store.StatusRunning {
		return fmt.Errorf("%w: zuerst beenden", ErrStillRunning)
	}
	if err := m.store.Update(func(st *store.State) error {
		idx := -1
		for i, meta := range st.Sessions {
			if meta.ID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrNotFound
		}
		if st.Sessions[idx].Status == store.StatusRunning {
			return fmt.Errorf("%w: zuerst beenden", ErrStillRunning)
		}
		st.Sessions = append(st.Sessions[:idx:idx], st.Sessions[idx+1:]...)
		return nil
	}); err != nil {
		return err
	}
	if ok {
		sess.Release()
		m.mu.Lock()
		delete(m.active, id)
		m.mu.Unlock()
	}
	return nil
}

// StopAll beendet alle laufenden Sessions, etwa beim Herunterfahren des Routers.
func (m *Manager) StopAll() {
	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.active))
	for _, s := range m.active {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()
	for _, s := range sessions {
		_ = s.Stop(m.grace)
	}
}
