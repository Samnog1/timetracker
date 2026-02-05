package main

import (
	"fmt"
	"os"

	"github.com/SaNog2/timetracker/internal/adapters/storage"
	"github.com/SaNog2/timetracker/internal/app"
	"github.com/SaNog2/timetracker/internal/app/tracker"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: timetracker <start | stop | switch | report>")
		os.Exit(1)
	}
	app := bootstrap()
	err := app.Run(os.Args)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func bootstrap() *app.App {
	return &app.App{
		TrackerService: buildTrackerService(),
	}
}

func buildTrackerService() *tracker.TrackerService {
	storage, _ := storage.NewJSONFileStorage()
	return tracker.NewTrackerService(storage)
}
