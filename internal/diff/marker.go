package diff

import (
	"fmt"
	"strings"

	"moco-jira-bridge/internal/model"
)

// CheckMarkers prüft den Klammerzusatz jeder Moco-Buchung gegen den für ihr
// Projekt konfigurierten Marker. Das automatisiert die bisherige Sichtprüfung,
// ob ein Ticket im richtigen Moco-Projekt gelandet ist.
//
// expected bildet Moco-Projekt-ID auf den erwarteten Marker ab. Projekte, die
// dort nicht vorkommen, werden nicht geprüft.
func CheckMarkers(moco []model.Entry, expected map[string]string) []model.Problem {
	var problems []model.Problem
	for _, e := range moco {
		want, ok := expected[e.ProjectID]
		if !ok || want == "" {
			continue
		}
		if markerMatches(e.Marker, want) {
			continue
		}
		detail := fmt.Sprintf("Projekt %s erwartet Marker %q, Buchung hat %q",
			e.ProjectID, want, e.Marker)
		if e.Marker == "" {
			detail = fmt.Sprintf("Projekt %s erwartet Marker %q, Buchung hat keinen",
				e.ProjectID, want)
		}
		problems = append(problems, model.Problem{
			Kind:   model.ProblemMarkerMismatch,
			Ticket: e.Ticket,
			Date:   model.Day(e.Date),
			Detail: detail,
		})
	}
	sortProblems(problems)
	return problems
}

// markerMatches akzeptiert den Marker an jeder Komma-Position und mit Zusatz:
// "Daily, RM", "Klärung, Umsetzung, RM" und "RM Sprint 4" gelten alle als "RM".
func markerMatches(got, want string) bool {
	want = strings.TrimSpace(strings.ToUpper(want))
	for _, part := range strings.Split(strings.ToUpper(got), ",") {
		part = strings.TrimSpace(part)
		if part == want || strings.HasPrefix(part, want+" ") {
			return true
		}
	}
	return false
}
