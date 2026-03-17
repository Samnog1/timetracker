package tracker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestTracker(t *testing.T) (*Tracker, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	return NewWithPath(path), dir
}

func seed(t *testing.T, tr *Tracker, sessions []Session) {
	t.Helper()
	if err := tr.save(sessions); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func readSessions(t *testing.T, tr *Tracker) []Session {
	t.Helper()
	sessions, err := tr.load("", 0)
	if err != nil {
		t.Fatalf("readSessions: %v", err)
	}
	return sessions
}

func TestParseJiraKey(t *testing.T) {
	tests := []struct {
		branch  string
		want    string
		wantErr bool
	}{
		{"AI-123", "AI-123", false},
		{"feature/AI-456-some-desc", "AI-456", false},
		{"AI-1-fix-login", "AI-1", false},
		{"main", "", true},
		{"develop", "", true},
		{"hotfix/no-ticket", "", true},
		{"feature/123-something", "", true},
	}

	for _, tc := range tests {
		got, err := parseJiraKey(tc.branch)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseJiraKey(%q): expected error, got %q", tc.branch, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseJiraKey(%q): unexpected error: %v", tc.branch, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseJiraKey(%q) = %q, want %q", tc.branch, got, tc.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0h00m"},
		{30 * time.Minute, "0h30m"},
		{90 * time.Minute, "1h30m"},
		{25*time.Hour + 5*time.Minute, "25h05m"},
	}
	for _, tc := range tests {
		got := formatDuration(tc.d)
		if got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	tr, _ := newTestTracker(t)
	now := time.Now().Round(time.Second)

	in := []Session{
		{TaskID: "AI-1", StartedAt: now.Add(-2 * time.Hour), EndedAt: now.Add(-time.Hour)},
		{TaskID: "AI-2", StartedAt: now.Add(-30 * time.Minute)}, // open
	}
	seed(t, tr, in)

	out := readSessions(t, tr)
	if len(out) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(out))
	}
	if out[0].TaskID != "AI-1" || out[1].TaskID != "AI-2" {
		t.Errorf("wrong task IDs: %v", out)
	}
	if !out[1].EndedAt.IsZero() {
		t.Error("second session should still be open")
	}
}

func TestLoadMissingFile(t *testing.T) {
	tr, _ := newTestTracker(t)
	sessions, err := tr.load("", 0)
	if err != nil {
		t.Fatalf("load on missing file should not error, got: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected empty slice, got %v", sessions)
	}
}

func TestStopIfOpen_stopsMatchingRepo(t *testing.T) {
	tr, _ := newTestTracker(t)
	now := time.Now()

	seed(t, tr, []Session{
		{TaskID: "AI-1", StartedAt: now.Add(-time.Hour), RepoPath: "/repo/a"},
		{TaskID: "AI-2", StartedAt: now.Add(-30 * time.Minute), RepoPath: "/repo/b"},
	})

	stopped, err := tr.StopIfOpen("/repo/a")
	if err != nil {
		t.Fatalf("StopIfOpen: %v", err)
	}
	if !stopped {
		t.Fatal("expected stopped=true")
	}

	sessions := readSessions(t, tr)
	if sessions[0].EndedAt.IsZero() {
		t.Error("session for /repo/a should be stopped")
	}
	if !sessions[1].EndedAt.IsZero() {
		t.Error("session for /repo/b should still be open")
	}
}

func TestStopIfOpen_noOpenSession(t *testing.T) {
	tr, _ := newTestTracker(t)
	now := time.Now()

	seed(t, tr, []Session{
		{TaskID: "AI-1", StartedAt: now.Add(-time.Hour), EndedAt: now, RepoPath: "/repo/a"},
	})

	stopped, err := tr.StopIfOpen("/repo/a")
	if err != nil {
		t.Fatalf("StopIfOpen: %v", err)
	}
	if stopped {
		t.Error("expected stopped=false when no open sessions")
	}
}

func TestStopIfOpen_emptyFile(t *testing.T) {
	tr, _ := newTestTracker(t)

	stopped, err := tr.StopIfOpen("/repo/a")
	if err != nil {
		t.Fatalf("StopIfOpen on empty file: %v", err)
	}
	if stopped {
		t.Error("expected stopped=false on empty sessions")
	}
}

func TestMarkUploaded(t *testing.T) {
	tr, _ := newTestTracker(t)
	now := time.Now()

	seed(t, tr, []Session{
		{TaskID: "AI-1", StartedAt: now.Add(-2 * time.Hour), EndedAt: now.Add(-time.Hour)},
		{TaskID: "AI-2", StartedAt: now.Add(-3 * time.Hour), EndedAt: now.Add(-2 * time.Hour), UploadedAt: now.Add(-time.Hour)},
		{TaskID: "AI-3", StartedAt: now.Add(-30 * time.Minute)},
	})

	if err := tr.MarkUploaded(); err != nil {
		t.Fatalf("MarkUploaded: %v", err)
	}

	sessions := readSessions(t, tr)

	if sessions[0].UploadedAt.IsZero() {
		t.Error("AI-1 should have been marked as uploaded")
	}
	prev := sessions[1].UploadedAt
	if sessions[1].UploadedAt != prev {
		t.Error("AI-2 upload timestamp should not have changed")
	}
	if !sessions[2].UploadedAt.IsZero() {
		t.Error("AI-3 (open) should not have been marked as uploaded")
	}
}

func TestSummarizeSessions_basic(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		{TaskID: "AI-1", StartedAt: now.Add(-2 * time.Hour), EndedAt: now.Add(-time.Hour)},
		{TaskID: "AI-1", StartedAt: now.Add(-30 * time.Minute), EndedAt: now},
	}

	summaries := summarizeSessions(sessions)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].TaskID != "AI-1" {
		t.Errorf("task ID = %q, want AI-1", summaries[0].TaskID)
	}
	if summaries[0].Sessions != 2 {
		t.Errorf("sessions = %d, want 2", summaries[0].Sessions)
	}
	want := 90 * time.Minute
	if summaries[0].Total != want {
		t.Errorf("total = %v, want %v", summaries[0].Total, want)
	}
}

func TestSummarizeSessions_skipsOpenSessions(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		{TaskID: "AI-1", StartedAt: now.Add(-time.Hour)}, // open — no EndedAt
	}
	summaries := summarizeSessions(sessions)
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries for open session, got %d", len(summaries))
	}
}

func TestSummarizeSessions_skipsUploadedSessions(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		{
			TaskID:     "AI-1",
			StartedAt:  now.Add(-2 * time.Hour),
			EndedAt:    now.Add(-time.Hour),
			UploadedAt: now,
		},
	}
	summaries := summarizeSessions(sessions)
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries for uploaded session, got %d", len(summaries))
	}
}

func TestSummarizeSessions_skipsInvalidTimeRange(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		{TaskID: "AI-1", StartedAt: now, EndedAt: now.Add(-time.Hour)},
	}
	summaries := summarizeSessions(sessions)
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries for invalid time range, got %d", len(summaries))
	}
}

func TestSummarizeSessions_multipleTasks(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		{TaskID: "AI-1", StartedAt: now.Add(-3 * time.Hour), EndedAt: now.Add(-2 * time.Hour)},
		{TaskID: "AI-2", StartedAt: now.Add(-2 * time.Hour), EndedAt: now.Add(-time.Hour)},
		{TaskID: "AI-1", StartedAt: now.Add(-time.Hour), EndedAt: now},
	}
	summaries := summarizeSessions(sessions)
	totals := make(map[string]time.Duration)
	for _, s := range summaries {
		totals[s.TaskID] = s.Total
	}
	if totals["AI-1"] != 2*time.Hour {
		t.Errorf("AI-1 total = %v, want 2h", totals["AI-1"])
	}
	if totals["AI-2"] != time.Hour {
		t.Errorf("AI-2 total = %v, want 1h", totals["AI-2"])
	}
}

func TestIdleDeadline_activeRepo(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(gitDir, "index")
	if err := os.WriteFile(indexPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline, ok := idleDeadline(dir, 30*time.Minute)
	if ok {
		t.Errorf("expected ok=false for active repo, got deadline=%v", deadline)
	}
}

func TestIdleDeadline_idleRepo(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(gitDir, "index")
	if err := os.WriteFile(indexPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(indexPath, past, past); err != nil {
		t.Fatal(err)
	}

	deadline, ok := idleDeadline(dir, 30*time.Minute)
	if !ok {
		t.Fatal("expected ok=true for idle repo")
	}
	wantDeadline := past.Add(30 * time.Minute)
	diff := deadline.Sub(wantDeadline)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("deadline = %v, want ~%v", deadline, wantDeadline)
	}
}

func TestIdleDeadline_missingIndex(t *testing.T) {
	dir := t.TempDir() // no .git/index
	_, ok := idleDeadline(dir, 30*time.Minute)
	if ok {
		t.Error("expected ok=false when .git/index does not exist")
	}
}

func TestLoad_lazyClosesIdleSession(t *testing.T) {
	tr, dir := newTestTracker(t)

	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(gitDir, "index")
	if err := os.WriteFile(indexPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(indexPath, past, past); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	seed(t, tr, []Session{
		{TaskID: "AI-1", StartedAt: now.Add(-2 * time.Hour), RepoPath: dir},
	})

	sessions, err := tr.load(dir, 30*time.Minute)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if sessions[0].EndedAt.IsZero() {
		t.Error("expected open session to be lazy-closed")
	}
	expectedEnd := past.Add(30 * time.Minute)
	diff := sessions[0].EndedAt.Sub(expectedEnd)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("EndedAt = %v, want ~%v", sessions[0].EndedAt, expectedEnd)
	}
}

func TestLoad_doesNotCloseFreshSession(t *testing.T) {
	tr, dir := newTestTracker(t)

	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "index"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	seed(t, tr, []Session{
		{TaskID: "AI-1", StartedAt: time.Now().Add(-10 * time.Minute), RepoPath: dir},
	})

	sessions, err := tr.load(dir, 30*time.Minute)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !sessions[0].EndedAt.IsZero() {
		t.Error("active session should not have been closed")
	}
}
