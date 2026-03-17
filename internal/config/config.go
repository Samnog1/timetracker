package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const defaultIdleTimeout = 30 * time.Minute

type Config struct {
	Repos       []string `json:"repos"`
	IdleTimeout duration `json:"idle_timeout"`
}

func Load() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}
	return loadFromPath(path)
}

// loadFromPath reads config from an explicit file path. Used by tests.
func loadFromPath(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaults(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.IdleTimeout.Duration == 0 {
		cfg.IdleTimeout.Duration = defaultIdleTimeout
	}
	return cfg, nil
}

func (c Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	return saveToPath(c, path)
}

// saveToPath writes config to an explicit file path. Used by tests.
func saveToPath(c Config, path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (c *Config) AddRepo(repoPath string) bool {
	for _, r := range c.Repos {
		if r == repoPath {
			return false
		}
	}
	c.Repos = append(c.Repos, repoPath)
	return true
}

// RemoveRepo removes repoPath from the repo list.
func (c *Config) RemoveRepo(repoPath string) bool {
	for i, r := range c.Repos {
		if r == repoPath {
			c.Repos = append(c.Repos[:i], c.Repos[i+1:]...)
			return true
		}
	}
	return false
}

// Dir returns the path to the timetracker config directory.
func Dir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config dir: %w", err)
	}
	dir := filepath.Join(cfg, "timetracker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return dir, nil
}

func configPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func defaults() Config {
	return Config{IdleTimeout: duration{defaultIdleTimeout}}
}

// duration wraps time.Duration with JSON support (stored as a string like "30m").
type duration struct{ time.Duration }

func (d duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}
