package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/SaNog2/timetracker/internal/tracker"
)

const pollInterval = 2 * time.Second

func Run(ctx context.Context) error {
	t, err := tracker.New()
	if err != nil {
		return fmt.Errorf("daemon: init tracker: %w", err)
	}

	locked := false
	initialized := false
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	check := func() {
		current, err := lockState()
		if err != nil {
			return
		}
		if !initialized {
			initialized = true
			locked = current
			if current {
				_, _ = t.Pause("lock")
			}
			return
		}
		if current == locked {
			return
		}
		locked = current
		if current {
			if _, err := t.Pause("lock"); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: pause on lock: %v\n", err)
			}
			return
		}
		status, err := t.Status()
		if err == nil && status.Active != nil && status.Active.Paused() && status.Active.PauseReason == "lock" {
			go offerResume(t, status.Active.ID, status.Active.TaskID)
		}
	}

	check()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			check()
		}
	}
}

func lockState() (bool, error) {
	out, err := exec.Command("omarchy", "shell", "lock", "isLocked").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

func offerResume(t *tracker.Tracker, sessionID, taskID string) {
	out, err := exec.Command("notify-send", "--app-name=Timetracker", "--wait", "--action=default=Resume", "--action=resume=Resume", "--action=later=Later", "Resume "+taskID+"?", "Click to resume the timer.").Output()
	if err != nil || !isResumeAction(string(out)) {
		return
	}
	locked, err := lockState()
	if err != nil || locked {
		return
	}
	status, err := t.Status()
	if err == nil && status.Active != nil && status.Active.ID == sessionID && status.Active.Paused() && status.Active.PauseReason == "lock" {
		_, _ = t.Resume()
	}
}

func isResumeAction(action string) bool {
	action = strings.TrimSpace(action)
	return action == "default" || action == "resume"
}
