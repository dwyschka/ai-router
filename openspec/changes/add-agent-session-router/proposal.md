## Why

Wenn ich über VPN an einem Projekt arbeite, laufen Claude-Code- bzw. OpenCode-Instanzen jeweils an das Terminal gebunden, in dem ich sie gestartet habe. Bricht die Verbindung ab oder wechsle ich das Gerät, ist die laufende Session samt Kontext verloren, und ich weiß nicht einmal mehr, welche Instanzen für welches Projekt noch offen sind.

Ein Router löst beides: Die Agent-Prozesse leben serverseitig weiter, der Browser ist nur noch ein austauschbarer Client, der sich wieder anhängt.

## What Changes

- Neuer Dienst **project-router**: Go-Backend (HTTP + WebSocket) plus React-WebUI, als ein Binary auslieferbar (WebUI eingebettet).
- **Projektverwaltung**: bestehende Projekte aus einer Registry auswählen oder ein neues Projekt initialisieren. Ein Projekt ist immer ein Git-Repository; der Pfad wird gegen die erlaubten Roots und auf `.git` validiert.
- **File-Explorer in der WebUI**: Navigation durch die konfigurierten Roots, um den Repo-Pfad für ein neues Projekt auszuwählen. Nur Verzeichnisse, kein Dateiinhalt.
- **Agent-Runtime-Adapter**: Claude Code und OpenCode werden über eine gemeinsame Adapterschicht gestartet (Kommando, Argumente, Env, Arbeitsverzeichnis). Pro Session wählbar, weitere Runtimes rein per Konfiguration ergänzbar.
- **Persistente Sessions**: Der Router hält die PTYs selbst, puffert Output in einem Scrollback-Ringpuffer und schreibt ein Session-Log auf Platte. Prozesse überleben jeden Client-Disconnect.
- **Terminal im Browser**: Reconnect spielt den Scrollback zurück und schaltet dann live weiter; Tastatureingaben und Resize werden an das PTY durchgereicht. Mehrere Browser-Tabs dürfen dieselbe Session gleichzeitig sehen.
- **Betriebsmodi**: per Konfiguration lokal (localhost, Auth optional) oder als erreichbarer Dienst (Bind-Adresse, Token-Auth verpflichtend). Projekt-Roots sind eine konfigurierte Liste.

## Capabilities

### New Capabilities
- `access-control`: Konfiguration der Betriebsmodi, erlaubte Projekt-Roots und Token-Authentifizierung für HTTP- und WebSocket-Zugriffe.
- `file-browser`: Lesendes Durchsuchen der Verzeichnisstruktur innerhalb der erlaubten Roots zur Auswahl eines Repo-Pfads.
- `project-registry`: Anlegen, Auflisten und Entfernen von Projekten; Validierung, dass ein Projektpfad ein Git-Repository innerhalb eines erlaubten Roots ist.
- `agent-runtime`: Adapterschicht, die Claude Code und OpenCode als austauschbare Runtimes beschreibt und startbar macht.
- `session-lifecycle`: Start, Persistenz, Attach/Reattach, Scrollback-Replay, Eingabe, Resize und Beendigung von Agent-Sessions.

### Modified Capabilities
<!-- Keine: Greenfield-Projekt, es existieren noch keine Specs. -->

## Impact

- **Neues Repository-Layout**: `cmd/router` (Go-Entrypoint), `internal/` (config, auth, fsbrowse, projects, runtime, session), `web/` (React + Vite + xterm.js), Build bettet das gebaute Frontend per `embed.FS` ein.
- **Neue Abhängigkeiten**: Go — `creack/pty` (PTY), `coder/websocket` oder `gorilla/websocket`, `go-chi/chi` (Routing). Frontend — React, Vite, `@xterm/xterm` inkl. Fit-Addon.
- **Laufzeitanforderungen**: `git` im PATH; `claude` bzw. `opencode` müssen auf dem Host installiert und im PATH auffindbar sein. Unix-PTYs — kein Windows-Support im ersten Wurf.
- **Zustand auf Platte**: Registry (Projekte, Sessions-Metadaten) und Session-Logs unter einem konfigurierbaren State-Verzeichnis (Default `~/.local/state/project-router`).
- **Sicherheitsfläche**: Der Dienst startet beliebige Prozesse und liest Verzeichnisse — Root-Allowlist und Token-Auth sind Kern der Capability `access-control`, kein Nachgedanke.
