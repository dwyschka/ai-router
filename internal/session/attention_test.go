package session

import (
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// testWatcher ist ein Beobachter, den der Test von außen auslösen kann. So prüft
// dieses Paket nur seine eigene Naht, nicht die Erkennung.
type testWatcher struct {
	mu      sync.Mutex
	gesehen []byte
	eingabe int
	zu      bool
	fire    func(string)
}

func (w *testWatcher) Observe(chunk []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gesehen = append(w.gesehen, chunk...)
}

func (w *testWatcher) Input() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.eingabe++
}

func (w *testWatcher) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.zu = true
}

func (w *testWatcher) zustand() (int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.eingabe, w.zu
}

// startMitWatcher startet eine Session, die ihre Ausgabe an den Testbeobachter meldet.
func startMitWatcher(t *testing.T, name string, args ...string) (*Session, *testWatcher) {
	t.Helper()
	w := &testWatcher{}
	cmd := exec.Command(name, args...)
	cmd.Dir = t.TempDir()
	s := Start(Config{
		ID:          "test",
		ProjectID:   "p1",
		RuntimeID:   "dummy",
		Cmd:         cmd,
		InitialSize: Size{Cols: 80, Rows: 24},
		BufferBytes: 4096,
		LogPath:     filepath.Join(t.TempDir(), "test.log"),
		Meta:        WatchMeta{ProjectName: "projekt"},
		Watch: func(meta WatchMeta, fire func(string)) Watcher {
			w.fire = fire
			return w
		},
	})
	t.Cleanup(func() { _ = s.Stop(100 * time.Millisecond) })
	return s, w
}

func TestWatcherSiehtDieAusgabe(t *testing.T) {
	s, w := startMitWatcher(t, "sh", "-c", "printf 'hallo'")
	s.Wait()

	w.mu.Lock()
	gesehen := string(w.gesehen)
	w.mu.Unlock()
	if gesehen != "hallo" {
		t.Errorf("Beobachter sah %q, erwartet die Ausgabe der Session", gesehen)
	}
}

func TestRueckfrageErreichtDieSubscriber(t *testing.T) {
	s, w := startMitWatcher(t, "sleep", "2")
	sub := s.Attach(Size{Cols: 80, Rows: 24})
	defer sub.Detach()

	w.fire("Do you want to proceed?")

	frame := naechsterFrame(t, sub, FrameAttention)
	if frame.Message != "Do you want to proceed?" {
		t.Errorf("Message = %q, erwartet den gemeldeten Ausschnitt", frame.Message)
	}
	if s.Attention() != "Do you want to proceed?" {
		t.Errorf("Attention = %q, erwartet die offene Rückfrage", s.Attention())
	}
}

// Wer tippt, beantwortet die Rückfrage: der Hinweis wird zurückgenommen, und der
// Beobachter erfährt davon.
func TestEingabeNimmtDieRueckfrageZurueck(t *testing.T) {
	s, w := startMitWatcher(t, "sh", "-c", "read x; sleep 1")
	sub := s.Attach(Size{Cols: 80, Rows: 24})
	defer sub.Detach()

	w.fire("Do you want to proceed?")
	naechsterFrame(t, sub, FrameAttention)

	if err := s.Write([]byte("j\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	frame := naechsterFrame(t, sub, FrameAttention)
	if frame.Message != "" {
		t.Errorf("Message = %q, erwartet eine leere Meldung als Rücknahme", frame.Message)
	}
	if s.Attention() != "" {
		t.Errorf("Attention = %q, erwartet leer nach der Eingabe", s.Attention())
	}
	warteAuf(t, time.Second, "die Eingabemeldung am Beobachter", func() bool {
		n, _ := w.zustand()
		return n > 0
	})
}

func TestEndeSchliesstDenWatcher(t *testing.T) {
	s, w := startMitWatcher(t, "true")
	s.Wait()
	warteAuf(t, time.Second, "das Schließen des Beobachters", func() bool {
		_, zu := w.zustand()
		return zu
	})
}

// Eine beendete Session meldet keine Rückfrage mehr — sonst bliebe ein Hinweis
// stehen, den niemand mehr beantworten kann.
func TestBeendeteSessionMeldetNichtMehr(t *testing.T) {
	s, w := startMitWatcher(t, "true")
	s.Wait()
	w.fire("Do you want to proceed?")
	if s.Attention() != "" {
		t.Errorf("Attention = %q, erwartet leer nach dem Ende", s.Attention())
	}
}

// naechsterFrame wartet auf den nächsten Frame der gesuchten Art.
func naechsterFrame(t *testing.T, sub *Subscription, kind FrameKind) Frame {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case f, ok := <-sub.Frames:
			if !ok {
				t.Fatalf("Kanal geschlossen, bevor ein %s-Frame kam", kind)
			}
			if f.Kind == kind {
				return f
			}
		case <-deadline:
			t.Fatalf("Zeitüberschreitung beim Warten auf einen %s-Frame", kind)
		}
	}
}
