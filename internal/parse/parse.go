// Package parse zieht Jira-Ticket-Key und Validierungsmarker aus einer
// Moco-Buchungsbeschreibung. Beide Muster sind konfigurierbar.
package parse

import (
	"fmt"
	"regexp"
	"strings"
)

type Parser struct {
	ticket *regexp.Regexp
	marker *regexp.Regexp
}

// NewParser übersetzt die konfigurierten Muster. Beide brauchen genau eine
// Capture-Group — sie liefert den gesuchten Wert.
func NewParser(ticketPattern, markerPattern string) (*Parser, error) {
	ticket, err := compileWithGroup(ticketPattern, "ticket")
	if err != nil {
		return nil, err
	}
	marker, err := compileWithGroup(markerPattern, "marker")
	if err != nil {
		return nil, err
	}
	return &Parser{ticket: ticket, marker: marker}, nil
}

func compileWithGroup(pattern, name string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s-Pattern %q ist kein gültiger regulärer Ausdruck: %w", name, pattern, err)
	}
	if re.NumSubexp() < 1 {
		return nil, fmt.Errorf("%s-Pattern %q braucht eine Capture-Group, z.B. (…)", name, pattern)
	}
	return re, nil
}

// Parse liefert Ticket-Key und Marker. Was nicht gefunden wird, kommt leer zurück.
func (p *Parser) Parse(description string) (ticket, marker string) {
	if m := p.ticket.FindStringSubmatch(description); m != nil {
		ticket = strings.TrimSpace(m[1])
	}
	if m := p.marker.FindStringSubmatch(description); m != nil {
		marker = strings.TrimSpace(m[1])
	}
	return ticket, marker
}
