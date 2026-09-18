# moco-jira-bridge — Design

Datum: 2026-09-18
Status: freigegeben, bereit für Implementierungsplan

## Problem

Zeiten werden in Moco auf Kundenprojekte gebucht (Tagesgeschäft und Roadmap
in getrennten Moco-Projekten). Die Rechnungsstellung über Moco setzt voraus,
dass dieselben Zeiten auch in Jira/Tempo des Kunden stehen — mit exakt
übereinstimmenden Beträgen. Moco ist immer das führende System.

Gebraucht werden zwei Fähigkeiten:

1. **Validierung** — Zusicherung, dass Moco und Jira/Tempo für einen Monat
   oder bis zu einem Stichtag übereinstimmen.
2. **Sync** — fehlende Zeiten aus Moco in Jira/Tempo nachziehen.

## Entscheidungen

| Frage | Entscheidung |
|---|---|
| Vergleichsebene | Pro Ticket und Tag, dazu Tages-, Wochen- und Monatssummen |
| Sync-Richtung | Ausschließlich Moco → Jira/Tempo |
| Datum beim Nachziehen | Moco-Datum wird übernommen (damit Ticket+Tag konsistent ist) |
| Ticket-Erkennung | Regex auf die Moco-Beschreibung |
| Moco-Eintrag ohne Ticket | Als Fehler melden, Lauf nicht abbrechen; erscheint bei jedem weiteren Lauf erneut |
| Idempotenz | Differenzbuchung je (Ticket, Tag) — kein Marker, keine State-Datei |
| Tempo-Überhang ohne Moco-Entsprechung | Nur melden, nie löschen |
| Scope | Nur konfigurierte Moco-Projekt-IDs |
| Konfiguration | YAML-Datei; API-Tokens aus Umgebungsvariablen |
| Sync-Default | Dry-Run; Schreiben nur mit `--apply` |
| Jira-Variante | Offen — hinter Interface gekapselt, erste Implementierung Jira Cloud native Worklogs |

## Architektur

Ein gemeinsamer Kern, zwei Darstellungen. Beide Kommandos benutzen dieselbe
Diff-Engine; `validate` rendert das Ergebnis, `sync` leitet Schreibaktionen
daraus ab. Damit kann der Sync per Konstruktion nichts anderes für korrekt
halten als die Validierung.

```
cmd/mjb            CLI (validate, sync, doctor)
internal/config    YAML laden, Env-Tokens, Validierung der Konfiguration
internal/model     Entry, Worklog, Diff, Problem
internal/diff      reine Vergleichslogik, kein IO
internal/moco      TimeSource-Implementierung
internal/jira      WorklogTarget-Implementierungen + Auswahl per Config
internal/report    Tabellen- und JSON-Ausgabe
```

### Datenmodell

```go
type Entry struct {
    Ticket      string        // z.B. "ABC-1234", leer wenn nicht erkannt
    Date        time.Time     // Kalendertag, lokale Zeitzone, auf 00:00 normalisiert
    Duration    time.Duration // in Minuten-Granularität
    Description string
    Source      Source        // Moco | Jira
    SourceID    string
    Marker      string        // Klammerzusatz aus der Moco-Beschreibung, z.B. "TG"
    ProjectID   string        // Moco-Projekt-ID
}

type Diff struct {
    Ticket string
    Date   time.Time
    Want   time.Duration // Moco
    Have   time.Duration // Jira/Tempo
    Delta  time.Duration // Want - Have
}
```

### Diff-Engine

Gruppiert beide Seiten nach `(Ticket, Datum)` und erzeugt je Gruppe einen
`Diff`. Tages-, Wochen-, Monats- und Ticketsummen werden aus denselben
Gruppen aggregiert — abgeleitet, nicht getrennt berechnet, damit die Ebenen
nicht auseinanderlaufen können.

Zusätzlich erkennt die Engine drei Problemklassen:

- **Kein Ticket-Key** in einer Moco-Beschreibung.
- **Marker passt nicht zum Projekt** — der Klammerzusatz (TG/RM) widerspricht
  dem für das Moco-Projekt konfigurierten Marker. Das automatisiert die
  bisherige manuelle Sichtprüfung.
- **Überhang in Jira** — Worklog ohne Moco-Entsprechung.

Alle Zeiten werden auf volle Minuten gerundet, bevor verglichen wird.

### Provider

```go
type TimeSource interface {
    Entries(ctx context.Context, from, to time.Time) ([]Entry, error)
}

type WorklogTarget interface {
    Worklogs(ctx context.Context, from, to time.Time) ([]Entry, error)
    Create(ctx context.Context, w Worklog) error
}
```

**Moco**: `GET /api/v1/activities` mit `from`/`to`, gefiltert auf die
konfigurierten `project_id`s und den eigenen User. Auth:
`Authorization: Token token=<MOCO_API_KEY>`.

**Jira**: `target` in der Config wählt die Implementierung.

- `jira-cloud` (erste Implementierung): `/rest/api/3/issue/{key}/worklog`,
  Basic Auth mit Mail + API-Token. Funktioniert ohne zusätzliche
  Tempo-Rechte; Tempo sieht diese Einträge ebenfalls.
- `tempo-cloud`: Stub mit klarer Fehlermeldung.
- `tempo-server`: Stub mit klarer Fehlermeldung.

Die Stubs werden aktiviert, sobald geklärt ist, was der Kunde einsetzt. Das
Kommando `doctor` prüft die konfigurierte Instanz und meldet, welcher
`target`-Wert passt.

## Sync-Semantik

Aus jeder Diff-Gruppe folgt genau eine Aktion:

| Fall | Aktion |
|---|---|
| `Want > Have` | Worklog über die Differenz anlegen, auf dem Moco-Datum, mit der Moco-Beschreibung |
| `Want == Have` | nichts |
| `Want < Have` | nur melden (Überhang in Jira) |
| kein Ticket-Key | melden, Lauf geht weiter |
| Ticket existiert nicht / API-Fehler | melden, Lauf geht weiter |

Weil nur die Differenz gebucht wird, ist ein zweiter Lauf ohne Änderungen in
Moco automatisch ein No-Op — ohne Marker und ohne lokalen State.

Fehler werden pro Eintrag gesammelt und gebündelt am Ende ausgegeben. Ein
einzelner Fehler beeinträchtigt den restlichen Lauf nicht; er erscheint beim
nächsten Start erneut, solange er nicht behoben ist.

Ohne `--apply` wird ausschließlich der Plan gedruckt.

## CLI

```
mjb validate --month 2026-09
mjb validate --from 2026-09-01 --to 2026-09-18
mjb validate --until 2026-09-18          # laufender Monat bis Stichtag
mjb sync --month 2026-09 [--apply]
mjb doctor
```

- Default-Zeitraum: laufender Monat bis heute.
- `--json` für maschinenlesbare Ausgabe, sonst Tabelle.
- Exit-Codes: `0` sauber, `1` Abweichungen oder gemeldete Probleme,
  `2` Konfigurations- oder Verbindungsfehler.

## Konfiguration

`~/.config/moco-jira-bridge/config.yaml`:

```yaml
moco:
  domain: mayflower
  user_id: 12345
  projects:
    - id: 1001
      marker: TG
    - id: 1002
      marker: RM

jira:
  target: jira-cloud
  base_url: https://kunde.atlassian.net
  user: jo.brunner@mayflower.de

ticket:
  pattern: '^([A-Z][A-Z0-9]+-\d+)'
  marker_pattern: '\(([^)]+)\)'
```

Tokens ausschließlich aus der Umgebung: `MOCO_API_KEY`, `JIRA_API_TOKEN`.
Fehlt ein Token, bricht das Tool mit Exit-Code 2 und einem Hinweis ab.

## Tests

- **`internal/diff`** — der Ort, an dem die Logik falsch sein kann, und ohne
  Netzwerk testbar. TDD mit Tabellentests: Rundung, Zeitzonen bei der
  Datumsnormalisierung, Wochengrenzen (ISO-Woche, Monatsübergang),
  Überhang, fehlende Ticket-Keys, Marker-Konflikte.
- **Provider** — Tests gegen `httptest`-Server mit aufgezeichneten
  Antwortformen; kein Zugriff auf echte Systeme im Testlauf.
- **CLI** — Ende-zu-Ende gegen Fake-Provider: Exit-Codes, Dry-Run schreibt
  nichts, Fehler brechen den Lauf nicht ab.

## Bewusst nicht enthalten

- Löschen oder Ändern bestehender Jira-Worklogs.
- Sync in Gegenrichtung (Jira → Moco).
- Lokale Datenbank oder Sync-Historie.
- Mehrbenutzerbetrieb.
