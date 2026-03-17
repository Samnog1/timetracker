package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SaNog2/timetracker/internal/tracker"
)

func makeRepo(t *testing.T, indexMtime time.Time) (repoPath string, indexPath string) {
	t.Helper()
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	indexPath = filepath.Join(gitDir, "index")
	if err := os.WriteFile(indexPath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.Chtimes(indexPath, indexMtime, indexMtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return dir, indexPath
}

func makeTracker(t *testing.T) *tracker.Tracker {
	t.Helper()
	return tracker.NewWithPath(filepath.Join(t.TempDir(), "sessions.json"))
}

func TestCheckIdle_stopsIdleSession(t *testing.T) {
	idleTimeout := 30 * time.Minute
	repoPath, indexPath := makeRepo(t, time.Now().Add(-time.Hour))
	tr := makeTracker(t)
	tr.SeedOpenSession("AI-1", repoPath)

	if err := checkIdle(tr, repoPath, indexPath, idleTimeout); err != nil {
		t.Fatalf("checkIdle: %v", err)
	}

	stopped, err := tr.StopIfOpen(repoPath)
	if err != nil {
		t.Fatalf("StopIfOpen after checkIdle: %v", err)
	}
	if stopped {
		t.Error("session still open after checkIdle should have closed it")
	}
}

func TestCheckIdle_skipsActiveRepo(t *testing.T) {
	idleTimeout := 30 * time.Minute
	repoPath, indexPath := makeRepo(t, time.Now().Add(-5*time.Minute))
	tr := makeTracker(t)
	tr.SeedOpenSession("AI-1", repoPath)

	if err := checkIdle(tr, repoPath, indexPath, idleTimeout); err != nil {
		t.Fatalf("checkIdle: %v", err)
	}

	stopped, err := tr.StopIfOpen(repoPath)
	if err != nil {
		t.Fatalf("StopIfOpen: %v", err)
	}
	if !stopped {
		t.Error("expected session to still be open for active repo")
	}
}

func TestCheckIdle_missingIndex(t *testing.T) {
	tr := makeTracker(t)
	err := checkIdle(tr, "/nonexistent/repo", "/nonexistent/repo/.git/index", 30*time.Minute)
	if err != nil {
		t.Fatalf("expected nil error for missing index, got: %v", err)
	}
}
