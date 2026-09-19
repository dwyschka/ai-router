// Package notify erkennt Rückfragen in der Ausgabe einer Agent-Session und meldet
// sie weiter: an die angehängten Browser und, sofern konfiguriert, per Webhook.
//
// Die Erkennung ist bewusst zweistufig. Ein Muster allein wäre zu laut — ein Agent
// schreibt die Frage schon, während er weiterarbeitet. Erst wenn die Ausgabe danach
// still steht, wartet er wirklich.
package notify

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// tailBytes ist die Menge bereinigten Texts, die zur Prüfung vorgehalten wird. Eine
// TUI zeichnet sich laufend neu; mehr als das letzte Bild braucht es nicht.
const tailBytes = 8192

// excerptRunes begrenzt den gemeldeten Ausschnitt.
const excerptRunes = 160

// defaultPatterns sind die eingebauten Muster. Sie werden ohne Rücksicht auf
// Groß-/Kleinschreibung gegen den bereinigten Text geprüft.
var defaultPatterns = []string{
	`do you want to (proceed|continue|make|create|allow|use)`,
	`\((y/n|y/N|N/y|yes/no|j/n)\)`,
	`\[(y/n|y/N|N/y)\]`,
	`press (enter|return) to continue`,
	`wait(ing|s) for (your )?(input|response|approval|confirmation|decision)`,
	`(needs|requires) (your )?(input|approval|permission|confirmation|decision)`,
	`(allow|approve) (this|the) (command|tool|edit|request|action)`,
	`don'?t ask again`,
	`❯\s*\d+\.\s`,
	`\b1\.\s+yes\b`,
	`(möchtest du|willst du|soll ich)[^?\n]{0,60}\?`,
	`(fortfahren|fortsetzen|überschreiben|bestätigen)\s*\?`,
	`\bconfirm\b[^.\n]{0,40}\?`,
	`(select|choose) an option`,
}

// CompilePatterns übersetzt die Muster der Konfiguration. extra ergänzt die
// eingebauten; replace ersetzt sie stattdessen.
func CompilePatterns(extra []string, replace bool) ([]*regexp.Regexp, error) {
	quellen := defaultPatterns
	if replace {
		quellen = nil
	}
	quellen = append(append([]string{}, quellen...), extra...)

	out := make([]*regexp.Regexp, 0, len(quellen))
	for _, muster := range quellen {
		re, err := regexp.Compile("(?i)" + muster)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}

// Detector beobachtet den Ausgabestrom einer Session. Er implementiert
// session.Watcher.
type Detector struct {
	patterns []*regexp.Regexp
	idle     time.Duration
	fire     func(excerpt string)

	mu       sync.Mutex
	strip    stripper
	tail     []byte
	timer    *time.Timer
	gemeldet string
	closed   bool
}

// NewDetector baut einen Beobachter. fire wird aus dem Timer aufgerufen, nie aus
// Observe — der heiße Pfad bleibt frei.
func NewDetector(patterns []*regexp.Regexp, idle time.Duration, fire func(excerpt string)) *Detector {
	d := &Detector{patterns: patterns, idle: idle, fire: fire}
	d.timer = time.AfterFunc(idle, d.pruefe)
	d.timer.Stop()
	return d
}

// Observe nimmt einen Ausgabe-Chunk entgegen: bereinigen, anhängen, Uhr zurückstellen.
// Solange Ausgabe kommt, arbeitet der Agent noch.
func (d *Detector) Observe(chunk []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.tail = d.strip.append(d.tail, chunk)
	if len(d.tail) > tailBytes {
		d.tail = append(d.tail[:0], d.tail[len(d.tail)-tailBytes:]...)
	}
	d.timer.Reset(d.idle)
}

// Input verwirft den gesammelten Text: die Rückfrage ist beantwortet, und die alte
// Bildschirmfläche darf nicht noch einmal auslösen.
func (d *Detector) Input() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.timer.Stop()
	d.tail = d.tail[:0]
	d.gemeldet = ""
}

// Close beendet den Beobachter; danach meldet er nichts mehr.
func (d *Detector) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	d.timer.Stop()
	d.tail = nil
}

// pruefe läuft, wenn die Ausgabe zur Ruhe gekommen ist. Der Meldeweg läuft
// ausdrücklich außerhalb des Locks: er greift zurück in die Session, die ihrerseits
// Observe aufruft.
func (d *Detector) pruefe() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	excerpt := treffer(d.patterns, string(d.tail))
	if excerpt == "" || excerpt == d.gemeldet {
		d.mu.Unlock()
		return
	}
	d.gemeldet = excerpt
	fire := d.fire
	d.mu.Unlock()

	if fire != nil {
		fire(excerpt)
	}
}

// treffer sucht das erste passende Muster und liefert die Zeile, in der es steht.
func treffer(patterns []*regexp.Regexp, text string) string {
	if text == "" {
		return ""
	}
	for _, re := range patterns {
		loc := re.FindStringIndex(text)
		if loc == nil {
			continue
		}
		return zeileUm(text, loc[0], loc[1])
	}
	return ""
}

// zeileUm schneidet die Zeile um einen Treffer heraus und putzt Rahmenzeichen weg,
// mit denen TUIs ihre Kästen malen.
func zeileUm(text string, von, bis int) string {
	start := strings.LastIndexByte(text[:von], '\n') + 1
	ende := strings.IndexByte(text[bis:], '\n')
	if ende < 0 {
		ende = len(text)
	} else {
		ende += bis
	}
	zeile := strings.TrimSpace(strings.Trim(strings.TrimSpace(text[start:ende]), rahmenzeichen))
	zeile = strings.Join(strings.Fields(zeile), " ")

	runen := []rune(zeile)
	if len(runen) > excerptRunes {
		zeile = strings.TrimSpace(string(runen[:excerptRunes])) + "…"
	}
	return zeile
}

// rahmenzeichen sind die Box-Drawing-Zeichen, die am Rand einer Zeile stehen.
const rahmenzeichen = "│┃|╭╮╰╯┌┐└┘─━═┄┈ \t"
