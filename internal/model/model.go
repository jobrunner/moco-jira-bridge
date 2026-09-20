// Package model enthält die gemeinsamen Datentypen von Moco- und Jira-Seite.
package model

import "time"

type Source string

const (
	SourceMoco Source = "moco"
	SourceJira Source = "jira"
)

// Entry ist eine einzelne Zeitbuchung, normalisiert aus einem der beiden Systeme.
type Entry struct {
	Ticket      string // z.B. "ABC-1234"; leer, wenn nicht erkannt
	Date        time.Time
	Duration    time.Duration
	Description string
	Source      Source
	SourceID    string
	Marker      string // Klammerzusatz aus der Moco-Beschreibung, z.B. "TG"
	ProjectID   string // Moco-Projekt-ID
}

// Diff ist der Vergleich einer (Ticket, Tag)-Gruppe zwischen beiden Systemen.
type Diff struct {
	Ticket string
	Date   time.Time
	Want   time.Duration // Moco — führend
	Have   time.Duration // Jira/Tempo
}

// Delta ist die Zeit, die in Jira noch fehlt. Negativ bedeutet Überhang in Jira.
func (d Diff) Delta() time.Duration { return d.Want - d.Have }

// Day schneidet eine Zeitangabe auf den lokalen Kalendertag zurück.
func Day(t time.Time) time.Time {
	t = t.In(time.Local)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

// RoundMinutes rundet auf volle Minuten. Alle Vergleiche laufen über diese
// Granularität, damit Float-Artefakte aus der Moco-API keine Abweichungen erzeugen.
func RoundMinutes(d time.Duration) time.Duration { return d.Round(time.Minute) }

type ProblemKind string

const (
	ProblemNoTicket       ProblemKind = "kein-ticket"
	ProblemMarkerMismatch ProblemKind = "marker-konflikt"
	ProblemJiraOverhang   ProblemKind = "jira-ueberhang"
	ProblemAPIError       ProblemKind = "api-fehler"
)

// Problem ist eine Auffälligkeit, die gemeldet, aber nicht automatisch behoben wird.
type Problem struct {
	Kind   ProblemKind
	Ticket string
	Date   time.Time
	Detail string
}
