## Purpose

Legt fest, wer den Router erreichen darf und welcher Ausschnitt des Dateisystems für ihn überhaupt existiert. Der Dienst startet beliebige Prozesse und liest Verzeichnisse, deshalb sind Betriebsmodus, Token-Prüfung und Root-Allowlist Teil des beobachtbaren Verhaltens.

## ADDED Requirements

### Requirement: Konfigurierbarer Betriebsmodus

Der Router MUSS zwei Betriebsmodi unterstützen: `local` (Bind an Loopback, Auth optional) und `networked` (Bind an eine konfigurierte Adresse, Auth verpflichtend). Der Modus, die Bind-Adresse und der Port MÜSSEN über Konfigurationsdatei und Umgebungsvariablen setzbar sein, wobei Umgebungsvariablen Vorrang haben.

#### Scenario: Standardstart ohne Konfiguration

- **WHEN** der Router ohne Konfigurationsdatei und ohne gesetzte Umgebungsvariablen gestartet wird
- **THEN** läuft er im Modus `local`, lauscht auf `127.0.0.1` und gibt die erreichbare URL auf stdout aus

#### Scenario: Netzwerkmodus ohne Token

- **WHEN** der Modus `networked` konfiguriert ist, aber kein Auth-Token hinterlegt wurde
- **THEN** bricht der Start mit einem Fehler ab, der das fehlende Token benennt, und es wird kein Port geöffnet

#### Scenario: Umgebungsvariable überschreibt Datei

- **WHEN** die Konfigurationsdatei Port `8080` setzt und die Umgebungsvariable für den Port `9000` setzt
- **THEN** lauscht der Router auf Port `9000`

### Requirement: Token-Authentifizierung

Ist ein Auth-Token konfiguriert, MUSS jeder HTTP-Request auf die API und jeder WebSocket-Verbindungsaufbau ein gültiges Token vorweisen. Requests ohne oder mit falschem Token MÜSSEN mit `401` abgelehnt werden, bevor irgendeine Aktion ausgeführt wird. Die Auslieferung der statischen WebUI-Assets und der Login-Einstieg MÜSSEN ohne Token erreichbar bleiben.

#### Scenario: API-Request ohne Token

- **WHEN** ein Client bei konfiguriertem Token die Projektliste ohne Token abruft
- **THEN** antwortet der Router mit `401` und liefert keine Projektdaten

#### Scenario: WebSocket-Attach mit falschem Token

- **WHEN** ein Client sich mit einem ungültigen Token an eine Session anhängen will
- **THEN** wird der Verbindungsaufbau abgelehnt, es wird kein Scrollback gesendet und die Session bleibt unverändert laufen

#### Scenario: Gültiges Token

- **WHEN** ein Client ein gültiges Token mitsendet
- **THEN** wird der Request normal verarbeitet

### Requirement: Allowlist erlaubter Roots

Die Konfiguration MUSS eine Liste erlaubter Wurzelverzeichnisse enthalten. Jeder vom Client übergebene Pfad MUSS zu einem absoluten, symlink-aufgelösten Pfad normalisiert und anschließend darauf geprüft werden, dass er innerhalb eines erlaubten Roots liegt. Pfade außerhalb MÜSSEN mit `403` abgelehnt werden — unabhängig davon, ob sie existieren.

#### Scenario: Pfad außerhalb der Roots

- **WHEN** ein Client `/etc` anfragt, während nur `/Volumes/Storage/Projects` erlaubt ist
- **THEN** antwortet der Router mit `403` und gibt keinerlei Information über den Inhalt des Pfads preis

#### Scenario: Traversal über relative Segmente

- **WHEN** ein Client einen Pfad mit `..`-Segmenten schickt, der nach Normalisierung außerhalb aller Roots landet
- **THEN** wird der Request mit `403` abgelehnt

#### Scenario: Symlink aus dem Root heraus

- **WHEN** ein Pfad innerhalb eines erlaubten Roots über einen Symlink auf ein Ziel außerhalb aller Roots zeigt
- **THEN** wird der Request mit `403` abgelehnt

#### Scenario: Leere Root-Liste

- **WHEN** der Router ohne konfigurierte Roots gestartet wird
- **THEN** bricht der Start mit einem Fehler ab, der auf die fehlende Root-Konfiguration hinweist
