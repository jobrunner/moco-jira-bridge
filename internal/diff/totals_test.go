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
