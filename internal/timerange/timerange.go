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
