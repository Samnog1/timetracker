package storage

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/SaNog2/timetracker/internal/domain"
)

type JSONFileStorage struct {
	path string
}

func NewJSONFileStorage() (*JSONFileStorage, error) {
	path, err := sessionFilePath()
	if err != nil {
		return nil, err
	}
	return &JSONFileStorage{path: path}, nil
}

func sessionFilePath() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfg, "timetracker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions.json"), nil
}

func (f *JSONFileStorage) LoadEntries() (domain.TrackingEntries, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return domain.TrackingEntries{}, err
	}

	var entries domain.TrackingEntries

	err = json.Unmarshal(data, &entries)
	if err != nil {
		return domain.TrackingEntries{}, err
	}

	return entries, nil
}

func (f *JSONFileStorage) SaveEntries(entries domain.TrackingEntries) error {
	jsonData, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	err = os.WriteFile(f.path, jsonData, 0644)
	if err != nil {
		return err
	}
	return nil
}
