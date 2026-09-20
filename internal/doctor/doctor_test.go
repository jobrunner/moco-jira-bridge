package doctor

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"moco-jira-bridge/internal/config"
)

func testConfig(jiraURL string) *config.Config {
	return &config.Config{
		Moco: config.MocoConfig{
			Domain: "test", UserID: 1, APIKey: "tok",
			Projects: []config.Project{{ID: "1001", Marker: "TG"}},
		},
		Jira: config.JiraConfig{
			Target: config.TargetJiraCloud, BaseURL: jiraURL,
			User: "a@b.de", APIToken: "tok",
		},
	}
}

func TestRunMeldetErfolgreicheChecks(t *testing.T) {
	jiraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/myself") {
			_, _ = w.Write([]byte(`{"accountId":"acc-1","emailAddress":"a@b.de"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer jiraSrv.Close()

	mocoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer mocoSrv.Close()

	checks := Run(context.Background(), testConfig(jiraSrv.URL), jiraSrv.Client(), mocoSrv.URL)

	var jiraCheck *Check
	for i := range checks {
		if strings.Contains(checks[i].Name, "Jira") {
			jiraCheck = &checks[i]
			break
		}
	}
	if jiraCheck == nil || !jiraCheck.OK {
		t.Fatalf("Jira-Check sollte erfolgreich sein: %+v", checks)
	}
}

func TestRunMeldetFehlgeschlageneChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	checks := Run(context.Background(), testConfig(srv.URL), srv.Client(), srv.URL)
	if AllOK(checks) {
		t.Fatalf("erwarte mindestens einen fehlgeschlagenen Check: %+v", checks)
	}
}

func TestWriteChecks(t *testing.T) {
	var buf bytes.Buffer
	err := WriteChecks(&buf, []Check{
		{Name: "Jira erreichbar", OK: true, Detail: "angemeldet als a@b.de"},
		{Name: "Moco erreichbar", OK: false, Detail: "Status 401"},
	})
	if err != nil {
		t.Fatalf("WriteChecks: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Jira erreichbar") || !strings.Contains(out, "Status 401") {
		t.Fatalf("Ausgabe unvollständig:\n%s", out)
	}
}

func TestAllOK(t *testing.T) {
	if !AllOK([]Check{{OK: true}, {OK: true}}) {
		t.Fatal("AllOK sollte true sein")
	}
	if AllOK([]Check{{OK: true}, {OK: false}}) {
		t.Fatal("AllOK sollte false sein")
	}
}
