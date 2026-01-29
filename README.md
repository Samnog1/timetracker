# Timetracker

`timetracker` is a small CLI tool I built to automatically track how much time I spend on Git branches, so I don’t have to manually log time in Jira.

## Why this exists

I’m tired of logging time manually in Jira.

- It’s annoying
- It’s imprecise
- I forget to do it
- It breaks my focus

What I actually want is:
“Just work, switch branches, and later see how much time I spent on each task.”

Since my workflow already revolves around Git branches, this tool tracks time per branch and generates reports from that.

## Motivation

This project is mostly a learning exercise and a personal tool.

Things I wanted to explore:
- Go
- Building CLI tools
- Git hooks
- OS and filesystem interactions

IMPORTANT:  
This is a very small personal project. I do not plan to actively maintain it or make it production-ready.

## How it works

- Each Git branch is treated as a task
- When tracking starts, the current branch becomes the active task
- When switching branches:
  - the previous task is stopped
  - a new task starts automatically
- Sessions are stored locally on your machine
- Reports aggregate total time spent per task (branch)

This is achieved by installing a Git `post-checkout` hook in the repository you want to track.

## Important limitations

- This tool is hardcoded to how I work
- Paths, assumptions, and behavior may not fit your workflow
- No configuration system
- No guarantees

Use it at your own risk, or as inspiration.

## Installation

Build the binary:

```bash
go build -o timetracker ./cmd
```
Place it somewhere on your system, for example:

```bash
~/bin/timetracker
```

Make sure it is executable and accessible in your PATH.

## Usage 
This is currently an MVP and still evolving.

Planned commands:

```bash
timetracker install             # install git hook in current repo
timetracker start               # start tracking current branch
timetracker stop                # stop tracking
timetracker report              # show time spent per branch
timetracker upload-workload     # uploads per-task workload duration
```
There is a plan to integrate with JIRA api to automaticly upload the worklog to the current task.

Once installed, switching branches with:

```bash
git switch <branch>
git checkout <branch>
```

will automatically update the active task.

## Status

- MVP / experimental
- Not production-ready
- No backward compatibility guarantees
- No support

## License

Do whatever you want with it.
If it breaks, you get to keep both pieces.