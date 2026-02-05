package main

import (
	"fmt"
	"os"

	"github.com/SaNog2/timetracker/internal/app/tracker"
)

func main() {
	command := os.Args[1]

	switch command {
	case "start":
		tracker.Start()
	case "stop":
		tracker.Stop()
	case "switch":
		tracker.Stop()
		tracker.Start()
	case "report":
		tracker.Report()
	case "install":
		panic("Not implemented")
		// tracker.Install("", "")
	default:
		fmt.Println("Unknown command. Usage: timetracker <start | stop | switch | report>")
		os.Exit(1)
	}
}
