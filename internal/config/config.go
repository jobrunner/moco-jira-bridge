// Package config lädt die YAML-Konfiguration und die API-Tokens aus der Umgebung.
// Secrets stehen niemals in der Datei.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	EnvMocoAPIKey   = "MOCO_API_KEY"
	EnvJiraAPIToken = "JIRA_API_TOKEN"

	DefaultTicketPattern = `^([A-Z][A-Z0-9]+-\d+)`
	DefaultMarkerPattern = `\(([^)]+)\)`

	TargetJiraCloud   = "jira-cloud"
	TargetTempoCloud  = "tempo-cloud"
	TargetTempoServer = "tempo-server"
)

type Project struct {
	ID     string `yaml:"id"`
	Marker string `yaml:"marker"`
}

type MocoConfig struct {
	Domain   string    `yaml:"domain"`
	UserID   int       `yaml:"user_id"`
	Projects []Project `yaml:"projects"`
	APIKey   string    `yaml:"-"` // aus der Umgebung
}

type JiraConfig struct {
	Target   string `yaml:"target"`
	BaseURL  string `yaml:"base_url"`
	User     string `yaml:"user"`
	APIToken string `yaml:"-"` // aus der Umgebung
}

type TicketConfig struct {
	Pattern       string `yaml:"pattern"`
	MarkerPattern string `yaml:"marker_pattern"`
}

type Config struct {
	Moco   MocoConfig   `yaml:"moco"`
	Jira   JiraConfig   `yaml:"jira"`
	Ticket TicketConfig `yaml:"ticket"`
}

// DefaultPath ist der Ort, an dem die Konfiguration ohne --config erwartet
// wird. Eine config.yaml im Arbeitsverzeichnis gewinnt; sonst der User-Config-Pfad.
func DefaultPath() string {
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "moco-jira-bridge", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(home, ".config", "moco-jira-bridge", "config.yaml")
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Konfiguration %s nicht lesbar: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("Konfiguration %s ist kein gültiges YAML: %w", path, err)
	}

	if c.Ticket.Pattern == "" {
		c.Ticket.Pattern = DefaultTicketPattern
	}
	if c.Ticket.MarkerPattern == "" {
		c.Ticket.MarkerPattern = DefaultMarkerPattern
	}
	if c.Jira.Target == "" {
		c.Jira.Target = TargetJiraCloud
	}

	c.Moco.APIKey = os.Getenv(EnvMocoAPIKey)
	c.Jira.APIToken = os.Getenv(EnvJiraAPIToken)

	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) validate() error {
	if c.Moco.Domain == "" {
		return fmt.Errorf("moco.domain fehlt in der Konfiguration")
	}
	if c.Moco.UserID == 0 {
		return fmt.Errorf("moco.user_id fehlt in der Konfiguration")
	}
	if len(c.Moco.Projects) == 0 {
		return fmt.Errorf("moco.projects ist leer — ohne Projekt-IDs weiß das Tool nicht, was es betrachten soll")
	}
	if c.Jira.BaseURL == "" {
		return fmt.Errorf("jira.base_url fehlt in der Konfiguration")
	}
	if c.Jira.User == "" {
		return fmt.Errorf("jira.user fehlt in der Konfiguration")
	}
	if c.Moco.APIKey == "" {
		return fmt.Errorf("Umgebungsvariable %s ist nicht gesetzt", EnvMocoAPIKey)
	}
	if c.Jira.APIToken == "" {
		return fmt.Errorf("Umgebungsvariable %s ist nicht gesetzt", EnvJiraAPIToken)
	}
	return nil
}

// ProjectIDs sind die Moco-Projekte, die betrachtet werden.
func (c *Config) ProjectIDs() []string {
	ids := make([]string, 0, len(c.Moco.Projects))
	for _, p := range c.Moco.Projects {
		ids = append(ids, p.ID)
	}
	return ids
}

// ExpectedMarkers bildet Moco-Projekt-ID auf den erwarteten Validierungsmarker ab.
func (c *Config) ExpectedMarkers() map[string]string {
	m := make(map[string]string, len(c.Moco.Projects))
	for _, p := range c.Moco.Projects {
		m[p.ID] = p.Marker
	}
	return m
}
