package jira

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestParseEnvFile_valid(t *testing.T) {
	path := writeEnvFile(t, `
# comment
JIRA_URL=https://example.atlassian.net
JIRA_EMAIL=user@example.com
JIRA_TOKEN=secret123
`)
	cfg, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "https://example.atlassian.net" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Email != "user@example.com" {
		t.Errorf("Email = %q", cfg.Email)
	}
	if cfg.Token != "secret123" {
		t.Errorf("Token = %q", cfg.Token)
	}
}

func TestParseEnvFile_quotedValues(t *testing.T) {
	path := writeEnvFile(t, `
JIRA_URL="https://quoted.atlassian.net"
JIRA_EMAIL='user@quoted.com'
JIRA_TOKEN="tok"
`)
	cfg, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "https://quoted.atlassian.net" {
		t.Errorf("BaseURL = %q (quotes not stripped)", cfg.BaseURL)
	}
	if cfg.Email != "user@quoted.com" {
		t.Errorf("Email = %q (quotes not stripped)", cfg.Email)
	}
}

func TestParseEnvFile_missingKey(t *testing.T) {
	path := writeEnvFile(t, `
JIRA_URL=https://example.atlassian.net
JIRA_EMAIL=user@example.com
`)
	// JIRA_TOKEN missing — should fail
	_, err := parseEnvFile(path)
	if err == nil {
		t.Fatal("expected error for missing JIRA_TOKEN, got nil")
	}
}

func TestParseEnvFile_commentsAndBlankLines(t *testing.T) {
	path := writeEnvFile(t, `
# this is a comment

JIRA_URL=https://example.atlassian.net
# another comment
JIRA_EMAIL=user@example.com
JIRA_TOKEN=tok
`)
	cfg, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Token != "tok" {
		t.Errorf("Token = %q", cfg.Token)
	}
}

func TestParseEnvFile_missingFile(t *testing.T) {
	_, err := parseEnvFile("/nonexistent/.env")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestUploadWorklog_success(t *testing.T) {
	received := make([]worklogRequest, 0)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify path pattern
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}

		var body worklogRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		received = append(received, body)

		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	cfg := Config{
		BaseURL: ts.URL,
		Email:   "user@example.com",
		Token:   "tok",
	}
	summaries := []tracker.TaskSummary{
		{TaskID: "AI-1", Total: 2 * time.Hour, Sessions: 1},
		{TaskID: "AI-2", Total: 30 * time.Minute, Sessions: 1},
	}

	if err := UploadWorklog(cfg, summaries); err != nil {
		t.Fatalf("UploadWorklog: %v", err)
	}
	if len(received) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(received))
	}
	if received[0].TimeSpentSeconds != int((2 * time.Hour).Seconds()) {
		t.Errorf("first entry seconds = %d", received[0].TimeSpentSeconds)
	}
	if received[1].TimeSpentSeconds != int((30 * time.Minute).Seconds()) {
		t.Errorf("second entry seconds = %d", received[1].TimeSpentSeconds)
	}
}

func TestUploadWorklog_minimumOneMinute(t *testing.T) {
	var receivedSecs int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body worklogRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		receivedSecs = body.TimeSpentSeconds
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	cfg := Config{BaseURL: ts.URL, Email: "u", Token: "t"}
	summaries := []tracker.TaskSummary{
		{TaskID: "AI-1", Total: 10 * time.Second}, // less than 1 minute
	}
	if err := UploadWorklog(cfg, summaries); err != nil {
		t.Fatalf("UploadWorklog: %v", err)
	}
	if receivedSecs != 60 {
		t.Errorf("expected minimum 60 seconds, got %d", receivedSecs)
	}
}

func TestUploadWorklog_serverError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	cfg := Config{BaseURL: ts.URL, Email: "u", Token: "t"}
	summaries := []tracker.TaskSummary{
		{TaskID: "AI-1", Total: time.Hour},
	}
	err := UploadWorklog(cfg, summaries)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestUploadWorklog_noSummaries(t *testing.T) {
	cfg := Config{BaseURL: "http://unused", Email: "u", Token: "t"}
	if err := UploadWorklog(cfg, nil); err != nil {
		t.Fatalf("expected nil error for empty summaries, got: %v", err)
	}
}
