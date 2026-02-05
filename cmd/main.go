package main

import (
	"fmt"
	"os"

	"github.com/SaNog2/timetracker/internal/app/tracker"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: timetracker <start | stop | switch | report>")
		os.Exit(1)
	}
	command := os.Args[1]

	var err error

	switch command {
	case "start":
		err = tracker.Start()
	case "stop":
		tracker.Stop()

	case "switch":
		tracker.Stop()
		err = tracker.Start()
	case "report":
		tracker.Report()
	case "install":
		panic("Not implemented")
		// tracker.Install("", "")
	default:
		fmt.Println("Unknown command. Usage: timetracker <start | stop | switch | report>")
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
