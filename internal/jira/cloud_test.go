package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"moco-jira-bridge/internal/config"
)

const searchPage1 = `{"issues":[{"key":"ABC-1234"}],"nextPageToken":"tok2"}`
const searchPage2 = `{"issues":[]}`

const worklogJSON = `{"worklogs":[
  {"id":"9001","started":"2026-09-18T09:00:00.000+0200","timeSpentSeconds":3600,
   "author":{"emailAddress":"jo.brunner@mayflower.de","accountId":"acc-1"}},
  {"id":"9002","started":"2026-09-18T11:00:00.000+0200","timeSpentSeconds":1800,
   "author":{"emailAddress":"jemand.anders@kunde.de","accountId":"acc-2"}},
  {"id":"9003","started":"2026-08-01T09:00:00.000+0200","timeSpentSeconds":3600,
   "author":{"emailAddress":"jo.brunner@mayflower.de","accountId":"acc-1"}}
]}`

func cloudServer(t *testing.T, onCreate func(body map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/search/jql"):
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("nextPageToken") == "tok2" {
				_, _ = w.Write([]byte(searchPage2))
				return
			}
			_, _ = w.Write([]byte(searchPage1))
		case strings.HasSuffix(r.URL.Path, "/worklog") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(worklogJSON))
		case strings.HasSuffix(r.URL.Path, "/worklog") && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if onCreate != nil {
				onCreate(body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"9100"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func testCloud(t *testing.T, srv *httptest.Server) *Cloud {
	t.Helper()
	return NewCloud(config.JiraConfig{
		Target: config.TargetJiraCloud, BaseURL: srv.URL,
		User: "jo.brunner@mayflower.de", APIToken: "tok",
	}, srv.Client())
}

func TestWorklogsFiltertAutorUndZeitraum(t *testing.T) {
	srv := cloudServer(t, nil)
	defer srv.Close()

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 9, 30, 0, 0, 0, 0, time.Local)
	entries, err := testCloud(t, srv).Worklogs(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Worklogs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("erwarte 1 eigenen Worklog im Zeitraum, got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Ticket != "ABC-1234" || e.Duration != time.Hour || e.SourceID != "9001" {
		t.Fatalf("Worklog falsch gemappt: %+v", e)
	}
	if e.Date.Format("2006-01-02") != "2026-09-18" {
		t.Fatalf("Datum falsch: %v", e.Date)
	}
}

func TestCreateSchicktStartedUndSekunden(t *testing.T) {
	var got map[string]any
	srv := cloudServer(t, func(body map[string]any) { got = body })
	defer srv.Close()

	err := testCloud(t, srv).Create(context.Background(), Worklog{
		Ticket:   "ABC-1234",
		Date:     time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local),
		Duration: 90 * time.Minute,
		Comment:  "ABC-1234 Importfehler (TG)",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got["timeSpentSeconds"] != float64(5400) {
		t.Fatalf("timeSpentSeconds = %v, want 5400", got["timeSpentSeconds"])
	}
	started, _ := got["started"].(string)
	if !strings.HasPrefix(started, "2026-09-18T") {
		t.Fatalf("started = %q, erwarte das Moco-Datum", started)
	}
}

func TestCreateFehlerBeiStatus400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
	}))
	defer srv.Close()

	err := testCloud(t, srv).Create(context.Background(), Worklog{
		Ticket: "ABC-9999", Date: time.Now(), Duration: time.Hour,
	})
	if err == nil {
		t.Fatal("erwarte Fehler bei HTTP 400")
	}
	if !strings.Contains(err.Error(), "ABC-9999") {
		t.Fatalf("Fehler sollte das Ticket nennen: %v", err)
	}
}
