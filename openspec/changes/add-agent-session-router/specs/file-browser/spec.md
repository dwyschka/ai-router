## Purpose

Erlaubt es, in der WebUI durch die freigegebenen Verzeichnisse zu navigieren, um den Pfad eines Git-Repositories auszuwählen. Es geht ausschließlich um Verzeichnisstruktur — Dateiinhalte gibt der Router nie heraus.

## ADDED Requirements

### Requirement: Verzeichnisse auflisten

Der Router MUSS für einen erlaubten Pfad dessen direkte Unterverzeichnisse zurückgeben, jeweils mit Name, absolutem Pfad und dem Hinweis, ob das Verzeichnis ein Git-Repository ist. Ohne Pfadangabe MÜSSEN die konfigurierten Roots als Einstiegspunkte zurückgegeben werden. Die Auflistung MUSS alphabetisch sortiert sein.

#### Scenario: Roots als Einstieg

- **WHEN** ein Client die Auflistung ohne Pfadangabe abruft
- **THEN** erhält er die Liste der konfigurierten Roots als navigierbare Einträge

#### Scenario: Unterverzeichnis auflisten

- **WHEN** ein Client ein Verzeichnis innerhalb eines erlaubten Roots auflistet
- **THEN** erhält er dessen direkte Unterverzeichnisse, alphabetisch sortiert, sowie den Pfad des übergeordneten Verzeichnisses, sofern dieses noch innerhalb eines Roots liegt

#### Scenario: Leeres Verzeichnis

- **WHEN** das angefragte Verzeichnis keine Unterverzeichnisse enthält
- **THEN** antwortet der Router mit einer leeren Liste und nicht mit einem Fehler

### Requirement: Keine Dateiinhalte

Die Auflistung MUSS auf Verzeichnisse beschränkt bleiben. Der Router DARF über diese Capability KEINE Dateiinhalte und keine Liste regulärer Dateien ausliefern.

#### Scenario: Verzeichnis mit Dateien

- **WHEN** ein Verzeichnis sowohl Unterverzeichnisse als auch Dateien enthält
- **THEN** enthält die Antwort nur die Unterverzeichnisse

#### Scenario: Pfad zeigt auf eine Datei

- **WHEN** ein Client einen Pfad auflistet, der auf eine reguläre Datei zeigt
- **THEN** antwortet der Router mit einem Fehler `400` und liefert keinen Dateiinhalt

### Requirement: Git-Repositories sind erkennbar

Jeder Verzeichniseintrag MUSS ausweisen, ob er ein Git-Repository ist, damit die Auswahl eines Projektpfads in der WebUI ohne Raten möglich ist.

#### Scenario: Repository im Listing

- **WHEN** ein aufgelistetes Verzeichnis ein `.git`-Verzeichnis enthält
- **THEN** ist der Eintrag als Git-Repository markiert

#### Scenario: Gewöhnlicher Ordner

- **WHEN** ein aufgelistetes Verzeichnis kein `.git` enthält
- **THEN** ist der Eintrag nicht als Git-Repository markiert, bleibt aber navigierbar

### Requirement: Fehlerhafte Pfade

Nicht existierende oder nicht lesbare Pfade MÜSSEN mit einer eindeutigen Fehlermeldung beantwortet werden, ohne Details über das umgebende Dateisystem preiszugeben. Die Prüfung gegen die Root-Allowlist MUSS vor der Existenzprüfung erfolgen.

#### Scenario: Pfad existiert nicht

- **WHEN** ein Client ein Verzeichnis innerhalb eines erlaubten Roots auflistet, das nicht existiert
- **THEN** antwortet der Router mit `404`

#### Scenario: Keine Leseberechtigung

- **WHEN** das Betriebssystem den Lesezugriff auf ein erlaubtes Verzeichnis verweigert
- **THEN** antwortet der Router mit einem Fehler, der die fehlende Berechtigung benennt, statt eine leere Liste zurückzugeben

#### Scenario: Nicht existierender Pfad außerhalb der Roots

- **WHEN** ein Client einen nicht existierenden Pfad außerhalb aller Roots auflistet
- **THEN** antwortet der Router mit `403` und nicht mit `404`, sodass die Existenz von Pfaden außerhalb nicht ableitbar ist
