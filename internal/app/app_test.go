package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"moco-jira-bridge/internal/jira"
	"moco-jira-bridge/internal/model"
	"moco-jira-bridge/internal/timerange"
)

func day(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.Local) }

var testRange = timerange.Range{From: day(1), To: day(30)}

type fakeMoco struct {
	entries []model.Entry
	err     error
}

func (f *fakeMoco) Entries(ctx context.Context, from, to time.Time) ([]model.Entry, error) {
	return f.entries, f.err
}

type fakeJira struct {
	entries []model.Entry
	created []jira.Worklog
	failOn  string
	missing map[string]bool // Tickets, die in Jira nicht existieren
}

func (f *fakeJira) IssueExists(ctx context.Context, key string) (bool, error) {
	return !f.missing[key], nil
}

func (f *fakeJira) Worklogs(ctx context.Context, from, to time.Time) ([]model.Entry, error) {
	return f.entries, nil
}

func (f *fakeJira) Create(ctx context.Context, w jira.Worklog) error {
	if w.Ticket == f.failOn {
		return errors.New("Issue does not exist")
	}
	f.created = append(f.created, w)
	return nil
}

func mocoEntry(ticket string, d, min int, marker, project string) model.Entry {
	return model.Entry{
		Ticket: ticket, Date: day(d), Duration: time.Duration(min) * time.Minute,
		Description: ticket + " Arbeit (" + marker + ")", Marker: marker,
		ProjectID: project, Source: model.SourceMoco,
	}
}

func TestValidateMeldetAbweichung(t *testing.T) {
	a := &App{
		Moco:            &fakeMoco{entries: []model.Entry{mocoEntry("ABC-1", 18, 120, "TG", "1001")}},
		Jira:            &fakeJira{entries: []model.Entry{{Ticket: "ABC-1", Date: day(18), Duration: 90 * time.Minute, Source: model.SourceJira}}},
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Validate(context.Background(), testRange)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.ExitCode() != 1 {
		t.Fatalf("ExitCode = %d, want 1", rep.ExitCode())
	}
	if len(rep.Diffs) != 1 || rep.Diffs[0].Delta() != 30*time.Minute {
		t.Fatalf("Diffs = %+v", rep.Diffs)
	}
}

func TestValidateSauberWennGleich(t *testing.T) {
	a := &App{
		Moco:            &fakeMoco{entries: []model.Entry{mocoEntry("ABC-1", 18, 120, "TG", "1001")}},
		Jira:            &fakeJira{entries: []model.Entry{{Ticket: "ABC-1", Date: day(18), Duration: 120 * time.Minute, Source: model.SourceJira}}},
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Validate(context.Background(), testRange)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.ExitCode() != 0 {
		t.Fatalf("ExitCode = %d, want 0. Probleme: %+v", rep.ExitCode(), rep.Problems)
	}
}

func TestValidateMeldetMarkerKonflikt(t *testing.T) {
	a := &App{
		Moco:            &fakeMoco{entries: []model.Entry{mocoEntry("ABC-1", 18, 60, "RM", "1001")}},
		Jira:            &fakeJira{entries: []model.Entry{{Ticket: "ABC-1", Date: day(18), Duration: 60 * time.Minute, Source: model.SourceJira}}},
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, _ := a.Validate(context.Background(), testRange)
	var found bool
	for _, p := range rep.Problems {
		if p.Kind == model.ProblemMarkerMismatch {
			found = true
		}
	}
	if !found {
		t.Fatalf("Marker-Konflikt nicht gemeldet: %+v", rep.Problems)
	}
}

func TestSyncDryRunSchreibtNichts(t *testing.T) {
	j := &fakeJira{}
	a := &App{
		Moco:            &fakeMoco{entries: []model.Entry{mocoEntry("ABC-1", 18, 120, "TG", "1001")}},
		Jira:            j,
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Sync(context.Background(), testRange, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(j.created) != 0 {
		t.Fatalf("Dry-Run darf nichts schreiben, got %+v", j.created)
	}
	if !rep.DryRun || len(rep.Planned) != 1 {
		t.Fatalf("Report falsch: DryRun=%v Planned=%+v", rep.DryRun, rep.Planned)
	}
}

func TestSyncApplySchreibtDifferenz(t *testing.T) {
	j := &fakeJira{entries: []model.Entry{{Ticket: "ABC-1", Date: day(18), Duration: 90 * time.Minute, Source: model.SourceJira}}}
	a := &App{
		Moco:            &fakeMoco{entries: []model.Entry{mocoEntry("ABC-1", 18, 120, "TG", "1001")}},
		Jira:            j,
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Sync(context.Background(), testRange, true)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(j.created) != 1 || j.created[0].Duration != 30*time.Minute {
		t.Fatalf("erwarte eine Buchung über 30m, got %+v", j.created)
	}
	if len(rep.Applied) != 1 {
		t.Fatalf("Applied = %+v", rep.Applied)
	}
}

func TestSyncLaeuftNachFehlerWeiterUndMeldetIhn(t *testing.T) {
	j := &fakeJira{failOn: "ABC-1"}
	a := &App{
		Moco: &fakeMoco{entries: []model.Entry{
			mocoEntry("ABC-1", 18, 60, "TG", "1001"),
			mocoEntry("ABC-2", 18, 60, "TG", "1001"),
		}},
		Jira:            j,
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Sync(context.Background(), testRange, true)
	if err != nil {
		t.Fatalf("Sync darf nicht abbrechen: %v", err)
	}
	if len(j.created) != 1 || j.created[0].Ticket != "ABC-2" {
		t.Fatalf("ABC-2 hätte gebucht werden müssen: %+v", j.created)
	}
	if rep.ExitCode() != 1 {
		t.Fatalf("ExitCode = %d, want 1", rep.ExitCode())
	}
}

func TestSyncMeldetEintragOhneTicketUndBuchtDenRest(t *testing.T) {
	j := &fakeJira{}
	a := &App{
		Moco: &fakeMoco{entries: []model.Entry{
			{Date: day(18), Duration: 30 * time.Minute, Description: "Interne Abstimmung", ProjectID: "1001", Source: model.SourceMoco},
			mocoEntry("ABC-2", 18, 60, "TG", "1001"),
		}},
		Jira:            j,
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Sync(context.Background(), testRange, true)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(j.created) != 1 {
		t.Fatalf("der Rest muss gebucht werden: %+v", j.created)
	}
	var found bool
	for _, p := range rep.Problems {
		if p.Kind == model.ProblemNoTicket {
			found = true
		}
	}
	if !found {
		t.Fatalf("fehlender Ticket-Key nicht gemeldet: %+v", rep.Problems)
	}
}

func TestNichtExistierendesTicketWirdUmgeleitet(t *testing.T) {
	j := &fakeJira{missing: map[string]bool{"PHT-0": true}}
	a := &App{
		Moco: &fakeMoco{entries: []model.Entry{
			mocoEntry("PHT-0", 18, 60, "TG", "1001"),
			mocoEntry("PHT-7771", 18, 30, "TG", "1001"),
		}},
		Jira:            j,
		ExpectedMarkers: map[string]string{"1001": "TG"},
		DefaultTicket:   "PHT-7771",
	}
	rep, err := a.Sync(context.Background(), testRange, true)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	// PHT-0 (60m) und PHT-7771 (30m) verschmelzen zu einer 90m-Buchung.
	if len(j.created) != 1 || j.created[0].Ticket != "PHT-7771" || j.created[0].Duration != 90*time.Minute {
		t.Fatalf("erwarte eine 90m-Buchung auf PHT-7771, got %+v", j.created)
	}
	var hint bool
	for _, p := range rep.Problems {
		if p.Kind == model.ProblemTicketRemapped {
			hint = true
		}
	}
	if !hint {
		t.Fatalf("Umleitung muss als Hinweis erscheinen: %+v", rep.Problems)
	}
}

func TestUmleitungOhneDefaultTicketBleibtAus(t *testing.T) {
	j := &fakeJira{missing: map[string]bool{"PHT-0": true}}
	a := &App{
		Moco:            &fakeMoco{entries: []model.Entry{mocoEntry("PHT-0", 18, 60, "TG", "1001")}},
		Jira:            j,
		ExpectedMarkers: map[string]string{"1001": "TG"},
	}
	rep, err := a.Sync(context.Background(), testRange, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(rep.Planned) != 1 || rep.Planned[0].Ticket != "PHT-0" {
		t.Fatalf("ohne default_ticket keine Umleitung, got %+v", rep.Planned)
	}
}

func TestValidateFehlerBeimLadenWirdDurchgereicht(t *testing.T) {
	a := &App{Moco: &fakeMoco{err: errors.New("Moco nicht erreichbar")}, Jira: &fakeJira{}}
	if _, err := a.Validate(context.Background(), testRange); err == nil {
		t.Fatal("Ladefehler müssen als Fehler zurückkommen (Exit-Code 2)")
	}
}
