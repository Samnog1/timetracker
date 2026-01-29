package domain

import "time"

type TrackingSession struct {
	TaskID      string    `json:"task_id"`
	DateStarted time.Time `json:"date_started"`
	DateEnded   time.Time `json:"date_ended"`
}

type TrackingEntries struct {
	Entries []TrackingSession `json:"entries"`
}
