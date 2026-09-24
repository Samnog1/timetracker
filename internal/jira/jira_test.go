package jira

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SaNog2/timetracker/internal/tracker"
)

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseEnvFile(t *testing.T) {
	path := writeEnvFile(t, `
JIRA_URL="https://example.atlassian.net"
JIRA_EMAIL='user@example.com'
JIRA_TOKEN=secret
`)
	cfg, err := parseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://example.atlassian.net" || cfg.Email != "user@example.com" || cfg.Token != "secret" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestAssignedIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("jql"), "assignee = currentUser()") {
			t.Errorf("jql = %q", r.URL.Query().Get("jql"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issues": []any{
			map[string]any{"key": "AI-7", "fields": map[string]any{"summary": "Fix it", "status": map[string]any{"name": "In Progress"}}},
		}})
	}))
	defer server.Close()

	issues, err := AssignedIssues(Config{BaseURL: server.URL, Email: "u", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Key != "AI-7" || issues[0].Summary != "Fix it" {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestUploadSession(t *testing.T) {
	var received worklogRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("notifyUsers") != "false" {
			t.Errorf("notifyUsers = %q", r.URL.Query().Get("notifyUsers"))
		}
		if r.URL.Path != "/rest/api/3/issue/AI-7/worklog" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	started := time.Date(2026, 9, 24, 9, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	session := tracker.Session{ID: "session-123", TaskID: "AI-7", StartedAt: started, EndedAt: started.Add(45 * time.Minute)}
	if err := UploadSession(Config{BaseURL: server.URL, Email: "u", Token: "t"}, session); err != nil {
		t.Fatal(err)
	}
	if received.TimeSpentSeconds != 2700 || received.Started != "2026-09-24T09:30:00.000-0400" {
		t.Fatalf("request = %#v", received)
	}
	propertyValue, ok := received.Properties[0].Value.(map[string]any)
	if len(received.Properties) != 1 || !ok || propertyValue["sessionId"] != "session-123" {
		t.Fatalf("properties = %#v", received.Properties)
	}
}

func TestUploadSessionRoundsShortSessionToOneMinute(t *testing.T) {
	var seconds int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body worklogRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		seconds = body.TimeSpentSeconds
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	now := time.Now()
	err := UploadSession(Config{BaseURL: server.URL, Email: "u", Token: "t"}, tracker.Session{ID: "x", TaskID: "AI-1", StartedAt: now, EndedAt: now.Add(10 * time.Second)})
	if err != nil || seconds != 60 {
		t.Fatalf("seconds = %d, err = %v", seconds, err)
	}
}

func TestUploadSessionIncludesJiraError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["bad worklog"]}`))
	}))
	defer server.Close()
	now := time.Now()
	err := UploadSession(Config{BaseURL: server.URL, Email: "u", Token: "t"}, tracker.Session{ID: "x", TaskID: "AI-1", StartedAt: now, EndedAt: now.Add(time.Hour)})
	if err == nil || !strings.Contains(err.Error(), "bad worklog") {
		t.Fatalf("error = %v", err)
	}
}

func TestWorklogExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"worklogs": []any{
			map[string]any{"properties": []any{map[string]any{"key": "timetracker.session-id", "value": map[string]any{"sessionId": "session-123"}}}},
		}})
	}))
	defer server.Close()
	exists, err := WorklogExists(Config{BaseURL: server.URL, Email: "u", Token: "t"}, "AI-1", "session-123")
	if err != nil || !exists {
		t.Fatalf("exists = %v, err = %v", exists, err)
	}
}
