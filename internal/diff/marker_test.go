package diff

import (
	"testing"

	"moco-jira-bridge/internal/model"
)

func TestCheckMarkersMeldetKonflikt(t *testing.T) {
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "1001", Marker: "RM", Description: "ABC-1 (RM)", Date: day(18)},
	}
	problems := CheckMarkers(entries, map[string]string{"1001": "TG", "1002": "RM"})
	if len(problems) != 1 {
		t.Fatalf("erwarte 1 Problem, got %+v", problems)
	}
	if problems[0].Kind != model.ProblemMarkerMismatch || problems[0].Ticket != "ABC-1" {
		t.Fatalf("unerwartetes Problem: %+v", problems[0])
	}
}

func TestCheckMarkersAkzeptiertPassendenMarker(t *testing.T) {
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "1001", Marker: "TG", Date: day(18)},
	}
	if problems := CheckMarkers(entries, map[string]string{"1001": "TG"}); len(problems) != 0 {
		t.Fatalf("erwarte keine Probleme, got %+v", problems)
	}
}

func TestCheckMarkersPraefixReichtAus(t *testing.T) {
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "1002", Marker: "RM Sprint 4", Date: day(18)},
	}
	if problems := CheckMarkers(entries, map[string]string{"1002": "RM"}); len(problems) != 0 {
		t.Fatalf("erwarte keine Probleme, got %+v", problems)
	}
}

func TestCheckMarkersMarkerAlsLetztesKommaElement(t *testing.T) {
	// Übliche Schreibweise: "(Daily, RM)" — der Marker steht hinten.
	cases := []string{"Daily, RM", "Klärung, Umsetzung, RM", "RM", "rm", "Daily,RM"}
	for _, marker := range cases {
		entries := []model.Entry{
			{Ticket: "ABC-1", ProjectID: "1002", Marker: marker, Date: day(18)},
		}
		if problems := CheckMarkers(entries, map[string]string{"1002": "RM"}); len(problems) != 0 {
			t.Fatalf("Marker %q sollte als RM gelten, got %+v", marker, problems)
		}
	}
}

func TestCheckMarkersSubstringReichtNicht(t *testing.T) {
	// "FIRMware" enthält RM, ist aber keiner.
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "1002", Marker: "Firmware", Date: day(18)},
	}
	if problems := CheckMarkers(entries, map[string]string{"1002": "RM"}); len(problems) != 1 {
		t.Fatalf("Substring darf nicht als Marker gelten, got %+v", problems)
	}
}

func TestCheckMarkersFehlenderMarkerIstKeinProblem(t *testing.T) {
	// Vergessene Klammer/vergessener Marker: die Projekt-Zuordnung kommt aus
	// Moco — gemeldet wird nur ein vorhandener, widersprechender Marker.
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "1001", Marker: "", Date: day(18)},
	}
	if problems := CheckMarkers(entries, map[string]string{"1001": "TG"}); len(problems) != 0 {
		t.Fatalf("fehlender Marker darf nicht gemeldet werden, got %+v", problems)
	}
}

func TestCheckMarkersIgnoriertProjekteOhneErwartung(t *testing.T) {
	entries := []model.Entry{
		{Ticket: "ABC-1", ProjectID: "9999", Marker: "XX", Date: day(18)},
	}
	if problems := CheckMarkers(entries, map[string]string{"1001": "TG"}); len(problems) != 0 {
		t.Fatalf("Projekte ohne konfigurierten Marker sind unauffällig, got %+v", problems)
	}
}
