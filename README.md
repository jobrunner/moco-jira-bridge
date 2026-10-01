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

## Konfiguration

Die Konfiguration ist eine einzige YAML-Datei. Secrets stehen nie darin — die
beiden Tokens kommen ausschließlich aus der Umgebung.

### Wo die Datei gesucht wird

In dieser Reihenfolge:

1. `--config PFAD`, falls angegeben
2. `./config.yaml` im Arbeitsverzeichnis (praktisch im Checkout)
3. `$XDG_CONFIG_HOME/moco-jira-bridge/config.yaml`, falls `XDG_CONFIG_HOME` gesetzt ist
4. `~/.config/moco-jira-bridge/config.yaml`

### Umgebungsvariablen

| Variable           | Pflicht | Bedeutung                                      |
| ------------------ | ------- | ---------------------------------------------- |
| `MOCO_API_KEY`   | ja      | Moco-API-Token                                 |
| `JIRA_API_TOKEN` | ja      | Jira-API-Token (Scopes siehe *Einrichtung*) |

Fehlt eine der beiden, bricht jedes Kommando mit Exit-Code 2 ab — auch `doctor`.

### Vollständiges Beispiel

```yaml
moco:
  domain: schluesselblume
  user_id: 12345             # eigene Moco-User-ID
  projects:
    - id: "1001"
      marker: TG             # Tagesgeschäft
    - id: "1002"
      marker: RM             # Roadmap

jira:
  target: jira-cloud         # jira-cloud | tempo-cloud | tempo-server
  base_url: https://kunde.atlassian.net
  user: mein.name@schluesselblume.de

ticket:
  pattern: '^([A-Z][A-Z0-9]+-\d+)'
  marker_pattern: '\(([^)]+)\)'
  default_ticket: JIRAPROJ-123   # leer = aus

sync:
  start_time: "08:30"        # Default 09:00
  timezone: Europe/Berlin    # Default: lokale Zeitzone
```

### Die Felder im Einzelnen

#### `moco`

| Feld         | Pflicht | Default | Bedeutung                                                                                                                                 |
| ------------ | ------- | ------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `domain`   | ja      | —      | Subdomain der Moco-Instanz; die URL wird als`https://<domain>.mocoapp.com` gebaut. Nur die Subdomain, kein Protokoll.                   |
| `user_id`  | ja      | —      | Eigene Moco-User-ID (Zahl). Es werden ausschließlich die eigenen Buchungen gelesen.                                                      |
| `projects` | ja      | —      | Liste der betrachteten Moco-Projekte. Leer ist ein Konfigurationsfehler — ohne Projekt-IDs weiß das Tool nicht, worauf es schauen soll. |

Jeder Eintrag unter `projects`:

| Feld       | Pflicht | Bedeutung                                                                                                              |
| ---------- | ------- | ---------------------------------------------------------------------------------------------------------------------- |
| `id`     | ja      | Moco-Projekt-ID, als String in Anführungszeichen.                                                                     |
| `marker` | nein    | Erwarteter Klammerzusatz in der Buchungsbeschreibung. Leer oder weggelassen = für dieses Projekt wird nicht geprüft. |

Zur Marker-Prüfung: gemeldet wird nur ein *widersprechender* Marker. Ein
fehlender Marker ist in Ordnung — die Projektzuordnung steht ohnehin in Moco
fest. Der Marker gilt an jeder Komma-Position und darf einen Zusatz tragen:
bei `marker: RM` passen `(RM)`, `(Daily, RM)`, `(Klärung, Umsetzung, RM)` und
`(RM Sprint 4)`. Groß-/Kleinschreibung spielt keine Rolle.

#### `jira`

| Feld         | Pflicht | Default        | Bedeutung                                                                                                                                                                                    |
| ------------ | ------- | -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `target`   | nein    | `jira-cloud` | `jira-cloud` schreibt in die nativen Jira-Worklogs; Tempo liest die mit, deshalb braucht es keine Tempo-Rechte. `tempo-cloud` und `tempo-server` sind Stubs und brechen beim Start ab. |
| `base_url` | ja      | —             | Root der Jira-API, ohne abschließendes Schräger (Slash).                                                                                                                                  |
| `user`     | ja      | —             | Atlassian-Account (E-Mail). Zusammen mit`JIRA_API_TOKEN` ergibt das die Basic Auth.                                                                                                        |

Zu `base_url`: funktioniert hat bisher die Variante über die Cloud-ID,
`https://api.atlassian.com/ex/jira/<cloud-id>`. Die direkte Instanz-URL
`https://kunde.atlassian.net` kann je nach Token-Typ an der Authentifizierung
scheitern — `mjb doctor` sagt, welche der beiden hier trägt.

#### `ticket`

| Feld               | Pflicht | Default                   | Bedeutung                                                            |
| ------------------ | ------- | ------------------------- | -------------------------------------------------------------------- |
| `pattern`        | nein    | `^([A-Z][A-Z0-9]+-\d+)` | Zieht den Jira-Ticket-Key aus der Moco-Beschreibung.                 |
| `marker_pattern` | nein    | `\(([^)]+)\)`           | Zieht den Validierungsmarker aus der Moco-Beschreibung.              |
| `default_ticket` | nein    | leer (= aus)              | Auffangticket für Buchungen auf Tickets, die es in Jira nicht gibt. |

Beide Muster sind Go-Regexes und brauchen **genau eine Capture-Group** — deren
Inhalt ist der gesuchte Wert. Ohne Capture-Group bricht der Start mit einer
Fehlermeldung ab. Findet ein Muster nichts, bleibt der Wert leer; eine Buchung
ohne erkennbaren Ticket-Key wird gemeldet, aber nicht gebucht.

Ist `default_ticket` gesetzt, landen Buchungen auf nicht existierenden Tickets
(etwa dem Platzhalter `PHT-0`) dort und erscheinen als Hinweis. Ist es leer,
entfällt die Umleitung.

#### `sync`

| Feld           | Pflicht | Default         | Bedeutung                                                                                   |
| -------------- | ------- | --------------- | ------------------------------------------------------------------------------------------- |
| `start_time` | nein    | `09:00`       | Startzeit des ersten Worklogs eines Tages, Format`HH:MM`. Jeder weitere folgt lückenlos. |
| `timezone`   | nein    | lokale Zeitzone | IANA-Name, z.B.`Europe/Berlin`.                                                           |

Die Uhrzeiten sind reine Kosmetik für die Sichtprüfung in Tempo — abgerechnet
wird der Zeitbetrag. Ein ungültiges Format bzw. ein unbekannter Zonenname ist
ein Konfigurationsfehler (Exit-Code 2).

### Prüfen

    ./mjb doctor

meldet, ob die Datei gelesen werden konnte, beide Tokens gesetzt sind und
beide Systeme antworten.

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
