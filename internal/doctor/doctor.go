// Package doctor prüft Konfiguration und Erreichbarkeit beider Systeme und
// hilft bei der offenen Frage, welche Jira-/Tempo-Variante der Kunde einsetzt.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"moco-jira-bridge/internal/config"
)

type Check struct {
	Name   string
	OK     bool
	Detail string
}

func AllOK(checks []Check) bool {
	for _, c := range checks {
		if !c.OK {
			return false
		}
	}
	return true
}

// Run prüft Moco und Jira. mocoBaseURL überschreibt die aus der Domain gebaute
// URL — leer lassen, außer in Tests.
func Run(ctx context.Context, cfg *config.Config, httpClient *http.Client, mocoBaseURL string) []Check {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if mocoBaseURL == "" {
		mocoBaseURL = fmt.Sprintf("https://%s.mocoapp.com", cfg.Moco.Domain)
	}

	checks := []Check{checkMoco(ctx, cfg, httpClient, mocoBaseURL)}
	checks = append(checks, checkJiraSelf(ctx, cfg, httpClient))
	checks = append(checks, checkTempoEndpoints(ctx, cfg, httpClient)...)
	return checks
}

// checkMoco fragt eine Aktivität des konfigurierten Users ab. Das prüft Token,
// Domain und User-ID in einem Zug.
func checkMoco(ctx context.Context, cfg *config.Config, c *http.Client, baseURL string) Check {
	const name = "Moco erreichbar"
	today := time.Now().Format("2006-01-02")
	q := url.Values{}
	q.Set("from", today)
	q.Set("to", today)
	q.Set("user_id", strconv.Itoa(cfg.Moco.UserID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/activities?"+q.Encode(), nil)
	if err != nil {
		return Check{Name: name, OK: false, Detail: err.Error()}
	}
	req.Header.Set("Authorization", "Token token="+cfg.Moco.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return Check{Name: name, OK: false, Detail: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return Check{Name: name, OK: false,
			Detail: fmt.Sprintf("Status %d — %s, moco.domain und moco.user_id prüfen",
				resp.StatusCode, config.EnvMocoAPIKey)}
	}
	return Check{Name: name, OK: true,
		Detail: fmt.Sprintf("Token gültig, %d Projekt(e) konfiguriert", len(cfg.Moco.Projects))}
}

func checkJiraSelf(ctx context.Context, cfg *config.Config, c *http.Client) Check {
	const name = "Jira erreichbar"
	endpoint := strings.TrimRight(cfg.Jira.BaseURL, "/") + "/rest/api/3/myself"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Check{Name: name, OK: false, Detail: err.Error()}
	}
	req.SetBasicAuth(cfg.Jira.User, cfg.Jira.APIToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return Check{Name: name, OK: false, Detail: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return Check{Name: name, OK: false,
			Detail: fmt.Sprintf("Status %d — %s oder jira.base_url prüfen",
				resp.StatusCode, config.EnvJiraAPIToken)}
	}
	var self struct {
		AccountID    string `json:"accountId"`
		EmailAddress string `json:"emailAddress"`
	}
	_ = json.Unmarshal(body, &self)
	return Check{Name: name, OK: true,
		Detail: fmt.Sprintf("angemeldet als %s (%s) — target %q ist nutzbar",
			self.EmailAddress, self.AccountID, config.TargetJiraCloud)}
}

// checkTempoEndpoints tastet ab, welche Tempo-Variante die Instanz anbietet.
// Ein Fehlschlag ist kein Problem — er beantwortet nur die offene Frage,
// welcher jira.target-Wert langfristig passt.
func checkTempoEndpoints(ctx context.Context, cfg *config.Config, c *http.Client) []Check {
	probes := []struct {
		name string
		path string
	}{
		{"Tempo Server/DC vorhanden", "/rest/tempo-timesheets/4/worklogs"},
		{"Tempo Cloud Plugin sichtbar", "/rest/tempo-core/1/globalconfiguration"},
	}
	var checks []Check
	for _, p := range probes {
		endpoint := strings.TrimRight(cfg.Jira.BaseURL, "/") + p.path
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			checks = append(checks, Check{Name: p.name, OK: false, Detail: err.Error()})
			continue
		}
		req.SetBasicAuth(cfg.Jira.User, cfg.Jira.APIToken)
		resp, err := c.Do(req)
		if err != nil {
			checks = append(checks, Check{Name: p.name, OK: false, Detail: err.Error()})
			continue
		}
		resp.Body.Close()

		var ok bool
		var detail string
		switch {
		case resp.StatusCode == http.StatusNotFound:
			ok, detail = false, fmt.Sprintf("Status %d — nicht vorhanden", resp.StatusCode)
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			ok, detail = false, fmt.Sprintf("Status %d — keine Aussage möglich, solange die Jira-Anmeldung fehlschlägt", resp.StatusCode)
		default:
			ok, detail = true, fmt.Sprintf("Status %d — Endpunkt antwortet, Variante kommt in Frage", resp.StatusCode)
		}
		checks = append(checks, Check{Name: p.name, OK: ok, Detail: detail})
	}
	return checks
}

func WriteChecks(w io.Writer, checks []Check) error {
	for _, c := range checks {
		mark := "ok  "
		if !c.OK {
			mark = "FEHL"
		}
		if _, err := fmt.Fprintf(w, "[%s] %s: %s\n", mark, c.Name, c.Detail); err != nil {
			return err
		}
	}
	return nil
}
