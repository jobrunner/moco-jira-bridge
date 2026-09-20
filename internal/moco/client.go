// Package moco liest Zeitbuchungen aus der Moco-API. Nur lesend — Moco ist
// das führende System und wird von diesem Tool niemals verändert.
package moco

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"moco-jira-bridge/internal/config"
	"moco-jira-bridge/internal/model"
	"moco-jira-bridge/internal/parse"
)

type Client struct {
	cfg     config.MocoConfig
	parser  *parse.Parser
	http    *http.Client
	baseURL string
}

func New(cfg config.MocoConfig, p *parse.Parser, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		cfg:     cfg,
		parser:  p,
		http:    httpClient,
		baseURL: fmt.Sprintf("https://%s.mocoapp.com", cfg.Domain),
	}
}

// WithBaseURL überschreibt die aus der Domain gebaute URL. Für Tests.
func (c *Client) WithBaseURL(u string) *Client {
	c.baseURL = u
	return c
}

type activity struct {
	ID          int     `json:"id"`
	Date        string  `json:"date"`
	Hours       float64 `json:"hours"`
	Description string  `json:"description"`
	Project     struct {
		ID int `json:"id"`
	} `json:"project"`
}

// Entries lädt alle Aktivitäten des konfigurierten Users in den konfigurierten
// Projekten im Zeitraum [from, to].
func (c *Client) Entries(ctx context.Context, from, to time.Time) ([]model.Entry, error) {
	var all []model.Entry
	for _, project := range c.cfg.Projects {
		entries, err := c.entriesForProject(ctx, project.ID, from, to)
		if err != nil {
			return nil, err
		}
		all = append(all, entries...)
	}
	return all, nil
}

func (c *Client) entriesForProject(ctx context.Context, projectID string, from, to time.Time) ([]model.Entry, error) {
	q := url.Values{}
	q.Set("from", from.Format("2006-01-02"))
	q.Set("to", to.Format("2006-01-02"))
	q.Set("user_id", strconv.Itoa(c.cfg.UserID))
	q.Set("project_id", projectID)

	endpoint := c.baseURL + "/api/v1/activities?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("Moco-Anfrage nicht baubar: %w", err)
	}
	req.Header.Set("Authorization", "Token token="+c.cfg.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Moco nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("Moco-Antwort nicht lesbar: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Moco antwortet mit Status %d für Projekt %s", resp.StatusCode, projectID)
	}

	var activities []activity
	if err := json.Unmarshal(body, &activities); err != nil {
		return nil, fmt.Errorf("Moco-Antwort ist kein erwartetes JSON: %w", err)
	}

	entries := make([]model.Entry, 0, len(activities))
	for _, a := range activities {
		date, err := time.ParseInLocation("2006-01-02", a.Date, time.Local)
		if err != nil {
			return nil, fmt.Errorf("Moco-Aktivität %d hat ungültiges Datum %q: %w", a.ID, a.Date, err)
		}
		ticket, marker := c.parser.Parse(a.Description)
		entries = append(entries, model.Entry{
			Ticket:      ticket,
			Date:        model.Day(date),
			Duration:    model.RoundMinutes(time.Duration(a.Hours * float64(time.Hour))),
			Description: a.Description,
			Source:      model.SourceMoco,
			SourceID:    strconv.Itoa(a.ID),
			Marker:      marker,
			ProjectID:   strconv.Itoa(a.Project.ID),
		})
	}
	return entries, nil
}
