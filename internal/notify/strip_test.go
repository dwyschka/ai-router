package notify

import "testing"

func bereinige(chunks ...string) string {
	var s stripper
	var out []byte
	for _, c := range chunks {
		out = s.append(out, []byte(c))
	}
	return string(out)
}

func TestStripperEntferntSequenzen(t *testing.T) {
	faelle := []struct {
		name string
		ein  string
		will string
	}{
		{name: "Farben", ein: "\x1b[31mrot\x1b[0m", will: "rot"},
		{name: "Titel setzen", ein: "\x1b]0;Titel\x07Text", will: "Text"},
		{name: "Titel mit ST", ein: "\x1b]0;Titel\x1b\\Text", will: "Text"},
		{name: "Reset", ein: "\x1bcText", will: "Text"},
		{name: "Zeichensatz", ein: "\x1b(BText", will: "Text"},
		{name: "Glocke faellt weg", ein: "a\x07b", will: "ab"},
		{name: "Tab wird Leerzeichen", ein: "a\tb", will: "a b"},
		{name: "CRLF ist eine Grenze", ein: "a\r\nb", will: "a\nb"},
		{name: "Cursor positionieren trennt", ein: "a\x1b[2;1Hb", will: "a\nb"},
		{name: "Schirm loeschen trennt", ein: "a\x1b[2Jb", will: "a\nb"},
		{name: "Zeile loeschen trennt nicht", ein: "a\x1b[Kb", will: "ab"},
		{name: "Cursor vor trennt nicht", ein: "a\x1b[3Cb", will: "ab"},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			if got := bereinige(f.ein); got != f.will {
				t.Errorf("bereinigt = %q, erwartet %q", got, f.will)
			}
		})
	}
}

// Eine Sequenz kann zwischen zwei PTY-Lesevorgängen zerfallen: der Zustand muss über
// die Chunk-Grenze hinweg halten, sonst bleibt ihr Rest als Müll im Text stehen.
func TestStripperHaeltZustandUeberChunkGrenzen(t *testing.T) {
	faelle := [][]string{
		{"\x1b", "[31mrot"},
		{"\x1b[", "31mrot"},
		{"\x1b[31", "mrot"},
		{"\x1b]0;Ti", "tel\x07rot"},
	}
	for _, chunks := range faelle {
		if got := bereinige(chunks...); got != "rot" {
			t.Errorf("bereinige(%q) = %q, erwartet %q", chunks, got, "rot")
		}
	}
}

func TestStripperHaeltRueckfrageImKasten(t *testing.T) {
	// So malt eine TUI ihre Nachfrage: Rahmen, Farben, Cursor-Sprünge.
	roh := "\x1b[2J\x1b[H\x1b[1m╭─────────────╮\x1b[0m\r\n" +
		"\x1b[1m│\x1b[0m Do you want to proceed? \x1b[1m│\x1b[0m\r\n" +
		"\x1b[1m│\x1b[0m \x1b[36m❯ 1. Yes\x1b[0m \x1b[1m│\x1b[0m\r\n"

	patterns, err := CompilePatterns(nil, false)
	if err != nil {
		t.Fatalf("CompilePatterns: %v", err)
	}
	got := treffer(patterns, bereinige(roh))
	if got != "Do you want to proceed?" {
		t.Errorf("Ausschnitt = %q, erwartet die Frage ohne Rahmen", got)
	}
}
