package tracker

import (
	"fmt"
	"time"

	"github.com/SaNog2/timetracker/internal/adapters"
)

type TaskTotal struct {
	TaskID    string
	Duration  time.Duration
	Sessions  int
	LastEnded time.Time
}

func Report() {
	entries, err := adapters.LoadEntries()
	if err != nil {
		panic(err)
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
}

func showReport(reports map[string]TaskTotal) {
	for _, report := range reports {
		if report.Sessions > 0 {
			fmt.Println("Task ID: ", report.TaskID, "- work_duration: ", report.Duration)
		}
	}
}
