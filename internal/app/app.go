// Package app verdrahtet Laden, Vergleichen und Schreiben zu den beiden
// Kommandos. Beide benutzen denselben Vergleich — der Sync kann per
// Konstruktion nichts anderes für korrekt halten als die Validierung.
package app

import (
	"context"
	"fmt"
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
	Clock           syncpkg.Clock
	// DefaultTicket fängt Buchungen auf nicht existierende Tickets auf
	// (z.B. das Platzhalter-Ticket PHT-0). Leer = keine Umleitung.
	DefaultTicket string
}

// remapMissing leitet Moco-Buchungen, deren Ticket in Jira nicht existiert,
// auf das Default-Ticket um — vor dem Vergleich, damit Validierung und Sync
// dieselbe Sicht haben. Jede Umleitung wird als Hinweis gemeldet, damit auch
// Tippfehler in Ticket-Nummern sichtbar bleiben.
func (a *App) remapMissing(ctx context.Context, entries []model.Entry) ([]model.Entry, []model.Problem, error) {
	if a.DefaultTicket == "" {
		return entries, nil, nil
	}

	exists := map[string]bool{}
	for _, e := range entries {
		if e.Ticket == "" || e.Ticket == a.DefaultTicket {
			continue
		}
		if _, done := exists[e.Ticket]; done {
			continue
		}
		ok, err := a.Jira.IssueExists(ctx, e.Ticket)
		if err != nil {
			return nil, nil, err
		}
		exists[e.Ticket] = ok
	}

	var problems []model.Problem
	out := make([]model.Entry, len(entries))
	for i, e := range entries {
		if e.Ticket != "" && e.Ticket != a.DefaultTicket && !exists[e.Ticket] {
			problems = append(problems, model.Problem{
				Kind:   model.ProblemTicketRemapped,
				Ticket: e.Ticket,
				Date:   e.Date,
				Detail: fmt.Sprintf("%s existiert nicht in Jira — gebucht auf %s: %q",
					e.Ticket, a.DefaultTicket, e.Description),
			})
			e.Ticket = a.DefaultTicket
		}
		out[i] = e
	}
	return out, problems, nil
}

// compare lädt beide Seiten und baut den gemeinsamen Report-Rumpf.
func (a *App) compare(ctx context.Context, r timerange.Range) (report.Report, []model.Entry, []model.Diff, error) {
	mocoEntries, err := a.Moco.Entries(ctx, r.From, r.To)
	if err != nil {
		return report.Report{}, nil, nil, err
	}
	mocoEntries, remapped, err := a.remapMissing(ctx, mocoEntries)
	if err != nil {
		return report.Report{}, nil, nil, err
	}
	jiraEntries, err := a.Jira.Worklogs(ctx, r.From, r.To)
	if err != nil {
		return report.Report{}, nil, nil, err
	}

	result := diff.Compare(mocoEntries, jiraEntries)
	problems := append(result.Problems, diff.CheckMarkers(mocoEntries, a.ExpectedMarkers)...)
	problems = append(problems, remapped...)

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

	actions := syncpkg.Plan(diffs, mocoEntries, a.Clock)
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
