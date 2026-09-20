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
	Ticket   string `json:"ticket"`
	Date     string `json:"date"`
	MocoMin  int    `json:"moco_minutes"`
	JiraMin  int    `json:"jira_minutes"`
	DeltaMin int    `json:"delta_minutes"`
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
	Ticket  string `json:"ticket"`
	Date    string `json:"date"`
	Minutes int    `json:"minutes"`
	Comment string `json:"comment,omitempty"`
}

func (r Report) WriteJSON(w io.Writer) error {
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

	problems := make([]jsonProblem, 0, len(r.Problems))
	for _, p := range r.Problems {
		jp := jsonProblem{Kind: string(p.Kind), Ticket: p.Ticket, Detail: p.Detail}
		if !p.Date.IsZero() {
			jp.Date = p.Date.Format("2006-01-02")
		}
		problems = append(problems, jp)
	}

	out := map[string]any{
		"period":    r.Period,
		"dry_run":   r.DryRun,
		"exit_code": r.ExitCode(),
		"diffs":     diffs,
		"totals": map[string]any{
			"days":    toJSONTotals(r.Totals.Days),
			"weeks":   toJSONTotals(r.Totals.Weeks),
			"months":  toJSONTotals(r.Totals.Months),
			"tickets": toJSONTotals(r.Totals.Tickets),
		},
		"problems": problems,
		"planned":  toJSONActions(r.Planned),
		"applied":  toJSONActions(r.Applied),
	}

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
