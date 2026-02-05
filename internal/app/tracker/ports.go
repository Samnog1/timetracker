package tracker

import "github.com/SaNog2/timetracker/internal/domain"

type Storage interface {
	SaveEntries(entries domain.TrackingEntries) error
	LoadEntries() (domain.TrackingEntries, error)
}
