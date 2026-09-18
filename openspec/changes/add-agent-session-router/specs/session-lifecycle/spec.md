## Purpose

Trägt den eigentlichen Zweck des Routers: Agent-Sessions laufen serverseitig weiter, unabhängig davon, ob ein Browser verbunden ist, und ein Client kann sich jederzeit wieder anhängen und den bisherigen Verlauf sehen. Der Browser ist nur noch ein austauschbarer Terminal-Client.

## ADDED Requirements

### Requirement: Session starten

Der Router MUSS für ein registriertes Projekt und eine verfügbare Runtime eine Session starten können. Eine Session MUSS eine stabile Kennung, das zugehörige Projekt, die Runtime, den Startzeitpunkt und einen Status (`running`, `exited`, `failed`) besitzen. Der Agent-Prozess MUSS in einem PTY laufen.

#### Scenario: Erfolgreicher Start

- **WHEN** ein Client eine Session für ein verfügbares Projekt und eine verfügbare Runtime startet
- **THEN** wird der Agent-Prozess im Repo-Verzeichnis gestartet und die Session-Kennung mit Status `running` zurückgegeben

#### Scenario: Mehrere Sessions pro Projekt

- **WHEN** für dasselbe Projekt eine zweite Session gestartet wird, während die erste läuft
- **THEN** laufen beide Sessions unabhängig voneinander mit eigenen Kennungen und eigenem Scrollback

#### Scenario: Projekt nicht verfügbar

- **WHEN** eine Session für ein Projekt gestartet wird, dessen Pfad nicht mehr existiert
- **THEN** wird der Start abgelehnt und es entsteht kein Prozess

### Requirement: Sessions überleben Client-Disconnects

Eine laufende Session MUSS unabhängig von verbundenen Clients weiterlaufen. Das Schließen eines Browser-Tabs, ein Netzwerkabbruch oder ein VPN-Wechsel DÜRFEN weder den Agent-Prozess beenden noch dessen Scrollback verwerfen.

#### Scenario: Browser-Tab geschlossen

- **WHEN** der letzte verbundene Client die Verbindung schließt, während der Agent noch arbeitet
- **THEN** läuft der Agent-Prozess weiter und die Session bleibt im Status `running`

#### Scenario: Verbindungsabbruch mitten in der Ausgabe

- **WHEN** die Verbindung abbricht, während der Agent Ausgabe produziert
- **THEN** wird die weitere Ausgabe im Scrollback-Puffer gesammelt und geht nicht verloren

#### Scenario: Wiederverbinden von einem anderen Gerät

- **WHEN** ein Client sich von einem anderen Gerät an dieselbe Session anhängt
- **THEN** erhält er denselben Verlauf und dieselbe laufende Session

### Requirement: Attach mit Scrollback-Replay

Beim Anhängen an eine Session MUSS der Router zuerst den gepufferten Scrollback ausliefern und danach nahtlos auf den Live-Stream umschalten. Dabei DÜRFEN KEINE Ausgaben verloren gehen und keine doppelt gesendet werden. Der Puffer MUSS eine konfigurierbare Obergrenze haben; wird sie überschritten, MÜSSEN die ältesten Daten verworfen und der Client MUSS über die Kürzung informiert werden.

#### Scenario: Reattach zeigt den Verlauf

- **WHEN** ein Client sich an eine Session anhängt, die zuvor Ausgabe produziert hat
- **THEN** sieht er zuerst den gepufferten Verlauf und danach neu eintreffende Ausgabe in korrekter Reihenfolge

#### Scenario: Ausgabe während des Replays

- **WHEN** der Agent während der Auslieferung des Scrollbacks weitere Ausgabe produziert
- **THEN** erscheint diese Ausgabe nach dem Scrollback, genau einmal und in der ursprünglichen Reihenfolge

#### Scenario: Puffergrenze überschritten

- **WHEN** die Ausgabe einer Session die konfigurierte Puffergrenze überschreitet
- **THEN** enthält der Replay die jüngsten Daten bis zur Grenze und eine Markierung, dass älterer Verlauf abgeschnitten wurde

### Requirement: Eingabe an die Session

Tastatureingaben eines verbundenen Clients MÜSSEN unverändert an das PTY der Session weitergereicht werden, inklusive Steuerzeichen wie `Ctrl-C`. Eingaben an eine beendete Session MÜSSEN abgelehnt werden.

#### Scenario: Prompt eingeben

- **WHEN** ein Client Text und Enter an eine laufende Session sendet
- **THEN** sieht der Agent-Prozess exakt diese Bytes auf seinem stdin

#### Scenario: Abbruchsignal

- **WHEN** ein Client `Ctrl-C` sendet
- **THEN** wird das entsprechende Steuerzeichen an das PTY übergeben, sodass der Agent seinen aktuellen Vorgang abbricht, und die Session bleibt bestehen

#### Scenario: Eingabe an beendete Session

- **WHEN** ein Client Eingaben an eine Session im Status `exited` sendet
- **THEN** wird die Eingabe verworfen und der Client erhält den Hinweis, dass die Session beendet ist

### Requirement: Terminalgröße

Ein Client MUSS die Terminalgröße seiner Ansicht melden können; der Router MUSS das PTY entsprechend anpassen. Sind mehrere Clients an derselben Session, MUSS eine deterministische Regel gelten: das PTY folgt der kleinsten gemeldeten Größe, damit kein Client abgeschnittene Ausgabe sieht.

#### Scenario: Fenstergröße ändern

- **WHEN** ein einzeln verbundener Client eine neue Terminalgröße meldet
- **THEN** wird das PTY auf diese Größe gesetzt und der Agent-Prozess über die Änderung benachrichtigt

#### Scenario: Zwei Clients mit unterschiedlicher Größe

- **WHEN** zwei Clients unterschiedliche Terminalgrößen melden
- **THEN** wird das PTY auf die jeweils kleinere Breite und Höhe gesetzt

### Requirement: Mehrere gleichzeitige Clients

Mehrere Clients MÜSSEN gleichzeitig an derselben Session hängen können. Alle MÜSSEN dieselbe Ausgabe sehen, und Eingaben jedes Clients MÜSSEN am selben PTY landen.

#### Scenario: Zweiter Client hängt sich an

- **WHEN** ein zweiter Client sich an eine bereits beobachtete Session anhängt
- **THEN** erhält er den Scrollback und sieht danach dieselbe Live-Ausgabe wie der erste Client

#### Scenario: Eingabe von einem von zwei Clients

- **WHEN** einer von zwei verbundenen Clients etwas eingibt
- **THEN** sehen beide Clients das Echo der Eingabe und die daraus folgende Ausgabe

### Requirement: Session beenden und Prozessende erkennen

Ein Client MUSS eine Session beenden können; der Router MUSS dem Prozess dann zuerst ein Terminierungssignal senden und ihn nach einer Karenzzeit hart beenden. Endet der Agent-Prozess von selbst, MUSS der Router den Status auf `exited` setzen, den Exit-Code festhalten und alle verbundenen Clients darüber informieren.

#### Scenario: Session beenden

- **WHEN** ein Client das Beenden einer laufenden Session anfordert
- **THEN** wird der Prozess terminiert, die Session erhält Status `exited` und verbundene Clients werden benachrichtigt

#### Scenario: Prozess reagiert nicht auf das Signal

- **WHEN** der Prozess nach Ablauf der Karenzzeit noch läuft
- **THEN** wird er hart beendet und die Session erhält den Status `exited`

#### Scenario: Agent beendet sich selbst

- **WHEN** der Agent-Prozess von selbst mit einem Exit-Code endet
- **THEN** erhält die Session Status `exited` mit diesem Exit-Code, verbundene Clients werden informiert und der Scrollback bleibt abrufbar

#### Scenario: Start scheitert sofort

- **WHEN** der Prozess unmittelbar nach dem Start mit einem Fehler endet
- **THEN** erhält die Session Status `failed` und die Ausgabe des Fehlschlags ist im Scrollback nachlesbar

### Requirement: Sessions auflisten

Der Router MUSS alle Sessions auflisten können — gefiltert nach Projekt oder übergreifend — jeweils mit Kennung, Projekt, Runtime, Status, Startzeit und, falls beendet, Endzeit und Exit-Code. Beendete Sessions MÜSSEN so lange sichtbar bleiben, bis sie ausdrücklich entfernt werden.

#### Scenario: Übersicht über alle Sessions

- **WHEN** ein Client die Sessions ohne Filter abruft
- **THEN** erhält er laufende und beendete Sessions aller Projekte mit ihrem jeweiligen Status

#### Scenario: Beendete Session entfernen

- **WHEN** ein Client eine beendete Session ausdrücklich entfernt
- **THEN** verschwindet sie aus der Liste und ihr Scrollback-Puffer wird freigegeben

### Requirement: Session-Log auf Platte

Der Router MUSS die Ausgabe jeder Session zusätzlich in eine Logdatei unterhalb des konfigurierten State-Verzeichnisses schreiben, damit der Verlauf auch nach einem Neustart des Routers nachlesbar bleibt. Scheitert das Schreiben des Logs, MUSS die Session weiterlaufen und der Fehler MUSS protokolliert werden.

#### Scenario: Log wird geschrieben

- **WHEN** eine Session Ausgabe produziert
- **THEN** landet dieselbe Ausgabe in der Logdatei der Session

#### Scenario: Log nicht schreibbar

- **WHEN** die Logdatei nicht angelegt werden kann
- **THEN** läuft die Session dennoch weiter und der Router meldet den Fehler, ohne die Session zu beenden

### Requirement: Verhalten beim Neustart des Routers

Beim Neustart des Routers enden die von ihm gehaltenen Agent-Prozesse. Der Router MUSS Sessions, die vor dem Neustart liefen, beim Start als `exited` markieren statt sie als laufend auszuweisen, und ihre Logdateien MÜSSEN weiterhin auffindbar sein.

#### Scenario: Nach dem Neustart

- **WHEN** der Router neu gestartet wird, während Sessions als `running` vermerkt waren
- **THEN** werden diese Sessions als `exited` geführt und nicht fälschlich als laufend angezeigt

#### Scenario: Log nach Neustart abrufbar

- **WHEN** nach einem Neustart der Verlauf einer vorherigen Session abgerufen wird
- **THEN** wird der Inhalt der zugehörigen Logdatei ausgeliefert
