package tracker

import (
	"errors"
	"time"

	"strings"

	"github.com/SaNog2/timetracker/internal/adapters"
	"github.com/SaNog2/timetracker/internal/domain"
	"github.com/SaNog2/timetracker/internal/git"
)

func Start() error {
	entries, err := adapters.LoadEntries()
	if err != nil {
		entries = domain.TrackingEntries{}
	}

	branchName, err := git.GetBranchStatus()
	if err != nil {
		return err
	}

	taskID, err := retrieveTaskID(branchName)
	if err != nil {
		return err
	}

	entries.Entries = append(entries.Entries, domain.TrackingSession{
		TaskID:      taskID,
		DateStarted: time.Now(),
		DateEnded:   time.Time{},
	})
	return adapters.SaveEntries(entries)
}

func retrieveTaskID(branchName string) (string, error) {
	parts := strings.Split(branchName, "-")
	if len(parts) < 2 {
		return "", errors.New("Unable to link to task, not logging time")
	}
	taskId := strings.Split(branchName, "-")[1]

	return taskId, nil
}
