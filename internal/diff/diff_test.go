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
