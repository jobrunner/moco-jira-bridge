package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valid = `
moco:
  domain: mayflower
  user_id: 12345
  projects:
    - id: "1001"
      marker: TG
    - id: "1002"
      marker: RM
jira:
  target: jira-cloud
  base_url: https://kunde.atlassian.net
  user: jo.brunner@mayflower.de
ticket:
  pattern: '^([A-Z][A-Z0-9]+-\d+)'
  marker_pattern: '\(([^)]+)\)'
`

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadLiestConfigUndTokens(t *testing.T) {
	t.Setenv("MOCO_API_KEY", "moco-secret")
	t.Setenv("JIRA_API_TOKEN", "jira-secret")

	c, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Moco.Domain != "mayflower" || c.Moco.UserID != 12345 {
		t.Fatalf("Moco falsch geladen: %+v", c.Moco)
	}
	if c.Moco.APIKey != "moco-secret" || c.Jira.APIToken != "jira-secret" {
		t.Fatal("Tokens wurden nicht aus der Umgebung übernommen")
	}
	if c.Jira.BaseURL != "https://kunde.atlassian.net" {
		t.Fatalf("BaseURL falsch: %q", c.Jira.BaseURL)
	}
}

func TestLoadFehlerBeiFehlendemToken(t *testing.T) {
	t.Setenv("MOCO_API_KEY", "")
	t.Setenv("JIRA_API_TOKEN", "jira-secret")

	_, err := Load(write(t, valid))
	if err == nil || !strings.Contains(err.Error(), "MOCO_API_KEY") {
		t.Fatalf("erwarte Fehler zu MOCO_API_KEY, got %v", err)
	}
}

func TestLoadFehlerBeiFehlenderDatei(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "gibt-es-nicht.yaml")); err == nil {
		t.Fatal("erwarte Fehler bei fehlender Datei")
	}
}

func TestLoadValidiertPflichtfelder(t *testing.T) {
	t.Setenv("MOCO_API_KEY", "x")
	t.Setenv("JIRA_API_TOKEN", "y")

	_, err := Load(write(t, "moco:\n  domain: \"\"\n"))
	if err == nil {
		t.Fatal("erwarte Validierungsfehler bei leerer Konfiguration")
	}
}

func TestLoadSetztDefaultPatterns(t *testing.T) {
	t.Setenv("MOCO_API_KEY", "x")
	t.Setenv("JIRA_API_TOKEN", "y")

	ohnePatterns := strings.Split(valid, "ticket:")[0]
	c, err := Load(write(t, ohnePatterns))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Ticket.Pattern == "" || c.Ticket.MarkerPattern == "" {
		t.Fatal("Default-Patterns sollten gesetzt werden")
	}
}

func TestDefaultPathBevorzugtArbeitsverzeichnis(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DefaultPath(); got != "config.yaml" {
		t.Fatalf("DefaultPath() = %q, want config.yaml im Arbeitsverzeichnis", got)
	}
}

func TestDefaultPathFallbackOhneLokaleDatei(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := DefaultPath(); got == "config.yaml" {
		t.Fatal("ohne lokale Datei darf nicht das Arbeitsverzeichnis gewinnen")
	}
}

func TestExpectedMarkers(t *testing.T) {
	t.Setenv("MOCO_API_KEY", "x")
	t.Setenv("JIRA_API_TOKEN", "y")

	c, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := c.ExpectedMarkers()
	if m["1001"] != "TG" || m["1002"] != "RM" {
		t.Fatalf("ExpectedMarkers = %+v", m)
	}
}
