package config

import (
	"path/filepath"
	"testing"
	"time"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config.json")
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := loadFromPath("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.IdleTimeout.Duration != defaultIdleTimeout {
		t.Errorf("idle timeout = %v, want %v", cfg.IdleTimeout.Duration, defaultIdleTimeout)
	}
	if len(cfg.Repos) != 0 {
		t.Errorf("expected empty repos, got %v", cfg.Repos)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := tempPath(t)
	cfg := Config{
		Repos:       []string{"/repo/a", "/repo/b"},
		IdleTimeout: duration{45 * time.Minute},
	}
	if err := saveToPath(cfg, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadFromPath(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Repos) != 2 || got.Repos[0] != "/repo/a" || got.Repos[1] != "/repo/b" {
		t.Errorf("repos = %v, want [/repo/a /repo/b]", got.Repos)
	}
	if got.IdleTimeout.Duration != 45*time.Minute {
		t.Errorf("idle_timeout = %v, want 45m", got.IdleTimeout.Duration)
	}
}

func TestLoadMissingIdleTimeout(t *testing.T) {
	path := tempPath(t)
	cfg := Config{Repos: []string{"/repo/x"}}
	if err := saveToPath(cfg, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadFromPath(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.IdleTimeout.Duration != defaultIdleTimeout {
		t.Errorf("idle_timeout = %v, want %v", got.IdleTimeout.Duration, defaultIdleTimeout)
	}
}

func TestAddRepo_addsNewRepo(t *testing.T) {
	cfg := Config{}
	added := cfg.AddRepo("/repo/a")
	if !added {
		t.Fatal("expected AddRepo to return true for a new repo")
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0] != "/repo/a" {
		t.Errorf("repos = %v, want [/repo/a]", cfg.Repos)
	}
}

func TestAddRepo_dedup(t *testing.T) {
	cfg := Config{}
	cfg.AddRepo("/repo/a")
	added := cfg.AddRepo("/repo/a")
	if added {
		t.Fatal("expected AddRepo to return false for a duplicate repo")
	}
	if len(cfg.Repos) != 1 {
		t.Errorf("expected 1 repo after duplicate add, got %d", len(cfg.Repos))
	}
}

func TestRemoveRepo_removesExisting(t *testing.T) {
	cfg := Config{Repos: []string{"/repo/a", "/repo/b", "/repo/c"}}
	removed := cfg.RemoveRepo("/repo/b")
	if !removed {
		t.Fatal("expected RemoveRepo to return true")
	}
	if len(cfg.Repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(cfg.Repos))
	}
	for _, r := range cfg.Repos {
		if r == "/repo/b" {
			t.Error("/repo/b still present after removal")
		}
	}
}

func TestRemoveRepo_notFound(t *testing.T) {
	cfg := Config{Repos: []string{"/repo/a"}}
	removed := cfg.RemoveRepo("/repo/z")
	if removed {
		t.Fatal("expected RemoveRepo to return false for unknown repo")
	}
	if len(cfg.Repos) != 1 {
		t.Errorf("repo list mutated unexpectedly: %v", cfg.Repos)
	}
}

func TestDurationMarshalUnmarshal(t *testing.T) {
	path := tempPath(t)
	cfg := Config{IdleTimeout: duration{90 * time.Minute}}
	if err := saveToPath(cfg, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadFromPath(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.IdleTimeout.Duration != 90*time.Minute {
		t.Errorf("duration = %v, want 90m", got.IdleTimeout.Duration)
	}
}
