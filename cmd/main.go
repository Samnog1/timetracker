package main

import (
	"fmt"
	"os"

	"github.com/SaNog2/timetracker/internal/app"
)

func main() {
	command := os.Args[1]

	switch command {
	case "start":
		app.Start()
	case "stop":
		app.Stop()
	case "switch":
		app.Stop()
		app.Start()
	case "report":
		app.Report()
	case "install":
		panic("Not implemented")
		// app.Install("", "")
	default:
		fmt.Println("Unknown command. Usage: timetracker <start | stop | switch | report>")
		os.Exit(1)
	}
}
