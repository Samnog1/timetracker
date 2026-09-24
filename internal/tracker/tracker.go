// Package tracker owns local time-tracking state.
package tracker

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

type Session struct {
	ID            string        `json:"id"`
	TaskID        string        `json:"task_id"`
	Summary       string        `json:"summary,omitempty"`
	StartedAt     time.Time     `json:"started_at"`
	EndedAt       time.Time     `json:"ended_at"`
	UploadedAt    time.Time     `json:"uploaded_at,omitempty"`
	PausedAt      time.Time     `json:"paused_at,omitempty"`
	PausedFor     time.Duration `json:"paused_for,omitempty"`
	PauseReason   string        `json:"pause_reason,omitempty"`
	RepoPath      string        `json:"repo_path,omitempty"`
	UploadError   string        `json:"upload_error,omitempty"`
	UploadAttempt time.Time     `json:"upload_attempt,omitempty"`
	ConfirmToken  string        `json:"confirm_token,omitempty"`
	ConfirmingAt  time.Time     `json:"confirming_at,omitempty"`
}

func (s Session) Active() bool { return s.EndedAt.IsZero() }
func (s Session) Paused() bool { return s.Active() && !s.PausedAt.IsZero() }

func (s Session) Duration(now time.Time) time.Duration {
	end := s.EndedAt
	if end.IsZero() {
		end = now
	}
	paused := s.PausedFor
	if !s.PausedAt.IsZero() {
		paused += end.Sub(s.PausedAt)
	}
	d := end.Sub(s.StartedAt) - paused
	if d < 0 {
		return 0
	}
	return d
}

type TaskSummary struct {
	TaskID   string
	Total    time.Duration
	Uploaded time.Duration
	Sessions int
}

type Status struct {
	Active       *Session
	Elapsed      time.Duration
	PendingCount int
}

type store struct {
	path string
}

type Tracker struct {
	store store
	now   func() time.Time
}

var jiraKeyPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)

func New() (*Tracker, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("config dir: %w", err)
	}
	dir := filepath.Join(cfg, "timetracker")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return NewWithPath(filepath.Join(dir, "sessions.json")), nil
}

func NewWithPath(sessionsPath string) *Tracker {
	return &Tracker{store: store{path: sessionsPath}, now: time.Now}
}

func (t *Tracker) Start(taskID, summary string) (*Session, *Session, error) {
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	if !jiraKeyPattern.MatchString(taskID) || jiraKeyPattern.FindString(taskID) != taskID {
		return nil, nil, fmt.Errorf("invalid Jira issue key %q", taskID)
	}
	repoPath, _ := repoRoot()
	now := t.now()
	var started *Session
	var stopped *Session
	err := t.update(func(sessions *[]Session) error {
		for i := range *sessions {
			s := &(*sessions)[i]
			if !s.Active() {
				continue
			}
			if s.TaskID == taskID {
				copy := *s
				started = &copy
				return nil
			}
			finishSession(s, now)
			copy := *s
			stopped = &copy
		}

		s := Session{
			ID:        newID(),
			TaskID:    taskID,
			Summary:   strings.TrimSpace(summary),
			StartedAt: now,
			RepoPath:  repoPath,
		}
		*sessions = append(*sessions, s)
		started = &s
		return nil
	})
	return started, stopped, err
}

func (t *Tracker) StartFromBranch() (*Session, *Session, error) {
	key, err := TaskIDFromBranch()
	if err != nil {
		return nil, nil, err
	}
	return t.Start(key, "")
}

// SyncBranch switches the single active timer to the branch's Jira issue.
// A branch without a Jira key simply stops the current timer.
func (t *Tracker) SyncBranch() (*Session, *Session, error) {
	key, keyErr := TaskIDFromBranch()
	if keyErr != nil {
		stopped, err := t.Stop()
		return nil, stopped, err
	}
	return t.Start(key, "")
}

// EnsureBranch starts the branch issue only when no timer is already active.
// Post-commit hooks use this so a manually selected timer survives commits on
// branches without a Jira key.
func (t *Tracker) EnsureBranch() (*Session, error) {
	status, err := t.Status()
	if err != nil || status.Active != nil {
		return status.Active, err
	}
	key, err := TaskIDFromBranch()
	if err != nil {
		return nil, nil
	}
	started, _, err := t.Start(key, "")
	return started, err
}

func (t *Tracker) Stop() (*Session, error) {
	now := t.now()
	var stopped *Session
	err := t.update(func(sessions *[]Session) error {
		for i := range *sessions {
			s := &(*sessions)[i]
			if !s.Active() {
				continue
			}
			finishSession(s, now)
			copy := *s
			stopped = &copy
		}
		return nil
	})
	return stopped, err
}

func (t *Tracker) Pause(reason string) (*Session, error) {
	now := t.now()
	var paused *Session
	err := t.update(func(sessions *[]Session) error {
		for i := range *sessions {
			s := &(*sessions)[i]
			if !s.Active() || s.Paused() {
				continue
			}
			s.PausedAt = now
			s.PauseReason = reason
			copy := *s
			paused = &copy
		}
		return nil
	})
	return paused, err
}

func (t *Tracker) Resume() (*Session, error) {
	now := t.now()
	var resumed *Session
	err := t.update(func(sessions *[]Session) error {
		for i := range *sessions {
			s := &(*sessions)[i]
			if !s.Paused() {
				continue
			}
			s.PausedFor += now.Sub(s.PausedAt)
			s.PausedAt = time.Time{}
			s.PauseReason = ""
			copy := *s
			resumed = &copy
		}
		return nil
	})
	return resumed, err
}

func (t *Tracker) TogglePause() (*Session, error) {
	status, err := t.Status()
	if err != nil || status.Active == nil {
		return nil, err
	}
	if status.Active.Paused() {
		return t.Resume()
	}
	return t.Pause("manual")
}

func (t *Tracker) Status() (Status, error) {
	sessions, err := t.load()
	if err != nil {
		return Status{}, err
	}
	now := t.now()
	status := Status{}
	for i := range sessions {
		s := sessions[i]
		if s.Active() {
			copy := s
			status.Active = &copy
			status.Elapsed = s.Duration(now)
		} else if s.UploadedAt.IsZero() {
			status.PendingCount++
		}
	}
	return status, nil
}

func (t *Tracker) Pending() ([]Session, error) {
	sessions, err := t.load()
	if err != nil {
		return nil, err
	}
	pending := make([]Session, 0)
	for _, s := range sessions {
		if !s.Active() && s.UploadedAt.IsZero() {
			pending = append(pending, s)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].EndedAt.Before(pending[j].EndedAt) })
	return pending, nil
}

func (t *Tracker) FindPending(id string) (Session, error) {
	pending, err := t.Pending()
	if err != nil {
		return Session{}, err
	}
	for _, s := range pending {
		if s.ID == id {
			return s, nil
		}
	}
	return Session{}, fmt.Errorf("pending session %q not found", id)
}

func (t *Tracker) ClaimConfirmation(id string) (string, bool, error) {
	now := t.now()
	token := newID()
	claimed := false
	err := t.update(func(sessions *[]Session) error {
		for i := range *sessions {
			s := &(*sessions)[i]
			if s.ID != id || s.Active() || !s.UploadedAt.IsZero() {
				continue
			}
			if s.ConfirmToken != "" && now.Sub(s.ConfirmingAt) < time.Hour {
				return nil
			}
			s.ConfirmToken = token
			s.ConfirmingAt = now
			claimed = true
			return nil
		}
		return nil
	})
	return token, claimed, err
}

func (t *Tracker) FindClaimed(id, token string) (Session, error) {
	sessions, err := t.load()
	if err != nil {
		return Session{}, err
	}
	for _, s := range sessions {
		if s.ID == id && s.ConfirmToken == token && !s.Active() && s.UploadedAt.IsZero() {
			return s, nil
		}
	}
	return Session{}, fmt.Errorf("confirmation for session %q is no longer active", id)
}

func (t *Tracker) ReleaseConfirmation(id, token string) error {
	return t.update(func(sessions *[]Session) error {
		for i := range *sessions {
			s := &(*sessions)[i]
			if s.ID == id && s.ConfirmToken == token {
				s.ConfirmToken = ""
				s.ConfirmingAt = time.Time{}
				return nil
			}
		}
		return nil
	})
}

func (t *Tracker) MarkUploaded(id string, confirmToken ...string) error {
	now := t.now()
	return t.update(func(sessions *[]Session) error {
		for i := range *sessions {
			s := &(*sessions)[i]
			if s.ID == id && !s.Active() && (len(confirmToken) == 0 || s.ConfirmToken == confirmToken[0]) {
				s.UploadedAt = now
				s.UploadError = ""
				s.UploadAttempt = now
				s.ConfirmToken = ""
				s.ConfirmingAt = time.Time{}
				return nil
			}
		}
		return fmt.Errorf("session %q not found", id)
	})
}

func (t *Tracker) MarkUploadFailed(id string, uploadErr error) error {
	now := t.now()
	return t.update(func(sessions *[]Session) error {
		for i := range *sessions {
			if (*sessions)[i].ID == id {
				(*sessions)[i].UploadError = uploadErr.Error()
				(*sessions)[i].UploadAttempt = now
				(*sessions)[i].ConfirmToken = ""
				(*sessions)[i].ConfirmingAt = time.Time{}
				return nil
			}
		}
		return fmt.Errorf("session %q not found", id)
	})
}

func (t *Tracker) Report(verbose bool) error {
	sessions, err := t.load()
	if err != nil {
		return err
	}
	result := reportSummaries(sessions, t.now(), verbose)
	if len(result) == 0 {
		if len(sessions) == 0 {
			fmt.Println("No sessions recorded.")
		} else if verbose {
			fmt.Println("No tracked time recorded.")
		} else {
			fmt.Println("All recorded time has been uploaded.")
		}
		return nil
	}
	printSummaries(result)
	return nil
}

func reportSummaries(sessions []Session, now time.Time, verbose bool) []TaskSummary {
	agg := make(map[string]*TaskSummary)
	for _, s := range sessions {
		d := s.Duration(now)
		if d <= 0 {
			continue
		}
		ts := agg[s.TaskID]
		if ts == nil {
			ts = &TaskSummary{TaskID: s.TaskID}
			agg[s.TaskID] = ts
		}
		ts.Total += d
		ts.Sessions++
		if !s.UploadedAt.IsZero() {
			ts.Uploaded += d
		}
	}
	result := make([]TaskSummary, 0, len(agg))
	for _, ts := range agg {
		if !verbose && ts.Total == ts.Uploaded {
			continue
		}
		result = append(result, *ts)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TaskID < result[j].TaskID })
	return result
}

func (t *Tracker) Clear() error {
	return t.update(func(sessions *[]Session) error {
		*sessions = []Session{}
		return nil
	})
}

func TaskIDFromBranch() (string, error) {
	out, err := exec.Command("git", "branch", "--show-current").Output()
	if err != nil {
		return "", fmt.Errorf("git branch: %w", err)
	}
	return parseJiraKey(strings.TrimSpace(string(out)))
}

func ParseJiraKey(value string) (string, error) { return parseJiraKey(value) }

func parseJiraKey(value string) (string, error) {
	match := jiraKeyPattern.FindString(strings.ToUpper(value))
	if match == "" {
		return "", fmt.Errorf("%q does not contain a Jira issue key", value)
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

func finishSession(s *Session, now time.Time) {
	if !s.PausedAt.IsZero() {
		s.PausedFor += now.Sub(s.PausedAt)
		s.PausedAt = time.Time{}
		s.PauseReason = ""
	}
	s.EndedAt = now
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func (t *Tracker) update(fn func(*[]Session) error) error {
	if err := os.MkdirAll(filepath.Dir(t.store.path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	lock, err := os.OpenFile(t.store.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open state lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock state: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	sessions, err := t.loadUnlocked()
	if err != nil {
		return err
	}
	if err := fn(&sessions); err != nil {
		return err
	}
	return t.saveUnlocked(sessions)
}

func (t *Tracker) load() ([]Session, error) {
	lock, err := os.OpenFile(t.store.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open state lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_SH); err != nil {
		return nil, fmt.Errorf("lock state: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	return t.loadUnlocked()
}

func (t *Tracker) loadUnlocked() ([]Session, error) {
	data, err := os.ReadFile(t.store.path)
	if os.IsNotExist(err) {
		return []Session{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sessions: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return []Session{}, nil
	}
	var sessions []Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, fmt.Errorf("parse sessions: %w", err)
	}
	for i := range sessions {
		if sessions[i].ID == "" {
			sessions[i].ID = legacyID(sessions[i], i)
		}
	}
	return sessions, nil
}

func legacyID(session Session, index int) string {
	value := fmt.Sprintf("%d|%s|%s|%s|%s", index, session.TaskID, session.StartedAt.Format(time.RFC3339Nano), session.EndedAt.Format(time.RFC3339Nano), session.RepoPath)
	sum := sha256.Sum256([]byte(value))
	return "legacy-" + hex.EncodeToString(sum[:12])
}

func (t *Tracker) saveUnlocked(sessions []Session) error {
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sessions: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(t.store.path), ".sessions-*.json")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("protect temporary state: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := os.Rename(tmpPath, t.store.path); err != nil {
		return fmt.Errorf("replace sessions: %w", err)
	}
	return nil
}

func printSummaries(summaries []TaskSummary) {
	fmt.Printf("%-12s  %10s  %10s  %10s\n", "TASK", "TOTAL", "UPLOADED", "PENDING")
	fmt.Println(strings.Repeat("-", 52))
	for _, ts := range summaries {
		fmt.Printf("%-12s  %10s  %10s  %10s\n", ts.TaskID, FormatDuration(ts.Total), FormatDuration(ts.Uploaded), FormatDuration(ts.Total-ts.Uploaded))
	}
}

func FormatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%02dm", h, m)
}
