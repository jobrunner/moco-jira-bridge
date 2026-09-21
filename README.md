# moco-jira-bridge

Harnessfreies AdHoc-CLI-Tool, das Moco-Zeitbuchungen mit einem gültigen JIRA-Ticket und konfigurierten Moco-Projekten mit einem Jira/Tempo abgleicht.

Moco ist immer das führende System und das Tool synct ausschließlich nach Jira (in die worklogs der Tickets), nie nach Moco. Aber aktuell auch nicht ins "Schatten"-Tempo. Daraus folgt: Fehlverhalten des Tools wird mit handgeschriebenen CURL-Requests nicht unter einem Monat bestraft.

Eine persönliche Bitte, sprich Dschira, nicht Tschaira. Man sagt auch nicht Kwentin Tarantaino.

## Installation

    go build -o mjb ./cmd/mjb

## Einrichtung

    mkdir -p ~/.config/moco-jira-bridge
    cp config.example.yaml ~/.config/moco-jira-bridge/config.yaml
    $EDITOR ~/.config/moco-jira-bridge/config.yaml

Es werden ein Moco-API-Token und weil das Tooling aktuell direkt auf die JIRA-Worklogs geht, auch ein JIRA-API-Token (bitte) mit etwas Einschränkung, benötigt:

- Scope-Typ: Classic
- Scope Namen:
  - read:me
  - read:jira-work
  - write:jira-work

Die Tokens müssen dann ins Environment (z.B. direnv):

    export MOCO_API_KEY="...
    export JIRA_API_TOKEN="...

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
Die Worklogs eines Tages werden gestaffelt: der erste beginnt zur
konfigurierten `sync.start_time` (Default 09:00), jeder weitere direkt nach
dem Ende des vorherigen — bereits in Jira gebuchte Zeit verschiebt den
Startpunkt nach hinten. Die Uhrzeiten sind Kosmetik für die manuelle Prüfung
in Tempo; abgerechnet wird der Zeitbetrag.
Ein zweiter Lauf ohne Änderungen in Moco ist damit automatisch ein No-Op —
es gibt weder Marker in den Jira-Kommentaren noch eine lokale State-Datei.

Was zwar gemeldet, nicht aber automatisch behoben wird:

- Moco-Buchungen ohne erkennbaren Ticket-Key (keine Arme, keine Kekse)
- Buchungen mit einem Marker, der dem Moco-Projekt widerspricht (nur für meine wirre Sichtprüfung; ein fehlender Marker ist kein Problem)
- Worklogs in Jira ohne Entsprechung in Moco

Buchungen auf Tickets, die in Jira nicht existieren (z.B. ein
Platzhalter-Ticket, wie es manche Team gerne machen  PROJ-0), werden auf `ticket.default_ticket` umgeleitet und als Hinweis gemeldet. Der Hinweis beeinflusst den Exit-Code nicht, macht
aber Tippfehler in Ticket-Nummern sichtbar. Für rote consolen bitte ein PR aufmachen.

Ein Fehler bei einem einzelnen Eintrag bricht den Lauf nicht ab. Er erscheint
beim nächsten Start erneut, solange er nicht behoben ist.

## Exit-Codes

| Code | Bedeutung                              |
| ---- | -------------------------------------- |
| 0    | alles stimmt überein                  |
| 1    | Abweichungen oder Hinweise             |
| 2    | Konfigurations- oder Verbindungsfehler |

## Offen gelassen

- `tempo-cloud` und `tempo-server` sind Stubs. `mjb doctor` meldet, welche
  Variante die Kundeninstanz anbietet; die passende Implementierung wird
  danach ergänzt. Unit-Tests sind zwar da, aber keine Gremlins und auch sonst keine Gates.
- In der CI ausführen, für ganz selbstbewusste Anwender.

## Lizenz

WTFPL
