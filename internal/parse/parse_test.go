package parse

import "testing"

const (
	ticketPat = `^([A-Z][A-Z0-9]+-\d+)`
	markerPat = `\(([^)]+)\)`
)

func TestParse(t *testing.T) {
	p, err := NewParser(ticketPat, markerPat)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	cases := []struct {
		name       string
		in         string
		wantTicket string
		wantMarker string
	}{
		{"ticket und marker", "ABC-1234 Fehler im Import (TG)", "ABC-1234", "TG"},
		{"ticket ohne marker", "ABC-1234 Fehler im Import", "ABC-1234", ""},
		{"roadmap-marker", "ABC-99 Neues Dashboard (RM Sprint 4)", "ABC-99", "RM Sprint 4"},
		{"kein ticket", "Interne Abstimmung (TG)", "", "TG"},
		{"ticket nicht am anfang wird nicht erkannt", "Siehe ABC-1234", "", ""},
		{"leere beschreibung", "", "", ""},
		{"mehrere klammern: erste gewinnt", "ABC-1 Text (TG) mehr (RM)", "ABC-1", "TG"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ticket, marker := p.Parse(c.in)
			if ticket != c.wantTicket || marker != c.wantMarker {
				t.Fatalf("Parse(%q) = (%q, %q), want (%q, %q)",
					c.in, ticket, marker, c.wantTicket, c.wantMarker)
			}
		})
	}
}

func TestNewParserLehntUngueltigesPatternAb(t *testing.T) {
	if _, err := NewParser(`([A-Z`, markerPat); err == nil {
		t.Fatal("NewParser sollte bei ungültigem Ticket-Pattern einen Fehler liefern")
	}
}

func TestNewParserBrauchtCaptureGroup(t *testing.T) {
	if _, err := NewParser(`^[A-Z]+-\d+`, markerPat); err == nil {
		t.Fatal("NewParser sollte ein Pattern ohne Capture-Group ablehnen")
	}
}
