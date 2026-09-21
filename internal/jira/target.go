// Package jira schreibt und liest Worklogs auf der Kundenseite. Welche
// Implementierung greift, entscheidet jira.target in der Konfiguration.
package jira

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"moco-jira-bridge/internal/config"
	"moco-jira-bridge/internal/model"
)

// Worklog ist eine zu schreibende Buchung.
type Worklog struct {
	Ticket   string
	Date     time.Time
	Start    time.Time // konkreter Startzeitpunkt; Fallback: 09:00 lokal am Date
	Duration time.Duration
	Comment  string
}

// Target ist die Kundenseite: Worklogs lesen und anlegen. Kein Ändern, kein Löschen.
type Target interface {
	Worklogs(ctx context.Context, from, to time.Time) ([]model.Entry, error)
	Create(ctx context.Context, w Worklog) error
}

func NewTarget(cfg config.JiraConfig, httpClient *http.Client) (Target, error) {
	switch cfg.Target {
	case config.TargetJiraCloud:
		return NewCloud(cfg, httpClient), nil
	case config.TargetTempoCloud:
		return nil, fmt.Errorf("target %q ist noch nicht implementiert — "+
			"führe `mjb doctor` aus, um zu prüfen, ob %q funktioniert",
			config.TargetTempoCloud, config.TargetJiraCloud)
	case config.TargetTempoServer:
		return nil, fmt.Errorf("target %q ist noch nicht implementiert — "+
			"führe `mjb doctor` aus, um zu prüfen, ob %q funktioniert",
			config.TargetTempoServer, config.TargetJiraCloud)
	default:
		return nil, fmt.Errorf("unbekanntes jira.target %q — erlaubt sind %q, %q, %q",
			cfg.Target, config.TargetJiraCloud, config.TargetTempoCloud, config.TargetTempoServer)
	}
}
