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

### Token-Prüfung abschalten

Wer den Router in einem Netz betreibt, dem er ohnehin vertraut, kann die Prüfung
ausdrücklich abschalten:

```yaml
disableAuth: true
```

oder `ROUTER_DISABLE_AUTH=1`. Das gilt auch im Modus `networked`: der Router startet
dann ohne Token und lässt jeden Request durch. Ein gesetztes `token` wird ignoriert,
und beim Start steht ein Hinweis auf stderr.

Das ist eine bewusste Entscheidung, keine Bequemlichkeit: Wer die Bind-Adresse
erreicht, kann über den Router beliebige Prozesse im Kontext des startenden Users
ausführen und in den erlaubten Roots lesen. Sinnvoll ist der Schalter für Loopback
oder ein VPN-only-Interface — nicht für ein Interface, das im LAN oder im Internet
hängt.

Der Dienst startet beliebige Prozesse im Kontext seines Users und gehört deshalb
hinter ein VPN, nicht ins offene Netz.

## Konfiguration

Siehe `router.example.yaml`. Umgebungsvariablen haben Vorrang vor der Datei:
`ROUTER_NAME`, `ROUTER_MODE`, `ROUTER_BIND`, `ROUTER_PORT`, `ROUTER_TOKEN`,
`ROUTER_ROOTS` (Pfade durch `:` getrennt), `ROUTER_STATE_DIR`,
`ROUTER_BUFFER_BYTES`, `ROUTER_BASE_URL`, `ROUTER_DISABLE_AUTH`, `ROUTER_NOTIFY`,
`ROUTER_NOTIFY_IDLE_AFTER`, `ROUTER_NOTIFY_WEBHOOK`.

Ohne konfigurierte Roots startet der Router nicht — die Root-Allowlist ist die
einzige Grenze zwischen der API und dem übrigen Dateisystem.

### Name der Instanz

```yaml
name: Homelab-Router
```

Der Name steht in der Überschrift und im Fenstertitel der WebUI, in der Startmeldung
auf stdout und als Absender in jeder Benachrichtigung. Wer mehrere Router betreibt,
unterscheidet sie daran. Ohne Angabe heißt die Instanz `project-router`.

Die Oberfläche holt ihn über `GET /api/info` — dieser eine Endpunkt liegt vor der
Token-Prüfung, damit der Name schon auf dem Token-Dialog steht. Ein Anzeigename ist
kein Geheimnis; alles andere bleibt hinter der Prüfung.

## Rückfragen melden

Wartet Claude Code oder OpenCode auf eine Entscheidung — „Do you want to proceed?",
eine Freigabe für ein Kommando, ein Auswahlmenü —, dann steht die Session still, bis
jemand antwortet. Der Router erkennt das und meldet es.

Erkannt wird in zwei Stufen, und die zweite ist die wichtigere:

1. Ein **Muster** in der Ausgabe. Eingebaut sind die üblichen Nachfragen beider
   Agenten, auf Deutsch wie auf Englisch; eigene kommen unter `notify.patterns` dazu.
2. Anschließende **Stille**. Ein arbeitender Agent schreibt die Frage schon, während
   er weitermacht — erst wenn danach für `notify.idleAfter` (Default 4s) nichts mehr
   kommt, wartet er wirklich.

Die Ausgabe wird dafür von Escape-Sequenzen befreit, und Cursor-Sprünge zählen als
Zeilengrenze: sonst liefe ein neu gezeichneter TUI-Kasten zu einer einzigen Zeile
zusammen. Gemeldet wird die Zeile mit dem Treffer, ohne Rahmenzeichen.

Dieselbe Rückfrage meldet sich nur einmal, auch wenn die Oberfläche sich neu zeichnet.
Sobald jemand etwas an die Session schickt, gilt sie als beantwortet — die nächste
meldet wieder.

### Browser-Benachrichtigung

Der Hauptweg. Einmal auf *Benachrichtigungen erlauben* klicken, dann meldet sich der
Router als Systembenachrichtigung — auch wenn der Tab im Hintergrund liegt. Ein Tipp
darauf öffnet genau die wartende Session.

Zwei Dinge sind dafür nötig, und beide kommen vom Browser, nicht vom Router:

**Ein Service Worker** (`web/public/sw.js`, wird mit eingebettet). Chrome auf Android
weigert sich, eine Benachrichtigung über `new Notification()` anzuzeigen — dort geht
es ausschließlich über `registration.showNotification()`. Ohne den Worker passiert
auf dem Handy also gar nichts. Er cacht bewusst nichts: ein Cache würde die WebUI
nach einem Update des Routers einfrieren. Er behandelt nur den Tipp auf die Meldung.

**Ein sicherer Kontext.** Benachrichtigungen gibt es nur über HTTPS oder auf
`localhost`. Unter `http://router.homelab:7777` verweigert der Browser sie — die
Oberfläche sagt das dann auch offen (*Benachrichtigungen brauchen HTTPS*), statt
stumm nichts zu tun. Wer sie auf dem Handy will, braucht also TLS vor dem Router
(Reverse Proxy mit eigenem Zertifikat, Tailscale mit MagicDNS-Zertifikat oder
Ähnliches) — oder nimmt den Webhook.

Unabhängig davon und ohne jede Erlaubnis meldet sich die Oberfläche immer selbst: ein
Banner über dem Terminal, ein Punkt in der Session-Leiste, ein Hinweis in der
Übersicht, eine Marke im Fenstertitel (`(1) ● Homelab-Router`) und ein kurzes
Vibrieren.

### Webhook

Der Weg für alles, was die Browser-Benachrichtigung nicht abdeckt: Router ohne TLS,
geschlossener Browser, oder Meldung in einen Chat statt aufs Schloss-Display.

```yaml
notify:
  webhook:
    url: https://ntfy.sh/mein-privates-topic
    contentType: text/plain
    headers:
      Title: Agent wartet
```

Ohne Template steht bei einem JSON-Content-Type das vollständige Ereignis im Body,
sonst eine lesbare Zeile:

```
Homelab-Router: webshop (claude-code) wartet auf eine Entscheidung — Do you want to proceed?
http://router.homelab:7777/?session=08d70eda0daf33cb
```

Der Link zeigt direkt auf die wartende Session — die Oberfläche öffnet sie beim Laden.
Er entsteht aus `baseURL`; ohne die bleibt er weg. Ein eigener Body geht über
`notify.webhook.template` (Go-Template über `.Router`, `.SessionID`, `.ProjectID`,
`.ProjectName`, `.RuntimeID`, `.Message`, `.URL`, `.Time`).

Ein hängender oder kaputter Webhook bremst die Session nie: der Aufruf läuft nebenher
und ein Fehler landet im Log. Ein unbrauchbares Muster, eine URL ohne `http`/`https`
oder ein kaputtes Template sind dagegen Startfehler — sie fallen beim Hochfahren auf,
nicht bei der ersten Rückfrage.

## Bedienung am Handy

Die Oberfläche ist auf dem Handy benutzbar, nicht nur lesbar: Übersicht und Dialoge
brechen auf eine Spalte um, und das Terminal bekommt so viel Fläche wie möglich.

Im Terminal wird **gewischt statt gescrollt**: xterm.js übersetzt Berührungen nicht
von sich aus, deshalb tut es der Router. Ein Wischen ab etwa sechs Pixeln blättert,
alles darunter bleibt ein Tippen und setzt wie gewohnt den Fokus — die Tastatur geht
also weiter auf.

Was „blättern" heißt, entscheidet dabei die Anwendung im PTY, nicht der Router — genau
wie ein Mausrad am Desktop:

| Zustand der Anwendung | Was die Wischgeste auslöst |
| --- | --- |
| liest Mausereignisse (**Claude Code**, OpenCode) | Mausrad-Ereignisse in SGR-Kodierung — die Anwendung scrollt ihre eigene Ansicht |
| Alternate Screen ohne Maus (`less`, `vim`) | Pfeil hoch/runter |
| gewöhnliche Ausgabe (Shell, Build-Log) | der Scrollback von xterm.js, dazu *↓ Ans Ende* |

Der erste Fall ist der wichtige und der Grund, warum eine reine Scrollback-Geste nicht
reicht: **Claude Code läuft im Alternate Screen**, und der hat prinzipbedingt keinen
Scrollback. Dort gibt es nichts zu schieben — die Anwendung hält den Verlauf selbst
und erwartet Radereignisse, um darin zu blättern. Wer nur `scrollLines()` aufruft,
sieht deshalb: nichts.

Eine Wischgeste in einer Anwendung mit Maus-Tracking geht als Eingabe an das PTY.
Eine offene Rückfrage gilt damit als beantwortet und ihr Hinweis verschwindet —
die Benachrichtigung ist zu dem Zeitpunkt längst raus.

Die Höhe der Seite rechnet in `dvh` statt `vh`: sonst verschwinden genau die Zeilen
unter der Adressleiste, die man lesen will.

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
| `GET` | `/api/info` | Name der Instanz und ob ein Token verlangt wird (**ohne Token erreichbar**) |
| `GET` | `/api/fs?path=` | Verzeichnisse auflisten (ohne `path`: die konfigurierten Roots) |
| `GET` | `/api/projects` | registrierte Projekte samt Sessionzahl und Verfügbarkeit |
| `POST` | `/api/projects` | Projekt anlegen (`{"path":"…","init":false}`) |
| `DELETE` | `/api/projects/{id}` | Projekt aus der Registry nehmen (löscht keine Dateien) |
| `GET` | `/api/runtimes` | Runtime-Katalog mit Verfügbarkeit und Herkunft |
| `POST` | `/api/runtimes` | eigene Runtime hinterlegen (`{"displayName":"…","commandLine":"ollama launch claude","env":{}}`) |
| `DELETE` | `/api/runtimes/{id}` | hinterlegte Runtime entfernen (überschriebene Definition lebt wieder auf) |
| `GET` | `/api/sessions[?projectId=]` | Sessions auflisten (`attention` steht darin, solange eine Rückfrage offen ist) |
| `POST` | `/api/sessions` | Session starten (`{"projectId":"…","runtimeId":"…","args":[],"cols":80,"rows":24}`) |
| `POST` | `/api/sessions/{id}/stop` | Session beenden, Eintrag bleibt sichtbar |
| `POST` | `/api/sessions/{id}/restart` | frische Session mit denselben Eckdaten starten (Projekt, Runtime, Argumente) |
| `DELETE` | `/api/sessions/{id}` | beendete Session entfernen (`?stop=true` beendet sie zuvor) |
| `GET` | `/api/sessions/{id}/attach` | WebSocket-Upgrade (siehe unten) |

Das Token gehört bei HTTP in den Header `Authorization: Bearer <token>`, beim
WebSocket in den Query-Parameter `token` — die Browser-WebSocket-API kann keine
Header setzen. Die statischen WebUI-Assets und `/api/info` sind ohne Token erreichbar.

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
| Server → Client | `{"type":"attention","message":"Do you want to proceed?"}` | der Agent wartet auf eine Entscheidung; eine leere `message` nimmt die Meldung zurück |
| Server → Client | `{"type":"exit","status":"exited","exitCode":0}` | der Agent-Prozess ist beendet |

Ablauf nach dem Upgrade: gegebenenfalls ein `truncated`-Hinweis, dann der
Scrollback als Binärframe (bei gekürztem Puffer mit vorangestelltem Terminal-Reset
`ESC c`), dann ein `status`-Frame — und danach der Live-Stream.
