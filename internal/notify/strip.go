package notify

import "strings"

// stripper entfernt Escape-Sequenzen und Steuerzeichen aus einem PTY-Strom. Er hält
// seinen Zustand über Chunk-Grenzen hinweg, denn eine Sequenz kann mitten im Lesen
// zerfallen — sonst bliebe ihr Rest als Müll im Text stehen.
type stripper struct {
	state zustand
}

type zustand int

const (
	// text: gewöhnliche Zeichen.
	text zustand = iota
	// escape: direkt nach ESC, die Art der Sequenz ist noch offen.
	escape
	// csi: ESC [ … bis zu einem Endbyte 0x40–0x7e.
	csi
	// zeichensatz: ESC ( ) * + und ein folgendes Byte.
	zeichensatz
	// zeichenkette: OSC/DCS/APC/PM — bis BEL oder ESC \.
	zeichenkette
	// kettenEscape: innerhalb einer Zeichenkette ein ESC gesehen.
	kettenEscape
)

// zeilenwechselnd sind die CSI-Endbytes, die den Cursor in eine andere Zeile setzen:
// Cursor positionieren (H, f, d), bewegen (A, B, E, F) oder den Schirm löschen (J).
// Eine TUI zeichnet sich damit neu, statt Zeilenumbrüche zu schreiben — ohne diese
// Übersetzung liefe der ganze Bildschirm zu einer einzigen Zeile zusammen.
const zeilenwechselnd = "HfdABEFJ"

// append hängt den bereinigten Inhalt von chunk an out an. Zeilenenden bleiben als
// "\n" erhalten, damit sich später die Zeile um einen Treffer herausschneiden lässt.
func (s *stripper) append(out []byte, chunk []byte) []byte {
	for _, b := range chunk {
		switch s.state {
		case text:
			switch {
			case b == 0x1b:
				s.state = escape
			case b == '\n' || b == '\r':
				// Ein TUI setzt den Cursor mit \r zurück; für die Texterkennung
				// ist beides schlicht eine Zeilengrenze.
				out = zeilenende(out)
			case b == '\t':
				out = append(out, ' ')
			case b < 0x20 || b == 0x7f:
				// Übrige Steuerzeichen (Glocke, Backspace …) fallen weg.
			default:
				out = append(out, b)
			}
		case escape:
			switch b {
			case '[':
				s.state = csi
			case ']', 'P', 'X', '^', '_':
				s.state = zeichenkette
			case '(', ')', '*', '+':
				s.state = zeichensatz
			default:
				// Kurze Sequenzen wie ESC c oder ESC 7 sind mit diesem Byte vorbei.
				s.state = text
			}
		case csi:
			if b >= 0x40 && b <= 0x7e {
				if strings.IndexByte(zeilenwechselnd, b) >= 0 {
					out = zeilenende(out)
				}
				s.state = text
			}
		case zeichensatz:
			s.state = text
		case zeichenkette:
			switch b {
			case 0x07:
				s.state = text
			case 0x1b:
				s.state = kettenEscape
			}
		case kettenEscape:
			// ESC \ beendet die Zeichenkette; alles andere gehört noch dazu.
			if b == '\\' {
				s.state = text
			} else {
				s.state = zeichenkette
			}
		}
	}
	return out
}

// zeilenende hängt einen Umbruch an, sofern nicht schon einer dasteht.
func zeilenende(out []byte) []byte {
	if n := len(out); n == 0 || out[n-1] == '\n' {
		return out
	}
	return append(out, '\n')
}
