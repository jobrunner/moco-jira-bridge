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
