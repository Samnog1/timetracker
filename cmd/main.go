package main

import (
	"fmt"
	"os"

	"github.com/SaNog2/timetracker/internal/adapters/git"
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
	storage, err := storage.NewJSONFileStorage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing storage: %v\n", err)
		os.Exit(1)
	}
	gitProvider, err := git.NewLocalGitRepository()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing git provider: %v\n", err)
		os.Exit(1)
	}
	return tracker.NewTrackerService(storage, gitProvider)
}
