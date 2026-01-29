package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SaNog2/timetracker/internal/domain"
)

// Saves entries to .config/timetracker/sessions.json for UNIX systems
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

func LoadEntries() (domain.TrackingEntries, error) {
	sessionFile, err := sessionFilePath()
	fmt.Println("Sent to", sessionFile)
	if err != nil {
		return domain.TrackingEntries{}, err
	}

	data, err := os.ReadFile(sessionFile)
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

func SaveEntries(entries domain.TrackingEntries) error {
	sessionFile, err := sessionFilePath()
	if err != nil {
		return err
	}
	jsonData, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	err = os.WriteFile(sessionFile, jsonData, 0644)
	if err != nil {
		return err
	}
	return nil
}
