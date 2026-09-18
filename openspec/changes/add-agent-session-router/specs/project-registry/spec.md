## Purpose

Hält fest, welche Git-Repositories dem Router als Projekte bekannt sind, damit sie nach einem Neustart oder von einem anderen Gerät aus wiedergefunden werden. Ein Projekt ist der Anker, unter dem Agent-Sessions laufen.

## ADDED Requirements

### Requirement: Projekt aus bestehendem Repository anlegen

Der Router MUSS ein Projekt aus einem vorhandenen Verzeichnis anlegen können. Der Pfad MUSS innerhalb eines erlaubten Roots liegen und ein Git-Repository sein. Jedes Projekt MUSS eine stabile Kennung, einen Anzeigenamen (Default: Verzeichnisname) und den absoluten Repo-Pfad besitzen.

#### Scenario: Gültiges Repository

- **WHEN** ein Client ein Projekt für ein Verzeichnis anlegt, das ein Git-Repository innerhalb eines erlaubten Roots ist
- **THEN** wird das Projekt mit stabiler Kennung angelegt und erscheint in der Projektliste

#### Scenario: Verzeichnis ist kein Git-Repository

- **WHEN** der angegebene Pfad existiert, aber kein Git-Repository ist
- **THEN** wird das Anlegen mit einem Fehler abgelehnt, der genau das benennt, und es wird kein Projekt gespeichert

#### Scenario: Pfad bereits registriert

- **WHEN** für denselben normalisierten Pfad bereits ein Projekt existiert
- **THEN** wird kein zweites Projekt angelegt und das bestehende Projekt zurückgegeben

### Requirement: Neues Repository initialisieren

Zeigt der gewählte Pfad auf ein Verzeichnis ohne Git-Repository, MUSS der Router auf ausdrückliche Anforderung `git init` in diesem Verzeichnis ausführen und das Projekt anschließend anlegen. Ohne diese ausdrückliche Anforderung DARF der Router KEIN Repository initialisieren.

#### Scenario: Initialisierung angefordert

- **WHEN** ein Client ein Projekt für ein leeres Verzeichnis innerhalb eines erlaubten Roots anlegt und die Initialisierung ausdrücklich anfordert
- **THEN** wird das Verzeichnis als Git-Repository initialisiert und das Projekt angelegt

#### Scenario: Verzeichnis existiert nicht

- **WHEN** der angegebene Pfad nicht existiert
- **THEN** wird das Verzeichnis angelegt, sofern der Elternpfad innerhalb eines erlaubten Roots liegt, andernfalls wird der Request mit `403` abgelehnt

#### Scenario: Initialisierung schlägt fehl

- **WHEN** `git init` fehlschlägt, etwa weil `git` nicht im PATH ist
- **THEN** wird kein Projekt angelegt und die Fehlerausgabe von git wird an den Client zurückgegeben

### Requirement: Projekte auflisten

Der Router MUSS alle registrierten Projekte auflisten können, jeweils mit Kennung, Name, Pfad, Anzahl der laufenden Sessions und dem Hinweis, ob der Pfad aktuell noch existiert und ein Git-Repository ist.

#### Scenario: Liste mit laufenden Sessions

- **WHEN** ein Projekt zwei laufende Sessions hat
- **THEN** weist der Listeneintrag zwei laufende Sessions aus

#### Scenario: Projektpfad verschwunden

- **WHEN** das Verzeichnis eines registrierten Projekts nicht mehr existiert
- **THEN** bleibt das Projekt in der Liste, ist aber als nicht verfügbar markiert, und das Starten neuer Sessions dafür wird abgelehnt

### Requirement: Projekt entfernen

Der Router MUSS ein Projekt aus der Registry entfernen können. Das Entfernen DARF KEINE Dateien auf der Platte löschen. Laufende Sessions des Projekts MÜSSEN zuvor beendet werden oder das Entfernen MUSS mit einem entsprechenden Hinweis abgelehnt werden.

#### Scenario: Projekt ohne Sessions entfernen

- **WHEN** ein Projekt ohne laufende Sessions entfernt wird
- **THEN** verschwindet es aus der Liste und das Repository auf der Platte bleibt unangetastet

#### Scenario: Projekt mit laufenden Sessions entfernen

- **WHEN** ein Projekt mit mindestens einer laufenden Session entfernt werden soll
- **THEN** wird das Entfernen abgelehnt mit dem Hinweis, dass zuerst die Sessions zu beenden sind

### Requirement: Registry überlebt Neustarts

Die Registry MUSS persistent auf Platte liegen und beim Start geladen werden. Eine beschädigte oder unlesbare Registry MUSS den Start mit einer klaren Fehlermeldung abbrechen, statt still mit leerem Zustand weiterzulaufen.

#### Scenario: Neustart des Routers

- **WHEN** der Router neu gestartet wird
- **THEN** sind alle zuvor registrierten Projekte weiterhin in der Liste

#### Scenario: Kaputte Registry-Datei

- **WHEN** die Registry-Datei beim Start nicht geparst werden kann
- **THEN** bricht der Start mit einer Fehlermeldung ab, die den Pfad der Datei nennt, und die Datei wird nicht überschrieben
