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
