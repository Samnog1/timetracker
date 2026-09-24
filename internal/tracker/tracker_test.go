package tracker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestTracker(t *testing.T) *Tracker {
	t.Helper()
	return NewWithPath(filepath.Join(t.TempDir(), "sessions.json"))
}

func readSessions(t *testing.T, tr *Tracker) []Session {
	t.Helper()
	sessions, err := tr.load()
	if err != nil {
		t.Fatal(err)
	}
	return sessions
}

func TestParseJiraKey(t *testing.T) {
	tests := map[string]string{
		"AI-123":                            "AI-123",
		"feature/AI-456-some-desc":          "AI-456",
		"swe-609-gateway-diagnostics":       "SWE-609",
		"feature/MOBILE2-18-something-else": "MOBILE2-18",
	}
	for branch, want := range tests {
		got, err := parseJiraKey(branch)
		if err != nil || got != want {
			t.Errorf("parseJiraKey(%q) = %q, %v; want %q", branch, got, err, want)
		}
	}
	if _, err := parseJiraKey("main"); err == nil {
		t.Fatal("expected main to have no Jira key")
	}
}

func TestStartMaintainsSingleActiveSession(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.Local)
	tr.now = func() time.Time { return now }

	first, stopped, err := tr.Start("AI-1", "First task")
	if err != nil || first == nil || stopped != nil {
		t.Fatalf("first start = %#v, %#v, %v", first, stopped, err)
	}
	now = now.Add(45 * time.Minute)
	second, stopped, err := tr.Start("SWE-2", "Second task")
	if err != nil || second == nil || stopped == nil {
		t.Fatalf("second start = %#v, %#v, %v", second, stopped, err)
	}
	if stopped.TaskID != "AI-1" || stopped.Duration(stopped.EndedAt) != 45*time.Minute {
		t.Errorf("stopped session = %#v", stopped)
	}

	status, err := tr.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Active == nil || status.Active.TaskID != "SWE-2" || status.PendingCount != 1 {
		t.Errorf("status = %#v", status)
	}
}

func TestStartSameIssueIsNoOp(t *testing.T) {
	tr := newTestTracker(t)
	first, _, err := tr.Start("AI-1", "First")
	if err != nil {
		t.Fatal(err)
	}
	second, stopped, err := tr.Start("AI-1", "Replacement")
	if err != nil || stopped != nil || second.ID != first.ID {
		t.Fatalf("same start = %#v, %#v, %v", second, stopped, err)
	}
	if len(readSessions(t, tr)) != 1 {
		t.Fatal("same issue created another session")
	}
}

func TestPauseResumeExcludesPausedTime(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.Local)
	tr.now = func() time.Time { return now }
	_, _, _ = tr.Start("AI-1", "")

	now = now.Add(30 * time.Minute)
	paused, err := tr.Pause("lock")
	if err != nil || paused == nil || !paused.Paused() {
		t.Fatalf("pause = %#v, %v", paused, err)
	}
	now = now.Add(20 * time.Minute)
	resumed, err := tr.Resume()
	if err != nil || resumed == nil || resumed.Paused() {
		t.Fatalf("resume = %#v, %v", resumed, err)
	}
	now = now.Add(10 * time.Minute)
	stopped, err := tr.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stopped.Duration(stopped.EndedAt), 40*time.Minute; got != want {
		t.Errorf("duration = %v, want %v", got, want)
	}
}

func TestStopWhilePausedExcludesPause(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.Local)
	tr.now = func() time.Time { return now }
	_, _, _ = tr.Start("AI-1", "")
	now = now.Add(15 * time.Minute)
	_, _ = tr.Pause("manual")
	now = now.Add(30 * time.Minute)
	stopped, err := tr.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if got := stopped.Duration(stopped.EndedAt); got != 15*time.Minute {
		t.Errorf("duration = %v, want 15m", got)
	}
}

func TestMarkUploadedMarksOnlyRequestedSession(t *testing.T) {
	tr := newTestTracker(t)
	_, _, _ = tr.Start("AI-1", "")
	first, _ := tr.Stop()
	_, _, _ = tr.Start("AI-2", "")
	second, _ := tr.Stop()

	if err := tr.MarkUploaded(first.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := tr.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != second.ID {
		t.Errorf("pending = %#v", pending)
	}
}

func TestConfirmationCanOnlyBeClaimedOnce(t *testing.T) {
	tr := newTestTracker(t)
	_, _, _ = tr.Start("AI-1", "")
	session, _ := tr.Stop()

	token, claimed, err := tr.ClaimConfirmation(session.ID)
	if err != nil || !claimed || token == "" {
		t.Fatalf("first claim = %q, %v, %v", token, claimed, err)
	}
	_, claimed, err = tr.ClaimConfirmation(session.ID)
	if err != nil || claimed {
		t.Fatalf("second claim = %v, %v", claimed, err)
	}
	if _, err := tr.FindClaimed(session.ID, token); err != nil {
		t.Fatal(err)
	}
	if err := tr.ReleaseConfirmation(session.ID, token); err != nil {
		t.Fatal(err)
	}
	_, claimed, err = tr.ClaimConfirmation(session.ID)
	if err != nil || !claimed {
		t.Fatalf("claim after release = %v, %v", claimed, err)
	}
}

func TestUploadFailureReleasesConfirmation(t *testing.T) {
	tr := newTestTracker(t)
	_, _, _ = tr.Start("AI-1", "")
	session, _ := tr.Stop()
	_, claimed, _ := tr.ClaimConfirmation(session.ID)
	if !claimed {
		t.Fatal("expected initial claim")
	}
	if err := tr.MarkUploadFailed(session.ID, os.ErrDeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	_, claimed, err := tr.ClaimConfirmation(session.ID)
	if err != nil || !claimed {
		t.Fatalf("claim after failure = %v, %v", claimed, err)
	}
}

func TestLegacySessionGetsStableID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	data := `[{"task_id":"AI-1","started_at":"2026-09-24T09:00:00Z","ended_at":"2026-09-24T10:00:00Z"}]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := NewWithPath(path).Pending()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewWithPath(path).Pending()
	if err != nil {
		t.Fatal(err)
	}
	if first[0].ID == "" || first[0].ID != second[0].ID {
		t.Fatalf("legacy IDs differ: %q and %q", first[0].ID, second[0].ID)
	}
}

func TestConcurrentUpdatesDoNotLoseSessions(t *testing.T) {
	tr := newTestTracker(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, _ = tr.Start("AI-1", "")
		}(i)
	}
	wg.Wait()
	if sessions := readSessions(t, tr); len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
}

func TestCorruptStateIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := NewWithPath(path)
	if _, _, err := tr.Start("AI-1", ""); err == nil {
		t.Fatal("expected corrupt state error")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "not json" {
		t.Fatalf("corrupt state was overwritten: %q", data)
	}
}

func TestClearWritesEmptyArray(t *testing.T) {
	tr := newTestTracker(t)
	_, _, _ = tr.Start("AI-1", "")
	if err := tr.Clear(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tr.store.path)
	if err != nil {
		t.Fatal(err)
	}
	var sessions []Session
	if err := json.Unmarshal(data, &sessions); err != nil || len(sessions) != 0 {
		t.Fatalf("state = %s, %v", data, err)
	}
}

func TestFormatDuration(t *testing.T) {
	if got := FormatDuration(25*time.Hour + 5*time.Minute); got != "25h05m" {
		t.Fatalf("FormatDuration = %q", got)
	}
}

func TestReportSummariesHidesFullyUploadedTasks(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	sessions := []Session{
		{TaskID: "AI-1", StartedAt: now.Add(-2 * time.Hour), EndedAt: now.Add(-time.Hour), UploadedAt: now},
		{TaskID: "AI-2", StartedAt: now.Add(-30 * time.Minute), EndedAt: now},
		{TaskID: "AI-3", StartedAt: now.Add(-15 * time.Minute)},
	}

	summaries := reportSummaries(sessions, now, false)
	if len(summaries) != 2 || summaries[0].TaskID != "AI-2" || summaries[1].TaskID != "AI-3" {
		t.Fatalf("summaries = %#v", summaries)
	}
}

func TestReportSummariesVerboseIncludesFullyUploadedTasks(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	sessions := []Session{
		{TaskID: "AI-1", StartedAt: now.Add(-2 * time.Hour), EndedAt: now.Add(-time.Hour), UploadedAt: now},
		{TaskID: "AI-2", StartedAt: now.Add(-30 * time.Minute), EndedAt: now},
	}

	summaries := reportSummaries(sessions, now, true)
	if len(summaries) != 2 || summaries[0].TaskID != "AI-1" || summaries[1].TaskID != "AI-2" {
		t.Fatalf("summaries = %#v", summaries)
	}
}
