## 1. Projektgerüst

- [x] 1.1 Go-Modul `project-router` anlegen (`go.mod`, Go 1.22+), Verzeichnisse `cmd/router/` und `internal/` erstellen; `go build ./...` läuft fehlerfrei durch
- [x] 1.2 Abhängigkeiten `go-chi/chi/v5`, `coder/websocket` und `creack/pty` hinzufügen; `go mod tidy` hinterlässt eine saubere `go.sum`
- [x] 1.3 `.gitignore` (Go-Binaries, `web/dist`, `web/node_modules`) und README-Stub mit Build- und Startanleitung anlegen
- [x] 1.4 Minimaler Entrypoint in `cmd/router/main.go`, der auf Loopback lauscht und `/healthz` mit `200` beantwortet; per `curl` verifizieren

## 2. Konfiguration und Pfadsicherheit (`access-control`)

- [x] 2.1 `internal/config`: Struktur für Modus, Bind-Adresse, Port, Token, Roots, State-Verzeichnis, Puffergröße und Runtime-Overrides; Laden aus YAML-Datei mit Defaults, Unit-Test deckt die Defaults ab
- [x] 2.2 Overlay aus Umgebungsvariablen mit Vorrang vor der Datei; Unit-Test belegt, dass die Port-Env die Datei überschreibt
- [x] 2.3 Startvalidierung: leere Root-Liste bricht ab, `mode: networked` ohne Token bricht ab und öffnet keinen Port; Unit-Tests für beide Abbruchfälle
- [x] 2.4 `internal/pathsafe`: `resolveWithin(roots, input)` mit `Abs` → `EvalSymlinks` → Präfixvergleich auf Segmentgrenze, inklusive Auflösung über den nächsten existierenden Elternpfad für noch nicht angelegte Verzeichnisse
- [x] 2.5 Tabellen-Test für `resolveWithin`: Treffer im Root, `..`-Traversal, Symlink aus dem Root heraus, Präfix-Falle `/a/bc` gegen Root `/a/b`, nicht existierender Pfad mit erlaubtem Elternpfad
- [x] 2.6 Auth-Middleware für HTTP mit konstant-zeitigem Token-Vergleich, `401` vor jeder Aktion, WebUI-Assets ausgenommen; Handler-Test deckt fehlendes, falsches und gültiges Token ab
- [x] 2.7 Token-Prüfung für den WebSocket-Upgrade per Query-Parameter plus Origin-Prüfung gegen die konfigurierte Basis-URL; Test belegt, dass ein ungültiges Token den Upgrade verhindert

## 3. File-Explorer-API (`file-browser`)

- [x] 3.1 `internal/fsbrowse`: Auflisten der direkten Unterverzeichnisse eines Pfads mit Name, absolutem Pfad, `isGitRepo` und Elternpfad; alphabetisch sortiert, Dateien werden ausgelassen
- [x] 3.2 Ohne Pfadangabe die konfigurierten Roots als Einstiegseinträge liefern; Test prüft Inhalt und Sortierung
- [x] 3.3 Fehlerabbildung in `GET /api/fs`: Root-Prüfung vor Existenzprüfung (`403` vor `404`), Pfad auf reguläre Datei ergibt `400`, fehlende Leseberechtigung ergibt einen benannten Fehler statt leerer Liste
- [x] 3.4 Handler-Tests über ein temporäres Verzeichnisgerüst decken alle Szenarien aus `specs/file-browser/spec.md` ab, inklusive nicht existierendem Pfad außerhalb der Roots mit `403`

## 4. Zustandsspeicher (`project-registry`)

- [x] 4.1 `internal/store`: JSON-Persistenz für Projekte und Session-Metadaten hinter einem schmalen Interface, Schreiben als Temp-Datei plus `os.Rename`; Test belegt, dass ein abgebrochener Schreibvorgang die alte Datei intakt lässt
- [x] 4.2 Laden beim Start; unparsbare Datei bricht den Start mit Pfadangabe ab und wird nicht überschrieben (Test)
- [x] 4.3 Beim Laden alle als `running` vermerkten Sessions auf `exited` setzen; Test mit vorbereiteter State-Datei
- [x] 4.4 State-Verzeichnis anlegen (Default `~/.local/state/project-router`, per Config überschreibbar) inklusive `sessions/`-Unterordner

## 5. Projektverwaltung (`project-registry`)

- [x] 5.1 `internal/projects`: Projekt aus bestehendem Pfad anlegen mit Root-Prüfung, `.git`-Prüfung, stabiler Kennung und Anzeigename aus dem Verzeichnisnamen
- [x] 5.2 Erneutes Anlegen desselben normalisierten Pfads liefert das bestehende Projekt statt eines Duplikats (Test)
- [x] 5.3 Initialisierung neuer Repositories: nur auf ausdrückliche Anforderung, legt fehlendes Verzeichnis innerhalb der Roots an, führt `git init` aus und gibt dessen Fehlerausgabe bei Fehlschlag durch; Tests für Erfolg, fehlende Anforderung und Elternpfad außerhalb der Roots (`403`)
- [x] 5.4 Auflisten mit Sessionzahl und Verfügbarkeitsflag; verschwundener Pfad bleibt gelistet, ist als nicht verfügbar markiert und lehnt neue Sessions ab (Test)
- [x] 5.5 Entfernen löscht keine Dateien und wird bei laufenden Sessions mit Hinweis abgelehnt (Test)
- [x] 5.6 Handler `GET|POST /api/projects` und `DELETE /api/projects/{id}` verdrahten; Handler-Tests decken die Szenarien aus `specs/project-registry/spec.md` ab

## 6. Runtime-Adapter (`agent-runtime`)

- [x] 6.1 `internal/runtime`: Runtime als Datenstruktur (`id`, `displayName`, `command`, `defaultArgs`, `env`) mit eingebauten Definitionen für `claude-code` und `opencode`
- [x] 6.2 Katalog mit Verfügbarkeitsprüfung per `exec.LookPath` bei jedem Abruf (nicht gecacht); Test mit manipuliertem PATH belegt beide Zustände
- [x] 6.3 Overrides aus der Konfiguration: neue Kennung ergänzt den Katalog, bekannte Kennung ersetzt die eingebaute Definition vollständig, fehlendes Kommando bricht den Start mit Benennung der Runtime ab (Tests)
- [x] 6.4 Startkontrakt: Argumentvektor über `exec.Command` ohne Shell, Arbeitsverzeichnis gleich Repo-Pfad, Umgebung gleich Router-Umgebung plus Runtime-Variablen plus `TERM`; Test belegt, dass Sonderzeichen in Zusatzargumenten unverändert ankommen
- [x] 6.5 Handler `GET /api/runtimes`; Start mit unbekannter Kennung ergibt `400`, Start mit nicht verfügbarer Runtime wird vor der Prozesserzeugung abgelehnt (Tests)

## 7. Session-Hub und PTY (`session-lifecycle`)

- [x] 7.1 `internal/session`: Ringpuffer mit konfigurierbarer Obergrenze und monotonem Byte-Offset, der bei Überlauf die ältesten Daten verwirft und die Kürzung meldet; Unit-Tests für Auffüllen, Überlauf und Offset-Fortschreibung
- [x] 7.2 Owner-Goroutine pro Session als einziger Leser/Schreiber des PTY, mit Subscriber-Menge gepufferter Channels und Fan-out der PTY-Ausgabe
- [x] 7.3 Attach als Snapshot-unter-Lock: Ringpuffer-Inhalt und Subscriber-Eintrag unter demselben Lock, sodass keine Bytes verloren gehen oder doppelt ankommen; nebenläufiger Test mit Ausgabe während des Attach belegt exakt-einmal-Zustellung in Reihenfolge
- [x] 7.4 Langsame Subscriber: volllaufender Channel führt zum Verwerfen mit `truncated`-Hinweis und Trennung, ohne den Agent-Prozess zu bremsen (Test)
- [x] 7.5 Session starten: PTY über `creack/pty` mit Startgröße des anfragenden Clients, Statusübergänge `running`/`exited`/`failed`, sofortiger Fehlschlag ergibt `failed` mit Fehlerausgabe im Scrollback (Tests mit einem Dummy-Kommando)
- [x] 7.6 Eingabe unverändert an das PTY durchreichen, inklusive Steuerzeichen; Eingabe an eine beendete Session wird verworfen und dem Client gemeldet (Test)
- [x] 7.7 Resize: gemeldete Größen je Subscriber halten, PTY auf das elementweise Minimum setzen, bei Trennung eines Clients neu berechnen; Tests für einen und für zwei Clients
- [x] 7.8 Beenden: Terminierungssignal, Karenzzeit, danach hartes Beenden; Prozessende von selbst setzt `exited` samt Exit-Code und benachrichtigt alle Subscriber (Tests inklusive eines Kommandos, das das Signal ignoriert)
- [x] 7.9 Session-Log append-only unter `sessions/<id>.log`; scheitert das Schreiben, läuft die Session weiter und der Fehler wird protokolliert (Test mit nicht beschreibbarem Verzeichnis)
- [x] 7.10 Replay stellt einen Terminal-Reset voran, wenn der Puffer gekürzt wurde

## 8. Session-API und WebSocket

- [x] 8.1 Handler `GET|POST /api/sessions` und `DELETE /api/sessions/{id}`: Auflisten mit Filter nach Projekt, Metadaten inklusive Start-/Endzeit und Exit-Code, beendete Sessions bleiben bis zum ausdrücklichen Entfernen sichtbar
- [x] 8.2 Entfernen einer beendeten Session gibt den Ringpuffer frei und nimmt sie aus der Liste (Test)
- [x] 8.3 `WS /api/sessions/{id}/attach`: Binärframes für PTY-Bytes, JSON-Textframes für `resize`, `status`, `truncated` und `exit`; Protokoll im README festhalten
- [x] 8.4 End-to-End-Test mit echtem WebSocket-Client: Session starten, Ausgabe erzeugen, trennen, weitere Ausgabe erzeugen, wieder anhängen und den lückenlosen Verlauf verifizieren
- [x] 8.5 End-to-End-Test mit zwei gleichzeitigen Clients: beide sehen dieselbe Ausgabe, Eingabe eines Clients wird von beiden gesehen

## 9. WebUI

- [x] 9.1 `web/` mit Vite, React und TypeScript aufsetzen; `npm run build` erzeugt `web/dist`, Dev-Server proxyt `/api` auf das Go-Backend
- [x] 9.2 API-Client mit Token-Handling (Eingabe, Ablage im Browser, `401` führt zurück zum Token-Dialog)
- [x] 9.3 Projektübersicht: registrierte Projekte mit Sessionzahl und Verfügbarkeit, Aktionen zum Öffnen und Entfernen
- [x] 9.4 File-Explorer-Dialog: Navigation durch Roots und Unterverzeichnisse, Markierung bestehender Git-Repositories, Auswahl eines Pfads für ein neues Projekt inklusive der ausdrücklichen Option zum Initialisieren
- [x] 9.5 Session-Start-Dialog: Runtime-Auswahl aus dem Katalog mit ausgegrauten nicht verfügbaren Einträgen und optionalen Zusatzargumenten
- [x] 9.6 Terminalansicht mit `@xterm/xterm` und Fit-Addon: Attach beim Öffnen, Scrollback-Replay, debounced Resize-Meldung, Anzeige von Statuswechseln, Kürzungshinweis und Exit-Code
- [x] 9.7 Reconnect-Verhalten im Client: getrennter WebSocket wird mit Backoff neu aufgebaut und der Verlauf neu geladen; im Browser gegen einen absichtlich unterbrochenen Server verifizieren
- [x] 9.8 Sessionliste über alle Projekte hinweg mit Statusanzeige und Möglichkeit zum Beenden und Entfernen

## 10. Auslieferung und Abnahme

- [x] 10.1 WebUI per `embed.FS` einbetten und als SPA mit Fallback auf `index.html` ausliefern; Test belegt, dass unbekannte Nicht-API-Pfade `index.html` liefern
- [x] 10.2 Build-Schritt (`Makefile` oder `go:generate`), der `npm run build` vor `go build` ausführt; ein Durchlauf ab sauberem Checkout erzeugt ein lauffähiges Binary
- [x] 10.3 Beispielkonfiguration und systemd-Unit-Vorlage für den Netzwerkbetrieb ergänzen; README beschreibt lokalen und Serverbetrieb
- [x] 10.4 `go vet ./...` und der komplette Testlauf sind grün
- [x] 10.5 Manuelle Abnahme des Kernszenarios: Projekt über den File-Explorer anlegen, Claude-Code-Session starten, Prompt absetzen, Browser-Tab schließen, Router weiterlaufen lassen, neu verbinden und den vollständigen Verlauf samt weiterlaufender Session vorfinden
- [ ] 10.6 Manuelle Abnahme des Netzwerkmodus über VPN: Start mit Token, Zugriff von einem zweiten Gerät, Zugriff ohne Token wird mit `401` abgewiesen
