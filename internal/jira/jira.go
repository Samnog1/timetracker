// Package jira handles uploading worklogs to Jira via the REST API.
// Credentials are read from a .env file (JIRA_URL, JIRA_EMAIL, JIRA_TOKEN).
package jira

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SaNog2/timetracker/internal/tracker"
)

type Config struct {
	BaseURL string
	Email   string
	Token   string
}

type worklogRequest struct {
	TimeSpentSeconds int    `json:"timeSpentSeconds"`
	Comment          string `json:"comment,omitempty"`
	Started          string `json:"started"`
}

func LoadConfig() (Config, error) {
	locations := []string{
		".env",
		filepath.Join(os.Getenv("HOME"), ".env"),
	}
	for _, loc := range locations {
		cfg, err := parseEnvFile(loc)
		if err == nil {
			return cfg, nil
		}
	}
	return Config{}, fmt.Errorf(
		"no .env file found; set JIRA_URL, JIRA_EMAIL, JIRA_TOKEN in ./.env or ~/.env",
	)
}

func parseEnvFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	vars := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vars[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}

	cfg := Config{
		BaseURL: vars["JIRA_URL"],
		Email:   vars["JIRA_EMAIL"],
		Token:   vars["JIRA_TOKEN"],
	}
	if cfg.BaseURL == "" || cfg.Email == "" || cfg.Token == "" {
		return Config{}, fmt.Errorf("JIRA_URL, JIRA_EMAIL, and JIRA_TOKEN must all be set in %s", path)
	}
	return cfg, nil
}

func UploadWorklog(cfg Config, summaries []tracker.TaskSummary) error {
	client := &http.Client{Timeout: 15 * time.Second}

	for _, ts := range summaries {
		secs := int(ts.Total.Seconds())
		if secs < 60 {
			secs = 60 // Jira requires at least 1 minute
		}

		body := worklogRequest{
			TimeSpentSeconds: secs,
			Started:          time.Now().UTC().Format("2006-01-02T15:04:05.000+0000"),
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal worklog for %s: %w", ts.TaskID, err)
		}

		url := fmt.Sprintf("%s/rest/api/3/issue/%s/worklog", strings.TrimRight(cfg.BaseURL, "/"), ts.TaskID)
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build request for %s: %w", ts.TaskID, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.SetBasicAuth(cfg.Email, cfg.Token)

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("upload %s: %w", ts.TaskID, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("upload %s: Jira returned HTTP %d", ts.TaskID, resp.StatusCode)
		}
		fmt.Printf("  uploaded %s\n", ts.TaskID)
	}
	return nil
}
