package app

import (
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

	taskID := retrieveTaskID(branchName)

	entries.Entries = append(entries.Entries, domain.TrackingSession{
		TaskID:      taskID,
		DateStarted: time.Now(),
		DateEnded:   time.Time{},
	})
	return adapters.SaveEntries(entries)
}

func retrieveTaskID(branchName string) string {
	taskId := strings.Split(branchName, "-")[1]
	return taskId
}
