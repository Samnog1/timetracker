package main

import (
	"bytes"
	"context"
	"encoding/json"
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

const usage = `Usage: timetracker <command> [arguments]

Commands:
  start [ISSUE]  Start the branch issue, an explicit issue, or the issue picker
  pick           Pick an assigned Jira issue with fzf
  sync           Switch the timer to the current branch issue (for git hooks)
  stop           Stop the active timer and ask before uploading
  pause          Pause the active timer
  resume         Resume the active timer
  toggle         Toggle pause/resume
  status [--bar] Show current timer state
  report [--verbose]
                 Show pending time, or all recorded time with --verbose
  upload         Ask before uploading each pending session
  install        Install git hooks and the background daemon
  uninstall      Remove git hooks and, when unused, the daemon
  daemon         Watch Omarchy lock state (managed by systemd/launchd)
`

const hookPostCheckout = `#!/bin/sh
# managed by timetracker - do not edit
[ "$3" = "1" ] && timetracker sync >/dev/null 2>&1 || true
`

const hookPostCommit = `#!/bin/sh
# managed by timetracker - do not edit
timetracker ensure >/dev/null 2>&1 || true
`

const hookMarker = "# managed by timetracker"

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	command := os.Args[1]
	if command == "daemon" {
		runDaemon()
		return
	}
	if command == "install" {
		runInstall()
		return
	}
	if command == "uninstall" {
		runUninstall()
		return
	}

	t, err := tracker.New()
	if err != nil {
		fatal("init tracker: %v", err)
	}

	switch command {
	case "start":
		err = runStart(t, os.Args[2:])
	case "pick":
		err = runPick(t)
	case "sync", "switch":
		err = runSync(t)
	case "ensure":
		_, err = t.EnsureBranch()
	case "stop":
		err = runStop(t)
	case "pause":
		err = runPause(t)
	case "resume":
		err = runResume(t)
	case "toggle":
		_, err = t.TogglePause()
	case "status":
		err = runStatus(t, len(os.Args) > 2 && os.Args[2] == "--bar")
	case "report":
		err = runReport(t, os.Args[2:])
	case "upload":
		err = queuePendingConfirmations(t)
	case "confirm-upload":
		if len(os.Args) != 4 {
			err = fmt.Errorf("confirm-upload requires a session ID and claim token")
		} else {
			err = confirmUpload(t, os.Args[2], os.Args[3])
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		os.Exit(1)
	}

	if err != nil {
		fatal("%v", err)
	}
}

func runReport(t *tracker.Tracker, args []string) error {
	verbose := false
	for _, arg := range args {
		if arg != "--verbose" {
			return fmt.Errorf("unknown report option %q", arg)
		}
		verbose = true
	}
	return t.Report(verbose)
}

func runStart(t *tracker.Tracker, args []string) error {
	var taskID string
	if len(args) > 0 {
		var err error
		taskID, err = tracker.ParseJiraKey(args[0])
		if err != nil {
			return err
		}
	} else if branchTask, err := tracker.TaskIDFromBranch(); err == nil {
		taskID = branchTask
	} else {
		return runPick(t)
	}

	started, stopped, err := t.Start(taskID, "")
	if err != nil {
		return err
	}
	if stopped != nil {
		queueConfirmation(t, stopped.ID)
	}
	if started != nil {
		fmt.Printf("Tracking %s\n", started.TaskID)
	}
	return nil
}

func runPick(t *tracker.Tracker) error {
	cfg, err := jira.LoadConfig()
	if err != nil {
		return err
	}
	issues, err := jira.AssignedIssues(cfg)
	if err != nil {
		return err
	}
	if len(issues) == 0 {
		return fmt.Errorf("no unresolved Jira issues are assigned to you")
	}

	var input strings.Builder
	for _, issue := range issues {
		fmt.Fprintf(&input, "%s\t%s\t%s\n", issue.Key, issue.Status, strings.ReplaceAll(issue.Summary, "\n", " "))
	}
	cmd := exec.Command("fzf", "--delimiter=\t", "--with-nth=1,2,3", "--prompt=Jira issue > ", "--height=100%", "--border")
	cmd.Stdin = strings.NewReader(input.String())
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 130 {
			return nil
		}
		return fmt.Errorf("pick Jira issue: %w", err)
	}
	fields := strings.SplitN(strings.TrimSpace(string(out)), "\t", 3)
	if len(fields) == 0 || fields[0] == "" {
		return nil
	}
	summary := ""
	if len(fields) == 3 {
		summary = fields[2]
	}
	started, stopped, err := t.Start(fields[0], summary)
	if err != nil {
		return err
	}
	if stopped != nil {
		queueConfirmation(t, stopped.ID)
	}
	if started != nil {
		fmt.Printf("Tracking %s: %s\n", started.TaskID, started.Summary)
	}
	return nil
}

func runSync(t *tracker.Tracker) error {
	started, stopped, err := t.SyncBranch()
	if err != nil {
		return err
	}
	if stopped != nil {
		queueConfirmation(t, stopped.ID)
	}
	if started != nil {
		fmt.Printf("Tracking %s\n", started.TaskID)
	}
	return nil
}

func runStop(t *tracker.Tracker) error {
	stopped, err := t.Stop()
	if err != nil {
		return err
	}
	if stopped == nil {
		fmt.Println("No active timer.")
		return nil
	}
	fmt.Printf("Stopped %s after %s\n", stopped.TaskID, tracker.FormatDuration(stopped.Duration(stopped.EndedAt)))
	queueConfirmation(t, stopped.ID)
	return nil
}

func runPause(t *tracker.Tracker) error {
	s, err := t.Pause("manual")
	if err != nil {
		return err
	}
	if s == nil {
		fmt.Println("No running timer to pause.")
	} else {
		fmt.Printf("Paused %s\n", s.TaskID)
	}
	return nil
}

func runResume(t *tracker.Tracker) error {
	s, err := t.Resume()
	if err != nil {
		return err
	}
	if s == nil {
		fmt.Println("No paused timer to resume.")
	} else {
		fmt.Printf("Resumed %s\n", s.TaskID)
	}
	return nil
}

func runStatus(t *tracker.Tracker, bar bool) error {
	status, err := t.Status()
	if err != nil {
		return err
	}
	if !bar {
		if status.Active == nil {
			fmt.Println("No active timer.")
		} else {
			state := "running"
			if status.Active.Paused() {
				state = "paused"
			}
			fmt.Printf("%s %s (%s)\n", status.Active.TaskID, tracker.FormatDuration(status.Elapsed), state)
		}
		fmt.Printf("Pending uploads: %d\n", status.PendingCount)
		return nil
	}

	output := map[string]any{"text": "No task", "tooltip": "Click to choose a Jira issue", "class": ""}
	if status.Active != nil {
		text := fmt.Sprintf("%s %s", status.Active.TaskID, shortDuration(status.Elapsed))
		state := "Timer running"
		if status.Active.Paused() {
			text = status.Active.TaskID + " PAUSED"
			state = "Timer paused"
		}
		tooltip := state
		if status.Active.Summary != "" {
			tooltip += "\n" + status.Active.Summary
		}
		output = map[string]any{"text": text, "tooltip": tooltip, "class": "active"}
	}
	if status.PendingCount > 0 {
		output["text"] = fmt.Sprintf("%s !%d", output["text"], status.PendingCount)
		output["tooltip"] = fmt.Sprintf("%s\n%d worklog(s) awaiting confirmation", output["tooltip"], status.PendingCount)
	}
	return json.NewEncoder(os.Stdout).Encode(output)
}

func shortDuration(d interface{ Minutes() float64 }) string {
	total := int(d.Minutes())
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

func queuePendingConfirmations(t *tracker.Tracker) error {
	pending, err := t.Pending()
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		fmt.Println("No worklogs are awaiting confirmation.")
		return nil
	}
	for _, session := range pending {
		queueConfirmation(t, session.ID)
	}
	fmt.Printf("Asked for confirmation on %d pending worklog(s).\n", len(pending))
	return nil
}

func queueConfirmation(t *tracker.Tracker, sessionID string) {
	token, claimed, err := t.ClaimConfirmation(sessionID)
	if err != nil || !claimed {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		_ = t.ReleaseConfirmation(sessionID, token)
		return
	}
	cmd := exec.Command(executable, "confirm-upload", sessionID, token)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	} else {
		_ = t.ReleaseConfirmation(sessionID, token)
	}
}

func confirmUpload(t *tracker.Tracker, sessionID, token string) error {
	session, err := t.FindClaimed(sessionID, token)
	if err != nil {
		return nil
	}
	duration := tracker.FormatDuration(session.Duration(session.EndedAt))
	message := fmt.Sprintf("Upload %s to %s?", duration, session.TaskID)
	if session.Summary != "" {
		message += "\n" + session.Summary
	}
	cmd := exec.Command("notify-send", "--app-name=Timetracker", "--wait", "--action=default=Upload", "--action=upload=Upload", "--action=later=Later", "Log time to Jira", message)
	out, err := cmd.Output()
	if err != nil || !isUploadAction(string(out)) {
		_ = t.ReleaseConfirmation(sessionID, token)
		return nil
	}
	cfg, err := jira.LoadConfig()
	if err == nil {
		var exists bool
		exists, err = jira.WorklogExists(cfg, session.TaskID, session.ID)
		if err == nil && exists {
			err = t.MarkUploaded(session.ID, token)
			if err == nil {
				notify("normal", "Time already logged", fmt.Sprintf("Found the existing Jira worklog for %s.", session.TaskID))
			}
			return err
		}
	}
	if err == nil {
		err = jira.UploadSession(cfg, session)
	}
	if err != nil {
		_ = t.MarkUploadFailed(session.ID, err)
		notify("critical", "Jira worklog failed", fmt.Sprintf("%s remains pending.\n%s", session.TaskID, err))
		return err
	}
	if err := t.MarkUploaded(session.ID, token); err != nil {
		return err
	}
	notify("normal", "Time logged", fmt.Sprintf("Uploaded %s to %s.", duration, session.TaskID))
	return nil
}

func isUploadAction(action string) bool {
	action = strings.TrimSpace(action)
	return action == "default" || action == "upload"
}

func notify(urgency, title, body string) {
	_ = exec.Command("notify-send", "--app-name=Timetracker", "--urgency="+urgency, title, body).Run()
}

func runDaemon() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if err := daemon.Run(ctx); err != nil {
		fatal("daemon: %v", err)
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
	if resolved, resolveErr := filepath.EvalSymlinks(binaryPath); resolveErr == nil {
		binaryPath = resolved
	}
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
	cfg.AddRepo(repoRoot)
	if err := cfg.Save(); err != nil {
		fatal("save config: %v", err)
	}
	if err := platform.InstallDaemon(binaryPath); err != nil {
		fatal("install daemon: %v", err)
	}
	fmt.Println("Installed Git hooks and lock watcher.")
}

func writeHook(repoRoot, name, content string) error {
	hooksPath, err := gitHooksPath(repoRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(hooksPath, 0o755); err != nil {
		return err
	}
	hookPath := filepath.Join(hooksPath, name)
	if existing, readErr := os.ReadFile(hookPath); readErr == nil && !strings.Contains(string(existing), hookMarker) {
		return fmt.Errorf("%s already exists and is not managed by timetracker", hookPath)
	}
	return os.WriteFile(hookPath, []byte(content), 0o755)
}

func runUninstall() {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		fatal("not inside a git repository: %v", err)
	}
	hooksPath, _ := gitHooksPath(repoRoot)
	for _, name := range []string{"post-checkout", "post-commit"} {
		path := filepath.Join(hooksPath, name)
		data, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(data), hookMarker) {
			_ = os.Remove(path)
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
	if len(cfg.Repos) == 0 {
		if err := platform.UninstallDaemon(); err != nil {
			fatal("uninstall daemon: %v", err)
		}
	} else {
		_ = restartDaemon()
	}
	fmt.Println("Uninstalled timetracker from this repository.")
}

func gitRepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	return strings.TrimSpace(string(out)), err
}

func gitHooksPath(repoRoot string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-path", "hooks")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, path)
	}
	return filepath.Clean(path), nil
}

func restartDaemon() error {
	switch platform.Detect() {
	case platform.KindSystemd:
		return exec.Command("systemctl", "--user", "restart", "timetracker").Run()
	case platform.KindLaunchd:
		return exec.Command("launchctl", "kickstart", "-k", "gui/"+fmt.Sprint(os.Getuid())+"/com.timetracker.daemon").Run()
	}
	return nil
}

func fatal(format string, args ...any) {
	var buffer bytes.Buffer
	fmt.Fprintf(&buffer, format, args...)
	fmt.Fprintln(os.Stderr, "error: "+buffer.String())
	os.Exit(1)
}
