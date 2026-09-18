package session

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"project-router/internal/store"
)

// startSess startet eine Session mit einem Dummy-Kommando.
func startSess(t *testing.T, bufBytes int, name string, args ...string) *Session {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = t.TempDir()
	s := Start(Config{
		ID:          "test",
		ProjectID:   "p1",
		RuntimeID:   "dummy",
		Cmd:         cmd,
		InitialSize: Size{Cols: 80, Rows: 24},
		BufferBytes: bufBytes,
		LogPath:     filepath.Join(t.TempDir(), "test.log"),
	})
	t.Cleanup(func() { _ = s.Stop(100 * time.Millisecond) })
	return s
}

// collect sammelt Datenbytes eines Subscribers bis zum Schließen des Kanals.
func collect(sub *Subscription) []byte {
	var out []byte
	for f := range sub.Frames {
		if f.Kind == FrameData {
			out = append(out, f.Data...)
		}
	}
	return out
}

// warteAuf pollt bis die Bedingung erfüllt ist oder die Frist abläuft.
func warteAuf(t *testing.T, frist time.Duration, was string, bedingung func() bool) {
	t.Helper()
	deadline := time.Now().Add(frist)
	for time.Now().Before(deadline) {
		if bedingung() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Zeitüberschreitung beim Warten auf %s", was)
}

func TestFanOutAnMehrereSubscriber(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "sleep 0.05; printf 'hallo welt'; sleep 0.3")
	a := s.Attach(Size{Cols: 80, Rows: 24})
	b := s.Attach(Size{Cols: 80, Rows: 24})

	var wg sync.WaitGroup
	var gotA, gotB []byte
	wg.Add(2)
	go func() { defer wg.Done(); gotA = collect(a) }()
	go func() { defer wg.Done(); gotB = collect(b) }()

	s.Wait()
	wg.Wait()

	if !strings.Contains(string(gotA), "hallo welt") {
		t.Errorf("Client A sah %q", gotA)
	}
	if !bytes.Equal(gotA, gotB) {
		t.Errorf("Clients sahen unterschiedliche Ausgabe:\nA: %q\nB: %q", gotA, gotB)
	}
}

func TestAttachLiefertScrollbackUndDannLive(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "printf 'erste zeile\\n'; sleep 0.3; printf 'zweite zeile\\n'; sleep 0.2")

	warteAuf(t, 2*time.Second, "erste Ausgabe", func() bool {
		snap, _, _ := s.snapshotForTest()
		return strings.Contains(string(snap), "erste")
	})

	sub := s.Attach(Size{Cols: 80, Rows: 24})
	if !strings.Contains(string(sub.Snapshot), "erste zeile") {
		t.Fatalf("Scrollback = %q, erwartet die erste Zeile", sub.Snapshot)
	}
	if strings.Contains(string(sub.Snapshot), "zweite") {
		t.Fatalf("Scrollback enthält bereits spätere Ausgabe: %q", sub.Snapshot)
	}

	live := collect(sub)
	if !strings.Contains(string(live), "zweite zeile") {
		t.Errorf("Live-Stream = %q, erwartet die zweite Zeile", live)
	}
	if strings.Contains(string(live), "erste zeile") {
		t.Errorf("erste Zeile kam doppelt: %q", live)
	}
}

func TestAttachWaehrendAusgabeVerliertNichtsUndDupliziertNichts(t *testing.T) {
	// Der Agent gibt durchgehend nummerierte Zeilen aus, während ein Client attacht.
	s := startSess(t, 1<<20, "sh", "-c", "i=1; while [ $i -le 400 ]; do printf 'zeile-%d\\n' $i; i=$((i+1)); done; sleep 0.2")

	time.Sleep(10 * time.Millisecond)
	sub := s.Attach(Size{Cols: 200, Rows: 50})
	gesamt := append(append([]byte(nil), sub.Snapshot...), collect(sub)...)
	s.Wait()

	text := string(gesamt)
	for i := 1; i <= 400; i++ {
		marke := "zeile-" + itoa(i)
		erste := strings.Index(text, marke+"\r\n")
		if erste < 0 {
			t.Fatalf("%s fehlt im Verlauf", marke)
		}
		if strings.Count(text, marke+"\r\n") != 1 {
			t.Fatalf("%s kommt %d-mal vor, erwartet genau einmal", marke, strings.Count(text, marke+"\r\n"))
		}
		if i > 1 {
			vorher := strings.Index(text, "zeile-"+itoa(i-1)+"\r\n")
			if vorher > erste {
				t.Fatalf("Reihenfolge verletzt: %s steht vor zeile-%d", marke, i-1)
			}
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var out []byte
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	return string(out)
}

func TestLangsamerSubscriberWirdVerworfen(t *testing.T) {
	s := startSess(t, 1<<20, "sh", "-c", "i=0; while [ $i -lt 20000 ]; do printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\\n'; i=$((i+1)); done; sleep 0.2")

	langsam := s.Attach(Size{Cols: 80, Rows: 24})
	schnell := s.Attach(Size{Cols: 80, Rows: 24})

	// Der schnelle Client liest weiter, der langsame gar nicht.
	gelesen := make(chan int)
	go func() {
		n := 0
		for range schnell.Frames {
			n++
		}
		gelesen <- n
	}()

	warteAuf(t, 10*time.Second, "Verwerfen des langsamen Subscribers", func() bool {
		return langsam.Dropped()
	})

	// Der Agent-Prozess läuft dabei weiter bzw. läuft zu Ende.
	s.Wait()
	if n := <-gelesen; n == 0 {
		t.Error("der schnelle Client bekam keine Frames")
	}
	// Der Kanal des verworfenen Subscribers läuft leer und ist dann geschlossen.
	geschlossen := false
	for range langsam.Frames {
	}
	select {
	case _, ok := <-langsam.Frames:
		geschlossen = !ok
	default:
	}
	if !geschlossen {
		t.Error("der Kanal des verworfenen Subscribers sollte geschlossen sein")
	}
}

func TestStatusUebergaengeUndExitCode(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "sleep 0.2; exit 0")
	if s.Status() != store.StatusRunning {
		t.Fatalf("Status = %q, erwartet running", s.Status())
	}
	sub := s.Attach(Size{Cols: 80, Rows: 24})

	var exitFrame *Frame
	for f := range sub.Frames {
		if f.Kind == FrameExit {
			cp := f
			exitFrame = &cp
		}
	}
	s.Wait()

	if s.Status() != store.StatusExited {
		t.Errorf("Status = %q, erwartet exited", s.Status())
	}
	if exitFrame == nil {
		t.Fatal("kein Exit-Frame an den Subscriber")
	}
	if exitFrame.ExitCode == nil || *exitFrame.ExitCode != 0 {
		t.Errorf("Exit-Code = %v, erwartet 0", exitFrame.ExitCode)
	}
	meta := s.Meta()
	if meta.EndedAt == nil {
		t.Error("Endzeit fehlt")
	}
}

func TestSofortigerFehlschlagErgibtFailed(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "echo 'fehlermeldung des agenten' >&2; exit 3")
	s.Wait()

	if s.Status() != store.StatusFailed {
		t.Errorf("Status = %q, erwartet failed", s.Status())
	}
	if code := s.Meta().ExitCode; code == nil || *code != 3 {
		t.Errorf("Exit-Code = %v, erwartet 3", code)
	}
	snap, _, _ := s.snapshotForTest()
	if !strings.Contains(string(snap), "fehlermeldung des agenten") {
		t.Errorf("Scrollback = %q, erwartet die Fehlerausgabe", snap)
	}
}

func TestStartEinesUnbekanntenKommandosErgibtFailed(t *testing.T) {
	cmd := exec.Command(filepath.Join(t.TempDir(), "gibtesnicht"))
	s := Start(Config{ID: "x", Cmd: cmd, BufferBytes: 4096, InitialSize: Size{Cols: 80, Rows: 24}})
	s.Wait()
	if s.Status() != store.StatusFailed {
		t.Fatalf("Status = %q, erwartet failed", s.Status())
	}
	snap, _, _ := s.snapshotForTest()
	if !strings.Contains(string(snap), "Start fehlgeschlagen") {
		t.Errorf("Scrollback = %q, erwartet die Fehlerausgabe im Verlauf", snap)
	}
}

func TestEingabeWirdDurchgereicht(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "read zeile; printf 'gelesen:%s\\n' \"$zeile\"; sleep 0.2")
	sub := s.Attach(Size{Cols: 80, Rows: 24})

	time.Sleep(100 * time.Millisecond)
	if err := s.Write([]byte("hallo\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := append(append([]byte(nil), sub.Snapshot...), collect(sub)...)
	if !strings.Contains(string(got), "gelesen:hallo") {
		t.Errorf("Ausgabe = %q, erwartet das Echo der Eingabe", got)
	}
}

func TestSteuerzeichenErreichenDasPTY(t *testing.T) {
	// Der Trap belegt, dass Ctrl-C als Signal am Prozess ankommt.
	s := startSess(t, 4096, "sh", "-c", "trap 'printf \"unterbrochen\\n\"; exit 0' INT; sleep 5")
	sub := s.Attach(Size{Cols: 80, Rows: 24})
	time.Sleep(200 * time.Millisecond)
	if err := s.Write([]byte{0x03}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := collect(sub)
	s.Wait()
	if !strings.Contains(string(got), "unterbrochen") {
		t.Errorf("Ausgabe = %q, erwartet die Reaktion auf Ctrl-C", got)
	}
}

func TestEingabeAnBeendeteSession(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "exit 0")
	s.Wait()
	if err := s.Write([]byte("hallo")); err != ErrSessionEnded {
		t.Fatalf("Fehler = %v, erwartet ErrSessionEnded", err)
	}
}

func TestResizeEinzelnerClient(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "sleep 1")
	sub := s.Attach(Size{Cols: 80, Rows: 24})
	sub.Resize(Size{Cols: 120, Rows: 40})

	warteAuf(t, time.Second, "Übernahme der Größe", func() bool {
		return s.CurrentSize() == Size{Cols: 120, Rows: 40}
	})
}

func TestResizeFolgtDemMinimumZweierClients(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "sleep 2")
	gross := s.Attach(Size{Cols: 200, Rows: 60})
	klein := s.Attach(Size{Cols: 80, Rows: 24})
	gross.Resize(Size{Cols: 200, Rows: 60})
	klein.Resize(Size{Cols: 80, Rows: 24})

	warteAuf(t, time.Second, "elementweises Minimum", func() bool {
		return s.CurrentSize() == Size{Cols: 80, Rows: 24}
	})

	// Gemischt: schmal aber hoch gegen breit aber niedrig.
	klein.Resize(Size{Cols: 60, Rows: 90})
	warteAuf(t, time.Second, "gemischtes Minimum", func() bool {
		return s.CurrentSize() == Size{Cols: 60, Rows: 60}
	})

	// Trennt sich der kleine Client, gilt wieder die Größe des großen.
	klein.Detach()
	warteAuf(t, time.Second, "Neuberechnung nach Trennung", func() bool {
		return s.CurrentSize() == Size{Cols: 200, Rows: 60}
	})
}

func TestStopTerminiert(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "sleep 30")
	start := time.Now()
	if err := s.Stop(2 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if s.Status() != store.StatusExited {
		t.Errorf("Status = %q, erwartet exited", s.Status())
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("Stop dauerte %v, erwartet schnelle Terminierung", time.Since(start))
	}
}

func TestStopBeendetHartWennDasSignalIgnoriertWird(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "trap '' TERM; sleep 30")
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	if err := s.Stop(300 * time.Millisecond); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if s.Status() != store.StatusExited {
		t.Errorf("Status = %q, erwartet exited", s.Status())
	}
	if time.Since(start) < 300*time.Millisecond {
		t.Error("die Karenzzeit wurde nicht abgewartet")
	}
}

func TestSubscriberWerdenUeberDasEndeInformiert(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "sleep 0.1; exit 7")
	a := s.Attach(Size{Cols: 80, Rows: 24})
	b := s.Attach(Size{Cols: 80, Rows: 24})

	for _, sub := range []*Subscription{a, b} {
		var sah bool
		for f := range sub.Frames {
			if f.Kind == FrameExit && f.ExitCode != nil && *f.ExitCode == 7 {
				sah = true
			}
		}
		if !sah {
			t.Error("ein Subscriber wurde nicht über das Ende informiert")
		}
	}
}

func TestSessionLogWirdGeschrieben(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "sess.log")
	cmd := exec.Command("sh", "-c", "printf 'im log\\n'; sleep 0.1")
	cmd.Dir = t.TempDir()
	s := Start(Config{ID: "log", Cmd: cmd, BufferBytes: 4096, InitialSize: Size{Cols: 80, Rows: 24}, LogPath: logPath})
	s.Wait()

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Log nicht lesbar: %v", err)
	}
	if !strings.Contains(string(raw), "im log") {
		t.Errorf("Log = %q, erwartet die Ausgabe der Session", raw)
	}
}

func TestSessionLaeuftTrotzUnschreibbaremLogWeiter(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("als root greifen Dateirechte nicht")
	}
	dir := filepath.Join(t.TempDir(), "gesperrt")
	if err := os.MkdirAll(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	cmd := exec.Command("sh", "-c", "printf 'laeuft trotzdem\\n'; sleep 0.1")
	cmd.Dir = t.TempDir()
	s := Start(Config{ID: "x", Cmd: cmd, BufferBytes: 4096, InitialSize: Size{Cols: 80, Rows: 24}, LogPath: filepath.Join(dir, "sess.log")})
	sub := s.Attach(Size{Cols: 80, Rows: 24})
	got := append(append([]byte(nil), sub.Snapshot...), collect(sub)...)
	s.Wait()

	if !strings.Contains(string(got), "laeuft trotzdem") {
		t.Errorf("Ausgabe = %q, die Session sollte trotz Log-Fehler weiterlaufen", got)
	}
	if s.Status() != store.StatusExited {
		t.Errorf("Status = %q, erwartet exited", s.Status())
	}
}

func TestReplayStelltResetVoranWennGekuerzt(t *testing.T) {
	s := startSess(t, 64, "sh", "-c", "i=0; while [ $i -lt 200 ]; do printf 'abcdefghij\\n'; i=$((i+1)); done; sleep 0.1")
	s.Wait()

	sub := s.Attach(Size{Cols: 80, Rows: 24})
	if !sub.Truncated {
		t.Fatal("Kürzung wurde nicht gemeldet")
	}
	if !strings.HasPrefix(string(sub.Snapshot), TerminalReset) {
		t.Errorf("Replay beginnt mit %q, erwartet einen vorangestellten Terminal-Reset", sub.Snapshot[:min(8, len(sub.Snapshot))])
	}
}

func TestReplayOhneKuerzungOhneReset(t *testing.T) {
	s := startSess(t, 1<<20, "sh", "-c", "printf 'kurz\\n'; sleep 0.1")
	s.Wait()
	sub := s.Attach(Size{Cols: 80, Rows: 24})
	if sub.Truncated {
		t.Fatal("ohne Überlauf darf nichts als gekürzt gelten")
	}
	if strings.HasPrefix(string(sub.Snapshot), TerminalReset) {
		t.Error("ohne Kürzung gehört kein Reset in den Replay")
	}
}

func TestAttachAnBeendeteSessionLiefertVerlauf(t *testing.T) {
	s := startSess(t, 4096, "sh", "-c", "printf 'verlauf\\n'; sleep 0.1")
	s.Wait()
	sub := s.Attach(Size{Cols: 80, Rows: 24})
	if !strings.Contains(string(sub.Snapshot), "verlauf") {
		t.Errorf("Scrollback = %q", sub.Snapshot)
	}
	if _, ok := <-sub.Frames; ok {
		t.Error("der Kanal einer beendeten Session sollte geschlossen sein")
	}
}
