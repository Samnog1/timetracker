package tracker

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SaNog2/timetracker/internal/domain"
	"github.com/SaNog2/timetracker/internal/git"
)

type TrackerService struct {
	storage Storage
}

type TaskTotal struct {
	TaskID    string
	Duration  time.Duration
	Sessions  int
	LastEnded time.Time
}

func NewTrackerService(storage Storage) *TrackerService {
	return &TrackerService{
		storage: storage,
	}
}

func (t *TrackerService) Start() error {
	entries, err := t.storage.LoadEntries()
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
	return t.storage.SaveEntries(entries)
}

func (t *TrackerService) Stop() error {
	entries, err := t.storage.LoadEntries()
	if err != nil {
		return err
	}

	for entry := range entries.Entries {
		if entries.Entries[entry].DateEnded.IsZero() {
			entries.Entries[entry].DateEnded = time.Now()
		}
	}

	err = t.storage.SaveEntries(entries)
	if err != nil {
		return err
	}
	return nil
}

func (t *TrackerService) Report() error {
	entries, err := t.storage.LoadEntries()
	if err != nil {
		return err
	}

	agg := make(map[string]TaskTotal, 64)

	for _, entry := range entries.Entries {
		endDate := entry.DateEnded
		if endDate.IsZero() {
			endDate = time.Now()
		}
		if endDate.Before(entry.DateStarted) {
			continue
		}
		duration := endDate.Sub(entry.DateStarted)

		t := agg[entry.TaskID]
		t.TaskID = entry.TaskID
		t.Duration += duration
		t.Sessions++
		if endDate.After(t.LastEnded) {
			t.LastEnded = endDate
		}
		agg[entry.TaskID] = t
	}
	showReport(agg)
	return nil
}

func showReport(reports map[string]TaskTotal) {
	for _, report := range reports {
		if report.Sessions > 0 {
			fmt.Println("Task ID: ", report.TaskID, "- work_duration: ", report.Duration)
		}
	}
}

func retrieveTaskID(branchName string) (string, error) {
	parts := strings.Split(branchName, "-")
	if len(parts) < 2 {
		return "", errors.New("Unable to link to task, not logging time")
	}
	taskId := strings.Split(branchName, "-")[1]

	return taskId, nil
}
