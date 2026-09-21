package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"moco-jira-bridge/internal/config"
	"moco-jira-bridge/internal/model"
)

// Cloud spricht die native Jira-Cloud-Worklog-API. Tempo liest diese Einträge
// ebenfalls, deshalb braucht es dafür keine zusätzlichen Tempo-Rechte.
type Cloud struct {
	cfg  config.JiraConfig
	http *http.Client
}

func NewCloud(cfg config.JiraConfig, httpClient *http.Client) *Cloud {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Cloud{cfg: cfg, http: httpClient}
}

func (c *Cloud) do(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("Anfrage nicht serialisierbar: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.BaseURL, "/")+path, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("Jira-Anfrage nicht baubar: %w", err)
	}
	req.SetBasicAuth(c.cfg.User, c.cfg.APIToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Jira nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("Jira-Antwort nicht lesbar: %w", err)
	}
	return raw, resp.StatusCode, nil
}

type searchResponse struct {
	Issues []struct {
		Key string `json:"key"`
	} `json:"issues"`
	NextPageToken string `json:"nextPageToken"`
}

type worklogResponse struct {
	Worklogs []struct {
		ID               string `json:"id"`
		Started          string `json:"started"`
		TimeSpentSeconds int    `json:"timeSpentSeconds"`
		Author           struct {
			EmailAddress string `json:"emailAddress"`
			AccountID    string `json:"accountId"`
		} `json:"author"`
	} `json:"worklogs"`
}

// Worklogs findet erst die Issues mit eigenen Worklogs im Zeitraum und lädt
// dann deren Worklogs. Der Umweg über JQL findet auch Überhänge auf Tickets,
// die in Moco gar nicht vorkommen.
//
// Der alte Endpunkt /rest/api/3/search liefert seit 2025 nur noch 410 Gone;
// der Nachfolger /search/jql paginiert per nextPageToken.
func (c *Cloud) Worklogs(ctx context.Context, from, to time.Time) ([]model.Entry, error) {
	jql := fmt.Sprintf("worklogAuthor = currentUser() AND worklogDate >= %q AND worklogDate <= %q",
		from.Format("2006-01-02"), to.Format("2006-01-02"))

	var issues []string
	pageToken := ""
	for {
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("fields", "key")
		q.Set("maxResults", "100")
		if pageToken != "" {
			q.Set("nextPageToken", pageToken)
		}

		raw, status, err := c.do(ctx, http.MethodGet, "/rest/api/3/search/jql?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("Jira-Suche antwortet mit Status %d", status)
		}
		var search searchResponse
		if err := json.Unmarshal(raw, &search); err != nil {
			return nil, fmt.Errorf("Jira-Suchantwort ist kein erwartetes JSON: %w", err)
		}
		for _, issue := range search.Issues {
			issues = append(issues, issue.Key)
		}
		if search.NextPageToken == "" {
			break
		}
		pageToken = search.NextPageToken
	}

	var entries []model.Entry
	for _, key := range issues {
		issueEntries, err := c.worklogsForIssue(ctx, key, from, to)
		if err != nil {
			return nil, err
		}
		entries = append(entries, issueEntries...)
	}
	return entries, nil
}

func (c *Cloud) worklogsForIssue(ctx context.Context, key string, from, to time.Time) ([]model.Entry, error) {
	raw, status, err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"/worklog", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("Worklogs für %s nicht abrufbar, Status %d", key, status)
	}
	var wr worklogResponse
	if err := json.Unmarshal(raw, &wr); err != nil {
		return nil, fmt.Errorf("Worklog-Antwort für %s ist kein erwartetes JSON: %w", key, err)
	}

	fromDay, toDay := model.Day(from), model.Day(to)
	var entries []model.Entry
	for _, w := range wr.Worklogs {
		if !strings.EqualFold(w.Author.EmailAddress, c.cfg.User) {
			continue
		}
		started, err := time.Parse("2006-01-02T15:04:05.000-0700", w.Started)
		if err != nil {
			return nil, fmt.Errorf("Worklog %s auf %s hat ungültiges Startdatum %q: %w",
				w.ID, key, w.Started, err)
		}
		day := model.Day(started)
		if day.Before(fromDay) || day.After(toDay) {
			continue
		}
		entries = append(entries, model.Entry{
			Ticket:   key,
			Date:     day,
			Duration: model.RoundMinutes(time.Duration(w.TimeSpentSeconds) * time.Second),
			Source:   model.SourceJira,
			SourceID: w.ID,
		})
	}
	return entries, nil
}

// Create legt einen Worklog zum geplanten Startzeitpunkt an. Die Uhrzeiten
// sind nur Kosmetik für die manuelle Prüfung in Tempo — abgerechnet wird der
// Zeitbetrag; deshalb staffelt der Planer sie lediglich überlappungsfrei.
func (c *Cloud) Create(ctx context.Context, w Worklog) error {
	started := w.Start
	if started.IsZero() {
		started = time.Date(w.Date.Year(), w.Date.Month(), w.Date.Day(), 9, 0, 0, 0, time.Local)
	}
	body := map[string]any{
		"started":          started.Format("2006-01-02T15:04:05.000-0700"),
		"timeSpentSeconds": int(w.Duration.Seconds()),
	}
	if w.Comment != "" {
		body["comment"] = adf(w.Comment)
	}

	raw, status, err := c.do(ctx, http.MethodPost,
		"/rest/api/3/issue/"+url.PathEscape(w.Ticket)+"/worklog", body)
	if err != nil {
		return fmt.Errorf("Worklog auf %s konnte nicht angelegt werden: %w", w.Ticket, err)
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("Worklog auf %s abgelehnt, Status %d: %s",
			w.Ticket, status, strings.TrimSpace(string(raw)))
	}
	return nil
}

// adf verpackt einen Kommentar im Atlassian Document Format, das die v3-API erwartet.
func adf(text string) map[string]any {
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []any{
			map[string]any{
				"type":    "paragraph",
				"content": []any{map[string]any{"type": "text", "text": text}},
			},
		},
	}
}
