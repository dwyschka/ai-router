package notify

import (
	"strings"
	"testing"
	"time"
)

// warteAufMeldung sammelt die erste Meldung des Detectors.
func warteAufMeldung(t *testing.T, gemeldet <-chan string, frist time.Duration) (string, bool) {
	t.Helper()
	select {
	case msg := <-gemeldet:
		return msg, true
	case <-time.After(frist):
		return "", false
	}
}

func neuerDetector(t *testing.T, idle time.Duration, extra ...string) (*Detector, <-chan string) {
	t.Helper()
	patterns, err := CompilePatterns(extra, false)
	if err != nil {
		t.Fatalf("CompilePatterns: %v", err)
	}
	gemeldet := make(chan string, 8)
	d := NewDetector(patterns, idle, func(excerpt string) { gemeldet <- excerpt })
	t.Cleanup(d.Close)
	return d, gemeldet
}

func TestMeldetRueckfrageNachStille(t *testing.T) {
	d, gemeldet := neuerDetector(t, 30*time.Millisecond)
	d.Observe([]byte("Do you want to proceed?\r\n"))

	msg, ok := warteAufMeldung(t, gemeldet, time.Second)
	if !ok {
		t.Fatal("keine Meldung, erwartet wurde die erkannte Rückfrage")
	}
	if !strings.Contains(msg, "Do you want to proceed?") {
		t.Errorf("Ausschnitt = %q, erwartet die Frage selbst", msg)
	}
}

func TestMeldetNichtSolangeAusgabeLaeuft(t *testing.T) {
	d, gemeldet := neuerDetector(t, 120*time.Millisecond)
	d.Observe([]byte("Do you want to proceed?\r\n"))
	// Ein arbeitender Agent schreibt weiter: die Uhr wird laufend zurückgestellt.
	for i := 0; i < 5; i++ {
		time.Sleep(30 * time.Millisecond)
		d.Observe([]byte("."))
	}
	if msg, ok := warteAufMeldung(t, gemeldet, 50*time.Millisecond); ok {
		t.Fatalf("Meldung %q, obwohl die Ausgabe nie zur Ruhe kam", msg)
	}
}

func TestMeldetGewoehnlicheAusgabeNicht(t *testing.T) {
	d, gemeldet := neuerDetector(t, 20*time.Millisecond)
	d.Observe([]byte("Reading src/main.go … 42 Zeilen geschrieben.\r\n$ "))

	if msg, ok := warteAufMeldung(t, gemeldet, 200*time.Millisecond); ok {
		t.Fatalf("Meldung %q, erwartet keine — das ist keine Rückfrage", msg)
	}
}

func TestMeldetDieselbeRueckfrageNurEinmal(t *testing.T) {
	d, gemeldet := neuerDetector(t, 20*time.Millisecond)
	d.Observe([]byte("Do you want to proceed? (y/n)"))
	if _, ok := warteAufMeldung(t, gemeldet, time.Second); !ok {
		t.Fatal("erste Meldung fehlt")
	}

	// Die TUI zeichnet denselben Kasten neu — das ist keine neue Entscheidung.
	d.Observe([]byte("\x1b[2J\x1b[HDo you want to proceed? (y/n)"))
	if msg, ok := warteAufMeldung(t, gemeldet, 200*time.Millisecond); ok {
		t.Fatalf("zweite Meldung %q, erwartet keine Wiederholung", msg)
	}
}

func TestEingabeErlaubtEineNeueMeldung(t *testing.T) {
	d, gemeldet := neuerDetector(t, 20*time.Millisecond)
	d.Observe([]byte("Do you want to proceed? (y/n)"))
	if _, ok := warteAufMeldung(t, gemeldet, time.Second); !ok {
		t.Fatal("erste Meldung fehlt")
	}

	d.Input() // beantwortet
	d.Observe([]byte("Do you want to proceed? (y/n)"))
	if _, ok := warteAufMeldung(t, gemeldet, time.Second); !ok {
		t.Fatal("nach beantworteter Rückfrage muss dieselbe Frage wieder melden")
	}
}

func TestGeschlossenerDetectorMeldetNicht(t *testing.T) {
	d, gemeldet := neuerDetector(t, 20*time.Millisecond)
	d.Close()
	d.Observe([]byte("Do you want to proceed?"))
	if msg, ok := warteAufMeldung(t, gemeldet, 200*time.Millisecond); ok {
		t.Fatalf("Meldung %q nach Close", msg)
	}
}

func TestAuswahlmenueUndEigeneMuster(t *testing.T) {
	faelle := []struct {
		name  string
		extra []string
		aus   string
		will  bool
	}{
		{name: "Auswahlmenü", aus: "❯ 1. Yes\r\n  2. No, and tell Claude what to do differently", will: true},
		{name: "Berechtigung", aus: "Allow this command to run?", will: true},
		{name: "deutsch", aus: "Soll ich die Datei überschreiben?", will: true},
		{name: "eigenes Muster", extra: []string{`braucht deine freigabe`}, aus: "Der Agent braucht deine Freigabe", will: true},
		{name: "Fließtext", aus: "Ich habe die Tests ausgeführt, alle grün.", will: false},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			d, gemeldet := neuerDetector(t, 20*time.Millisecond, f.extra...)
			d.Observe([]byte(f.aus))
			msg, ok := warteAufMeldung(t, gemeldet, 500*time.Millisecond)
			if ok != f.will {
				t.Fatalf("Meldung = %v (%q), erwartet %v", ok, msg, f.will)
			}
		})
	}
}

func TestEigeneMusterErsetzenDieEingebauten(t *testing.T) {
	patterns, err := CompilePatterns([]string{`nur dieses`}, true)
	if err != nil {
		t.Fatalf("CompilePatterns: %v", err)
	}
	if len(patterns) != 1 {
		t.Fatalf("%d Muster, erwartet genau das eigene", len(patterns))
	}
}

func TestUngueltigesMusterMeldetFehler(t *testing.T) {
	if _, err := CompilePatterns([]string{"("}, false); err == nil {
		t.Fatal("erwartet ein Fehler für ein unvollständiges Muster")
	}
}
