package tracker

import (
	"time"

	"github.com/SaNog2/timetracker/internal/adapters"
)

func Stop() {

	entries, err := adapters.LoadEntries()
	if err != nil {
		panic(err)
	}

	for entry := range entries.Entries {
		if entries.Entries[entry].DateEnded.IsZero() {
			entries.Entries[entry].DateEnded = time.Now()
		}
	}

	err = adapters.SaveEntries(entries)
}
