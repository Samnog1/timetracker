package app

import "github.com/SaNog2/timetracker/internal/app/tracker"

type App struct {
	TrackerService *tracker.TrackerService
}

func (a *App) Run(args []string) error {

	command := args[1]

	switch command {
	case "start":
		return a.TrackerService.Start()
	case "stop":
		return a.TrackerService.Stop()
	case "switch":
		if err := a.TrackerService.Stop(); err != nil {
			return err
		}
		return a.TrackerService.Start()
	case "report":
		return a.TrackerService.Report()
	default:
		return nil
	}
}
