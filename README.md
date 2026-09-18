# project-router

Ein Router für Agent-Sessions: Claude Code und OpenCode laufen serverseitig in einem PTY
weiter, der Browser ist nur ein austauschbarer Terminal-Client. Sessions überleben jeden
Client-Disconnect; beim Reattach wird der Scrollback zurückgespielt.

## Voraussetzungen

* Go 1.24+, Node 20+ (nur für den Build der WebUI)
* `git` im PATH
* `claude` bzw. `opencode` auf dem Host installiert und im PATH
* Unix mit PTYs — kein Windows-Support

## Build

```sh
make build   # npm ci + npm run build (web/dist) und anschließend go build -o router
```

`make build` ist der vollständige Weg ab sauberem Checkout: er baut die WebUI und
bettet sie per `embed.FS` in das Binary ein. `go build ./cmd/router` allein nutzt den
zuletzt gebauten Stand von `web/dist`.

## Lokaler Betrieb

```sh
ROUTER_ROOTS=~/code ./router
```

Der Router lauscht auf `127.0.0.1:7777`, die erreichbare URL steht beim Start auf
stdout. Ein Token ist lokal optional — auf dem Loopback schützt bereits die
Prozessgrenze des Users.

Entwicklungsmodus mit Hot Reload der Oberfläche:

```sh
ROUTER_ROOTS=~/code ./router &   # Backend auf :7777
cd web && npm run dev            # Vite auf :5173, proxyt /api inklusive WebSocket
```

## Serverbetrieb (VPN)

```sh
ROUTER_TOKEN=… ./router -config /etc/project-router/router.yaml
```

`router.example.yaml` zeigt eine vollständige Konfiguration, `project-router.service`
ist eine systemd-Unit-Vorlage. Im Modus `networked` ist ein Token Startbedingung —
fehlt es, öffnet der Router keinen Port. Zusätzlich prüft der WebSocket-Upgrade den
`Origin`-Header gegen `baseURL`.

Der Dienst startet beliebige Prozesse im Kontext seines Users und gehört deshalb
hinter ein VPN, nicht ins offene Netz.

## Konfiguration

Siehe `router.example.yaml`. Umgebungsvariablen haben Vorrang vor der Datei:
`ROUTER_MODE`, `ROUTER_BIND`, `ROUTER_PORT`, `ROUTER_TOKEN`, `ROUTER_ROOTS`
(Pfade durch `:` getrennt), `ROUTER_STATE_DIR`, `ROUTER_BUFFER_BYTES`,
`ROUTER_BASE_URL`.

Ohne konfigurierte Roots startet der Router nicht — die Root-Allowlist ist die
einzige Grenze zwischen der API und dem übrigen Dateisystem.

## Beendete Sessions

Eine beendete Session bleibt mit Endzeit, Exit-Code und Log in der Liste stehen, bis sie
ausdrücklich entfernt wird. Sie ist aber keine Sackgasse: *Neu starten* in der Übersicht —
oder *Neue Session starten* in der Terminalansicht — startet eine frische Session mit
denselben Eckdaten (Projekt, Runtime, Zusatzargumente) und hängt den Browser direkt daran.
Die neue Session bekommt eine eigene Kennung, ein eigenes Log und einen leeren Scrollback;
der alte Eintrag bleibt als Verlauf erhalten.

Das gilt auch nach einem Neustart des Routers, der alle Agent-Prozesse beendet: die alten
Sessions stehen auf `exited` und lassen sich mit einem Klick frisch starten.

## Runtimes

Eine Runtime ist nur eine Beschreibung: Kennung, Anzeigename, Kommando, Standardargumente
und zusätzliche Umgebungsvariablen. Ausgeliefert sind `claude-code` und `opencode`; weitere
kommen auf drei Wegen dazu, mit steigendem Vorrang:

1. **Ausgeliefert** — im Binary vorkompiliert.
2. **Konfigurationsdatei** — der Abschnitt `runtimes:` in `router.yaml`, siehe
   `router.example.yaml`. Gilt ab dem nächsten Start.
3. **In der WebUI hinterlegt** — Abschnitt *Runtimes* → „Eigene Runtime hinterlegen".
   Wirkt sofort und liegt im State-Verzeichnis, überlebt also Neustarts.

Das Startkommando wird als ganze Zeile eingegeben, zum Beispiel:

```
ollama launch claude
```

Der Router zerlegt sie **ohne Shell** in einen Argumentvektor: `ollama` ist das Kommando
(es muss im PATH liegen, sonst ist die Runtime als nicht verfügbar markiert), `launch` und
`claude` sind Standardargumente. Argumente mit Leerzeichen gehören in Anführungszeichen;
`$VAR`, `|`, `;` und Konsorten werden nie interpretiert, sondern unverändert übergeben.

Eine hinterlegte Runtime mit der Kennung einer ausgelieferten — etwa `claude-code` —
ersetzt diese vollständig. Beim Entfernen lebt die ursprüngliche Definition wieder auf,
die Oberfläche beschriftet den Knopf dann mit *Zurücksetzen*.

## Zustand und Neustarts

Registry und Session-Metadaten liegen als `state.json` im State-Verzeichnis
(Default `~/.local/state/project-router`), die Ausgabe jeder Session zusätzlich in
`sessions/<id>.log`. Ein Neustart des Routers beendet alle Agent-Prozesse: Sessions,
die als `running` vermerkt waren, werden beim Start ehrlich auf `exited` gesetzt,
ihre Logs bleiben lesbar.

## API

| Methode | Pfad | Zweck |
| --- | --- | --- |
| `GET` | `/api/fs?path=` | Verzeichnisse auflisten (ohne `path`: die konfigurierten Roots) |
| `GET` | `/api/projects` | registrierte Projekte samt Sessionzahl und Verfügbarkeit |
| `POST` | `/api/projects` | Projekt anlegen (`{"path":"…","init":false}`) |
| `DELETE` | `/api/projects/{id}` | Projekt aus der Registry nehmen (löscht keine Dateien) |
| `GET` | `/api/runtimes` | Runtime-Katalog mit Verfügbarkeit und Herkunft |
| `POST` | `/api/runtimes` | eigene Runtime hinterlegen (`{"displayName":"…","commandLine":"ollama launch claude","env":{}}`) |
| `DELETE` | `/api/runtimes/{id}` | hinterlegte Runtime entfernen (überschriebene Definition lebt wieder auf) |
| `GET` | `/api/sessions[?projectId=]` | Sessions auflisten |
| `POST` | `/api/sessions` | Session starten (`{"projectId":"…","runtimeId":"…","args":[],"cols":80,"rows":24}`) |
| `POST` | `/api/sessions/{id}/stop` | Session beenden, Eintrag bleibt sichtbar |
| `POST` | `/api/sessions/{id}/restart` | frische Session mit denselben Eckdaten starten (Projekt, Runtime, Argumente) |
| `DELETE` | `/api/sessions/{id}` | beendete Session entfernen (`?stop=true` beendet sie zuvor) |
| `GET` | `/api/sessions/{id}/attach` | WebSocket-Upgrade (siehe unten) |

Das Token gehört bei HTTP in den Header `Authorization: Bearer <token>`, beim
WebSocket in den Query-Parameter `token` — die Browser-WebSocket-API kann keine
Header setzen. Die statischen WebUI-Assets sind ohne Token erreichbar.

### WebSocket-Protokoll (`/api/sessions/{id}/attach`)

Query-Parameter: `cols`, `rows` (Startgröße des Clients), `token`.

* **Binärframes** transportieren PTY-Bytes in beide Richtungen: vom Server die
  Ausgabe des Agenten, vom Client die Tastatureingabe — unverändert, inklusive
  Steuerzeichen wie `Ctrl-C` (`0x03`).
* **Textframes** sind JSON-Steuernachrichten:

| Richtung | Nachricht | Bedeutung |
| --- | --- | --- |
| Client → Server | `{"type":"resize","cols":80,"rows":24}` | gemeldete Terminalgröße; das PTY folgt dem elementweisen Minimum aller Clients |
| Server → Client | `{"type":"status","status":"running"}` | aktueller Status, auch als Antwort auf Eingabe an eine beendete Session |
| Server → Client | `{"type":"truncated","message":"…"}` | älterer Verlauf wurde abgeschnitten, oder dieser Client war zu langsam und wurde getrennt |
| Server → Client | `{"type":"exit","status":"exited","exitCode":0}` | der Agent-Prozess ist beendet |

Ablauf nach dem Upgrade: gegebenenfalls ein `truncated`-Hinweis, dann der
Scrollback als Binärframe (bei gekürztem Puffer mit vorangestelltem Terminal-Reset
`ESC c`), dann ein `status`-Frame — und danach der Live-Stream.
