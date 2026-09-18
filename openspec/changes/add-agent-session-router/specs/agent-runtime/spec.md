## Purpose

Beschreibt Claude Code und OpenCode als austauschbare Runtimes hinter einer gemeinsamen Schnittstelle, sodass eine Session unabhängig davon gestartet werden kann, welches Agent-Tool dahintersteckt. Weitere Runtimes sollen ohne Codeänderung ergänzbar sein.

## ADDED Requirements

### Requirement: Runtime-Katalog

Der Router MUSS einen Katalog verfügbarer Runtimes bereitstellen. Ausgeliefert MÜSSEN mindestens die Runtimes `claude-code` und `opencode` sein. Jeder Eintrag MUSS Kennung, Anzeigename, das auszuführende Kommando und dessen Standardargumente ausweisen.

#### Scenario: Katalog abrufen

- **WHEN** ein Client den Runtime-Katalog abruft
- **THEN** enthält die Antwort mindestens `claude-code` und `opencode` mit Anzeigename und Verfügbarkeitsstatus

### Requirement: Verfügbarkeitsprüfung

Der Router MUSS für jede Runtime prüfen, ob ihr Kommando im PATH des Router-Prozesses auffindbar ist, und das Ergebnis im Katalog ausweisen. Der Start einer Session mit einer nicht verfügbaren Runtime MUSS abgelehnt werden, bevor ein Prozess erzeugt wird.

#### Scenario: Runtime nicht installiert

- **WHEN** das Kommando einer Runtime nicht im PATH liegt
- **THEN** ist die Runtime im Katalog als nicht verfügbar markiert und nennt das gesuchte Kommando

#### Scenario: Session mit nicht verfügbarer Runtime

- **WHEN** ein Client eine Session mit einer als nicht verfügbar markierten Runtime starten will
- **THEN** wird der Start mit einem Fehler abgelehnt, der das fehlende Kommando benennt, und es entsteht keine Session

#### Scenario: Unbekannte Runtime-Kennung

- **WHEN** ein Client eine Session mit einer Runtime-Kennung startet, die es im Katalog nicht gibt
- **THEN** wird der Start mit `400` abgelehnt

### Requirement: Einheitlicher Startkontrakt

Jede Runtime MUSS nach demselben Kontrakt gestartet werden: Arbeitsverzeichnis ist der Repo-Pfad des Projekts, das Kommando läuft in einem PTY, und die Umgebung des Prozesses besteht aus der Umgebung des Routers plus den in der Runtime-Definition ergänzten Variablen. Die Definition MUSS zusätzliche Argumente pro Session zulassen.

#### Scenario: Arbeitsverzeichnis

- **WHEN** eine Session für ein Projekt gestartet wird
- **THEN** ist das Arbeitsverzeichnis des Agent-Prozesses der Repo-Pfad genau dieses Projekts

#### Scenario: Zusätzliche Argumente

- **WHEN** beim Start zusätzliche Argumente übergeben werden
- **THEN** werden sie den Standardargumenten der Runtime angehängt und als Argumentvektor übergeben, ohne durch eine Shell interpretiert zu werden

#### Scenario: Terminal-Umgebung

- **WHEN** ein Agent-Prozess gestartet wird
- **THEN** erhält er eine für interaktive TUIs geeignete `TERM`-Variable und eine Terminalgröße, die der des anfragenden Clients entspricht

### Requirement: Runtimes sind per Konfiguration erweiterbar

Der Router MUSS zusätzliche Runtimes aus der Konfigurationsdatei laden — mit Kennung, Anzeigename, Kommando, Standardargumenten und Zusatz-Umgebungsvariablen. Eine konfigurierte Runtime mit der Kennung einer ausgelieferten Runtime MUSS diese überschreiben.

#### Scenario: Eigene Runtime ergänzen

- **WHEN** in der Konfiguration eine Runtime mit neuer Kennung definiert ist
- **THEN** erscheint sie im Katalog und Sessions können damit gestartet werden

#### Scenario: Ausgelieferte Runtime anpassen

- **WHEN** die Konfiguration eine Runtime mit der Kennung `claude-code` definiert
- **THEN** ersetzt diese Definition die ausgelieferte Variante vollständig

#### Scenario: Unvollständige Definition

- **WHEN** eine konfigurierte Runtime kein Kommando angibt
- **THEN** bricht der Start des Routers mit einer Fehlermeldung ab, die die fehlerhafte Runtime benennt
