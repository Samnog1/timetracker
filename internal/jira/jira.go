// Package jira reads assigned issues and uploads confirmed worklogs.
package jira

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

type Issue struct {
	Key     string
	Summary string
	Status  string
}

type worklogRequest struct {
	TimeSpentSeconds int               `json:"timeSpentSeconds"`
	Started          string            `json:"started"`
	Properties       []worklogProperty `json:"properties,omitempty"`
}

type worklogProperty struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func LoadConfig() (Config, error) {
	if cfg, ok := configFromEnvironment(); ok {
		return cfg, nil
	}
	locations := []string{}
	if configDir, err := os.UserConfigDir(); err == nil {
		locations = append(locations, filepath.Join(configDir, "timetracker", ".env"))
	}
	locations = append(locations, ".env", filepath.Join(os.Getenv("HOME"), ".env"))
	if executable, err := os.Executable(); err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
			locations = append(locations, filepath.Join(filepath.Dir(filepath.Dir(resolved)), ".env"))
		}
	}
	for _, loc := range locations {
		if cfg, err := parseEnvFile(loc); err == nil {
			return cfg, nil
		}
	}
	return Config{}, fmt.Errorf("Jira credentials not found; set JIRA_URL, JIRA_EMAIL, and JIRA_TOKEN")
}

func configFromEnvironment() (Config, bool) {
	cfg := Config{BaseURL: os.Getenv("JIRA_URL"), Email: os.Getenv("JIRA_EMAIL"), Token: os.Getenv("JIRA_TOKEN")}
	return cfg, cfg.BaseURL != "" && cfg.Email != "" && cfg.Token != ""
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
		if ok {
			vars[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	cfg := Config{BaseURL: vars["JIRA_URL"], Email: vars["JIRA_EMAIL"], Token: vars["JIRA_TOKEN"]}
	if cfg.BaseURL == "" || cfg.Email == "" || cfg.Token == "" {
		return Config{}, fmt.Errorf("JIRA_URL, JIRA_EMAIL, and JIRA_TOKEN must all be set in %s", path)
	}
	return cfg, scanner.Err()
}

func AssignedIssues(cfg Config) ([]Issue, error) {
	jql := `assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC`
	query := url.Values{
		"jql":        {jql},
		"fields":     {"summary,status"},
		"maxResults": {"100"},
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/rest/api/3/search/jql?" + query.Encode()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setHeaders(req, cfg)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("search assigned Jira issues: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, responseError("search Jira issues", resp)
	}
	var result struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name string `json:"name"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Jira issues: %w", err)
	}
	issues := make([]Issue, 0, len(result.Issues))
	for _, item := range result.Issues {
		issues = append(issues, Issue{Key: item.Key, Summary: item.Fields.Summary, Status: item.Fields.Status.Name})
	}
	return issues, nil
}

func UploadSession(cfg Config, session tracker.Session) error {
	seconds := int(session.Duration(session.EndedAt).Seconds())
	if seconds < 60 {
		seconds = 60
	}
	body := worklogRequest{
		TimeSpentSeconds: seconds,
		Started:          session.StartedAt.Format("2006-01-02T15:04:05.000-0700"),
		Properties:       []worklogProperty{{Key: "timetracker.session-id", Value: map[string]string{"sessionId": session.ID}}},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal worklog for %s: %w", session.TaskID, err)
	}
	endpoint := fmt.Sprintf("%s/rest/api/3/issue/%s/worklog?notifyUsers=false", strings.TrimRight(cfg.BaseURL, "/"), url.PathEscape(session.TaskID))
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request for %s: %w", session.TaskID, err)
	}
	setHeaders(req, cfg)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("upload %s: %w", session.TaskID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return responseError("upload "+session.TaskID, resp)
	}
	return nil
}

func WorklogExists(cfg Config, taskID, sessionID string) (bool, error) {
	endpoint := fmt.Sprintf("%s/rest/api/3/issue/%s/worklog?maxResults=5000&expand=properties", strings.TrimRight(cfg.BaseURL, "/"), url.PathEscape(taskID))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	setHeaders(req, cfg)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return false, fmt.Errorf("check existing worklog for %s: %w", taskID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, responseError("check existing worklog for "+taskID, resp)
	}
	var result struct {
		Worklogs []struct {
			Properties []worklogProperty `json:"properties"`
		} `json:"worklogs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decode worklogs for %s: %w", taskID, err)
	}
	for _, worklog := range result.Worklogs {
		for _, property := range worklog.Properties {
			value, ok := property.Value.(map[string]any)
			if property.Key == "timetracker.session-id" && ok && value["sessionId"] == sessionID {
				return true, nil
			}
		}
	}
	return false, nil
}

func setHeaders(req *http.Request, cfg Config) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(cfg.Email, cfg.Token)
}

func responseError(action string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return fmt.Errorf("%s: Jira returned HTTP %d", action, resp.StatusCode)
	}
	return fmt.Errorf("%s: Jira returned HTTP %d: %s", action, resp.StatusCode, detail)
}
