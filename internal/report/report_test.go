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

func TestExitCodeHinweisZaehltNicht(t *testing.T) {
	r := Report{Problems: []model.Problem{{Kind: model.ProblemTicketRemapped, Detail: "x"}}}
	if got := r.ExitCode(); got != 0 {
		t.Fatalf("ExitCode = %d, want 0 — Umleitungen sind nur Hinweise", got)
	}
}

func TestWriteTextZeigtAbweichungenUndProbleme(t *testing.T) {
	diffs := []model.Diff{{Ticket: "ABC-1", Date: day(18), Want: 120 * time.Minute, Have: 90 * time.Minute}}
	r := Report{
		Period: "2026-09-01 bis 2026-09-18",
		Diffs:  diffs,
		Totals: diff.Aggregate(diffs),
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
