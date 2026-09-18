# moco-jira-bridge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ein Go-CLI, das Moco-Zeitbuchungen gegen Jira/Tempo validiert und fehlende Zeiten aus Moco nach Jira nachzieht.

**Architecture:** Eine gemeinsame Diff-Engine normalisiert beide Seiten auf `(Ticket, Datum, Dauer)` und erzeugt daraus Abweichungen; `validate` rendert sie, `sync` leitet Schreibaktionen ab. Netzwerkzugriff liegt hinter zwei schmalen Interfaces (`TimeSource`, `WorklogTarget`), damit die Kernlogik ohne IO testbar ist.

**Tech Stack:** Go 1.22+, Standardbibliothek, `gopkg.in/yaml.v3` für die Konfiguration, `net/http/httptest` für Provider-Tests. Keine weiteren Abhängigkeiten.

**Spec:** `docs/superpowers/specs/2026-09-18-moco-jira-bridge-design.md`

## Global Constraints

- Modulpfad: `moco-jira-bridge`. Go-Version im `go.mod`: `1.22`.
- Einzige externe Abhängigkeit: `gopkg.in/yaml.v3`. Keine CLI-Framework-Bibliothek — `flag` aus der Stdlib reicht.
- Moco ist führend. Kein Schreibzugriff auf Moco, niemals. Kein Löschen oder Ändern bestehender Jira-Worklogs.
- Alle Dauern werden intern als `time.Duration` geführt und **vor jedem Vergleich auf volle Minuten gerundet**.
- Alle Daten sind Kalendertage: `time.Time` in `time.Local`, normalisiert auf 00:00:00.
- Sync schreibt nur mit `--apply`. Ohne das Flag wird ausschließlich der Plan gedruckt.
- Ein Fehler bei einem einzelnen Eintrag bricht den Lauf nie ab. Fehler werden gesammelt und am Ende gebündelt ausgegeben.
- Exit-Codes: `0` sauber, `1` Abweichungen oder gemeldete Probleme, `2` Konfigurations- oder Verbindungsfehler.
- Tokens ausschließlich aus der Umgebung: `MOCO_API_KEY`, `JIRA_API_TOKEN`. Niemals in Dateien schreiben, niemals loggen.
- Commits direkt auf `main`, keine Branches.

---

### Task 1: Projekt-Grundgerüst und Datenmodell

**Files:**
- Create: `go.mod`
- Create: `internal/model/model.go`
- Test: `internal/model/model_test.go`

**Interfaces:**
- Consumes: nichts
- Produces:
  - `type Source string` mit Konstanten `SourceMoco Source = "moco"`, `SourceJira Source = "jira"`
  - `type Entry struct { Ticket string; Date time.Time; Duration time.Duration; Description string; Source Source; SourceID string; Marker string; ProjectID string }`
  - `type Diff struct { Ticket string; Date time.Time; Want time.Duration; Have time.Duration }`
  - `func (d Diff) Delta() time.Duration`
  - `func Day(t time.Time) time.Time`
  - `func RoundMinutes(d time.Duration) time.Duration`
  - `type ProblemKind string` mit `ProblemNoTicket`, `ProblemMarkerMismatch`, `ProblemJiraOverhang`, `ProblemAPIError`
  - `type Problem struct { Kind ProblemKind; Ticket string; Date time.Time; Detail string }`

- [ ] **Step 1: Modul anlegen**

```bash
cd /Users/jbrunner/work/projects/moco-jira-bridge
go mod init moco-jira-bridge
go mod edit -go=1.22
```

- [ ] **Step 2: Failing test schreiben**

`internal/model/model_test.go`:

```go
package model

import (
	"testing"
	"time"
)

func TestDayNormalisiertAufMitternachtLokal(t *testing.T) {
	in := time.Date(2026, 9, 18, 23, 45, 12, 500, time.Local)
	got := Day(in)
	want := time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("Day() = %v, want %v", got, want)
	}
}

func TestDayKonvertiertNachLokalVorDemAbschneiden(t *testing.T) {
	// 2026-09-18 01:30 UTC ist in Europe/Berlin (UTC+2) der 18.09. 03:30.
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("Zeitzonendaten nicht verfügbar")
	}
	in := time.Date(2026, 9, 18, 1, 30, 0, 0, time.UTC)
	got := Day(in.In(berlin))
	if got.Day() != 18 || got.Month() != time.September {
		t.Fatalf("Day() = %v, want 2026-09-18", got)
	}
}

func TestRoundMinutes(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"exakt", 90 * time.Minute, 90 * time.Minute},
		{"abrunden", 90*time.Minute + 20*time.Second, 90 * time.Minute},
		{"aufrunden", 90*time.Minute + 40*time.Second, 91 * time.Minute},
		{"float-artefakt 0.1h", time.Duration(0.1*float64(time.Hour)) + 3, 6 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RoundMinutes(c.in); got != c.want {
				t.Fatalf("RoundMinutes(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestDiffDelta(t *testing.T) {
	d := Diff{Want: 120 * time.Minute, Have: 90 * time.Minute}
	if got := d.Delta(); got != 30*time.Minute {
		t.Fatalf("Delta() = %v, want 30m", got)
	}
	d = Diff{Want: 60 * time.Minute, Have: 90 * time.Minute}
	if got := d.Delta(); got != -30*time.Minute {
		t.Fatalf("Delta() = %v, want -30m", got)
	}
}
```

- [ ] **Step 3: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/model/`
Expected: FAIL — `undefined: Day`, `undefined: RoundMinutes`, `undefined: Diff`

- [ ] **Step 4: Minimale Implementierung**

`internal/model/model.go`:

```go
// Package model enthält die gemeinsamen Datentypen von Moco- und Jira-Seite.
package model

import "time"

type Source string

const (
	SourceMoco Source = "moco"
	SourceJira Source = "jira"
)

// Entry ist eine einzelne Zeitbuchung, normalisiert aus einem der beiden Systeme.
type Entry struct {
	Ticket      string        // z.B. "ABC-1234"; leer, wenn nicht erkannt
	Date        time.Time     // Kalendertag, lokal, 00:00
	Duration    time.Duration
	Description string
	Source      Source
	SourceID    string
	Marker      string // Klammerzusatz aus der Moco-Beschreibung, z.B. "TG"
	ProjectID   string // Moco-Projekt-ID
}

// Diff ist der Vergleich einer (Ticket, Tag)-Gruppe zwischen beiden Systemen.
type Diff struct {
	Ticket string
	Date   time.Time
	Want   time.Duration // Moco — führend
	Have   time.Duration // Jira/Tempo
}

// Delta ist die Zeit, die in Jira noch fehlt. Negativ bedeutet Überhang in Jira.
func (d Diff) Delta() time.Duration { return d.Want - d.Have }

// Day schneidet eine Zeitangabe auf den lokalen Kalendertag zurück.
func Day(t time.Time) time.Time {
	t = t.In(time.Local)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

// RoundMinutes rundet auf volle Minuten. Alle Vergleiche laufen über diese
// Granularität, damit Float-Artefakte aus der Moco-API keine Abweichungen erzeugen.
func RoundMinutes(d time.Duration) time.Duration { return d.Round(time.Minute) }

type ProblemKind string

const (
	ProblemNoTicket       ProblemKind = "kein-ticket"
	ProblemMarkerMismatch ProblemKind = "marker-konflikt"
	ProblemJiraOverhang   ProblemKind = "jira-ueberhang"
	ProblemAPIError       ProblemKind = "api-fehler"
)

// Problem ist eine Auffälligkeit, die gemeldet, aber nicht automatisch behoben wird.
type Problem struct {
	Kind   ProblemKind
	Ticket string
	Date   time.Time
	Detail string
}
```

- [ ] **Step 5: Tests laufen lassen**

Run: `go test ./internal/model/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go.mod internal/model/
git commit -m "feat: Datenmodell für Zeitbuchungen und Abweichungen"
```

---

### Task 2: Ticket- und Marker-Parsing aus Moco-Beschreibungen

**Files:**
- Create: `internal/parse/parse.go`
- Test: `internal/parse/parse_test.go`

**Interfaces:**
- Consumes: nichts aus anderen Tasks
- Produces:
  - `type Parser struct{ ... }`
  - `func NewParser(ticketPattern, markerPattern string) (*Parser, error)`
  - `func (p *Parser) Parse(description string) (ticket, marker string)` — leere Strings, wenn nichts passt

- [ ] **Step 1: Failing test schreiben**

`internal/parse/parse_test.go`:

```go
package parse

import "testing"

const (
	ticketPat = `^([A-Z][A-Z0-9]+-\d+)`
	markerPat = `\(([^)]+)\)`
)

func TestParse(t *testing.T) {
	p, err := NewParser(ticketPat, markerPat)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	cases := []struct {
		name       string
		in         string
		wantTicket string
		wantMarker string
	}{
		{"ticket und marker", "ABC-1234 Fehler im Import (TG)", "ABC-1234", "TG"},
		{"ticket ohne marker", "ABC-1234 Fehler im Import", "ABC-1234", ""},
		{"roadmap-marker", "ABC-99 Neues Dashboard (RM Sprint 4)", "ABC-99", "RM Sprint 4"},
		{"kein ticket", "Interne Abstimmung (TG)", "", "TG"},
		{"ticket nicht am anfang wird nicht erkannt", "Siehe ABC-1234", "", ""},
		{"leere beschreibung", "", "", ""},
		{"mehrere klammern: erste gewinnt", "ABC-1 Text (TG) mehr (RM)", "ABC-1", "TG"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ticket, marker := p.Parse(c.in)
			if ticket != c.wantTicket || marker != c.wantMarker {
				t.Fatalf("Parse(%q) = (%q, %q), want (%q, %q)",
					c.in, ticket, marker, c.wantTicket, c.wantMarker)
			}
		})
	}
}

func TestNewParserLehntUngueltigesPatternAb(t *testing.T) {
	if _, err := NewParser(`([A-Z`, markerPat); err == nil {
		t.Fatal("NewParser sollte bei ungültigem Ticket-Pattern einen Fehler liefern")
	}
}

func TestNewParserBrauchtCaptureGroup(t *testing.T) {
	if _, err := NewParser(`^[A-Z]+-\d+`, markerPat); err == nil {
		t.Fatal("NewParser sollte ein Pattern ohne Capture-Group ablehnen")
	}
}
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/parse/`
Expected: FAIL — `undefined: NewParser`

- [ ] **Step 3: Implementierung**

`internal/parse/parse.go`:

```go
// Package parse zieht Jira-Ticket-Key und Validierungsmarker aus einer
// Moco-Buchungsbeschreibung. Beide Muster sind konfigurierbar.
package parse

import (
	"fmt"
	"regexp"
	"strings"
)

type Parser struct {
	ticket *regexp.Regexp
	marker *regexp.Regexp
}

// NewParser übersetzt die konfigurierten Muster. Beide brauchen genau eine
// Capture-Group — sie liefert den gesuchten Wert.
func NewParser(ticketPattern, markerPattern string) (*Parser, error) {
	ticket, err := compileWithGroup(ticketPattern, "ticket")
	if err != nil {
		return nil, err
	}
	marker, err := compileWithGroup(markerPattern, "marker")
	if err != nil {
		return nil, err
	}
	return &Parser{ticket: ticket, marker: marker}, nil
}

func compileWithGroup(pattern, name string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s-Pattern %q ist kein gültiger regulärer Ausdruck: %w", name, pattern, err)
	}
	if re.NumSubexp() < 1 {
		return nil, fmt.Errorf("%s-Pattern %q braucht eine Capture-Group, z.B. (…)", name, pattern)
	}
	return re, nil
}

// Parse liefert Ticket-Key und Marker. Was nicht gefunden wird, kommt leer zurück.
func (p *Parser) Parse(description string) (ticket, marker string) {
	if m := p.ticket.FindStringSubmatch(description); m != nil {
		ticket = strings.TrimSpace(m[1])
	}
	if m := p.marker.FindStringSubmatch(description); m != nil {
		marker = strings.TrimSpace(m[1])
	}
	return ticket, marker
}
```

- [ ] **Step 4: Tests laufen lassen**

Run: `go test ./internal/parse/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/parse/
git commit -m "feat: Ticket- und Marker-Parsing aus Moco-Beschreibungen"
```

---

### Task 3: Diff-Engine — Gruppierung nach Ticket und Tag

**Files:**
- Create: `internal/diff/diff.go`
- Test: `internal/diff/diff_test.go`

**Interfaces:**
- Consumes: `model.Entry`, `model.Diff`, `model.Day`, `model.RoundMinutes`, `model.Problem`, `model.ProblemNoTicket`, `model.ProblemJiraOverhang`
- Produces:
  - `type Result struct { Diffs []model.Diff; Problems []model.Problem }`
  - `func Compare(moco, jira []model.Entry) Result`
  - `Result.Diffs` ist sortiert nach Datum, dann Ticket — deterministische Ausgabe.

- [ ] **Step 1: Failing test schreiben**

`internal/diff/diff_test.go`:

```go
package diff

import (
	"testing"
	"time"

	"moco-jira-bridge/internal/model"
)

func day(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.Local) }

func mocoEntry(ticket string, d int, min int, desc string) model.Entry {
	return model.Entry{
		Ticket: ticket, Date: day(d), Duration: time.Duration(min) * time.Minute,
		Description: desc, Source: model.SourceMoco,
	}
}

func jiraEntry(ticket string, d int, min int) model.Entry {
	return model.Entry{
		Ticket: ticket, Date: day(d), Duration: time.Duration(min) * time.Minute,
		Source: model.SourceJira,
	}
}

func findDiff(t *testing.T, r Result, ticket string, d int) model.Diff {
	t.Helper()
	for _, df := range r.Diffs {
		if df.Ticket == ticket && df.Date.Equal(day(d)) {
			return df
		}
	}
	t.Fatalf("kein Diff für %s am %v gefunden: %+v", ticket, day(d), r.Diffs)
	return model.Diff{}
}

func TestCompareFehlendeZeitInJira(t *testing.T) {
	r := Compare(
		[]model.Entry{mocoEntry("ABC-1", 18, 120, "ABC-1 (TG)")},
		[]model.Entry{jiraEntry("ABC-1", 18, 90)},
	)
	got := findDiff(t, r, "ABC-1", 18)
	if got.Delta() != 30*time.Minute {
		t.Fatalf("Delta = %v, want 30m", got.Delta())
	}
}

func TestCompareSummiertMehrereEintraegeProTicketUndTag(t *testing.T) {
	r := Compare(
		[]model.Entry{
			mocoEntry("ABC-1", 18, 60, "ABC-1 vormittags (TG)"),
			mocoEntry("ABC-1", 18, 45, "ABC-1 nachmittags (TG)"),
		},
		[]model.Entry{jiraEntry("ABC-1", 18, 60)},
	)
	got := findDiff(t, r, "ABC-1", 18)
	if got.Want != 105*time.Minute || got.Have != 60*time.Minute {
		t.Fatalf("Want/Have = %v/%v, want 105m/60m", got.Want, got.Have)
	}
}

func TestCompareTrenntNachTag(t *testing.T) {
	r := Compare(
		[]model.Entry{mocoEntry("ABC-1", 17, 60, "x"), mocoEntry("ABC-1", 18, 30, "x")},
		[]model.Entry{jiraEntry("ABC-1", 17, 60)},
	)
	if d := findDiff(t, r, "ABC-1", 17); d.Delta() != 0 {
		t.Fatalf("17.09.: Delta = %v, want 0", d.Delta())
	}
	if d := findDiff(t, r, "ABC-1", 18); d.Delta() != 30*time.Minute {
		t.Fatalf("18.09.: Delta = %v, want 30m", d.Delta())
	}
}

func TestCompareMeldetJiraUeberhang(t *testing.T) {
	r := Compare(nil, []model.Entry{jiraEntry("ABC-9", 18, 30)})
	var found bool
	for _, p := range r.Problems {
		if p.Kind == model.ProblemJiraOverhang && p.Ticket == "ABC-9" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Überhang nicht gemeldet: %+v", r.Problems)
	}
}

func TestCompareMeldetMocoEintragOhneTicket(t *testing.T) {
	r := Compare([]model.Entry{mocoEntry("", 18, 60, "Interne Abstimmung")}, nil)
	var found bool
	for _, p := range r.Problems {
		if p.Kind == model.ProblemNoTicket {
			found = true
			if p.Detail == "" {
				t.Fatal("Detail sollte die Beschreibung nennen")
			}
		}
	}
	if !found {
		t.Fatalf("fehlender Ticket-Key nicht gemeldet: %+v", r.Problems)
	}
	if len(r.Diffs) != 0 {
		t.Fatalf("Einträge ohne Ticket dürfen keinen Diff erzeugen: %+v", r.Diffs)
	}
}

func TestCompareRundetAufMinuten(t *testing.T) {
	// 0.1h aus der Moco-API kann als 5m59.9s ankommen.
	m := mocoEntry("ABC-1", 18, 0, "x")
	m.Duration = time.Duration(0.1 * float64(time.Hour))
	r := Compare([]model.Entry{m}, []model.Entry{jiraEntry("ABC-1", 18, 6)})
	if d := findDiff(t, r, "ABC-1", 18); d.Delta() != 0 {
		t.Fatalf("Delta = %v, want 0 (Rundung auf Minuten)", d.Delta())
	}
}

func TestCompareSortiertDeterministisch(t *testing.T) {
	r := Compare(
		[]model.Entry{mocoEntry("B-2", 19, 30, "x"), mocoEntry("A-1", 18, 30, "x"), mocoEntry("A-2", 18, 30, "x")},
		nil,
	)
	want := []string{"A-1", "A-2", "B-2"}
	if len(r.Diffs) != 3 {
		t.Fatalf("erwarte 3 Diffs, got %d", len(r.Diffs))
	}
	for i, w := range want {
		if r.Diffs[i].Ticket != w {
			t.Fatalf("Diffs[%d].Ticket = %s, want %s", i, r.Diffs[i].Ticket, w)
		}
	}
}
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/diff/`
Expected: FAIL — `undefined: Compare`

- [ ] **Step 3: Implementierung**

`internal/diff/diff.go`:

```go
// Package diff vergleicht Moco- und Jira-Buchungen. Reine Logik, kein IO.
package diff

import (
	"fmt"
	"sort"
	"time"

	"moco-jira-bridge/internal/model"
)

// Result ist das Ergebnis eines Vergleichs: Abweichungen plus Auffälligkeiten,
// die gemeldet, aber nicht automatisch behoben werden.
type Result struct {
	Diffs    []model.Diff
	Problems []model.Problem
}

type key struct {
	ticket string
	date   time.Time
}

// Compare gruppiert beide Seiten nach (Ticket, Tag) und bildet je Gruppe einen Diff.
// Moco ist führend: Want kommt aus Moco, Have aus Jira.
func Compare(moco, jira []model.Entry) Result {
	want := map[key]time.Duration{}
	have := map[key]time.Duration{}
	var problems []model.Problem

	for _, e := range moco {
		if e.Ticket == "" {
			problems = append(problems, model.Problem{
				Kind: model.ProblemNoTicket,
				Date: model.Day(e.Date),
				Detail: fmt.Sprintf("Moco-Buchung ohne Ticket-Key: %q (%s)",
					e.Description, model.RoundMinutes(e.Duration)),
			})
			continue
		}
		k := key{e.Ticket, model.Day(e.Date)}
		want[k] += e.Duration
	}

	for _, e := range jira {
		if e.Ticket == "" {
			continue
		}
		k := key{e.Ticket, model.Day(e.Date)}
		have[k] += e.Duration
	}

	keys := map[key]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range have {
		keys[k] = true
	}

	diffs := make([]model.Diff, 0, len(keys))
	for k := range keys {
		d := model.Diff{
			Ticket: k.ticket,
			Date:   k.date,
			Want:   model.RoundMinutes(want[k]),
			Have:   model.RoundMinutes(have[k]),
		}
		diffs = append(diffs, d)
		if d.Delta() < 0 {
			problems = append(problems, model.Problem{
				Kind:   model.ProblemJiraOverhang,
				Ticket: d.Ticket,
				Date:   d.Date,
				Detail: fmt.Sprintf("in Jira %s gebucht, in Moco nur %s", d.Have, d.Want),
			})
		}
	}

	sort.Slice(diffs, func(i, j int) bool {
		if !diffs[i].Date.Equal(diffs[j].Date) {
			return diffs[i].Date.Before(diffs[j].Date)
		}
		return diffs[i].Ticket < diffs[j].Ticket
	})
	sortProblems(problems)

	return Result{Diffs: diffs, Problems: problems}
}

func sortProblems(ps []model.Problem) {
	sort.Slice(ps, func(i, j int) bool {
		if !ps[i].Date.Equal(ps[j].Date) {
			return ps[i].Date.Before(ps[j].Date)
		}
		if ps[i].Kind != ps[j].Kind {
			return ps[i].Kind < ps[j].Kind
		}
		return ps[i].Ticket < ps[j].Ticket
	})
}
```

- [ ] **Step 4: Tests laufen lassen**

Run: `go test ./internal/diff/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/diff/
git commit -m "feat: Diff-Engine für Ticket/Tag-Vergleich"
```

---

### Task 4: Marker-Prüfung und Summenaggregation

**Files:**
- Create: `internal/diff/marker.go`
- Create: `internal/diff/totals.go`
- Test: `internal/diff/marker_test.go`
- Test: `internal/diff/totals_test.go`

**Interfaces:**
- Consumes: `model.Entry`, `model.Problem`, `model.Diff`, `diff.Result`
- Produces:
  - `func CheckMarkers(moco []model.Entry, expected map[string]string) []model.Problem` — `expected` bildet Moco-Projekt-ID auf erwarteten Marker ab
  - `type Total struct { Label string; Want time.Duration; Have time.Duration }`
  - `func (t Total) Delta() time.Duration`
  - `type Totals struct { Days []Total; Weeks []Total; Months []Total; Tickets []Total }`
  - `func Aggregate(diffs []model.Diff) Totals`

- [ ] **Step 1: Failing test für Marker-Prüfung**

`internal/diff/marker_test.go`:

```go
package diff

import (
	"testing"

	"moco-jira-bridge/internal/model"
)

func TestCheckMarkersMeldetKonflikt(t *testing.T) {
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "1001", Marker: "RM", Description: "ABC-1 (RM)", Date: day(18)},
	}
	problems := CheckMarkers(entries, map[string]string{"1001": "TG", "1002": "RM"})
	if len(problems) != 1 {
		t.Fatalf("erwarte 1 Problem, got %+v", problems)
	}
	if problems[0].Kind != model.ProblemMarkerMismatch || problems[0].Ticket != "ABC-1" {
		t.Fatalf("unerwartetes Problem: %+v", problems[0])
	}
}

func TestCheckMarkersAkzeptiertPassendenMarker(t *testing.T) {
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "1001", Marker: "TG", Date: day(18)},
	}
	if problems := CheckMarkers(entries, map[string]string{"1001": "TG"}); len(problems) != 0 {
		t.Fatalf("erwarte keine Probleme, got %+v", problems)
	}
}

func TestCheckMarkersPraefixReichtAus(t *testing.T) {
	// "(RM Sprint 4)" gilt als RM.
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "1002", Marker: "RM Sprint 4", Date: day(18)},
	}
	if problems := CheckMarkers(entries, map[string]string{"1002": "RM"}); len(problems) != 0 {
		t.Fatalf("erwarte keine Probleme, got %+v", problems)
	}
}

func TestCheckMarkersMeldetFehlendenMarker(t *testing.T) {
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "1001", Marker: "", Date: day(18)},
	}
	if problems := CheckMarkers(entries, map[string]string{"1001": "TG"}); len(problems) != 1 {
		t.Fatalf("fehlender Marker sollte gemeldet werden, got %+v", problems)
	}
}

func TestCheckMarkersIgnoriertProjekteOhneErwartung(t *testing.T) {
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "9999", Marker: "XX", Date: day(18)},
	}
	if problems := CheckMarkers(entries, map[string]string{"1001": "TG"}); len(problems) != 0 {
		t.Fatalf("Projekte ohne konfigurierten Marker sind unauffällig, got %+v", problems)
	}
}
```

- [ ] **Step 2: Failing test für Aggregation**

`internal/diff/totals_test.go`:

```go
package diff

import (
	"testing"
	"time"

	"moco-jira-bridge/internal/model"
)

func d(ticket string, dayN, wantMin, haveMin int) model.Diff {
	return model.Diff{
		Ticket: ticket, Date: day(dayN),
		Want: time.Duration(wantMin) * time.Minute,
		Have: time.Duration(haveMin) * time.Minute,
	}
}

func total(t *testing.T, list []Total, label string) Total {
	t.Helper()
	for _, x := range list {
		if x.Label == label {
			return x
		}
	}
	t.Fatalf("kein Total mit Label %q in %+v", label, list)
	return Total{}
}

func TestAggregateTagessumme(t *testing.T) {
	tot := Aggregate([]model.Diff{d("A-1", 18, 60, 60), d("A-2", 18, 120, 90)})
	got := total(t, tot.Days, "2026-09-18")
	if got.Want != 180*time.Minute || got.Have != 150*time.Minute {
		t.Fatalf("Tag: %v/%v, want 180m/150m", got.Want, got.Have)
	}
}

func TestAggregateWochensummeNachISOWoche(t *testing.T) {
	// 2026-09-17 und 2026-09-18 liegen in derselben ISO-Woche.
	tot := Aggregate([]model.Diff{d("A-1", 17, 60, 60), d("A-1", 18, 60, 30)})
	_, week := day(18).ISOWeek()
	label := "2026-W" + pad2(week)
	got := total(t, tot.Weeks, label)
	if got.Want != 120*time.Minute || got.Have != 90*time.Minute {
		t.Fatalf("Woche: %v/%v, want 120m/90m", got.Want, got.Have)
	}
}

func TestAggregateMonatssumme(t *testing.T) {
	tot := Aggregate([]model.Diff{d("A-1", 1, 60, 60), d("A-1", 30, 60, 0)})
	got := total(t, tot.Months, "2026-09")
	if got.Want != 120*time.Minute || got.Have != 60*time.Minute {
		t.Fatalf("Monat: %v/%v, want 120m/60m", got.Want, got.Have)
	}
}

func TestAggregateTicketsummeUeberTageHinweg(t *testing.T) {
	tot := Aggregate([]model.Diff{d("A-1", 17, 60, 60), d("A-1", 18, 30, 0), d("B-2", 18, 15, 15)})
	got := total(t, tot.Tickets, "A-1")
	if got.Want != 90*time.Minute || got.Have != 60*time.Minute {
		t.Fatalf("Ticket A-1: %v/%v, want 90m/60m", got.Want, got.Have)
	}
}

func TestAggregateIstSortiert(t *testing.T) {
	tot := Aggregate([]model.Diff{d("B-1", 19, 60, 60), d("A-1", 17, 60, 60)})
	if tot.Days[0].Label != "2026-09-17" {
		t.Fatalf("Days nicht sortiert: %+v", tot.Days)
	}
	if tot.Tickets[0].Label != "A-1" {
		t.Fatalf("Tickets nicht sortiert: %+v", tot.Tickets)
	}
}
```

- [ ] **Step 3: Tests laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/diff/`
Expected: FAIL — `undefined: CheckMarkers`, `undefined: Aggregate`, `undefined: pad2`

- [ ] **Step 4: Marker-Prüfung implementieren**

`internal/diff/marker.go`:

```go
package diff

import (
	"fmt"
	"strings"

	"moco-jira-bridge/internal/model"
)

// CheckMarkers prüft den Klammerzusatz jeder Moco-Buchung gegen den für ihr
// Projekt konfigurierten Marker. Das automatisiert die bisherige Sichtprüfung,
// ob ein Ticket im richtigen Moco-Projekt gelandet ist.
//
// expected bildet Moco-Projekt-ID auf den erwarteten Marker ab. Projekte, die
// dort nicht vorkommen, werden nicht geprüft.
func CheckMarkers(moco []model.Entry, expected map[string]string) []model.Problem {
	var problems []model.Problem
	for _, e := range moco {
		want, ok := expected[e.ProjectID]
		if !ok || want == "" {
			continue
		}
		if markerMatches(e.Marker, want) {
			continue
		}
		detail := fmt.Sprintf("Projekt %s erwartet Marker %q, Buchung hat %q",
			e.ProjectID, want, e.Marker)
		if e.Marker == "" {
			detail = fmt.Sprintf("Projekt %s erwartet Marker %q, Buchung hat keinen",
				e.ProjectID, want)
		}
		problems = append(problems, model.Problem{
			Kind:   model.ProblemMarkerMismatch,
			Ticket: e.Ticket,
			Date:   model.Day(e.Date),
			Detail: detail,
		})
	}
	sortProblems(problems)
	return problems
}

// markerMatches akzeptiert Zusätze hinter dem Marker: "RM Sprint 4" gilt als "RM".
func markerMatches(got, want string) bool {
	got = strings.TrimSpace(strings.ToUpper(got))
	want = strings.TrimSpace(strings.ToUpper(want))
	if got == want {
		return true
	}
	return strings.HasPrefix(got, want+" ")
}
```

- [ ] **Step 5: Aggregation implementieren**

`internal/diff/totals.go`:

```go
package diff

import (
	"fmt"
	"sort"
	"time"

	"moco-jira-bridge/internal/model"
)

// Total ist eine Summenzeile über mehrere Diffs.
type Total struct {
	Label string
	Want  time.Duration
	Have  time.Duration
}

func (t Total) Delta() time.Duration { return t.Want - t.Have }

// Totals sind die abgeleiteten Summenebenen. Sie entstehen aus denselben
// Diff-Gruppen wie der Detailvergleich, damit die Ebenen nicht auseinanderlaufen.
type Totals struct {
	Days    []Total
	Weeks   []Total
	Months  []Total
	Tickets []Total
}

func Aggregate(diffs []model.Diff) Totals {
	days := map[string]Total{}
	weeks := map[string]Total{}
	months := map[string]Total{}
	tickets := map[string]Total{}

	for _, d := range diffs {
		year, week := d.Date.ISOWeek()
		add(days, d.Date.Format("2006-01-02"), d)
		add(weeks, fmt.Sprintf("%d-W%s", year, pad2(week)), d)
		add(months, d.Date.Format("2006-01"), d)
		add(tickets, d.Ticket, d)
	}

	return Totals{
		Days:    sorted(days),
		Weeks:   sorted(weeks),
		Months:  sorted(months),
		Tickets: sorted(tickets),
	}
}

func add(m map[string]Total, label string, d model.Diff) {
	t := m[label]
	t.Label = label
	t.Want += d.Want
	t.Have += d.Have
	m[label] = t
}

func sorted(m map[string]Total) []Total {
	out := make([]Total, 0, len(m))
	for _, t := range m {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

func pad2(n int) string { return fmt.Sprintf("%02d", n) }
```

- [ ] **Step 6: Tests laufen lassen**

Run: `go test ./internal/diff/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/diff/
git commit -m "feat: Marker-Prüfung und Tages-/Wochen-/Monats-/Ticketsummen"
```

---

### Task 5: Konfiguration

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Create: `config.example.yaml`
- Modify: `go.mod` (Abhängigkeit `gopkg.in/yaml.v3`)

**Interfaces:**
- Consumes: nichts
- Produces:
  - `type Config struct { Moco MocoConfig; Jira JiraConfig; Ticket TicketConfig }`
  - `type MocoConfig struct { Domain string; UserID int; Projects []Project; APIKey string }`
  - `type Project struct { ID string; Marker string }`
  - `type JiraConfig struct { Target string; BaseURL string; User string; APIToken string }`
  - `type TicketConfig struct { Pattern string; MarkerPattern string }`
  - `func Load(path string) (*Config, error)` — liest YAML, ergänzt Tokens aus der Umgebung, validiert
  - `func DefaultPath() string` — `$XDG_CONFIG_HOME/moco-jira-bridge/config.yaml`, sonst `~/.config/…`
  - `func (c *Config) ExpectedMarkers() map[string]string`
  - Fehler aus `Load` sind für Exit-Code 2 gedacht.

- [ ] **Step 1: Abhängigkeit hinzufügen**

```bash
go get gopkg.in/yaml.v3@v3.0.1
```

- [ ] **Step 2: Failing test schreiben**

`internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valid = `
moco:
  domain: mayflower
  user_id: 12345
  projects:
    - id: "1001"
      marker: TG
    - id: "1002"
      marker: RM
jira:
  target: jira-cloud
  base_url: https://kunde.atlassian.net
  user: jo.brunner@mayflower.de
ticket:
  pattern: '^([A-Z][A-Z0-9]+-\d+)'
  marker_pattern: '\(([^)]+)\)'
`

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadLiestConfigUndTokens(t *testing.T) {
	t.Setenv("MOCO_API_KEY", "moco-secret")
	t.Setenv("JIRA_API_TOKEN", "jira-secret")

	c, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Moco.Domain != "mayflower" || c.Moco.UserID != 12345 {
		t.Fatalf("Moco falsch geladen: %+v", c.Moco)
	}
	if c.Moco.APIKey != "moco-secret" || c.Jira.APIToken != "jira-secret" {
		t.Fatal("Tokens wurden nicht aus der Umgebung übernommen")
	}
	if c.Jira.BaseURL != "https://kunde.atlassian.net" {
		t.Fatalf("BaseURL falsch: %q", c.Jira.BaseURL)
	}
}

func TestLoadFehlerBeiFehlendemToken(t *testing.T) {
	t.Setenv("MOCO_API_KEY", "")
	t.Setenv("JIRA_API_TOKEN", "jira-secret")

	_, err := Load(write(t, valid))
	if err == nil || !strings.Contains(err.Error(), "MOCO_API_KEY") {
		t.Fatalf("erwarte Fehler zu MOCO_API_KEY, got %v", err)
	}
}

func TestLoadFehlerBeiFehlenderDatei(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "gibt-es-nicht.yaml")); err == nil {
		t.Fatal("erwarte Fehler bei fehlender Datei")
	}
}

func TestLoadValidiertPflichtfelder(t *testing.T) {
	t.Setenv("MOCO_API_KEY", "x")
	t.Setenv("JIRA_API_TOKEN", "y")

	_, err := Load(write(t, "moco:\n  domain: \"\"\n"))
	if err == nil {
		t.Fatal("erwarte Validierungsfehler bei leerer Konfiguration")
	}
}

func TestLoadSetztDefaultPatterns(t *testing.T) {
	t.Setenv("MOCO_API_KEY", "x")
	t.Setenv("JIRA_API_TOKEN", "y")

	ohnePatterns := strings.Split(valid, "ticket:")[0]
	c, err := Load(write(t, ohnePatterns))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Ticket.Pattern == "" || c.Ticket.MarkerPattern == "" {
		t.Fatal("Default-Patterns sollten gesetzt werden")
	}
}

func TestExpectedMarkers(t *testing.T) {
	t.Setenv("MOCO_API_KEY", "x")
	t.Setenv("JIRA_API_TOKEN", "y")

	c, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := c.ExpectedMarkers()
	if m["1001"] != "TG" || m["1002"] != "RM" {
		t.Fatalf("ExpectedMarkers = %+v", m)
	}
}
```

- [ ] **Step 3: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/config/`
Expected: FAIL — `undefined: Load`

- [ ] **Step 4: Implementierung**

`internal/config/config.go`:

```go
// Package config lädt die YAML-Konfiguration und die API-Tokens aus der Umgebung.
// Secrets stehen niemals in der Datei.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	EnvMocoAPIKey  = "MOCO_API_KEY"
	EnvJiraAPIToken = "JIRA_API_TOKEN"

	DefaultTicketPattern = `^([A-Z][A-Z0-9]+-\d+)`
	DefaultMarkerPattern = `\(([^)]+)\)`

	TargetJiraCloud  = "jira-cloud"
	TargetTempoCloud = "tempo-cloud"
	TargetTempoServer = "tempo-server"
)

type Project struct {
	ID     string `yaml:"id"`
	Marker string `yaml:"marker"`
}

type MocoConfig struct {
	Domain   string    `yaml:"domain"`
	UserID   int       `yaml:"user_id"`
	Projects []Project `yaml:"projects"`
	APIKey   string    `yaml:"-"` // aus der Umgebung
}

type JiraConfig struct {
	Target   string `yaml:"target"`
	BaseURL  string `yaml:"base_url"`
	User     string `yaml:"user"`
	APIToken string `yaml:"-"` // aus der Umgebung
}

type TicketConfig struct {
	Pattern       string `yaml:"pattern"`
	MarkerPattern string `yaml:"marker_pattern"`
}

type Config struct {
	Moco   MocoConfig   `yaml:"moco"`
	Jira   JiraConfig   `yaml:"jira"`
	Ticket TicketConfig `yaml:"ticket"`
}

// DefaultPath ist der Ort, an dem die Konfiguration ohne --config erwartet wird.
func DefaultPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "moco-jira-bridge", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(home, ".config", "moco-jira-bridge", "config.yaml")
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Konfiguration %s nicht lesbar: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("Konfiguration %s ist kein gültiges YAML: %w", path, err)
	}

	if c.Ticket.Pattern == "" {
		c.Ticket.Pattern = DefaultTicketPattern
	}
	if c.Ticket.MarkerPattern == "" {
		c.Ticket.MarkerPattern = DefaultMarkerPattern
	}
	if c.Jira.Target == "" {
		c.Jira.Target = TargetJiraCloud
	}

	c.Moco.APIKey = os.Getenv(EnvMocoAPIKey)
	c.Jira.APIToken = os.Getenv(EnvJiraAPIToken)

	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) validate() error {
	if c.Moco.Domain == "" {
		return fmt.Errorf("moco.domain fehlt in der Konfiguration")
	}
	if c.Moco.UserID == 0 {
		return fmt.Errorf("moco.user_id fehlt in der Konfiguration")
	}
	if len(c.Moco.Projects) == 0 {
		return fmt.Errorf("moco.projects ist leer — ohne Projekt-IDs weiß das Tool nicht, was es betrachten soll")
	}
	if c.Jira.BaseURL == "" {
		return fmt.Errorf("jira.base_url fehlt in der Konfiguration")
	}
	if c.Jira.User == "" {
		return fmt.Errorf("jira.user fehlt in der Konfiguration")
	}
	if c.Moco.APIKey == "" {
		return fmt.Errorf("Umgebungsvariable %s ist nicht gesetzt", EnvMocoAPIKey)
	}
	if c.Jira.APIToken == "" {
		return fmt.Errorf("Umgebungsvariable %s ist nicht gesetzt", EnvJiraAPIToken)
	}
	return nil
}

// ProjectIDs sind die Moco-Projekte, die betrachtet werden.
func (c *Config) ProjectIDs() []string {
	ids := make([]string, 0, len(c.Moco.Projects))
	for _, p := range c.Moco.Projects {
		ids = append(ids, p.ID)
	}
	return ids
}

// ExpectedMarkers bildet Moco-Projekt-ID auf den erwarteten Validierungsmarker ab.
func (c *Config) ExpectedMarkers() map[string]string {
	m := make(map[string]string, len(c.Moco.Projects))
	for _, p := range c.Moco.Projects {
		m[p.ID] = p.Marker
	}
	return m
}
```

- [ ] **Step 5: Beispielkonfiguration schreiben**

`config.example.yaml`:

```yaml
# Kopieren nach ~/.config/moco-jira-bridge/config.yaml
# Tokens kommen aus der Umgebung: MOCO_API_KEY, JIRA_API_TOKEN

moco:
  domain: mayflower          # https://<domain>.mocoapp.com
  user_id: 12345             # eigene Moco-User-ID
  projects:
    - id: "1001"
      marker: TG             # Tagesgeschäft
    - id: "1002"
      marker: RM             # Roadmap

jira:
  target: jira-cloud         # jira-cloud | tempo-cloud | tempo-server
  base_url: https://kunde.atlassian.net
  user: jo.brunner@mayflower.de

ticket:
  pattern: '^([A-Z][A-Z0-9]+-\d+)'
  marker_pattern: '\(([^)]+)\)'
```

- [ ] **Step 6: Tests laufen lassen**

Run: `go test ./internal/config/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/config/ config.example.yaml
git commit -m "feat: YAML-Konfiguration mit Tokens aus der Umgebung"
```

---

### Task 6: Moco-Client

**Files:**
- Create: `internal/moco/client.go`
- Test: `internal/moco/client_test.go`

**Interfaces:**
- Consumes: `model.Entry`, `model.Day`, `parse.Parser`, `config.MocoConfig`
- Produces:
  - `type Client struct{ ... }`
  - `func New(cfg config.MocoConfig, p *parse.Parser, httpClient *http.Client) *Client`
  - `func (c *Client) Entries(ctx context.Context, from, to time.Time) ([]model.Entry, error)` — erfüllt `TimeSource`
  - `func (c *Client) WithBaseURL(u string) *Client` — nur für Tests, überschreibt die aus der Domain gebaute URL

- [ ] **Step 1: Failing test schreiben**

`internal/moco/client_test.go`:

```go
package moco

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"moco-jira-bridge/internal/config"
	"moco-jira-bridge/internal/parse"
)

func testParser(t *testing.T) *parse.Parser {
	t.Helper()
	p, err := parse.NewParser(config.DefaultTicketPattern, config.DefaultMarkerPattern)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

const activitiesJSON = `[
  {"id": 1, "date": "2026-09-18", "hours": 1.5,
   "description": "ABC-1234 Importfehler (TG)",
   "project": {"id": 1001}, "user": {"id": 12345}},
  {"id": 2, "date": "2026-09-18", "hours": 0.25,
   "description": "Interne Abstimmung",
   "project": {"id": 1001}, "user": {"id": 12345}}
]`

func TestEntriesMapptAktivitaeten(t *testing.T) {
	var gotQuery string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(activitiesJSON))
	}))
	defer srv.Close()

	cfg := config.MocoConfig{
		Domain: "mayflower", UserID: 12345, APIKey: "secret",
		Projects: []config.Project{{ID: "1001", Marker: "TG"}},
	}
	c := New(cfg, testParser(t), srv.Client()).WithBaseURL(srv.URL)

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 9, 30, 0, 0, 0, 0, time.Local)
	entries, err := c.Entries(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("erwarte 2 Einträge, got %d", len(entries))
	}

	first := entries[0]
	if first.Ticket != "ABC-1234" || first.Marker != "TG" {
		t.Fatalf("Parsing falsch: %+v", first)
	}
	if first.Duration != 90*time.Minute {
		t.Fatalf("Duration = %v, want 90m", first.Duration)
	}
	if first.ProjectID != "1001" || first.SourceID != "1" {
		t.Fatalf("Metadaten falsch: %+v", first)
	}
	if first.Date.Format("2006-01-02") != "2026-09-18" {
		t.Fatalf("Datum falsch: %v", first.Date)
	}

	if entries[1].Ticket != "" {
		t.Fatalf("zweiter Eintrag hat kein Ticket, got %q", entries[1].Ticket)
	}

	if gotAuth != "Token token=secret" {
		t.Fatalf("Authorization-Header = %q", gotAuth)
	}
	for _, want := range []string{"from=2026-09-01", "to=2026-09-30", "user_id=12345", "project_id=1001"} {
		if !contains(gotQuery, want) {
			t.Fatalf("Query %q enthält %q nicht", gotQuery, want)
		}
	}
}

func TestEntriesFehlerBeiHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid token"}`))
	}))
	defer srv.Close()

	cfg := config.MocoConfig{Domain: "x", UserID: 1, APIKey: "bad",
		Projects: []config.Project{{ID: "1001"}}}
	c := New(cfg, testParser(t), srv.Client()).WithBaseURL(srv.URL)

	_, err := c.Entries(context.Background(), time.Now(), time.Now())
	if err == nil {
		t.Fatal("erwarte Fehler bei HTTP 401")
	}
	if contains(err.Error(), "bad") {
		t.Fatal("Fehlermeldung darf das Token nicht enthalten")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/moco/`
Expected: FAIL — `undefined: New`

- [ ] **Step 3: Implementierung**

`internal/moco/client.go`:

```go
// Package moco liest Zeitbuchungen aus der Moco-API. Nur lesend — Moco ist
// das führende System und wird von diesem Tool niemals verändert.
package moco

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"moco-jira-bridge/internal/config"
	"moco-jira-bridge/internal/model"
	"moco-jira-bridge/internal/parse"
)

type Client struct {
	cfg     config.MocoConfig
	parser  *parse.Parser
	http    *http.Client
	baseURL string
}

func New(cfg config.MocoConfig, p *parse.Parser, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		cfg:     cfg,
		parser:  p,
		http:    httpClient,
		baseURL: fmt.Sprintf("https://%s.mocoapp.com", cfg.Domain),
	}
}

// WithBaseURL überschreibt die aus der Domain gebaute URL. Für Tests.
func (c *Client) WithBaseURL(u string) *Client {
	c.baseURL = u
	return c
}

type activity struct {
	ID          int     `json:"id"`
	Date        string  `json:"date"`
	Hours       float64 `json:"hours"`
	Description string  `json:"description"`
	Project     struct {
		ID int `json:"id"`
	} `json:"project"`
}

// Entries lädt alle Aktivitäten des konfigurierten Users in den konfigurierten
// Projekten im Zeitraum [from, to].
func (c *Client) Entries(ctx context.Context, from, to time.Time) ([]model.Entry, error) {
	var all []model.Entry
	for _, projectID := range c.cfg.Projects {
		entries, err := c.entriesForProject(ctx, projectID.ID, from, to)
		if err != nil {
			return nil, err
		}
		all = append(all, entries...)
	}
	return all, nil
}

func (c *Client) entriesForProject(ctx context.Context, projectID string, from, to time.Time) ([]model.Entry, error) {
	q := url.Values{}
	q.Set("from", from.Format("2006-01-02"))
	q.Set("to", to.Format("2006-01-02"))
	q.Set("user_id", strconv.Itoa(c.cfg.UserID))
	q.Set("project_id", projectID)

	endpoint := c.baseURL + "/api/v1/activities?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("Moco-Anfrage nicht baubar: %w", err)
	}
	req.Header.Set("Authorization", "Token token="+c.cfg.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Moco nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("Moco-Antwort nicht lesbar: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Moco antwortet mit Status %d für Projekt %s", resp.StatusCode, projectID)
	}

	var activities []activity
	if err := json.Unmarshal(body, &activities); err != nil {
		return nil, fmt.Errorf("Moco-Antwort ist kein erwartetes JSON: %w", err)
	}

	entries := make([]model.Entry, 0, len(activities))
	for _, a := range activities {
		date, err := time.ParseInLocation("2006-01-02", a.Date, time.Local)
		if err != nil {
			return nil, fmt.Errorf("Moco-Aktivität %d hat ungültiges Datum %q: %w", a.ID, a.Date, err)
		}
		ticket, marker := c.parser.Parse(a.Description)
		entries = append(entries, model.Entry{
			Ticket:      ticket,
			Date:        model.Day(date),
			Duration:    model.RoundMinutes(time.Duration(a.Hours * float64(time.Hour))),
			Description: a.Description,
			Source:      model.SourceMoco,
			SourceID:    strconv.Itoa(a.ID),
			Marker:      marker,
			ProjectID:   strconv.Itoa(a.Project.ID),
		})
	}
	return entries, nil
}
```

- [ ] **Step 4: Tests laufen lassen**

Run: `go test ./internal/moco/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/moco/
git commit -m "feat: Moco-Client zum Lesen von Aktivitäten"
```

---

### Task 7: Jira-Client (jira-cloud) und Target-Auswahl

**Files:**
- Create: `internal/jira/target.go`
- Create: `internal/jira/cloud.go`
- Test: `internal/jira/cloud_test.go`
- Test: `internal/jira/target_test.go`

**Interfaces:**
- Consumes: `model.Entry`, `model.Day`, `config.JiraConfig`
- Produces:
  - `type Worklog struct { Ticket string; Date time.Time; Duration time.Duration; Comment string }`
  - `type Target interface { Worklogs(ctx context.Context, from, to time.Time) ([]model.Entry, error); Create(ctx context.Context, w Worklog) error }`
  - `func NewTarget(cfg config.JiraConfig, httpClient *http.Client) (Target, error)` — `jira-cloud` liefert `*Cloud`, die anderen Targets einen klaren Fehler
  - `type Cloud struct{ ... }` mit `func NewCloud(cfg config.JiraConfig, httpClient *http.Client) *Cloud`

Der Cloud-Client findet Worklogs in zwei Schritten: JQL-Suche nach Issues mit eigenen Worklogs im Zeitraum, dann je Issue die Worklogs abrufen und auf eigenen Autor und Zeitraum filtern. So werden auch Überhänge auf Tickets gefunden, die in Moco gar nicht vorkommen.

- [ ] **Step 1: Failing test für die Target-Auswahl**

`internal/jira/target_test.go`:

```go
package jira

import (
	"strings"
	"testing"

	"moco-jira-bridge/internal/config"
)

func TestNewTargetJiraCloud(t *testing.T) {
	tgt, err := NewTarget(config.JiraConfig{
		Target: config.TargetJiraCloud, BaseURL: "https://x.atlassian.net",
		User: "a@b.de", APIToken: "tok",
	}, nil)
	if err != nil {
		t.Fatalf("NewTarget: %v", err)
	}
	if _, ok := tgt.(*Cloud); !ok {
		t.Fatalf("erwarte *Cloud, got %T", tgt)
	}
}

func TestNewTargetStubsMeldenKlar(t *testing.T) {
	for _, target := range []string{config.TargetTempoCloud, config.TargetTempoServer} {
		_, err := NewTarget(config.JiraConfig{Target: target, BaseURL: "https://x", User: "a", APIToken: "t"}, nil)
		if err == nil {
			t.Fatalf("%s sollte noch nicht implementiert sein", target)
		}
		if !strings.Contains(err.Error(), target) {
			t.Fatalf("Fehlermeldung sollte %q nennen: %v", target, err)
		}
	}
}

func TestNewTargetUnbekannt(t *testing.T) {
	if _, err := NewTarget(config.JiraConfig{Target: "quatsch"}, nil); err == nil {
		t.Fatal("erwarte Fehler bei unbekanntem Target")
	}
}
```

- [ ] **Step 2: Failing test für den Cloud-Client**

`internal/jira/cloud_test.go`:

```go
package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"moco-jira-bridge/internal/config"
)

const searchJSON = `{"issues":[{"key":"ABC-1234"}]}`

const worklogJSON = `{"worklogs":[
  {"id":"9001","started":"2026-09-18T09:00:00.000+0200","timeSpentSeconds":3600,
   "author":{"emailAddress":"jo.brunner@mayflower.de","accountId":"acc-1"}},
  {"id":"9002","started":"2026-09-18T11:00:00.000+0200","timeSpentSeconds":1800,
   "author":{"emailAddress":"jemand.anders@kunde.de","accountId":"acc-2"}},
  {"id":"9003","started":"2026-08-01T09:00:00.000+0200","timeSpentSeconds":3600,
   "author":{"emailAddress":"jo.brunner@mayflower.de","accountId":"acc-1"}}
]}`

func cloudServer(t *testing.T, onCreate func(body map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/search"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(searchJSON))
		case strings.HasSuffix(r.URL.Path, "/worklog") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(worklogJSON))
		case strings.HasSuffix(r.URL.Path, "/worklog") && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if onCreate != nil {
				onCreate(body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"9100"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func testCloud(t *testing.T, srv *httptest.Server) *Cloud {
	t.Helper()
	return NewCloud(config.JiraConfig{
		Target: config.TargetJiraCloud, BaseURL: srv.URL,
		User: "jo.brunner@mayflower.de", APIToken: "tok",
	}, srv.Client())
}

func TestWorklogsFiltertAutorUndZeitraum(t *testing.T) {
	srv := cloudServer(t, nil)
	defer srv.Close()

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 9, 30, 0, 0, 0, 0, time.Local)
	entries, err := testCloud(t, srv).Worklogs(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Worklogs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("erwarte 1 eigenen Worklog im Zeitraum, got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Ticket != "ABC-1234" || e.Duration != time.Hour || e.SourceID != "9001" {
		t.Fatalf("Worklog falsch gemappt: %+v", e)
	}
	if e.Date.Format("2006-01-02") != "2026-09-18" {
		t.Fatalf("Datum falsch: %v", e.Date)
	}
}

func TestCreateSchicktStartedUndSekunden(t *testing.T) {
	var got map[string]any
	srv := cloudServer(t, func(body map[string]any) { got = body })
	defer srv.Close()

	err := testCloud(t, srv).Create(context.Background(), Worklog{
		Ticket:   "ABC-1234",
		Date:     time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local),
		Duration: 90 * time.Minute,
		Comment:  "ABC-1234 Importfehler (TG)",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got["timeSpentSeconds"] != float64(5400) {
		t.Fatalf("timeSpentSeconds = %v, want 5400", got["timeSpentSeconds"])
	}
	started, _ := got["started"].(string)
	if !strings.HasPrefix(started, "2026-09-18T") {
		t.Fatalf("started = %q, erwarte das Moco-Datum", started)
	}
}

func TestCreateFehlerBeiStatus400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
	}))
	defer srv.Close()

	err := testCloud(t, srv).Create(context.Background(), Worklog{
		Ticket: "ABC-9999", Date: time.Now(), Duration: time.Hour,
	})
	if err == nil {
		t.Fatal("erwarte Fehler bei HTTP 400")
	}
	if !strings.Contains(err.Error(), "ABC-9999") {
		t.Fatalf("Fehler sollte das Ticket nennen: %v", err)
	}
}
```

- [ ] **Step 3: Tests laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/jira/`
Expected: FAIL — `undefined: NewTarget`, `undefined: NewCloud`

- [ ] **Step 4: Target-Auswahl implementieren**

`internal/jira/target.go`:

```go
// Package jira schreibt und liest Worklogs auf der Kundenseite. Welche
// Implementierung greift, entscheidet jira.target in der Konfiguration.
package jira

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"moco-jira-bridge/internal/config"
	"moco-jira-bridge/internal/model"
)

// Worklog ist eine zu schreibende Buchung.
type Worklog struct {
	Ticket   string
	Date     time.Time
	Duration time.Duration
	Comment  string
}

// Target ist die Kundenseite: Worklogs lesen und anlegen. Kein Ändern, kein Löschen.
type Target interface {
	Worklogs(ctx context.Context, from, to time.Time) ([]model.Entry, error)
	Create(ctx context.Context, w Worklog) error
}

func NewTarget(cfg config.JiraConfig, httpClient *http.Client) (Target, error) {
	switch cfg.Target {
	case config.TargetJiraCloud:
		return NewCloud(cfg, httpClient), nil
	case config.TargetTempoCloud:
		return nil, fmt.Errorf("target %q ist noch nicht implementiert — "+
			"führe `mjb doctor` aus, um zu prüfen, ob %q funktioniert",
			config.TargetTempoCloud, config.TargetJiraCloud)
	case config.TargetTempoServer:
		return nil, fmt.Errorf("target %q ist noch nicht implementiert — "+
			"führe `mjb doctor` aus, um zu prüfen, ob %q funktioniert",
			config.TargetTempoServer, config.TargetJiraCloud)
	default:
		return nil, fmt.Errorf("unbekanntes jira.target %q — erlaubt sind %q, %q, %q",
			cfg.Target, config.TargetJiraCloud, config.TargetTempoCloud, config.TargetTempoServer)
	}
}
```

- [ ] **Step 5: Cloud-Client implementieren**

`internal/jira/cloud.go`:

```go
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"moco-jira-bridge/internal/config"
	"moco-jira-bridge/internal/model"
)

// Cloud spricht die native Jira-Cloud-Worklog-API. Tempo liest diese Einträge
// ebenfalls, deshalb braucht es dafür keine zusätzlichen Tempo-Rechte.
type Cloud struct {
	cfg  config.JiraConfig
	http *http.Client
}

func NewCloud(cfg config.JiraConfig, httpClient *http.Client) *Cloud {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Cloud{cfg: cfg, http: httpClient}
}

func (c *Cloud) do(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("Anfrage nicht serialisierbar: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.BaseURL, "/")+path, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("Jira-Anfrage nicht baubar: %w", err)
	}
	req.SetBasicAuth(c.cfg.User, c.cfg.APIToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Jira nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("Jira-Antwort nicht lesbar: %w", err)
	}
	return raw, resp.StatusCode, nil
}

type searchResponse struct {
	Issues []struct {
		Key string `json:"key"`
	} `json:"issues"`
}

type worklogResponse struct {
	Worklogs []struct {
		ID               string `json:"id"`
		Started          string `json:"started"`
		TimeSpentSeconds int    `json:"timeSpentSeconds"`
		Comment          any    `json:"comment"`
		Author           struct {
			EmailAddress string `json:"emailAddress"`
			AccountID    string `json:"accountId"`
		} `json:"author"`
	} `json:"worklogs"`
}

// Worklogs findet erst die Issues mit eigenen Worklogs im Zeitraum und lädt
// dann deren Worklogs. Der Umweg über JQL findet auch Überhänge auf Tickets,
// die in Moco gar nicht vorkommen.
func (c *Cloud) Worklogs(ctx context.Context, from, to time.Time) ([]model.Entry, error) {
	jql := fmt.Sprintf("worklogAuthor = currentUser() AND worklogDate >= %q AND worklogDate <= %q",
		from.Format("2006-01-02"), to.Format("2006-01-02"))
	q := url.Values{}
	q.Set("jql", jql)
	q.Set("fields", "key")
	q.Set("maxResults", "200")

	raw, status, err := c.do(ctx, http.MethodGet, "/rest/api/3/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("Jira-Suche antwortet mit Status %d", status)
	}
	var search searchResponse
	if err := json.Unmarshal(raw, &search); err != nil {
		return nil, fmt.Errorf("Jira-Suchantwort ist kein erwartetes JSON: %w", err)
	}

	var entries []model.Entry
	for _, issue := range search.Issues {
		issueEntries, err := c.worklogsForIssue(ctx, issue.Key, from, to)
		if err != nil {
			return nil, err
		}
		entries = append(entries, issueEntries...)
	}
	return entries, nil
}

func (c *Cloud) worklogsForIssue(ctx context.Context, key string, from, to time.Time) ([]model.Entry, error) {
	raw, status, err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"/worklog", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("Worklogs für %s nicht abrufbar, Status %d", key, status)
	}
	var wr worklogResponse
	if err := json.Unmarshal(raw, &wr); err != nil {
		return nil, fmt.Errorf("Worklog-Antwort für %s ist kein erwartetes JSON: %w", key, err)
	}

	fromDay, toDay := model.Day(from), model.Day(to)
	var entries []model.Entry
	for _, w := range wr.Worklogs {
		if !strings.EqualFold(w.Author.EmailAddress, c.cfg.User) {
			continue
		}
		started, err := time.Parse("2006-01-02T15:04:05.000-0700", w.Started)
		if err != nil {
			return nil, fmt.Errorf("Worklog %s auf %s hat ungültiges Startdatum %q: %w",
				w.ID, key, w.Started, err)
		}
		day := model.Day(started)
		if day.Before(fromDay) || day.After(toDay) {
			continue
		}
		entries = append(entries, model.Entry{
			Ticket:   key,
			Date:     day,
			Duration: model.RoundMinutes(time.Duration(w.TimeSpentSeconds) * time.Second),
			Source:   model.SourceJira,
			SourceID: w.ID,
		})
	}
	return entries, nil
}

// Create legt einen Worklog auf dem Moco-Datum an. Die Uhrzeit ist bewusst
// fix auf 09:00 lokal — wann gearbeitet wurde, ist für die Abrechnung irrelevant.
func (c *Cloud) Create(ctx context.Context, w Worklog) error {
	started := time.Date(w.Date.Year(), w.Date.Month(), w.Date.Day(), 9, 0, 0, 0, time.Local)
	body := map[string]any{
		"started":          started.Format("2006-01-02T15:04:05.000-0700"),
		"timeSpentSeconds": int(w.Duration.Seconds()),
	}
	if w.Comment != "" {
		body["comment"] = adf(w.Comment)
	}

	raw, status, err := c.do(ctx, http.MethodPost,
		"/rest/api/3/issue/"+url.PathEscape(w.Ticket)+"/worklog", body)
	if err != nil {
		return fmt.Errorf("Worklog auf %s konnte nicht angelegt werden: %w", w.Ticket, err)
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("Worklog auf %s abgelehnt, Status %d: %s",
			w.Ticket, status, strings.TrimSpace(string(raw)))
	}
	return nil
}

// adf verpackt einen Kommentar im Atlassian Document Format, das die v3-API erwartet.
func adf(text string) map[string]any {
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []any{
			map[string]any{
				"type":    "paragraph",
				"content": []any{map[string]any{"type": "text", "text": text}},
			},
		},
	}
}
```

- [ ] **Step 6: Tests laufen lassen**

Run: `go test ./internal/jira/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/jira/
git commit -m "feat: Jira-Cloud-Client für Worklogs, Target-Auswahl per Config"
```

---

### Task 8: Sync-Planer

**Files:**
- Create: `internal/sync/plan.go`
- Test: `internal/sync/plan_test.go`

**Interfaces:**
- Consumes: `model.Diff`, `model.Entry`, `jira.Worklog`, `jira.Target`, `model.Problem`
- Produces:
  - `type Action struct { Ticket string; Date time.Time; Duration time.Duration; Comment string }`
  - `func Plan(diffs []model.Diff, moco []model.Entry) []Action` — nur `Delta > 0`, Kommentar aus der ersten passenden Moco-Beschreibung
  - `type Outcome struct { Applied []Action; Failed []model.Problem }`
  - `func Apply(ctx context.Context, tgt jira.Target, actions []Action) Outcome` — sammelt Fehler, bricht nie ab

- [ ] **Step 1: Failing test schreiben**

`internal/sync/plan_test.go`:

```go
package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"moco-jira-bridge/internal/jira"
	"moco-jira-bridge/internal/model"
)

func day(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.Local) }

func TestPlanBuchtNurDieDifferenz(t *testing.T) {
	diffs := []model.Diff{{Ticket: "ABC-1", Date: day(18), Want: 120 * time.Minute, Have: 90 * time.Minute}}
	moco := []model.Entry{{Ticket: "ABC-1", Date: day(18), Description: "ABC-1 Importfehler (TG)"}}

	actions := Plan(diffs, moco)
	if len(actions) != 1 {
		t.Fatalf("erwarte 1 Aktion, got %+v", actions)
	}
	if actions[0].Duration != 30*time.Minute {
		t.Fatalf("Duration = %v, want 30m (nur die Differenz)", actions[0].Duration)
	}
	if actions[0].Comment != "ABC-1 Importfehler (TG)" {
		t.Fatalf("Comment = %q", actions[0].Comment)
	}
	if !actions[0].Date.Equal(day(18)) {
		t.Fatalf("Date = %v, want Moco-Datum", actions[0].Date)
	}
}

func TestPlanIgnoriertAusgeglicheneUndUeberhang(t *testing.T) {
	diffs := []model.Diff{
		{Ticket: "A", Date: day(18), Want: 60 * time.Minute, Have: 60 * time.Minute},
		{Ticket: "B", Date: day(18), Want: 30 * time.Minute, Have: 60 * time.Minute},
	}
	if actions := Plan(diffs, nil); len(actions) != 0 {
		t.Fatalf("erwarte keine Aktionen, got %+v", actions)
	}
}

func TestPlanOhnePassendeBeschreibungNutztFallback(t *testing.T) {
	diffs := []model.Diff{{Ticket: "ABC-1", Date: day(18), Want: 60 * time.Minute}}
	actions := Plan(diffs, nil)
	if len(actions) != 1 || actions[0].Comment == "" {
		t.Fatalf("erwarte eine Aktion mit Fallback-Kommentar, got %+v", actions)
	}
}

func TestPlanIstIdempotentWennAusgeglichen(t *testing.T) {
	// Nach dem ersten Lauf steht in Jira, was in Moco steht — zweiter Lauf: nichts zu tun.
	diffs := []model.Diff{{Ticket: "ABC-1", Date: day(18), Want: 120 * time.Minute, Have: 120 * time.Minute}}
	if actions := Plan(diffs, nil); len(actions) != 0 {
		t.Fatalf("zweiter Lauf muss ein No-Op sein, got %+v", actions)
	}
}

type fakeTarget struct {
	created []jira.Worklog
	failOn  string
}

func (f *fakeTarget) Worklogs(ctx context.Context, from, to time.Time) ([]model.Entry, error) {
	return nil, nil
}

func (f *fakeTarget) Create(ctx context.Context, w jira.Worklog) error {
	if w.Ticket == f.failOn {
		return errors.New("Issue does not exist")
	}
	f.created = append(f.created, w)
	return nil
}

func TestApplySchreibtAlleAktionen(t *testing.T) {
	tgt := &fakeTarget{}
	out := Apply(context.Background(), tgt, []Action{
		{Ticket: "A-1", Date: day(18), Duration: 30 * time.Minute, Comment: "x"},
		{Ticket: "A-2", Date: day(18), Duration: 60 * time.Minute, Comment: "y"},
	})
	if len(out.Applied) != 2 || len(out.Failed) != 0 {
		t.Fatalf("Outcome = %+v", out)
	}
	if len(tgt.created) != 2 {
		t.Fatalf("erwarte 2 Worklogs, got %+v", tgt.created)
	}
}

func TestApplyLaeuftNachFehlerWeiter(t *testing.T) {
	tgt := &fakeTarget{failOn: "A-1"}
	out := Apply(context.Background(), tgt, []Action{
		{Ticket: "A-1", Date: day(18), Duration: 30 * time.Minute},
		{Ticket: "A-2", Date: day(18), Duration: 60 * time.Minute},
	})
	if len(out.Applied) != 1 || out.Applied[0].Ticket != "A-2" {
		t.Fatalf("A-2 hätte gebucht werden müssen: %+v", out.Applied)
	}
	if len(out.Failed) != 1 || out.Failed[0].Kind != model.ProblemAPIError {
		t.Fatalf("Fehler nicht korrekt gemeldet: %+v", out.Failed)
	}
	if out.Failed[0].Ticket != "A-1" {
		t.Fatalf("Fehler sollte A-1 nennen: %+v", out.Failed[0])
	}
}
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/sync/`
Expected: FAIL — `undefined: Plan`, `undefined: Apply`

- [ ] **Step 3: Implementierung**

`internal/sync/plan.go`:

```go
// Package sync leitet Schreibaktionen aus dem Vergleichsergebnis ab und führt
// sie aus. Es wird ausschließlich angelegt, nie geändert oder gelöscht.
package sync

import (
	"context"
	"fmt"
	"time"

	"moco-jira-bridge/internal/jira"
	"moco-jira-bridge/internal/model"
)

// Action ist ein geplanter Worklog: die Differenz, die in Jira noch fehlt.
type Action struct {
	Ticket   string
	Date     time.Time
	Duration time.Duration
	Comment  string
}

// Plan erzeugt für jede Gruppe mit fehlender Zeit genau eine Aktion über die
// Differenz. Weil nur die Differenz gebucht wird, ist ein zweiter Lauf ohne
// Änderungen in Moco automatisch ein No-Op — ohne Marker und ohne State-Datei.
func Plan(diffs []model.Diff, moco []model.Entry) []Action {
	var actions []Action
	for _, d := range diffs {
		delta := d.Delta()
		if delta <= 0 {
			continue
		}
		actions = append(actions, Action{
			Ticket:   d.Ticket,
			Date:     d.Date,
			Duration: delta,
			Comment:  comment(d, moco),
		})
	}
	return actions
}

// comment nimmt die Beschreibung der ersten Moco-Buchung derselben Gruppe.
func comment(d model.Diff, moco []model.Entry) string {
	for _, e := range moco {
		if e.Ticket == d.Ticket && model.Day(e.Date).Equal(d.Date) && e.Description != "" {
			return e.Description
		}
	}
	return fmt.Sprintf("%s (Moco %s)", d.Ticket, d.Date.Format("2006-01-02"))
}

// Outcome ist das Ergebnis eines Schreiblaufs.
type Outcome struct {
	Applied []Action
	Failed  []model.Problem
}

// Apply schreibt alle Aktionen. Ein Fehler bei einer Aktion beeinträchtigt die
// übrigen nicht — er wird gesammelt und am Ende gemeldet.
func Apply(ctx context.Context, tgt jira.Target, actions []Action) Outcome {
	var out Outcome
	for _, a := range actions {
		err := tgt.Create(ctx, jira.Worklog{
			Ticket:   a.Ticket,
			Date:     a.Date,
			Duration: a.Duration,
			Comment:  a.Comment,
		})
		if err != nil {
			out.Failed = append(out.Failed, model.Problem{
				Kind:   model.ProblemAPIError,
				Ticket: a.Ticket,
				Date:   a.Date,
				Detail: err.Error(),
			})
			continue
		}
		out.Applied = append(out.Applied, a)
	}
	return out
}
```

- [ ] **Step 4: Tests laufen lassen**

Run: `go test ./internal/sync/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sync/
git commit -m "feat: Sync-Planer mit Differenzbuchung und fehlertoleranter Ausführung"
```

---

### Task 9: Ausgabe — Tabelle und JSON

**Files:**
- Create: `internal/report/report.go`
- Test: `internal/report/report_test.go`

**Interfaces:**
- Consumes: `model.Diff`, `model.Problem`, `diff.Totals`, `diff.Total`, `sync.Action`
- Produces:
  - `type Report struct { Period string; Diffs []model.Diff; Totals diff.Totals; Problems []model.Problem; Planned []sync.Action; Applied []sync.Action; DryRun bool }`
  - `func (r Report) WriteText(w io.Writer) error`
  - `func (r Report) WriteJSON(w io.Writer) error`
  - `func (r Report) ExitCode() int` — `0` sauber, `1` Abweichungen oder Probleme
  - `func FormatDuration(d time.Duration) string` — `"1:30"` statt `"1h30m0s"`

- [ ] **Step 1: Failing test schreiben**

`internal/report/report_test.go`:

```go
package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"moco-jira-bridge/internal/diff"
	"moco-jira-bridge/internal/model"
	syncpkg "moco-jira-bridge/internal/sync"
)

func day(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.Local) }

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{90 * time.Minute, "1:30"},
		{0, "0:00"},
		{-30 * time.Minute, "-0:30"},
		{8 * time.Hour, "8:00"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.in); got != c.want {
			t.Fatalf("FormatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExitCodeSauber(t *testing.T) {
	r := Report{Diffs: []model.Diff{{Ticket: "A", Date: day(18), Want: time.Hour, Have: time.Hour}}}
	if got := r.ExitCode(); got != 0 {
		t.Fatalf("ExitCode = %d, want 0", got)
	}
}

func TestExitCodeBeiAbweichung(t *testing.T) {
	r := Report{Diffs: []model.Diff{{Ticket: "A", Date: day(18), Want: time.Hour}}}
	if got := r.ExitCode(); got != 1 {
		t.Fatalf("ExitCode = %d, want 1", got)
	}
}

func TestExitCodeBeiProblemOhneAbweichung(t *testing.T) {
	r := Report{Problems: []model.Problem{{Kind: model.ProblemNoTicket, Detail: "x"}}}
	if got := r.ExitCode(); got != 1 {
		t.Fatalf("ExitCode = %d, want 1", got)
	}
}

func TestWriteTextZeigtAbweichungenUndProbleme(t *testing.T) {
	r := Report{
		Period: "2026-09-01 bis 2026-09-18",
		Diffs:  []model.Diff{{Ticket: "ABC-1", Date: day(18), Want: 120 * time.Minute, Have: 90 * time.Minute}},
		Totals: diff.Aggregate([]model.Diff{{Ticket: "ABC-1", Date: day(18), Want: 120 * time.Minute, Have: 90 * time.Minute}}),
		Problems: []model.Problem{
			{Kind: model.ProblemNoTicket, Date: day(18), Detail: "Moco-Buchung ohne Ticket-Key: \"Interne Abstimmung\""},
		},
	}
	var buf bytes.Buffer
	if err := r.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"ABC-1", "2026-09-18", "0:30", "kein-ticket", "Interne Abstimmung"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Ausgabe enthält %q nicht:\n%s", want, out)
		}
	}
}

func TestWriteTextZeigtDryRunHinweis(t *testing.T) {
	r := Report{
		DryRun:  true,
		Planned: []syncpkg.Action{{Ticket: "ABC-1", Date: day(18), Duration: 30 * time.Minute}},
	}
	var buf bytes.Buffer
	if err := r.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if !strings.Contains(buf.String(), "--apply") {
		t.Fatalf("Dry-Run-Hinweis fehlt:\n%s", buf.String())
	}
}

func TestWriteJSONIstGueltig(t *testing.T) {
	r := Report{
		Period: "2026-09",
		Diffs:  []model.Diff{{Ticket: "ABC-1", Date: day(18), Want: 120 * time.Minute, Have: 90 * time.Minute}},
	}
	var buf bytes.Buffer
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Ausgabe ist kein gültiges JSON: %v\n%s", err, buf.String())
	}
	if parsed["period"] != "2026-09" {
		t.Fatalf("period fehlt: %+v", parsed)
	}
}
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/report/`
Expected: FAIL — `undefined: Report`

- [ ] **Step 3: Implementierung**

`internal/report/report.go`:

```go
// Package report rendert das Vergleichsergebnis für Menschen und für Skripte.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"moco-jira-bridge/internal/diff"
	"moco-jira-bridge/internal/model"
	syncpkg "moco-jira-bridge/internal/sync"
)

type Report struct {
	Period   string
	Diffs    []model.Diff
	Totals   diff.Totals
	Problems []model.Problem
	Planned  []syncpkg.Action
	Applied  []syncpkg.Action
	DryRun   bool
}

// FormatDuration schreibt Dauern als "1:30" — so, wie Zeiten in Moco und Jira gelesen werden.
func FormatDuration(d time.Duration) string {
	sign := ""
	if d < 0 {
		sign, d = "-", -d
	}
	return fmt.Sprintf("%s%d:%02d", sign, int(d.Hours()), int(d.Minutes())%60)
}

// ExitCode ist 0, wenn alles übereinstimmt und nichts gemeldet wurde.
func (r Report) ExitCode() int {
	if len(r.Problems) > 0 {
		return 1
	}
	for _, d := range r.Diffs {
		if d.Delta() != 0 {
			return 1
		}
	}
	return 0
}

func (r Report) WriteText(w io.Writer) error {
	if r.Period != "" {
		if _, err := fmt.Fprintf(w, "Zeitraum: %s\n\n", r.Period); err != nil {
			return err
		}
	}

	if err := r.writeDiffs(w); err != nil {
		return err
	}
	if err := r.writeTotals(w); err != nil {
		return err
	}
	if err := r.writeActions(w); err != nil {
		return err
	}
	return r.writeProblems(w)
}

func (r Report) writeDiffs(w io.Writer) error {
	var abweichend []model.Diff
	for _, d := range r.Diffs {
		if d.Delta() != 0 {
			abweichend = append(abweichend, d)
		}
	}
	if len(abweichend) == 0 {
		if len(r.Diffs) > 0 {
			fmt.Fprintf(w, "Alle %d Ticket/Tag-Gruppen stimmen überein.\n\n", len(r.Diffs))
		}
		return nil
	}

	fmt.Fprintf(w, "Abweichungen (%d):\n", len(abweichend))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DATUM\tTICKET\tMOCO\tJIRA\tDIFFERENZ")
	for _, d := range abweichend {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			d.Date.Format("2006-01-02"), d.Ticket,
			FormatDuration(d.Want), FormatDuration(d.Have), FormatDuration(d.Delta()))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(w)
	return nil
}

func (r Report) writeTotals(w io.Writer) error {
	sections := []struct {
		title  string
		totals []diff.Total
	}{
		{"Tagessummen", r.Totals.Days},
		{"Wochensummen", r.Totals.Weeks},
		{"Monatssummen", r.Totals.Months},
	}
	for _, s := range sections {
		var abweichend []diff.Total
		for _, t := range s.totals {
			if t.Delta() != 0 {
				abweichend = append(abweichend, t)
			}
		}
		if len(abweichend) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s mit Abweichung:\n", s.title)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ZEITRAUM\tMOCO\tJIRA\tDIFFERENZ")
		for _, t := range abweichend {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t.Label,
				FormatDuration(t.Want), FormatDuration(t.Have), FormatDuration(t.Delta()))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}
	return nil
}

func (r Report) writeActions(w io.Writer) error {
	if len(r.Planned) > 0 {
		verb := "Würde buchen"
		if !r.DryRun {
			verb = "Geplant"
		}
		fmt.Fprintf(w, "%s (%d):\n", verb, len(r.Planned))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "DATUM\tTICKET\tDAUER\tKOMMENTAR")
		for _, a := range r.Planned {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				a.Date.Format("2006-01-02"), a.Ticket, FormatDuration(a.Duration), a.Comment)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		if r.DryRun {
			fmt.Fprintln(w, "\nNichts geschrieben. Mit --apply ausführen.")
		}
		fmt.Fprintln(w)
	}

	if len(r.Applied) > 0 {
		var total time.Duration
		for _, a := range r.Applied {
			total += a.Duration
		}
		fmt.Fprintf(w, "Gebucht: %d Worklogs, zusammen %s.\n\n", len(r.Applied), FormatDuration(total))
	}
	return nil
}

func (r Report) writeProblems(w io.Writer) error {
	if len(r.Problems) == 0 {
		return nil
	}
	fmt.Fprintf(w, "Zu klären (%d):\n", len(r.Problems))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DATUM\tART\tTICKET\tDETAIL")
	for _, p := range r.Problems {
		date := ""
		if !p.Date.IsZero() {
			date = p.Date.Format("2006-01-02")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", date, p.Kind, p.Ticket, p.Detail)
	}
	return tw.Flush()
}

type jsonDiff struct {
	Ticket     string `json:"ticket"`
	Date       string `json:"date"`
	MocoMin    int    `json:"moco_minutes"`
	JiraMin    int    `json:"jira_minutes"`
	DeltaMin   int    `json:"delta_minutes"`
}

type jsonTotal struct {
	Label    string `json:"label"`
	MocoMin  int    `json:"moco_minutes"`
	JiraMin  int    `json:"jira_minutes"`
	DeltaMin int    `json:"delta_minutes"`
}

type jsonProblem struct {
	Kind   string `json:"kind"`
	Ticket string `json:"ticket,omitempty"`
	Date   string `json:"date,omitempty"`
	Detail string `json:"detail"`
}

type jsonAction struct {
	Ticket   string `json:"ticket"`
	Date     string `json:"date"`
	Minutes  int    `json:"minutes"`
	Comment  string `json:"comment,omitempty"`
}

func (r Report) WriteJSON(w io.Writer) error {
	out := map[string]any{
		"period":    r.Period,
		"dry_run":   r.DryRun,
		"exit_code": r.ExitCode(),
		"diffs":     make([]jsonDiff, 0, len(r.Diffs)),
		"totals": map[string]any{
			"days":    toJSONTotals(r.Totals.Days),
			"weeks":   toJSONTotals(r.Totals.Weeks),
			"months":  toJSONTotals(r.Totals.Months),
			"tickets": toJSONTotals(r.Totals.Tickets),
		},
		"problems": make([]jsonProblem, 0, len(r.Problems)),
		"planned":  toJSONActions(r.Planned),
		"applied":  toJSONActions(r.Applied),
	}

	diffs := make([]jsonDiff, 0, len(r.Diffs))
	for _, d := range r.Diffs {
		diffs = append(diffs, jsonDiff{
			Ticket:   d.Ticket,
			Date:     d.Date.Format("2006-01-02"),
			MocoMin:  minutes(d.Want),
			JiraMin:  minutes(d.Have),
			DeltaMin: minutes(d.Delta()),
		})
	}
	out["diffs"] = diffs

	problems := make([]jsonProblem, 0, len(r.Problems))
	for _, p := range r.Problems {
		jp := jsonProblem{Kind: string(p.Kind), Ticket: p.Ticket, Detail: p.Detail}
		if !p.Date.IsZero() {
			jp.Date = p.Date.Format("2006-01-02")
		}
		problems = append(problems, jp)
	}
	out["problems"] = problems

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func toJSONTotals(ts []diff.Total) []jsonTotal {
	out := make([]jsonTotal, 0, len(ts))
	for _, t := range ts {
		out = append(out, jsonTotal{
			Label: t.Label, MocoMin: minutes(t.Want),
			JiraMin: minutes(t.Have), DeltaMin: minutes(t.Delta()),
		})
	}
	return out
}

func toJSONActions(as []syncpkg.Action) []jsonAction {
	out := make([]jsonAction, 0, len(as))
	for _, a := range as {
		out = append(out, jsonAction{
			Ticket: a.Ticket, Date: a.Date.Format("2006-01-02"),
			Minutes: minutes(a.Duration), Comment: a.Comment,
		})
	}
	return out
}

func minutes(d time.Duration) int { return int(d.Round(time.Minute) / time.Minute) }
```

- [ ] **Step 4: Tests laufen lassen**

Run: `go test ./internal/report/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/report/
git commit -m "feat: Text- und JSON-Ausgabe für Vergleichsergebnisse"
```

---

### Task 10: Zeitraum-Auflösung

**Files:**
- Create: `internal/timerange/timerange.go`
- Test: `internal/timerange/timerange_test.go`

**Interfaces:**
- Consumes: `model.Day`
- Produces:
  - `type Range struct { From, To time.Time }`
  - `func (r Range) String() string` — `"2026-09-01 bis 2026-09-18"`
  - `func Resolve(month, from, to, until string, today time.Time) (Range, error)` — leere Strings bedeuten "nicht gesetzt"; ohne jede Angabe: laufender Monat bis `today`

- [ ] **Step 1: Failing test schreiben**

`internal/timerange/timerange_test.go`:

```go
package timerange

import (
	"testing"
	"time"
)

var today = time.Date(2026, 9, 18, 14, 30, 0, 0, time.Local)

func check(t *testing.T, r Range, from, to string) {
	t.Helper()
	if r.From.Format("2006-01-02") != from || r.To.Format("2006-01-02") != to {
		t.Fatalf("Range = %s bis %s, want %s bis %s",
			r.From.Format("2006-01-02"), r.To.Format("2006-01-02"), from, to)
	}
}

func TestResolveDefaultIstLaufenderMonatBisHeute(t *testing.T) {
	r, err := Resolve("", "", "", "", today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	check(t, r, "2026-09-01", "2026-09-18")
}

func TestResolveMonatIstGanzerMonat(t *testing.T) {
	r, err := Resolve("2026-08", "", "", "", today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	check(t, r, "2026-08-01", "2026-08-31")
}

func TestResolveMonatFebruarSchaltjahr(t *testing.T) {
	r, err := Resolve("2028-02", "", "", "", today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	check(t, r, "2028-02-01", "2028-02-29")
}

func TestResolveUntilIstLaufenderMonatBisStichtag(t *testing.T) {
	r, err := Resolve("", "", "", "2026-09-10", today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	check(t, r, "2026-09-01", "2026-09-10")
}

func TestResolveFromTo(t *testing.T) {
	r, err := Resolve("", "2026-09-05", "2026-09-12", "", today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	check(t, r, "2026-09-05", "2026-09-12")
}

func TestResolveFehlerBeiKombination(t *testing.T) {
	if _, err := Resolve("2026-09", "2026-09-05", "", "", today); err == nil {
		t.Fatal("--month und --from zusammen sollten einen Fehler geben")
	}
}

func TestResolveFehlerWennNurFromGesetzt(t *testing.T) {
	if _, err := Resolve("", "2026-09-05", "", "", today); err == nil {
		t.Fatal("--from ohne --to sollte einen Fehler geben")
	}
}

func TestResolveFehlerBeiVerdrehtemZeitraum(t *testing.T) {
	if _, err := Resolve("", "2026-09-12", "2026-09-05", "", today); err == nil {
		t.Fatal("To vor From sollte einen Fehler geben")
	}
}

func TestResolveFehlerBeiUngueltigemDatum(t *testing.T) {
	if _, err := Resolve("2026-13", "", "", "", today); err == nil {
		t.Fatal("ungültiger Monat sollte einen Fehler geben")
	}
}

func TestStringFormat(t *testing.T) {
	r, _ := Resolve("2026-09", "", "", "", today)
	if got := r.String(); got != "2026-09-01 bis 2026-09-30" {
		t.Fatalf("String() = %q", got)
	}
}
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/timerange/`
Expected: FAIL — `undefined: Resolve`

- [ ] **Step 3: Implementierung**

`internal/timerange/timerange.go`:

```go
// Package timerange übersetzt die Zeitraum-Flags der CLI in konkrete Daten.
package timerange

import (
	"fmt"
	"time"

	"moco-jira-bridge/internal/model"
)

type Range struct {
	From time.Time
	To   time.Time
}

func (r Range) String() string {
	return fmt.Sprintf("%s bis %s", r.From.Format("2006-01-02"), r.To.Format("2006-01-02"))
}

// Resolve bestimmt den Zeitraum. Leere Strings bedeuten "Flag nicht gesetzt".
// Ohne jede Angabe: laufender Monat bis heute.
func Resolve(month, from, to, until string, today time.Time) (Range, error) {
	hasMonth := month != ""
	hasFromTo := from != "" || to != ""
	hasUntil := until != ""

	switch {
	case hasMonth && hasFromTo, hasMonth && hasUntil, hasFromTo && hasUntil:
		return Range{}, fmt.Errorf("--month, --from/--to und --until schließen sich gegenseitig aus")
	}

	switch {
	case hasMonth:
		start, err := time.ParseInLocation("2006-01", month, time.Local)
		if err != nil {
			return Range{}, fmt.Errorf("--month %q ist kein gültiger Monat im Format JJJJ-MM: %w", month, err)
		}
		end := start.AddDate(0, 1, -1)
		return Range{From: model.Day(start), To: model.Day(end)}, nil

	case hasFromTo:
		if from == "" || to == "" {
			return Range{}, fmt.Errorf("--from und --to müssen zusammen gesetzt werden")
		}
		f, err := parseDay(from, "--from")
		if err != nil {
			return Range{}, err
		}
		t, err := parseDay(to, "--to")
		if err != nil {
			return Range{}, err
		}
		if t.Before(f) {
			return Range{}, fmt.Errorf("--to (%s) liegt vor --from (%s)", to, from)
		}
		return Range{From: f, To: t}, nil

	case hasUntil:
		t, err := parseDay(until, "--until")
		if err != nil {
			return Range{}, err
		}
		return Range{From: monthStart(t), To: t}, nil

	default:
		t := model.Day(today)
		return Range{From: monthStart(t), To: t}, nil
	}
}

func parseDay(value, flag string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %q ist kein gültiges Datum im Format JJJJ-MM-TT: %w", flag, value, err)
	}
	return model.Day(t), nil
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.Local)
}
```

- [ ] **Step 4: Tests laufen lassen**

Run: `go test ./internal/timerange/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/timerange/
git commit -m "feat: Auflösung der Zeitraum-Flags"
```

---

### Task 11: CLI — validate und sync

**Files:**
- Create: `cmd/mjb/main.go`
- Create: `internal/app/app.go`
- Test: `internal/app/app_test.go`

**Interfaces:**
- Consumes: alle bisherigen Pakete
- Produces:
  - `type TimeSource interface { Entries(ctx context.Context, from, to time.Time) ([]model.Entry, error) }`
  - `type App struct { Moco TimeSource; Jira jira.Target; ExpectedMarkers map[string]string }`
  - `func (a *App) Validate(ctx context.Context, r timerange.Range) (report.Report, error)`
  - `func (a *App) Sync(ctx context.Context, r timerange.Range, apply bool) (report.Report, error)`
  - `main.go` verdrahtet Flags, Config und Provider und schreibt den Report.

- [ ] **Step 1: Failing test schreiben**

`internal/app/app_test.go`:

```go
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"moco-jira-bridge/internal/jira"
	"moco-jira-bridge/internal/model"
	"moco-jira-bridge/internal/timerange"
)

func day(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.Local) }

var testRange = timerange.Range{From: day(1), To: day(30)}

type fakeMoco struct {
	entries []model.Entry
	err     error
}

func (f *fakeMoco) Entries(ctx context.Context, from, to time.Time) ([]model.Entry, error) {
	return f.entries, f.err
}

type fakeJira struct {
	entries []model.Entry
	created []jira.Worklog
	failOn  string
}

func (f *fakeJira) Worklogs(ctx context.Context, from, to time.Time) ([]model.Entry, error) {
	return f.entries, nil
}

func (f *fakeJira) Create(ctx context.Context, w jira.Worklog) error {
	if w.Ticket == f.failOn {
		return errors.New("Issue does not exist")
	}
	f.created = append(f.created, w)
	return nil
}

func mocoEntry(ticket string, d, min int, marker, project string) model.Entry {
	return model.Entry{
		Ticket: ticket, Date: day(d), Duration: time.Duration(min) * time.Minute,
		Description: ticket + " Arbeit (" + marker + ")", Marker: marker,
		ProjectID: project, Source: model.SourceMoco,
	}
}

func TestValidateMeldetAbweichung(t *testing.T) {
	a := &App{
		Moco: &fakeMoco{entries: []model.Entry{mocoEntry("ABC-1", 18, 120, "TG", "1001")}},
		Jira: &fakeJira{entries: []model.Entry{{Ticket: "ABC-1", Date: day(18), Duration: 90 * time.Minute, Source: model.SourceJira}}},
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Validate(context.Background(), testRange)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.ExitCode() != 1 {
		t.Fatalf("ExitCode = %d, want 1", rep.ExitCode())
	}
	if len(rep.Diffs) != 1 || rep.Diffs[0].Delta() != 30*time.Minute {
		t.Fatalf("Diffs = %+v", rep.Diffs)
	}
}

func TestValidateSauberWennGleich(t *testing.T) {
	a := &App{
		Moco: &fakeMoco{entries: []model.Entry{mocoEntry("ABC-1", 18, 120, "TG", "1001")}},
		Jira: &fakeJira{entries: []model.Entry{{Ticket: "ABC-1", Date: day(18), Duration: 120 * time.Minute, Source: model.SourceJira}}},
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Validate(context.Background(), testRange)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.ExitCode() != 0 {
		t.Fatalf("ExitCode = %d, want 0. Probleme: %+v", rep.ExitCode(), rep.Problems)
	}
}

func TestValidateMeldetMarkerKonflikt(t *testing.T) {
	a := &App{
		Moco: &fakeMoco{entries: []model.Entry{mocoEntry("ABC-1", 18, 60, "RM", "1001")}},
		Jira: &fakeJira{entries: []model.Entry{{Ticket: "ABC-1", Date: day(18), Duration: 60 * time.Minute, Source: model.SourceJira}}},
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, _ := a.Validate(context.Background(), testRange)
	var found bool
	for _, p := range rep.Problems {
		if p.Kind == model.ProblemMarkerMismatch {
			found = true
		}
	}
	if !found {
		t.Fatalf("Marker-Konflikt nicht gemeldet: %+v", rep.Problems)
	}
}

func TestSyncDryRunSchreibtNichts(t *testing.T) {
	j := &fakeJira{}
	a := &App{
		Moco: &fakeMoco{entries: []model.Entry{mocoEntry("ABC-1", 18, 120, "TG", "1001")}},
		Jira: j,
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Sync(context.Background(), testRange, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(j.created) != 0 {
		t.Fatalf("Dry-Run darf nichts schreiben, got %+v", j.created)
	}
	if !rep.DryRun || len(rep.Planned) != 1 {
		t.Fatalf("Report falsch: DryRun=%v Planned=%+v", rep.DryRun, rep.Planned)
	}
}

func TestSyncApplySchreibtDifferenz(t *testing.T) {
	j := &fakeJira{entries: []model.Entry{{Ticket: "ABC-1", Date: day(18), Duration: 90 * time.Minute, Source: model.SourceJira}}}
	a := &App{
		Moco: &fakeMoco{entries: []model.Entry{mocoEntry("ABC-1", 18, 120, "TG", "1001")}},
		Jira: j,
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Sync(context.Background(), testRange, true)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(j.created) != 1 || j.created[0].Duration != 30*time.Minute {
		t.Fatalf("erwarte eine Buchung über 30m, got %+v", j.created)
	}
	if len(rep.Applied) != 1 {
		t.Fatalf("Applied = %+v", rep.Applied)
	}
}

func TestSyncLaeuftNachFehlerWeiterUndMeldetIhn(t *testing.T) {
	j := &fakeJira{failOn: "ABC-1"}
	a := &App{
		Moco: &fakeMoco{entries: []model.Entry{
			mocoEntry("ABC-1", 18, 60, "TG", "1001"),
			mocoEntry("ABC-2", 18, 60, "TG", "1001"),
		}},
		Jira: j,
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Sync(context.Background(), testRange, true)
	if err != nil {
		t.Fatalf("Sync darf nicht abbrechen: %v", err)
	}
	if len(j.created) != 1 || j.created[0].Ticket != "ABC-2" {
		t.Fatalf("ABC-2 hätte gebucht werden müssen: %+v", j.created)
	}
	if rep.ExitCode() != 1 {
		t.Fatalf("ExitCode = %d, want 1", rep.ExitCode())
	}
}

func TestSyncMeldetEintragOhneTicketUndBuchtDenRest(t *testing.T) {
	j := &fakeJira{}
	a := &App{
		Moco: &fakeMoco{entries: []model.Entry{
			{Date: day(18), Duration: 30 * time.Minute, Description: "Interne Abstimmung", ProjectID: "1001", Source: model.SourceMoco},
			mocoEntry("ABC-2", 18, 60, "TG", "1001"),
		}},
		Jira: j,
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Sync(context.Background(), testRange, true)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(j.created) != 1 {
		t.Fatalf("der Rest muss gebucht werden: %+v", j.created)
	}
	var found bool
	for _, p := range rep.Problems {
		if p.Kind == model.ProblemNoTicket {
			found = true
		}
	}
	if !found {
		t.Fatalf("fehlender Ticket-Key nicht gemeldet: %+v", rep.Problems)
	}
}

func TestValidateFehlerBeimLadenWirdDurchgereicht(t *testing.T) {
	a := &App{Moco: &fakeMoco{err: errors.New("Moco nicht erreichbar")}, Jira: &fakeJira{}}
	if _, err := a.Validate(context.Background(), testRange); err == nil {
		t.Fatal("Ladefehler müssen als Fehler zurückkommen (Exit-Code 2)")
	}
}
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/app/`
Expected: FAIL — `undefined: App`

- [ ] **Step 3: App implementieren**

`internal/app/app.go`:

```go
// Package app verdrahtet Laden, Vergleichen und Schreiben zu den beiden
// Kommandos. Beide benutzen denselben Vergleich — der Sync kann per
// Konstruktion nichts anderes für korrekt halten als die Validierung.
package app

import (
	"context"
	"time"

	"moco-jira-bridge/internal/diff"
	"moco-jira-bridge/internal/jira"
	"moco-jira-bridge/internal/model"
	"moco-jira-bridge/internal/report"
	syncpkg "moco-jira-bridge/internal/sync"
	"moco-jira-bridge/internal/timerange"
)

// TimeSource ist die führende Seite: Moco.
type TimeSource interface {
	Entries(ctx context.Context, from, to time.Time) ([]model.Entry, error)
}

type App struct {
	Moco            TimeSource
	Jira            jira.Target
	ExpectedMarkers map[string]string
}

// compare lädt beide Seiten und baut den gemeinsamen Report-Rumpf.
func (a *App) compare(ctx context.Context, r timerange.Range) (report.Report, []model.Entry, []model.Diff, error) {
	mocoEntries, err := a.Moco.Entries(ctx, r.From, r.To)
	if err != nil {
		return report.Report{}, nil, nil, err
	}
	jiraEntries, err := a.Jira.Worklogs(ctx, r.From, r.To)
	if err != nil {
		return report.Report{}, nil, nil, err
	}

	result := diff.Compare(mocoEntries, jiraEntries)
	problems := append(result.Problems, diff.CheckMarkers(mocoEntries, a.ExpectedMarkers)...)

	rep := report.Report{
		Period:   r.String(),
		Diffs:    result.Diffs,
		Totals:   diff.Aggregate(result.Diffs),
		Problems: problems,
	}
	return rep, mocoEntries, result.Diffs, nil
}

// Validate vergleicht beide Seiten, ohne etwas zu schreiben.
func (a *App) Validate(ctx context.Context, r timerange.Range) (report.Report, error) {
	rep, _, _, err := a.compare(ctx, r)
	return rep, err
}

// Sync plant die fehlenden Buchungen und schreibt sie, wenn apply gesetzt ist.
func (a *App) Sync(ctx context.Context, r timerange.Range, apply bool) (report.Report, error) {
	rep, mocoEntries, diffs, err := a.compare(ctx, r)
	if err != nil {
		return report.Report{}, err
	}

	actions := syncpkg.Plan(diffs, mocoEntries)
	rep.Planned = actions
	rep.DryRun = !apply
	if !apply {
		return rep, nil
	}

	outcome := syncpkg.Apply(ctx, a.Jira, actions)
	rep.Applied = outcome.Applied
	rep.Problems = append(rep.Problems, outcome.Failed...)
	return rep, nil
}
```

- [ ] **Step 4: Tests laufen lassen**

Run: `go test ./internal/app/`
Expected: PASS

- [ ] **Step 5: CLI implementieren**

`cmd/mjb/main.go`:

```go
// Command mjb gleicht Moco-Zeitbuchungen mit Jira/Tempo ab.
// Moco ist immer das führende System.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"moco-jira-bridge/internal/app"
	"moco-jira-bridge/internal/config"
	"moco-jira-bridge/internal/jira"
	"moco-jira-bridge/internal/moco"
	"moco-jira-bridge/internal/parse"
	"moco-jira-bridge/internal/timerange"
)

const usage = `mjb — Moco-Zeiten gegen Jira/Tempo abgleichen

Kommandos:
  validate    Prüft, ob Moco und Jira übereinstimmen (schreibt nie)
  sync        Zieht fehlende Zeiten aus Moco in Jira nach
  doctor      Prüft Konfiguration und Erreichbarkeit beider Systeme

Zeitraum (für validate und sync):
  --month JJJJ-MM        ganzer Monat
  --from JJJJ-MM-TT      Beginn (zusammen mit --to)
  --to JJJJ-MM-TT        Ende
  --until JJJJ-MM-TT     laufender Monat bis zu diesem Stichtag
  ohne Angabe            laufender Monat bis heute

Weitere Flags:
  --apply                nur bei sync: tatsächlich schreiben (Default: Dry-Run)
  --json                 maschinenlesbare Ausgabe
  --config PFAD          Konfigurationsdatei (Default: ~/.config/moco-jira-bridge/config.yaml)

Tokens kommen aus der Umgebung: MOCO_API_KEY, JIRA_API_TOKEN

Exit-Codes: 0 sauber, 1 Abweichungen oder Hinweise, 2 Konfigurations-/Verbindungsfehler
`

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	command := os.Args[1]
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		month      = fs.String("month", "", "Monat JJJJ-MM")
		from       = fs.String("from", "", "Beginn JJJJ-MM-TT")
		to         = fs.String("to", "", "Ende JJJJ-MM-TT")
		until      = fs.String("until", "", "Stichtag JJJJ-MM-TT im laufenden Monat")
		apply      = fs.Bool("apply", false, "tatsächlich nach Jira schreiben")
		asJSON     = fs.Bool("json", false, "maschinenlesbare Ausgabe")
		configPath = fs.String("config", config.DefaultPath(), "Pfad zur Konfiguration")
	)

	switch command {
	case "validate", "sync", "doctor":
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unbekanntes Kommando %q\n\n%s", command, usage)
		return 2
	}

	if err := fs.Parse(os.Args[2:]); err != nil {
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fehler: %v\n", err)
		return 2
	}

	parser, err := parse.NewParser(cfg.Ticket.Pattern, cfg.Ticket.MarkerPattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fehler: %v\n", err)
		return 2
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	target, err := jira.NewTarget(cfg.Jira, httpClient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fehler: %v\n", err)
		return 2
	}

	application := &app.App{
		Moco:            moco.New(cfg.Moco, parser, httpClient),
		Jira:            target,
		ExpectedMarkers: cfg.ExpectedMarkers(),
	}

	ctx := context.Background()

	if command == "doctor" {
		return doctor(ctx, cfg, application)
	}

	rng, err := timerange.Resolve(*month, *from, *to, *until, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fehler: %v\n", err)
		return 2
	}

	switch command {
	case "validate":
		r, err := application.Validate(ctx, rng)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Fehler: %v\n", err)
			return 2
		}
		if *asJSON {
			if err := r.WriteJSON(os.Stdout); err != nil {
				fmt.Fprintf(os.Stderr, "Fehler bei der Ausgabe: %v\n", err)
				return 2
			}
		} else if err := r.WriteText(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Fehler bei der Ausgabe: %v\n", err)
			return 2
		}
		return r.ExitCode()

	case "sync":
		r, err := application.Sync(ctx, rng, *apply)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Fehler: %v\n", err)
			return 2
		}
		if *asJSON {
			if err := r.WriteJSON(os.Stdout); err != nil {
				fmt.Fprintf(os.Stderr, "Fehler bei der Ausgabe: %v\n", err)
				return 2
			}
		} else if err := r.WriteText(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Fehler bei der Ausgabe: %v\n", err)
			return 2
		}
		return r.ExitCode()
	}

	return 2
}
```

**Hinweis für den Implementierer:** `doctor` wird erst in Task 12 ergänzt;
bis dahin genügt eine Funktion
`func doctor(ctx context.Context, cfg *config.Config, a *app.App) int { return 0 }`
in derselben Datei, damit das Paket baut.

- [ ] **Step 6: Bauen und Ende-zu-Ende prüfen**

```bash
go build ./... && go vet ./... && go test ./...
go run ./cmd/mjb --help
```

Expected: Build und Tests grün, Hilfetext erscheint.

- [ ] **Step 7: Commit**

```bash
git add cmd/ internal/app/
git commit -m "feat: CLI mit validate und sync"
```

---

### Task 12: doctor-Kommando und README

**Files:**
- Create: `internal/doctor/doctor.go`
- Test: `internal/doctor/doctor_test.go`
- Modify: `cmd/mjb/main.go` (die Platzhalter-Funktion `doctor` durch den echten Aufruf ersetzen)
- Create: `README.md`

**Interfaces:**
- Consumes: `config.Config`
- Produces:
  - `type Check struct { Name string; OK bool; Detail string }`
  - `func Run(ctx context.Context, cfg *config.Config, httpClient *http.Client) []Check`
  - `func WriteChecks(w io.Writer, checks []Check) error`
  - `func AllOK(checks []Check) bool`

`Run` prüft der Reihe nach: Moco erreichbar und Token gültig, Jira erreichbar
und Token gültig, und welche Worklog-Endpunkte antworten — `/rest/api/3/myself`
für `jira-cloud`, `/rest/tempo-timesheets/4/worklogs` für `tempo-server`. Aus
den Antworten leitet es eine Empfehlung für `jira.target` ab.

- [ ] **Step 1: Failing test schreiben**

`internal/doctor/doctor_test.go`:

```go
package doctor

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"moco-jira-bridge/internal/config"
)

func testConfig(mocoURL, jiraURL string) *config.Config {
	return &config.Config{
		Moco: config.MocoConfig{
			Domain: "test", UserID: 1, APIKey: "tok",
			Projects: []config.Project{{ID: "1001", Marker: "TG"}},
		},
		Jira: config.JiraConfig{
			Target: config.TargetJiraCloud, BaseURL: jiraURL,
			User: "a@b.de", APIToken: "tok",
		},
	}
}

func TestRunMeldetErfolgreicheChecks(t *testing.T) {
	jiraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/myself") {
			_, _ = w.Write([]byte(`{"accountId":"acc-1","emailAddress":"a@b.de"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer jiraSrv.Close()

	cfg := testConfig("", jiraSrv.URL)
	checks := Run(context.Background(), cfg, jiraSrv.Client())

	var jiraCheck *Check
	for i := range checks {
		if strings.Contains(checks[i].Name, "Jira") {
			jiraCheck = &checks[i]
			break
		}
	}
	if jiraCheck == nil || !jiraCheck.OK {
		t.Fatalf("Jira-Check sollte erfolgreich sein: %+v", checks)
	}
}

func TestRunMeldetFehlgeschlageneChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	checks := Run(context.Background(), testConfig("", srv.URL), srv.Client())
	if AllOK(checks) {
		t.Fatalf("erwarte mindestens einen fehlgeschlagenen Check: %+v", checks)
	}
}

func TestWriteChecks(t *testing.T) {
	var buf bytes.Buffer
	err := WriteChecks(&buf, []Check{
		{Name: "Jira erreichbar", OK: true, Detail: "angemeldet als a@b.de"},
		{Name: "Moco erreichbar", OK: false, Detail: "Status 401"},
	})
	if err != nil {
		t.Fatalf("WriteChecks: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Jira erreichbar") || !strings.Contains(out, "Status 401") {
		t.Fatalf("Ausgabe unvollständig:\n%s", out)
	}
}

func TestAllOK(t *testing.T) {
	if !AllOK([]Check{{OK: true}, {OK: true}}) {
		t.Fatal("AllOK sollte true sein")
	}
	if AllOK([]Check{{OK: true}, {OK: false}}) {
		t.Fatal("AllOK sollte false sein")
	}
}
```

- [ ] **Step 2: Test laufen lassen, Fehlschlag bestätigen**

Run: `go test ./internal/doctor/`
Expected: FAIL — `undefined: Run`

- [ ] **Step 3: Implementierung**

`internal/doctor/doctor.go`:

```go
// Package doctor prüft Konfiguration und Erreichbarkeit beider Systeme und
// hilft bei der offenen Frage, welche Jira-/Tempo-Variante der Kunde einsetzt.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"moco-jira-bridge/internal/config"
)

type Check struct {
	Name   string
	OK     bool
	Detail string
}

func AllOK(checks []Check) bool {
	for _, c := range checks {
		if !c.OK {
			return false
		}
	}
	return true
}

func Run(ctx context.Context, cfg *config.Config, httpClient *http.Client) []Check {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	var checks []Check
	checks = append(checks, checkJiraSelf(ctx, cfg, httpClient))
	checks = append(checks, checkTempoEndpoints(ctx, cfg, httpClient)...)
	return checks
}

func checkJiraSelf(ctx context.Context, cfg *config.Config, c *http.Client) Check {
	url := strings.TrimRight(cfg.Jira.BaseURL, "/") + "/rest/api/3/myself"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Check{Name: "Jira erreichbar", OK: false, Detail: err.Error()}
	}
	req.SetBasicAuth(cfg.Jira.User, cfg.Jira.APIToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return Check{Name: "Jira erreichbar", OK: false, Detail: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return Check{Name: "Jira erreichbar", OK: false,
			Detail: fmt.Sprintf("Status %d — Token oder base_url prüfen", resp.StatusCode)}
	}
	var self struct {
		AccountID    string `json:"accountId"`
		EmailAddress string `json:"emailAddress"`
	}
	_ = json.Unmarshal(body, &self)
	return Check{Name: "Jira erreichbar", OK: true,
		Detail: fmt.Sprintf("angemeldet als %s (%s) — target %q ist nutzbar",
			self.EmailAddress, self.AccountID, config.TargetJiraCloud)}
}

func checkTempoEndpoints(ctx context.Context, cfg *config.Config, c *http.Client) []Check {
	paths := map[string]string{
		"Tempo Server/DC vorhanden": "/rest/tempo-timesheets/4/worklogs",
		"Tempo Cloud Plugin sichtbar": "/rest/tempo-core/1/globalconfiguration",
	}
	var checks []Check
	for name, path := range paths {
		url := strings.TrimRight(cfg.Jira.BaseURL, "/") + path
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			checks = append(checks, Check{Name: name, OK: false, Detail: err.Error()})
			continue
		}
		req.SetBasicAuth(cfg.Jira.User, cfg.Jira.APIToken)
		resp, err := c.Do(req)
		if err != nil {
			checks = append(checks, Check{Name: name, OK: false, Detail: err.Error()})
			continue
		}
		resp.Body.Close()
		found := resp.StatusCode != http.StatusNotFound
		detail := fmt.Sprintf("Status %d", resp.StatusCode)
		if found {
			detail += " — Endpunkt antwortet, Variante kommt in Frage"
		} else {
			detail += " — nicht vorhanden"
		}
		checks = append(checks, Check{Name: name, OK: found, Detail: detail})
	}
	return checks
}

func WriteChecks(w io.Writer, checks []Check) error {
	for _, c := range checks {
		mark := "ok  "
		if !c.OK {
			mark = "FEHL"
		}
		if _, err := fmt.Fprintf(w, "[%s] %s: %s\n", mark, c.Name, c.Detail); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: doctor in die CLI einhängen**

In `cmd/mjb/main.go` die Platzhalter-Funktion durch diese ersetzen und den
Import `moco-jira-bridge/internal/doctor` ergänzen:

```go
func doctor(ctx context.Context, cfg *config.Config, _ *app.App) int {
	checks := doctorpkg.Run(ctx, cfg, &http.Client{Timeout: 30 * time.Second})
	if err := doctorpkg.WriteChecks(os.Stdout, checks); err != nil {
		fmt.Fprintf(os.Stderr, "Fehler bei der Ausgabe: %v\n", err)
		return 2
	}
	if !doctorpkg.AllOK(checks) {
		return 1
	}
	return 0
}
```

Import als `doctorpkg "moco-jira-bridge/internal/doctor"`, damit er nicht mit
der Funktion kollidiert. Die Tempo-Checks dürfen fehlschlagen, ohne dass
`jira-cloud` unbrauchbar wäre — `doctor` liefert deshalb Exit-Code 1 nur als
Hinweis, nicht als Fehler.

- [ ] **Step 5: README schreiben**

`README.md`:

```markdown
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
```

- [ ] **Step 6: Alles bauen und testen**

```bash
go build ./... && go vet ./... && go test ./...
```

Expected: Alles grün.

- [ ] **Step 7: Commit**

```bash
git add internal/doctor/ cmd/ README.md
git commit -m "feat: doctor-Kommando und README"
```

---

## Offene Punkte für später

- `tempo-cloud` und `tempo-server` sind Stubs. Sobald `mjb doctor` gegen die
  echte Kundeninstanz gelaufen ist, wird die passende Implementierung
  ergänzt — eine neue Datei in `internal/jira/`, die `Target` erfüllt, plus
  ein Zweig in `NewTarget`.
- Der Jira-Cloud-Client paginiert nicht. Bei mehr als 200 Issues mit eigenen
  Worklogs im Zeitraum fehlen Einträge. Das ist für einen Monat Arbeit an
  einem Kundenprojekt unrealistisch, sollte aber bei Bedarf nachgezogen werden.
