# Timetracker

`timetracker` tracks one Jira issue at a time, shows the timer in the Omarchy bar, and asks before every Jira worklog upload.

## Workflow

- A branch containing a Jira key starts that issue through the installed Git hooks.
- Any Jira project key is accepted, for example `AI-6390` or `SWE-609`.
- When no branch key is available, `timetracker pick` shows unresolved issues assigned to you in `fzf`.
- Starting another issue closes the previous session.
- Stopping shows a desktop notification with **Upload** and **Later** actions.
- Jira is called only after **Upload** is selected.
- Choosing **Later**, dismissing the notification, or encountering a Jira error keeps the session pending locally.
- Locking Omarchy pauses the timer. Unlocking offers to resume it.

## Omarchy bar

The command module runs `timetracker status --bar` once per second.

- Left click opens the Jira issue picker.
- Middle click pauses or resumes the timer.
- Right click stops the timer and asks whether to upload.
- `!N` means `N` worklogs are waiting for confirmation.

## Commands

```text
timetracker start [ISSUE]  Start the branch issue, an explicit issue, or the picker
timetracker pick           Pick an assigned Jira issue
timetracker sync           Match the timer to the current branch
timetracker stop           Stop and ask before uploading
timetracker pause          Pause the timer
timetracker resume         Resume the timer
timetracker toggle         Toggle pause/resume
timetracker status [--bar] Show the current timer
timetracker report         Show issues with unuploaded time
timetracker report --verbose
                           Show all recorded issues, including fully uploaded ones
timetracker upload         Ask about all pending worklogs again
timetracker install        Install Git hooks and the lock watcher
timetracker uninstall      Remove integration from the current repository
```

## Jira credentials

Set the following environment variables or put them in `~/.config/timetracker/.env`, the current repository's `.env`, or `~/.env`:

```text
JIRA_URL=https://yourcompany.atlassian.net
JIRA_EMAIL=you@yourcompany.com
JIRA_TOKEN=your_api_token
```

The repository `.env` fallback keeps existing installations working. A user-level file is more reliable for the background service and desktop launcher.

## Local state

Sessions are stored in `~/.config/timetracker/sessions.json`. Writes use a file lock and atomic replacement so Git hooks, the bar, and the daemon cannot overwrite each other's changes.

A stopped session stays pending until Jira accepts it. Each Jira worklog carries the local session ID as a worklog property for diagnostics.

Jira's minimum worklog duration is one minute. Sessions shorter than one minute are submitted as one minute after confirmation.

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o bin/timetracker ./cmd
```
