package moco

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"moco-jira-bridge/internal/config"
	"moco-jira-bridge/internal/parse"
)

func testParser(t *testing.T) *parse.Parser {
	t.Helper()
	p, err := parse.NewParser(config.DefaultTicketPattern, config.DefaultMarkerPattern)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

const activitiesJSON = `[
  {"id": 1, "date": "2026-09-18", "hours": 1.5,
   "description": "ABC-1234 Importfehler (TG)",
   "project": {"id": 1001}, "user": {"id": 12345}},
  {"id": 2, "date": "2026-09-18", "hours": 0.25,
   "description": "Interne Abstimmung",
   "project": {"id": 1001}, "user": {"id": 12345}}
]`

func TestEntriesMapptAktivitaeten(t *testing.T) {
	var gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(activitiesJSON))
	}))
	defer srv.Close()

	cfg := config.MocoConfig{
		Domain: "mayflower", UserID: 12345, APIKey: "secret",
		Projects: []config.Project{{ID: "1001", Marker: "TG"}},
	}
	c := New(cfg, testParser(t), srv.Client()).WithBaseURL(srv.URL)

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 9, 30, 0, 0, 0, 0, time.Local)
	entries, err := c.Entries(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("erwarte 2 Einträge, got %d", len(entries))
	}

	first := entries[0]
	if first.Ticket != "ABC-1234" || first.Marker != "TG" {
		t.Fatalf("Parsing falsch: %+v", first)
	}
	if first.Duration != 90*time.Minute {
		t.Fatalf("Duration = %v, want 90m", first.Duration)
	}
	if first.ProjectID != "1001" || first.SourceID != "1" {
		t.Fatalf("Metadaten falsch: %+v", first)
	}
	if first.Date.Format("2006-01-02") != "2026-09-18" {
		t.Fatalf("Datum falsch: %v", first.Date)
	}
	if entries[1].Ticket != "" {
		t.Fatalf("zweiter Eintrag hat kein Ticket, got %q", entries[1].Ticket)
	}

	if gotAuth != "Token token=secret" {
		t.Fatalf("Authorization-Header = %q", gotAuth)
	}
	for _, want := range []string{"from=2026-09-01", "to=2026-09-30", "user_id=12345", "project_id=1001"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("Query %q enthält %q nicht", gotQuery, want)
		}
	}
}

func TestEntriesFehlerBeiHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid token"}`))
	}))
	defer srv.Close()

	cfg := config.MocoConfig{Domain: "x", UserID: 1, APIKey: "geheim",
		Projects: []config.Project{{ID: "1001"}}}
	c := New(cfg, testParser(t), srv.Client()).WithBaseURL(srv.URL)

	_, err := c.Entries(context.Background(), time.Now(), time.Now())
	if err == nil {
		t.Fatal("erwarte Fehler bei HTTP 401")
	}
	if strings.Contains(err.Error(), "geheim") {
		t.Fatal("Fehlermeldung darf das Token nicht enthalten")
	}
}
