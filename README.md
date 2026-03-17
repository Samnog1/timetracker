# Timetracker

`timetracker` is a CLI tool that automatically tracks how much time I spend on Git branches and uploads it to Jira — without ever having to think about it.

## Why this exists

I'm tired of logging time manually in Jira.

- It's annoying
- It's imprecise
- I forget to do it
- It breaks my focus

What I actually want is:
"Just work, switch branches, and later see how much time I spent on each task."

Since my workflow already revolves around Git branches (one branch per Jira ticket), this tool tracks time per branch and uploads it automatically.

## Motivation

This project is mostly a learning exercise and a personal tool.

Things I wanted to explore:
- Go
- Building CLI tools
- Git hooks
- OS and filesystem interactions
- Jira REST API

IMPORTANT:
This is a very small personal project. I do not plan to actively maintain it or make it production-ready.

## How it works

- Each Git branch is treated as a Jira task. The branch must contain a Jira key in the format `AI-<number>` (e.g. `feature/AI-123-my-feature`).
- When you switch branches, the previous session is stopped and a new one starts automatically via a `post-checkout` git hook.
- When you stop working, a background daemon detects inactivity (no git activity for 30 minutes) and closes the open session automatically.
- Sessions are stored locally at `~/.config/timetracker/sessions.json`.
- When you are ready to log time, `timetracker upload` shows you a summary and asks for confirmation before pushing anything to Jira.

### Automatic stop detection

Activity is measured by the modification time of `.git/index` — git updates this file on every commit, add, checkout, and switch. If it has not been touched in 30 minutes the open session is closed.

On platforms where a background daemon cannot be installed, a heartbeat fallback is used instead: the next time any timetracker command runs, it checks the index mtime and closes any stale open sessions before proceeding.

### Branch naming

Branches must contain a Jira issue key matching `AI-<number>` anywhere in the name:

```
AI-123                        → AI-123
feature/AI-123-fix-login      → AI-123
AI-123-some-description       → AI-123
main, develop, hotfix/foo     → ignored (no session started)
```

## Installation

### 1. Build the binary

```bash
go build -o timetracker ./cmd
```

Place it somewhere on your `$PATH`, for example:

```bash
mv timetracker ~/bin/timetracker
```

### 2. Set up Jira credentials

Create a `.env` file in your project directory or in `~/.env`:

```
JIRA_URL=https://yourcompany.atlassian.net
JIRA_EMAIL=you@yourcompany.com
JIRA_TOKEN=your_api_token
```

You can generate an API token at https://id.atlassian.com/manage-profile/security/api-tokens.

### 3. Install in a repository

Run this inside the git repository you want to track:

```bash
timetracker install
```

This will:
- Write a `post-checkout` hook that calls `timetracker switch` on branch changes
- Write a `post-commit` hook that calls `timetracker start` on each commit (no-op if already tracking)
- Register the repo in `~/.config/timetracker/config.json`
- Install and start a background daemon (systemd on Linux, launchd on macOS)

If a `post-checkout` hook already exists and was not created by timetracker, it is left alone and you are shown the line to add manually.

To remove timetracker from a repository:

```bash
timetracker uninstall
```

## Usage

```
timetracker start      Start tracking the current branch's Jira task
timetracker stop       Stop all running sessions
timetracker switch     Stop current session and start a new one (called by the git hook)
timetracker report     Show a summary of time logged per task
timetracker upload     Review pending time and upload worklogs to Jira
timetracker install    Install git hooks and the background daemon in the current repo
timetracker uninstall  Remove git hooks and (if no repos left) the daemon
```

### Typical workflow

```bash
git switch AI-5424-my-feature    # post-checkout fires → session starts automatically
# ... work and commit ...        # post-commit fires   → no-op, session already open
git switch main                  # post-checkout fires → session stops, no new session (main has no AI key)
# ... stop working ...           # after 30 min idle   → daemon stops the session automatically

timetracker report               # review time per task
timetracker upload               # confirm and push to Jira
```

### Report output

```
TASK               TOTAL    UPLOADED     PENDING
----------------------------------------------------
AI-5424           14h30m      12h00m      2h30m
AI-5576            8h15m       8h15m      0h00m
```

- **TOTAL** — all time recorded for this task
- **UPLOADED** — time already sent to Jira
- **PENDING** — time not yet uploaded

### Upload confirmation

`timetracker upload` always shows a summary and requires explicit confirmation before sending anything to Jira:

```
Pending time to upload to Jira:

  TASK          TIME
  ----------------------------
  AI-5424       2h30m

Upload these worklogs to Jira? [y/N]
```

Sessions are marked as uploaded only after Jira confirms every entry. If the upload fails mid-way, nothing is marked and you can safely retry.

## Platform support

| Platform | Background daemon | Heartbeat fallback |
|---|---|---|
| Linux (systemd) | systemd user service | always active |
| macOS | launchd Launch Agent | always active |
| WSL 2 with systemd | systemd user service | always active |
| WSL 2 without systemd / WSL 1 | not installed | primary mechanism |

## Configuration

`~/.config/timetracker/config.json` is managed automatically by `install`/`uninstall`. You can edit it manually if needed:

```json
{
  "repos": [
    "/home/you/projects/my-repo"
  ],
  "idle_timeout": "30m0s"
}
```

Sessions are stored at `~/.config/timetracker/sessions.json`.

## Important limitations

- Hardcoded to the `AI-<number>` Jira key format
- Idle detection is approximate — sessions are closed at `last git activity + idle timeout`, not the exact moment you stopped
- No support for multiple Jira projects or key prefixes other than `AI`
- No guarantees

Use it at your own risk, or as inspiration.

## License

Do whatever you want with it.
If it breaks, you get to keep both pieces.
