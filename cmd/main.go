package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/SaNog2/timetracker/internal/config"
	"github.com/SaNog2/timetracker/internal/daemon"
	"github.com/SaNog2/timetracker/internal/jira"
	"github.com/SaNog2/timetracker/internal/platform"
	"github.com/SaNog2/timetracker/internal/tracker"
)

const usage = `Usage: timetracker <command>

Commands:
  start      Start tracking time on the current branch's Jira task
  stop       Stop all running sessions
  switch     Stop current session and start a new one (for git hooks)
  report     Show a summary of time logged per task
  upload     Review pending time and upload worklogs to Jira
  install    Install git hooks and the background daemon in the current repo
  uninstall  Remove git hooks and (if no repos left) the daemon
  daemon     Run the idle watchdog (managed by systemd/launchd — not for direct use)
`

const hookPostCheckout = `#!/bin/sh
# managed by timetracker — do not edit
[ "$3" = "1" ] && timetracker switch
`

const hookPostCommit = `#!/bin/sh
# managed by timetracker — do not edit
timetracker start
`

const hookMarker = "# managed by timetracker"

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "daemon":
		runDaemon()
		return
	case "install":
		runInstall()
		return
	case "uninstall":
		runUninstall()
		return
	}

	t, err := tracker.New()
	if err != nil {
		fatal("init tracker: %v", err)
	}

	switch os.Args[1] {
	case "start":
		err = t.Start()
	case "stop":
		err = t.Stop()
	case "switch":
		if err = t.Stop(); err == nil {
			err = t.Start()
		}
	case "report":
		err = t.Report()
	case "upload":
		err = runUpload(t)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}

	if err != nil {
		fatal("%v", err)
	}
}

func runInstall() {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		fatal("not inside a git repository: %v", err)
	}

	binaryPath, err := os.Executable()
	if err != nil {
		fatal("resolve binary path: %v", err)
	}

	if resolved, err := filepath.EvalSymlinks(binaryPath); err == nil {
		binaryPath = resolved
	}

	fmt.Printf("Installing timetracker in %s\n\n", repoRoot)

	if err := writeHook(repoRoot, "post-checkout", hookPostCheckout); err != nil {
		fatal("write post-checkout hook: %v", err)
	}
	if err := writeHook(repoRoot, "post-commit", hookPostCommit); err != nil {
		fatal("write post-commit hook: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
	}
	added := cfg.AddRepo(repoRoot)
	if err := cfg.Save(); err != nil {
		fatal("save config: %v", err)
	}
	if added {
		fmt.Printf("  registered repo: %s\n", repoRoot)
	}

	if platform.DaemonInstalled() {
		if err := restartDaemon(); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not restart daemon: %v\n", err)
		} else {
			fmt.Println("  restarted daemon to pick up new repo")
		}
	} else {
		if err := platform.InstallDaemon(binaryPath); err != nil {
			fatal("install daemon: %v", err)
		}
	}

	fmt.Println()
	fmt.Println("Done. Timetracker will now:")
	fmt.Println("  • start/switch sessions automatically on branch changes")
	fmt.Println("  • stop sessions after 30 min of git inactivity")
	fmt.Println()
	fmt.Println("Run 'timetracker start' to begin tracking now.")
}

func writeHook(repoRoot, name, content string) error {
	hookPath := filepath.Join(repoRoot, ".git", "hooks", name)

	if existing, err := os.ReadFile(hookPath); err == nil {
		if !strings.Contains(string(existing), hookMarker) {
			fmt.Printf("  warning: %s hook already exists and was not created by timetracker — skipping\n", name)
			fmt.Printf("           add the following line manually: timetracker %s\n",
				map[string]string{"post-checkout": "switch", "post-commit": "start"}[name])
			return nil
		}
	}

	if err := os.WriteFile(hookPath, []byte(content), 0o755); err != nil {
		return err
	}
	fmt.Printf("  wrote %s\n", hookPath)
	return nil
}

func runUninstall() {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		fatal("not inside a git repository: %v", err)
	}

	fmt.Printf("Uninstalling timetracker from %s\n\n", repoRoot)

	for _, name := range []string{"post-checkout", "post-commit"} {
		hookPath := filepath.Join(repoRoot, ".git", "hooks", name)
		data, err := os.ReadFile(hookPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: read %s: %v\n", name, err)
			continue
		}
		if !strings.Contains(string(data), hookMarker) {
			fmt.Printf("  skipping %s — not managed by timetracker\n", name)
			continue
		}
		if err := os.Remove(hookPath); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: remove %s: %v\n", hookPath, err)
		} else {
			fmt.Printf("  removed %s\n", hookPath)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
	}
	cfg.RemoveRepo(repoRoot)
	if err := cfg.Save(); err != nil {
		fatal("save config: %v", err)
	}
	fmt.Printf("  unregistered repo: %s\n", repoRoot)

	if len(cfg.Repos) == 0 {
		fmt.Println("  no more repos — removing daemon")
		if err := platform.UninstallDaemon(); err != nil {
			fatal("uninstall daemon: %v", err)
		}
	} else {
		if err := restartDaemon(); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not restart daemon: %v\n", err)
		} else {
			fmt.Println("  restarted daemon")
		}
	}

	fmt.Println("\nDone.")
}

func runDaemon() {
	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := daemon.Run(ctx, cfg.Repos, cfg.IdleTimeout.Duration); err != nil {
		fatal("daemon: %v", err)
	}
}

func runUpload(t *tracker.Tracker) error {
	summaries, err := t.Summarize()
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		fmt.Println("Nothing to upload — no pending closed sessions.")
		return nil
	}

	fmt.Println("\nPending time to upload to Jira:")
	fmt.Println()
	fmt.Printf("  %-12s  %s\n", "TASK", "TIME")
	fmt.Println("  " + strings.Repeat("-", 28))
	for _, ts := range summaries {
		h := int(ts.Total.Hours())
		m := int(ts.Total.Minutes()) % 60
		fmt.Printf("  %-12s  %dh%02dm\n", ts.TaskID, h, m)
	}
	fmt.Println()

	if !confirm("Upload these worklogs to Jira? [y/N] ") {
		fmt.Println("Aborted.")
		return nil
	}

	cfg, err := jira.LoadConfig()
	if err != nil {
		return err
	}

	fmt.Println("\nUploading...")
	if err := jira.UploadWorklog(cfg, summaries); err != nil {
		return err
	}

	if err := t.MarkUploaded(); err != nil {
		return fmt.Errorf("uploaded to Jira but failed to mark sessions locally: %w", err)
	}

	fmt.Println("Done.")
	return nil
}

func gitRepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func restartDaemon() error {
	switch platform.Detect() {
	case platform.KindSystemd:
		return exec.Command("systemctl", "--user", "restart", "timetracker").Run()
	case platform.KindLaunchd:
		return exec.Command("launchctl", "kickstart", "-k",
			"gui/"+fmt.Sprint(os.Getuid())+"/com.timetracker.daemon").Run()
	}
	return nil
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
