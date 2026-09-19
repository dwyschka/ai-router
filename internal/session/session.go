// Package session hält die Agent-Prozesse: pro Session eine Owner-Goroutine, die als
// einzige das PTY liest, den Ringpuffer füllt, das Log schreibt und die Ausgabe an alle
// Subscriber verteilt.
package session

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"project-router/internal/store"
)

// Fehlerfälle, die Handler und WebSocket-Schicht auswerten.
var (
	// ErrSessionEnded: Eingabe an eine beendete Session.
	ErrSessionEnded = errors.New("Session ist beendet")
)

// FrameKind unterscheidet PTY-Bytes von Steuernachrichten.
type FrameKind string

const (
	FrameData      FrameKind = "data"
	FrameStatus    FrameKind = "status"
	FrameTruncated FrameKind = "truncated"
	FrameExit      FrameKind = "exit"
	// FrameAttention meldet, dass der Agent auf eine Entscheidung wartet. Eine
	// leere Message hebt die Meldung wieder auf.
	FrameAttention FrameKind = "attention"
)

// Watcher beobachtet die Ausgabe einer Session, um Rückfragen zu erkennen. Die
// Implementierung liegt außerhalb dieses Pakets; hier steht nur die Naht.
//
// Observe läuft im heißen Pfad unter dem Session-Lock und darf deshalb weder
// blockieren noch den Meldeweg synchron auslösen.
type Watcher interface {
	// Observe bekommt jeden Ausgabe-Chunk der Session.
	Observe(chunk []byte)
	// Input meldet, dass der Nutzer etwas geschickt hat — eine offene Rückfrage
	// gilt damit als beantwortet.
	Input()
	// Close gibt den Beobachter am Ende der Session frei.
	Close()
}

// WatchMeta benennt die Session gegenüber dem Beobachter.
type WatchMeta struct {
	SessionID   string
	ProjectID   string
	ProjectName string
	RuntimeID   string
}

// WatchFunc erzeugt den Beobachter einer Session. fire meldet eine erkannte
// Rückfrage; der Aufruf kommt aus einer eigenen Goroutine, nie aus Observe.
type WatchFunc func(meta WatchMeta, fire func(excerpt string)) Watcher

// Frame ist eine Nachricht an einen Subscriber.
type Frame struct {
	Kind     FrameKind           `json:"type"`
	Data     []byte              `json:"-"`
	Status   store.SessionStatus `json:"status,omitempty"`
	ExitCode *int                `json:"exitCode,omitempty"`
	Message  string              `json:"message,omitempty"`
}

// TerminalReset (RIS) stellt das Terminal auf einen definierten Zustand zurück.
const TerminalReset = "\x1bc"

// failedStartWindow: endet der Prozess innerhalb dieser Spanne mit Fehler, gilt der
// Start als fehlgeschlagen.
const failedStartWindow = 2 * time.Second

// subscriberQueue ist die Puffertiefe je Subscriber. Läuft sie voll, wird der
// Subscriber verworfen, statt den Agent-Prozess zu bremsen.
const subscriberQueue = 256

// Size ist eine gemeldete Terminalgröße.
type Size struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type subscriber struct {
	id      uint64
	frames  chan Frame
	size    Size
	dropped bool
	closed  bool
}

// Session ist eine laufende oder beendete Agent-Session.
type Session struct {
	id        string
	projectID string
	runtimeID string
	args      []string

	mu          sync.Mutex
	buf         *ringBuffer
	subs        map[uint64]*subscriber
	droppedIDs  map[uint64]bool
	nextSubID   uint64
	currentSize Size

	ptmx     *os.File
	cmd      *exec.Cmd
	logFile  *os.File
	logBroke bool

	// watcher erkennt Rückfragen in der Ausgabe; attention hält die zuletzt
	// erkannte, solange sie unbeantwortet ist.
	watcher     Watcher
	attention   string
	attentionAt *time.Time

	status    store.SessionStatus
	stopping  bool // vom Nutzer angefordertes Beenden, kein fehlgeschlagener Start
	exitCode  *int
	errMsg    string
	startedAt time.Time
	endedAt   *time.Time
	logPath   string

	done      chan struct{}
	closeOnce sync.Once

	// persist meldet Statusänderungen an den Store.
	persist func(store.Session)
}

// Config beschreibt eine zu startende Session.
type Config struct {
	ID          string
	ProjectID   string
	RuntimeID   string
	Args        []string
	Cmd         *exec.Cmd
	InitialSize Size
	BufferBytes int
	LogPath     string
	Persist     func(store.Session)
	// Watch erzeugt den Beobachter für Rückfragen. Nil heißt: keine Erkennung.
	Watch WatchFunc
	Meta  WatchMeta
}

// Start erzeugt das PTY, startet den Prozess und die Owner-Goroutine. Scheitert der
// Start selbst, entsteht eine Session im Status `failed`, deren Fehlerausgabe im
// Scrollback nachlesbar ist.
func Start(cfg Config) *Session {
	s := &Session{
		id:         cfg.ID,
		projectID:  cfg.ProjectID,
		runtimeID:  cfg.RuntimeID,
		args:       cfg.Args,
		buf:        newRingBuffer(cfg.BufferBytes),
		subs:       map[uint64]*subscriber{},
		droppedIDs: map[uint64]bool{},
		cmd:        cfg.Cmd,
		status:     store.StatusRunning,
		startedAt:  time.Now().UTC(),
		logPath:    cfg.LogPath,
		done:       make(chan struct{}),
		persist:    cfg.Persist,
	}
	s.openLog()
	if cfg.Watch != nil {
		meta := cfg.Meta
		if meta.SessionID == "" {
			meta.SessionID = cfg.ID
		}
		s.watcher = cfg.Watch(meta, s.raiseAttention)
	}

	size := cfg.InitialSize
	if size.Cols == 0 || size.Rows == 0 {
		size = Size{Cols: 80, Rows: 24}
	}
	ptmx, err := pty.StartWithSize(cfg.Cmd, &pty.Winsize{Cols: size.Cols, Rows: size.Rows})
	if err != nil {
		msg := fmt.Sprintf("Start fehlgeschlagen: %v\r\n", err)
		s.mu.Lock()
		s.writeOut([]byte(msg))
		s.mu.Unlock()
		s.finish(store.StatusFailed, nil, err.Error())
		return s
	}
	s.ptmx = ptmx
	go s.readLoop()
	s.publishMeta()
	return s
}

// openLog öffnet das append-only Log. Scheitert das, läuft die Session weiter und der
// Fehler wird protokolliert.
func (s *Session) openLog() {
	if s.logPath == "" {
		return
	}
	f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		s.logBroke = true
		log.Printf("Session %s: Log %s nicht schreibbar, Session läuft weiter: %v", s.id, s.logPath, err)
		return
	}
	s.logFile = f
}

// readLoop ist die Owner-Goroutine: einziger Leser des PTY.
func (s *Session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.mu.Lock()
			s.writeOut(chunk)
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	s.reap()
}

// writeOut schreibt in Ringpuffer und Log und verteilt an alle Subscriber.
// Muss unter s.mu laufen, damit Attach keine Bytes verpasst oder doppelt sieht.
func (s *Session) writeOut(chunk []byte) {
	s.buf.Write(chunk)
	if s.logFile != nil {
		if _, err := s.logFile.Write(chunk); err != nil && !s.logBroke {
			s.logBroke = true
			log.Printf("Session %s: Schreiben ins Log %s fehlgeschlagen, Session läuft weiter: %v", s.id, s.logPath, err)
		}
	}
	if s.watcher != nil {
		s.watcher.Observe(chunk)
	}
	s.broadcast(Frame{Kind: FrameData, Data: chunk})
}

// raiseAttention hält eine erkannte Rückfrage fest und meldet sie den Clients. Der
// Aufruf kommt aus der Goroutine des Beobachters, nie aus writeOut.
func (s *Session) raiseAttention(excerpt string) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != store.StatusRunning {
		return
	}
	s.attention = excerpt
	s.attentionAt = &now
	s.broadcast(Frame{Kind: FrameAttention, Message: excerpt})
}

// clearAttentionLocked nimmt eine offene Rückfrage zurück und meldet das — ein
// Attention-Frame ohne Message. Muss unter s.mu laufen.
func (s *Session) clearAttentionLocked() {
	if s.attention == "" {
		return
	}
	s.attention = ""
	s.attentionAt = nil
	s.broadcast(Frame{Kind: FrameAttention})
}

// Attention liefert die offene Rückfrage, oder eine leere Zeichenkette.
func (s *Session) Attention() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attention
}

// broadcast verteilt einen Frame. Ein volllaufender Subscriber wird verworfen und
// getrennt, statt den Agent-Prozess zu bremsen. Muss unter s.mu laufen.
func (s *Session) broadcast(f Frame) {
	for id, sub := range s.subs {
		if sub.closed {
			continue
		}
		select {
		case sub.frames <- f:
		default:
			sub.dropped = true
			sub.closed = true
			close(sub.frames)
			s.droppedIDs[id] = true
			delete(s.subs, id)
		}
	}
	s.applySizeLocked()
}

// reap wartet auf das Prozessende und setzt den Endstatus.
func (s *Session) reap() {
	err := s.cmd.Wait()
	code := 0
	if s.cmd.ProcessState != nil {
		code = s.cmd.ProcessState.ExitCode()
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}

	s.mu.Lock()
	angefordert := s.stopping
	s.mu.Unlock()

	status := store.StatusExited
	// Ein Prozess, der unmittelbar nach dem Start mit Fehler endet, gilt als
	// fehlgeschlagener Start, nicht als reguläres Ende. Ein vom Nutzer angefordertes
	// Beenden ist dagegen immer ein reguläres Ende.
	if !angefordert && code != 0 && time.Since(s.startedAt) < failedStartWindow {
		status = store.StatusFailed
	}
	s.finish(status, &code, msg)
}

// finish setzt den Endstatus genau einmal, benachrichtigt alle Subscriber und gibt
// das PTY frei.
func (s *Session) finish(status store.SessionStatus, code *int, msg string) {
	s.closeOnce.Do(func() {
		now := time.Now().UTC()
		s.mu.Lock()
		s.status = status
		s.exitCode = code
		s.errMsg = msg
		s.endedAt = &now
		s.clearAttentionLocked()
		s.broadcast(Frame{Kind: FrameExit, Status: status, ExitCode: code, Message: msg})
		for id, sub := range s.subs {
			if !sub.closed {
				sub.closed = true
				close(sub.frames)
			}
			delete(s.subs, id)
		}
		if s.logFile != nil {
			_ = s.logFile.Close()
			s.logFile = nil
		}
		s.mu.Unlock()
		if s.watcher != nil {
			s.watcher.Close()
		}
		if s.ptmx != nil {
			_ = s.ptmx.Close()
		}
		// Erst persistieren, dann done schließen: wer auf das Ende wartet, sieht
		// anschließend auch den persistierten Endstatus.
		s.publishMeta()
		close(s.done)
	})
}

// Subscription ist der Zugang eines Clients zu einer Session.
type Subscription struct {
	// Snapshot ist der Scrollback zum Zeitpunkt des Attach.
	Snapshot []byte
	// Truncated meldet, dass älterer Verlauf abgeschnitten wurde.
	Truncated bool
	// Frames liefert alles, was nach dem Snapshot passiert.
	Frames <-chan Frame

	session *Session
	id      uint64
}

// Attach nimmt Scrollback-Snapshot und Subscriber-Eintrag unter demselben Lock vor.
// Damit gibt es kein Fenster, in dem Bytes verloren gehen oder doppelt ankommen.
func (s *Session) Attach(size Size) *Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, _, truncated := s.buf.Snapshot()
	if truncated {
		// Nach einer Kürzung kann der Verlauf mitten in einer Escape-Sequenz
		// beginnen: ein Reset stellt das Terminal auf einen definierten Zustand.
		snapshot = append([]byte(TerminalReset), snapshot...)
	}
	sub := &subscriber{
		id:     s.nextSubID,
		frames: make(chan Frame, subscriberQueue),
		size:   size,
	}
	s.nextSubID++

	if s.status != store.StatusRunning {
		// Beendete Session: Verlauf ausliefern, Kanal sofort schließen.
		sub.closed = true
		close(sub.frames)
	} else {
		s.subs[sub.id] = sub
		s.applySizeLocked()
	}

	return &Subscription{
		Snapshot:  snapshot,
		Truncated: truncated,
		Frames:    sub.frames,
		session:   s,
		id:        sub.id,
	}
}

// Dropped meldet, ob der Subscriber wegen eines vollgelaufenen Puffers verworfen wurde.
func (sub *Subscription) Dropped() bool {
	sub.session.mu.Lock()
	defer sub.session.mu.Unlock()
	s, ok := sub.session.subs[sub.id]
	if !ok {
		return sub.session.droppedIDs[sub.id]
	}
	return s.dropped
}

// Detach entfernt den Subscriber und berechnet die PTY-Größe neu.
func (sub *Subscription) Detach() {
	s := sub.session
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.subs[sub.id]; ok {
		if !existing.closed {
			existing.closed = true
			close(existing.frames)
		}
		delete(s.subs, sub.id)
	}
	s.applySizeLocked()
}

// Resize meldet die Größe dieses Clients; das PTY folgt dem elementweisen Minimum
// aller gemeldeten Größen.
func (sub *Subscription) Resize(size Size) {
	s := sub.session
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.subs[sub.id]; ok {
		existing.size = size
	}
	s.applySizeLocked()
}

// applySizeLocked setzt das PTY auf das elementweise Minimum aller Subscriber-Größen.
func (s *Session) applySizeLocked() {
	if s.ptmx == nil || s.status != store.StatusRunning {
		return
	}
	var min Size
	for _, sub := range s.subs {
		if sub.size.Cols == 0 || sub.size.Rows == 0 {
			continue
		}
		if min.Cols == 0 || sub.size.Cols < min.Cols {
			min.Cols = sub.size.Cols
		}
		if min.Rows == 0 || sub.size.Rows < min.Rows {
			min.Rows = sub.size.Rows
		}
	}
	if min.Cols == 0 || min.Rows == 0 {
		return // kein Client mit gemeldeter Größe: letzte Größe bleibt stehen
	}
	if min == s.currentSize {
		return
	}
	if err := pty.Setsize(s.ptmx, &pty.Winsize{Cols: min.Cols, Rows: min.Rows}); err != nil {
		log.Printf("Session %s: Terminalgröße konnte nicht gesetzt werden: %v", s.id, err)
		return
	}
	s.currentSize = min
}

// Write reicht Eingabe unverändert an das PTY durch, inklusive Steuerzeichen.
func (s *Session) Write(p []byte) error {
	s.mu.Lock()
	ended := s.status != store.StatusRunning || s.ptmx == nil
	ptmx := s.ptmx
	watcher := s.watcher
	if !ended {
		// Wer tippt, beantwortet die offene Rückfrage — auch wenn er nur scrollt
		// oder Ctrl-C schickt. Eine neue Rückfrage meldet sich erneut.
		s.clearAttentionLocked()
	}
	s.mu.Unlock()
	if !ended && watcher != nil {
		watcher.Input()
	}
	if ended {
		return ErrSessionEnded
	}
	_, err := ptmx.Write(p)
	if err != nil {
		if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
			return ErrSessionEnded
		}
		return err
	}
	return nil
}

// Stop sendet ein Terminierungssignal und beendet den Prozess nach der Karenzzeit hart.
func (s *Session) Stop(grace time.Duration) error {
	s.mu.Lock()
	running := s.status == store.StatusRunning && s.cmd != nil && s.cmd.Process != nil
	if !running {
		s.mu.Unlock()
		// Die Session endete bereits von selbst: auf den Abschluss warten, damit
		// nach Stop auch der Endstatus persistiert ist.
		select {
		case <-s.done:
		case <-time.After(grace):
		}
		return nil
	}
	proc := s.cmd.Process
	s.stopping = true
	s.mu.Unlock()

	signalProcess(proc, syscall.SIGTERM)
	select {
	case <-s.done:
		return nil
	case <-time.After(grace):
	}
	signalProcess(proc, syscall.SIGKILL)
	select {
	case <-s.done:
	case <-time.After(grace):
	}
	return nil
}

// signalProcess schickt das Signal an die Prozessgruppe des PTY; sie existiert, weil
// pty.Start eine eigene Session mit dem Prozess als Gruppenführer aufmacht.
func signalProcess(proc *os.Process, sig syscall.Signal) {
	if proc == nil {
		return
	}
	if err := syscall.Kill(-proc.Pid, sig); err != nil {
		_ = proc.Signal(sig)
	}
}

// Wait blockiert bis zum Prozessende.
func (s *Session) Wait() { <-s.done }

// Done wird geschlossen, sobald die Session beendet ist.
func (s *Session) Done() <-chan struct{} { return s.done }

// Release gibt den Ringpuffer einer beendeten Session frei.
func (s *Session) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Reset()
}

// Meta sind die persistierbaren Metadaten der Session.
func (s *Session) Meta() store.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metaLocked()
}

func (s *Session) metaLocked() store.Session {
	meta := store.Session{
		ID:        s.id,
		ProjectID: s.projectID,
		RuntimeID: s.runtimeID,
		Args:      s.args,
		Status:    s.status,
		StartedAt: s.startedAt,
		EndedAt:   s.endedAt,
		ExitCode:  s.exitCode,
		Error:     s.errMsg,
		LogPath:   s.logPath,
	}
	return meta
}

func (s *Session) publishMeta() {
	if s.persist == nil {
		return
	}
	s.persist(s.Meta())
}

// ID liefert die Kennung der Session.
func (s *Session) ID() string { return s.id }

// Status liefert den aktuellen Status.
func (s *Session) Status() store.SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// CurrentSize ist die zuletzt am PTY gesetzte Größe.
func (s *Session) CurrentSize() Size {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentSize
}
