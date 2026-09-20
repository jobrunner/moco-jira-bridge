package jira

import (
	"strings"
	"testing"

	"moco-jira-bridge/internal/config"
)

func TestNewTargetJiraCloud(t *testing.T) {
	tgt, err := NewTarget(config.JiraConfig{
		Target: config.TargetJiraCloud, BaseURL: "https://x.atlassian.net",
		User: "a@b.de", APIToken: "tok",
	}, nil)
	if err != nil {
		t.Fatalf("NewTarget: %v", err)
	}
	if _, ok := tgt.(*Cloud); !ok {
		t.Fatalf("erwarte *Cloud, got %T", tgt)
	}
}

func TestNewTargetStubsMeldenKlar(t *testing.T) {
	for _, target := range []string{config.TargetTempoCloud, config.TargetTempoServer} {
		_, err := NewTarget(config.JiraConfig{Target: target, BaseURL: "https://x", User: "a", APIToken: "t"}, nil)
		if err == nil {
			t.Fatalf("%s sollte noch nicht implementiert sein", target)
		}
		if !strings.Contains(err.Error(), target) {
			t.Fatalf("Fehlermeldung sollte %q nennen: %v", target, err)
		}
	}
}

func TestNewTargetUnbekannt(t *testing.T) {
	if _, err := NewTarget(config.JiraConfig{Target: "quatsch"}, nil); err == nil {
		t.Fatal("erwarte Fehler bei unbekanntem Target")
	}
}
