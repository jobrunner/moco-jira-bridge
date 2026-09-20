# moco-jira-bridge

Gleicht Moco-Zeitbuchungen mit Jira/Tempo ab. Moco ist immer das führende
System — dieses Tool schreibt ausschließlich nach Jira, nie nach Moco, und
es löscht oder ändert dort nichts.

## Installation

    go build -o mjb ./cmd/mjb

## Einrichtung

    mkdir -p ~/.config/moco-jira-bridge
    cp config.example.yaml ~/.config/moco-jira-bridge/config.yaml
    $EDITOR ~/.config/moco-jira-bridge/config.yaml

Tokens kommen aus der Umgebung, nie aus der Datei:

    export MOCO_API_KEY=...
    export JIRA_API_TOKEN=...

Dann prüfen, ob beide Systeme erreichbar sind:

    ./mjb doctor

## Benutzung

Prüfen, ob der laufende Monat bis heute übereinstimmt:

    ./mjb validate

Ganzen Monat prüfen:

    ./mjb validate --month 2026-09

Sehen, was in Jira fehlt — ohne etwas zu schreiben:

    ./mjb sync --month 2026-09

Fehlende Zeiten tatsächlich nachtragen:

    ./mjb sync --month 2026-09 --apply

## Wie der Abgleich funktioniert

Beide Seiten werden auf `(Ticket, Tag, Dauer)` normalisiert und nach Ticket
und Kalendertag gruppiert. Verglichen wird auf Minutengenauigkeit, zusätzlich
auf Tages-, Wochen- und Monatssummen.

Beim Sync wird je Gruppe nur die **Differenz** gebucht, auf dem Moco-Datum.
Ein zweiter Lauf ohne Änderungen in Moco ist damit automatisch ein No-Op —
es gibt weder Marker in den Jira-Kommentaren noch eine lokale State-Datei.

Was gemeldet, aber nicht automatisch behoben wird:

- Moco-Buchungen ohne erkennbaren Ticket-Key
- Buchungen, deren Marker nicht zum Moco-Projekt passt (die TG/RM-Sichtprüfung)
- Worklogs in Jira ohne Entsprechung in Moco

Ein Fehler bei einem einzelnen Eintrag bricht den Lauf nie ab. Er erscheint
beim nächsten Start erneut, solange er nicht behoben ist.

## Exit-Codes

| Code | Bedeutung |
|---|---|
| 0 | alles stimmt überein |
| 1 | Abweichungen oder Hinweise |
| 2 | Konfigurations- oder Verbindungsfehler |

## Noch offen

- `tempo-cloud` und `tempo-server` sind Stubs. `mjb doctor` sagt, welche
  Variante die Kundeninstanz anbietet; die passende Implementierung wird
  danach ergänzt.
- Der Jira-Cloud-Client paginiert nicht: Bei mehr als 200 Issues mit eigenen
  Worklogs im Zeitraum fehlen Einträge.
