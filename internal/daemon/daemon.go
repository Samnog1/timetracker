package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/SaNog2/timetracker/internal/tracker"
)

const pollInterval = 60 * time.Second

func Run(ctx context.Context, repos []string, idleTimeout time.Duration) error {
	if len(repos) == 0 {
		fmt.Println("daemon: no repos configured, waiting for install")
		<-ctx.Done()
		return nil
	}

	t, err := tracker.New()
	if err != nil {
		return fmt.Errorf("daemon: init tracker: %w", err)
	}

	var wg sync.WaitGroup
	for _, repo := range repos {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			watchRepo(ctx, t, repo, idleTimeout)
		}(repo)
	}
	wg.Wait()
	return nil
}

func watchRepo(ctx context.Context, t *tracker.Tracker, repo string, idleTimeout time.Duration) {
	indexPath := filepath.Join(repo, ".git", "index")
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := checkIdle(t, repo, indexPath, idleTimeout); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: %s: %v\n", repo, err)
			}
		}
	}
}

func checkIdle(t *tracker.Tracker, repo string, indexPath string, idleTimeout time.Duration) error {
	info, err := os.Stat(indexPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", indexPath, err)
	}

	idleSince := time.Since(info.ModTime())
	if idleSince < idleTimeout {
		return nil
	}

	stopped, err := t.StopIfOpen(repo)
	if err != nil {
		return fmt.Errorf("stop session: %w", err)
	}
	if stopped {
		fmt.Printf("daemon: stopped idle session in %s (idle for %s)\n",
			filepath.Base(repo), idleSince.Round(time.Minute))
	}
	return nil
}
