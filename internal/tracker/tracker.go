// Package tracker handles all time-tracking logic: reading the current git
// branch, persisting sessions to disk, and aggregating them for reporting.
package tracker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Session struct {
	TaskID     string    `json:"task_id"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	UploadedAt time.Time `json:"uploaded_at,omitempty"`
	RepoPath   string    `json:"repo_path,omitempty"`
}

type TaskSummary struct {
	TaskID   string
	Total    time.Duration
	Uploaded time.Duration
	Sessions int
}

type store struct {
	path string
}

type Tracker struct {
	store store
}

var jiraKeyPattern = regexp.MustCompile(`\bAI-\d+\b`)

func New() (*Tracker, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("config dir: %w", err)
	}
	dir := filepath.Join(cfg, "timetracker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return &Tracker{store: store{path: filepath.Join(dir, "sessions.json")}}, nil
}

// NewWithPath creates a Tracker that reads/writes from a custom sessions file.
// Exported so that tests in other packages can create isolated trackers.
func NewWithPath(sessionsPath string) *Tracker {
	return &Tracker{store: store{path: sessionsPath}}
}

// SeedOpenSession appends an open session directly to disk without going
// through Start() (which requires a real git repo). Intended for tests only.
func (t *Tracker) SeedOpenSession(taskID, repoPath string) {
	sessions, _ := t.load("", 0)
	sessions = append(sessions, Session{
		TaskID:    taskID,
		StartedAt: time.Now().Add(-2 * time.Hour),
		RepoPath:  repoPath,
	})
	_ = t.save(sessions)
}

func (t *Tracker) Start() error {
	taskID, err := taskIDFromBranch()
	if err != nil {
		return err
	}

	repoPath, err := repoRoot()
	if err != nil {
		return err
	}

	sessions, _ := t.load(repoPath, 0)

	for _, s := range sessions {
		if s.TaskID == taskID && s.EndedAt.IsZero() {
			return nil
		}
	}

	sessions = append(sessions, Session{
		TaskID:    taskID,
		StartedAt: time.Now(),
		RepoPath:  repoPath,
	})
	if err := t.save(sessions); err != nil {
		return err
	}
	fmt.Printf("Started tracking %s\n", taskID)
	return nil
}

func (t *Tracker) Stop() error {
	repoPath, err := repoRoot()
	if err != nil {
		return err
	}

	sessions, err := t.load(repoPath, 0)
	if err != nil {
		return err
	}

	now := time.Now()
	stopped := 0
	for i := range sessions {
		if sessions[i].EndedAt.IsZero() {
			sessions[i].EndedAt = now
			stopped++
		}
	}
	if err := t.save(sessions); err != nil {
		return err
	}
	if stopped > 0 {
		fmt.Printf("Stopped %d session(s)\n", stopped)
	}
	return nil
}

func (t *Tracker) StopIfOpen(repoPath string) (bool, error) {
	sessions, err := t.load(repoPath, 0)
	if err != nil {
		return false, err
	}

	now := time.Now()
	stopped := 0
	for i := range sessions {
		if sessions[i].RepoPath == repoPath && sessions[i].EndedAt.IsZero() {
			sessions[i].EndedAt = now
			stopped++
		}
	}
	if stopped == 0 {
		return false, nil
	}
	return true, t.save(sessions)
}

func (t *Tracker) Report() error {
	repoPath, _ := repoRoot()

	idleTimeout := defaultIdleTimeout()
	sessions, err := t.load(repoPath, idleTimeout)
	if err != nil {
		return err
	}

	agg := make(map[string]*TaskSummary)
	for _, s := range sessions {
		end := s.EndedAt
		if end.IsZero() {
			end = time.Now()
		}
		if end.Before(s.StartedAt) {
			continue
		}
		ts, ok := agg[s.TaskID]
		if !ok {
			ts = &TaskSummary{TaskID: s.TaskID}
			agg[s.TaskID] = ts
		}
		ts.Total += end.Sub(s.StartedAt)
		ts.Sessions++
		if !s.UploadedAt.IsZero() {
			ts.Uploaded += end.Sub(s.StartedAt)
		}
	}

	result := make([]TaskSummary, 0, len(agg))
	for _, ts := range agg {
		result = append(result, *ts)
	}
	printSummaries(result)
	return nil
}

func (t *Tracker) Summarize() ([]TaskSummary, error) {
	repoPath, _ := repoRoot()

	idleTimeout := defaultIdleTimeout()
	sessions, err := t.load(repoPath, idleTimeout)
	if err != nil {
		return nil, err
	}
	return summarizeSessions(sessions), nil
}

// summarizeSessions is the pure aggregation used by Summarize and tests.
func summarizeSessions(sessions []Session) []TaskSummary {
	agg := make(map[string]*TaskSummary)
	for _, s := range sessions {
		if !s.UploadedAt.IsZero() {
			continue
		}
		if s.EndedAt.IsZero() {
			continue
		}
		if s.EndedAt.Before(s.StartedAt) {
			continue
		}
		ts, ok := agg[s.TaskID]
		if !ok {
			ts = &TaskSummary{TaskID: s.TaskID}
			agg[s.TaskID] = ts
		}
		ts.Total += s.EndedAt.Sub(s.StartedAt)
		ts.Sessions++
	}

	result := make([]TaskSummary, 0, len(agg))
	for _, ts := range agg {
		result = append(result, *ts)
	}
	return result
}

func (t *Tracker) MarkUploaded() error {
	sessions, err := t.load("", 0)
	if err != nil {
		return err
	}
	now := time.Now()
	for i := range sessions {
		if sessions[i].UploadedAt.IsZero() && !sessions[i].EndedAt.IsZero() {
			sessions[i].UploadedAt = now
		}
	}
	return t.save(sessions)
}

func printSummaries(summaries []TaskSummary) {
	if len(summaries) == 0 {
		fmt.Println("No sessions recorded.")
		return
	}
	fmt.Printf("%-12s  %10s  %10s  %10s\n", "TASK", "TOTAL", "UPLOADED", "PENDING")
	fmt.Println(strings.Repeat("-", 52))
	for _, ts := range summaries {
		pending := ts.Total - ts.Uploaded
		fmt.Printf("%-12s  %10s  %10s  %10s\n",
			ts.TaskID,
			formatDuration(ts.Total),
			formatDuration(ts.Uploaded),
			formatDuration(pending),
		)
	}
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%02dm", h, m)
}

func taskIDFromBranch() (string, error) {
	out, err := exec.Command("git", "branch", "--show-current").Output()
	if err != nil {
		return "", fmt.Errorf("git branch: %w", err)
	}
	branch := strings.TrimSpace(string(out))
	return parseJiraKey(branch)
}

// parseJiraKey extracts the first AI-<number> key from a branch name.
// Exported for tests.
func parseJiraKey(branch string) (string, error) {
	match := jiraKeyPattern.FindString(branch)
	if match == "" {
		return "", fmt.Errorf("branch %q does not contain a Jira key (expected AI-<number>)", branch)
	}
	return match, nil
}

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (t *Tracker) load(repoPath string, idleTimeout time.Duration) ([]Session, error) {
	data, err := os.ReadFile(t.store.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sessions: %w", err)
	}

	var sessions []Session

	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, fmt.Errorf("parse sessions: %w", err)
	}

	if idleTimeout > 0 {
		dirty := false
		for i := range sessions {
			if !sessions[i].EndedAt.IsZero() {
				continue
			}
			repo := sessions[i].RepoPath
			if repo == "" {
				repo = repoPath
			}
			if repo == "" {
				continue
			}
			deadline, ok := idleDeadline(repo, idleTimeout)
			if ok {
				sessions[i].EndedAt = deadline
				dirty = true
			}
		}
		if dirty {
			_ = t.save(sessions)
		}
	}

	return sessions, nil
}

func idleDeadline(repoPath string, idleTimeout time.Duration) (time.Time, bool) {
	indexPath := filepath.Join(repoPath, ".git", "index")
	info, err := os.Stat(indexPath)
	if err != nil {
		return time.Time{}, false
	}
	deadline := info.ModTime().Add(idleTimeout)
	if time.Now().Before(deadline) {
		return time.Time{}, false // still within the idle window
	}
	return deadline, true
}

func (t *Tracker) save(sessions []Session) error {
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sessions: %w", err)
	}
	if err := os.WriteFile(t.store.path, data, 0o644); err != nil {
		return fmt.Errorf("write sessions: %w", err)
	}
	return nil
}

func defaultIdleTimeout() time.Duration {
	return 30 * time.Minute
}
