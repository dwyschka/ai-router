## Context

Greenfield-Repository: außer `openspec/` und `.claude/` existiert nichts. Motivation siehe `proposal.md — Why`, das geforderte Verhalten steht in `specs/`.

Rahmenbedingungen, die die Architektur bestimmen:

- **Ein Nutzer, ein Host.** Der Router läuft entweder lokal auf dem Mac oder auf einer Homelab-Maschine, die über VPN erreichbar ist. Kein Mandantenmodell, keine horizontale Skalierung — der Prozess, der die PTYs hält, ist derselbe, der HTTP ausliefert.
- **Der Router hat volle Rechte des startenden Users.** Er führt `git` aus, startet Agent-Prozesse und liest Verzeichnisse. Die Root-Allowlist und die Token-Prüfung sind damit die einzige Grenze, die es gibt.
- **Vorgegebener Stack** (vom Nutzer entschieden): Go-Backend, React-Frontend, eigener PTY-Daemon mit Scrollback-Ringpuffer, beide Agent-Runtimes hinter einer Adapterschicht.
- **Unix-only.** PTYs über `creack/pty`; Windows ist ausdrücklich außen vor.

## Goals / Non-Goals

**Goals:**

- Ein einzelnes Binary, das die WebUI mitbringt — `scp` auf den Server, starten, fertig.
- Sessions überleben jeden Client-Disconnect; Reattach liefert lückenlosen Verlauf.
- Neue Agent-Runtimes ohne Codeänderung ergänzbar.
- Dieselbe Binary bedient den lokalen und den Netzwerk-Fall, unterschieden allein durch Konfiguration.

**Non-Goals:**

- Sessions überleben *keinen* Neustart des Routers. Der Ringpuffer liegt im Speicher; nach dem Neustart ist nur noch das Session-Log lesbar (siehe `session-lifecycle`).
- Kein Multi-User, keine Rollen, keine Benutzerverwaltung — ein Token, alles oder nichts.
- Kein Editor, kein Datei-Viewer, kein Git-UI. Der File-Explorer wählt Pfade aus, mehr nicht.
- Keine eigene Umsetzung der Agent-Protokolle. Der Router kennt nur Prozess, PTY und Bytes; was Claude Code oder OpenCode damit tun, ist deren Sache.

## Decisions

### Ein Prozess hält PTYs, HTTP und WebSockets

Der Router ist ein Go-Prozess. HTTP-Routing über `go-chi/chi`, PTYs über `creack/pty`, WebSockets über `coder/websocket` (moderneres API, `context`-nativ, keine Hijack-Tricks gegenüber `gorilla/websocket`).

*Alternative — getrennter Session-Daemon plus Frontend-Server:* saubere Trennung, aber der Nutzen (Router-Neustart ohne Session-Verlust) wird durch tmux als Backend billiger erreicht, und die IPC-Schicht kostet echte Komplexität. Verworfen.

*Alternative — tmux als Session-Backend:* hätte Sessions über Router-Neustarts gerettet, macht aber tmux zur harten Abhängigkeit und das Output-Handling fummelig (Scrollback über `capture-pane`, Escape-Sequenzen doppelt interpretiert). Vom Nutzer bewusst abgewählt; die Konsequenz steht als Non-Goal oben und als Requirement „Verhalten beim Neustart des Routers" in der Spec.

### Session-Hub: ein Goroutine-Owner pro Session

Jede Session bekommt eine Owner-Goroutine, die als einzige das PTY liest und schreibt. Sie hält:

- den **Ringpuffer** (Default 1 MiB, konfigurierbar) mit einem monoton wachsenden Byte-Offset,
- die Menge der **Subscriber** (je ein gepufferter Channel pro WebSocket),
- den **Log-Writer** (append-only Datei).

Attach läuft als Snapshot-unter-Lock: der Hub nimmt unter demselben Lock den Ringpuffer-Inhalt *und* trägt den neuen Subscriber ein. Damit gibt es kein Fenster, in dem Bytes zwischen Snapshot und Subscription verloren gehen oder doppelt ankommen — genau die Anforderung „Ausgabe während des Replays".

Ein langsamer Client darf den Agent nicht ausbremsen: Läuft sein Channel voll, wird er verworfen und mit einem `truncated`-Hinweis getrennt. Er kann sofort neu attachen und bekommt dann den aktuellen Ringpuffer.

*Alternative — jeder WebSocket liest selbst vom PTY:* geht nicht, ein PTY-Master hat genau einen Leser. Fan-out muss zentral passieren.

### Resize folgt der kleinsten Client-Größe

Bei mehreren Clients pro Session hält der Hub die gemeldeten Größen je Subscriber und setzt das PTY auf das elementweise Minimum. Das ist die einzige Regel, bei der kein Client abgeschnittene TUI-Ausgabe sieht. Trennt sich ein Client, wird neu berechnet.

*Alternative — „letzter Resize gewinnt":* einfacher, aber ein großer Desktop-Client zerschießt die Ansicht des Handy-Clients. Verworfen.

### Runtime-Adapter als Daten, nicht als Code

Eine Runtime ist eine Struktur: `id`, `displayName`, `command`, `defaultArgs`, `env`. Die eingebauten Definitionen für `claude-code` und `opencode` sind dieselbe Struktur, nur im Binary vorkompiliert; Konfigurationseinträge überschreiben sie per `id`. Es gibt bewusst *kein* Go-Interface pro Runtime — ein Interface würde runtime-spezifisches Verhalten suggerieren, das es nicht gibt.

Prozessstart immer als Argumentvektor über `exec.Command`, nie über eine Shell. Damit sind Pfade mit Leerzeichen und Argumente mit Sonderzeichen unkritisch und es existiert keine Kommandoinjektion über Session-Argumente.

Verfügbarkeit via `exec.LookPath` beim Katalogabruf, nicht gecacht — sonst zeigt der Katalog nach einer Installation tagelang „nicht verfügbar".

### Pfadprüfung an genau einer Stelle

Eine Funktion `resolveWithin(roots, input) (string, error)`: `filepath.Abs` → `filepath.EvalSymlinks` → Präfixvergleich auf Segmentgrenze (also `/a/b` erlaubt `/a/b/c`, aber nicht `/a/bc`). Jeder Handler, der einen Pfad vom Client annimmt, geht durch sie. Existiert der Pfad noch nicht (Projekt-Init), wird der nächstgelegene existierende Elternpfad aufgelöst und geprüft.

Die Reihenfolge ist Teil der Spec und nicht verhandelbar: **erst** Root-Prüfung (`403`), **dann** Existenzprüfung (`404`). Andernfalls verrät der Statuscode, welche Pfade außerhalb der Roots existieren.

### Persistenz: JSON-Datei mit atomarem Rewrite

Projekte und Session-Metadaten liegen in einer einzigen JSON-Datei unter dem State-Verzeichnis (Default `~/.local/state/project-router`, per Config überschreibbar). Schreiben immer als Temp-Datei plus `os.Rename`. Session-Logs daneben als `sessions/<id>.log`.

Beim Start werden alle Sessions, die als `running` vermerkt sind, auf `exited` gesetzt — der Prozess, der sie hielt, existiert nicht mehr.

*Alternative — SQLite:* korrekt und überdimensioniert. Bei einem einzelnen Nutzer mit ein paar Dutzend Projekten kostet es eine cgo- oder Pure-Go-Abhängigkeit ohne Gegenwert. Der Wechsel bleibt später möglich, weil der Store hinter einem schmalen Interface liegt.

### Auth: ein Bearer-Token, Query-Parameter für WebSockets

Ein statisches Token, gesetzt per Config oder `ROUTER_TOKEN`. HTTP-API prüft den `Authorization`-Header; die Browser-WebSocket-API kann keine Header setzen, deshalb wird das Token dort als Query-Parameter akzeptiert und der Vergleich läuft konstant-zeitig (`subtle.ConstantTimeCompare`).

Für den `networked`-Modus ist das Token Startbedingung: fehlt es, öffnet der Router keinen Port. Zusätzlich prüft der WebSocket-Upgrade den `Origin`-Header gegen die konfigurierte Basis-URL, damit eine beliebige Webseite im Browser des Nutzers nicht heimlich attachen kann.

Im `local`-Modus ist das Token optional, weil auf dem Loopback bereits die Prozessgrenze des Users schützt.

### Frontend: React + Vite + xterm.js, eingebettet per `embed.FS`

`web/` baut nach `web/dist`, Go bettet das Verzeichnis ein und liefert es als SPA-Fallback aus. Kein zweiter Prozess im Betrieb; im Entwicklungsmodus proxyt der Vite-Dev-Server auf das Go-Backend.

Terminal-Rendering mit `@xterm/xterm` plus `fit`-Addon; der Fit-Wert wird debounced als Resize-Nachricht gesendet. Der WebSocket transportiert Binärframes für PTY-Bytes in beide Richtungen und JSON-Textframes für Steuernachrichten (`resize`, `status`, `truncated`, `exit`). Die Unterscheidung über den Frame-Typ spart eine Enkodierungsschicht und hält den heißen Pfad frei von JSON.

### API-Zuschnitt

REST für alles Zustandsbehaftete, WebSocket nur für das Terminal:

- `GET /api/fs?path=` — Verzeichnisse auflisten
- `GET|POST /api/projects`, `DELETE /api/projects/{id}`
- `GET /api/runtimes`
- `GET|POST /api/sessions`, `DELETE /api/sessions/{id}`
- `WS /api/sessions/{id}/attach`

## Risks / Trade-offs

- **Router-Neustart killt alle Sessions** → Bewusst akzeptiert. Abfederung: Session-Logs bleiben lesbar, Status wird ehrlich auf `exited` gesetzt statt „läuft" vorzutäuschen. Wer das nicht will, kann den Router selbst unter tmux oder als systemd-Service mit `Restart=no` fahren.
- **Ringpuffer im Speicher, viele Sessions** → 1 MiB pro Session ist die Obergrenze; bei 20 offenen Sessions also ~20 MiB. Konfigurierbar, und der Log auf Platte hält ohnehin den vollen Verlauf.
- **Der Dienst ist eine Remote-Code-Execution-Maschine per Design** → Ein Token, das leakt, gibt vollen Shell-Zugriff im Kontext des Users. Abfederung: `networked`-Modus erzwingt das Token, Origin-Prüfung beim Upgrade, Root-Allowlist begrenzt zumindest, wo Projekte liegen dürfen. Der Betrieb gehört hinter VPN und nicht ins offene Netz — das ist eine Betriebsannahme, keine technische Garantie.
- **Ringpuffer schneidet mitten in einer Escape-Sequenz** → Nach dem Abschneiden kann das Terminal in einem halben Zustand starten. Abfederung: Beim Replay wird ein Terminal-Reset vorangestellt und die Kürzung dem Client gemeldet.
- **Agent-CLIs ändern ihr Verhalten** → Die Adapterschicht kennt nur Kommando und Argumente; ändert Claude Code oder OpenCode seine Flags, reicht ein Eintrag in der Konfiguration. Genau dafür sind Runtimes Daten und kein Code.
- **`git init` in einem falsch gewählten Verzeichnis** → Nur auf ausdrückliche Anforderung, nur innerhalb der Roots, und der File-Explorer markiert bestehende Repositories, damit die Auswahl nicht raten muss.

## Migration Plan

Kein Bestandssystem, keine Migration. Rollout:

1. `go build` erzeugt das Binary inklusive eingebetteter WebUI.
2. Lokal starten: ohne Konfiguration, Loopback, Roots per Flag oder Config.
3. Für den Serverbetrieb: Konfiguration mit `mode: networked`, Bind-Adresse, Token und Roots; als systemd-Unit oder Container hinter das VPN.

Rollback ist das Beenden des Prozesses — der Router hält keinen Zustand, den andere Werkzeuge brauchen; Repositories bleiben unangetastet, die Registry ist eine löschbare JSON-Datei.

## Open Questions

- Soll das Session-Log rotiert oder nach einer Aufbewahrungsfrist gelöscht werden? Bis dahin wächst es unbegrenzt. Betrifft weder Specs noch Aufgabenschnitt und lässt sich später ergänzen.
- Lohnt ein optionaler `--open`-Schalter, der beim Start den Browser öffnet? Reine Bequemlichkeit.
