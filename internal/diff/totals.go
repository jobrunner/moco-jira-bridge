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
