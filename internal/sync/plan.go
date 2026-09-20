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
