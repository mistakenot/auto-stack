package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# auto watch quickstart

Monitor repositories and run tasks automatically — on cron schedules or when new files appear.

## Setup

` + "```" + `bash
# 1. Initialize (global config + project registration)
cd /path/to/your/repo
auto watch init

# 2. Verify prerequisites
auto watch doctor
` + "```" + `

## Cron trigger — run tasks on a schedule

` + "```" + `bash
# Create a bash task
auto watch task create --id run-etl --bash "auto etl run"

# Create a claude task (starts a Claude Code session in a worktree)
auto watch task create --id regression-review --claude "/regression-review on commits from last 24 hours"

# Create a cron trigger — fires daily at midnight
auto watch trigger create --id daily --cron "0 0 * * *"

# Optionally skip if no new commits on a branch
auto watch trigger create --id weekly --cron "0 9 * * 1" --only-if-branch-changed main

# Link tasks to the trigger
auto watch trigger add-task --trigger daily --task run-etl
auto watch trigger add-task --trigger daily --task regression-review
` + "```" + `

Cron expressions are 5-field (minute hour dom month dow), evaluated in the host's local timezone.

## File-created trigger — react when new files appear

Fires when files matching a glob pattern are created in the project directory. Useful for
processing new docs, config files, or any file-based workflow trigger.

` + "```" + `bash
# Create a task that processes new documentation
auto watch task create --id process-doc --bash "echo 'new doc detected'"

# Create a file_created trigger watching for new markdown files
auto watch trigger create --type file_created --glob "docs/**/*.md" --id watch-docs

# Link the task
auto watch trigger add-task --trigger watch-docs --task process-doc
` + "```" + `

How it works:
- Checked every 60 seconds on the daemon tick loop (poll-based, not inotify)
- **First tick:** seeds a baseline snapshot of existing files — does not fire
- **Subsequent ticks:** compares current files against the snapshot; new files trigger linked tasks
- Deleted files are removed from the snapshot; re-creating a previously deleted file fires again
- Glob patterns are relative to the project root and support ` + "`" + `**` + "`" + ` for recursive matching

Example patterns:
` + "```" + `
docs/*.md          — markdown files directly in docs/
docs/**/*.md       — markdown files anywhere under docs/
*.json             — JSON files in the project root
src/**/*.go        — Go files anywhere under src/
` + "```" + `

## Starting the daemon

` + "```" + `bash
# Run continuously (blocks, outputs events to stdout)
auto watch start

# Run a single tick then exit (useful for testing)
auto watch start --once
` + "```" + `

### Run in the background (no sudo)

` + "```" + `bash
# Install a per-user systemd unit — writes ~/.config/systemd/user/autowatch.service,
# then enables + starts it. No sudo required.
auto watch daemon install

# auto update keeps the daemon current: it re-runs the installer and
# restarts an active user daemon for you.
auto update

# Roll a new binary onto a running daemon manually
auto watch daemon restart
` + "```" + `

To survive logout / start at boot, the user unit needs linger:
` + "```" + `bash
loginctl enable-linger "$USER"   # may need a one-time: sudo loginctl enable-linger "$USER"
` + "```" + `
` + "`" + `auto watch daemon install` + "`" + ` attempts this for you and warns if it could not be set.

### System-wide (headless / multi-user) — opt in

A user unit needs an active user D-Bus session (` + "`" + `XDG_RUNTIME_DIR` + "`" + `). For headless or
multi-user hosts, install a system unit instead:
` + "```" + `bash
sudo "$(command -v auto)" watch daemon install --system
` + "```" + `

## Managing tasks and triggers

` + "```" + `bash
# List tasks and triggers
auto watch task list --json
auto watch trigger list --json

# Test a task immediately (foreground, no trigger needed)
auto watch task run --id run-etl

# Unlink a task from a trigger
auto watch trigger remove-task --trigger daily --task run-etl

# Remove a trigger or task
auto watch trigger remove --id daily
auto watch task remove --id run-etl
` + "```" + `

## Monitoring

` + "```" + `bash
# View recent events
auto watch logs
auto watch logs --since 24h --level error
auto watch logs --project my-project --task run-etl --json

# Daemon status and health
auto watch status
auto watch health

# Manual worktree cleanup
auto watch clean
` + "```" + `

## Configuration files

| File | Purpose |
|------|---------|
| ` + "`" + `~/.auto/watch/settings.json` + "`" + ` | Global config — registered projects |
| ` + "`" + `.auto/watch/project.json` + "`" + ` | Per-repo config — tasks and triggers |
| ` + "`" + `~/.auto/watch/logs.sqlite` + "`" + ` | Event log and run state |

## Example project config

` + "```" + `json
{
  "id": "my-project",
  "tasks": {
    "run-etl": {
      "type": "bash",
      "command": "auto etl run"
    },
    "review-docs": {
      "type": "claude",
      "prompt": "/review new documentation for accuracy"
    }
  },
  "triggers": {
    "daily": {
      "type": "cron",
      "when": "0 0 * * *",
      "tasks": ["run-etl"]
    },
    "new-docs": {
      "type": "file_created",
      "glob": "docs/**/*.md",
      "tasks": ["review-docs"]
    }
  }
}
` + "```" + `

## Trigger types

| Type | Fires when | Key flags |
|------|-----------|-----------|
| ` + "`" + `cron` + "`" + ` | Cron schedule is due | ` + "`" + `--cron` + "`" + `, ` + "`" + `--only-if-branch-changed` + "`" + ` |
| ` + "`" + `file_created` + "`" + ` | New files match glob | ` + "`" + `--glob` + "`" + ` |

## Prerequisites

- ` + "`" + `tmux` + "`" + ` v3.0+ (execution backend)
- ` + "`" + `claude` + "`" + ` CLI on PATH (for claude tasks)
- Git 2.20+ (worktree support)

Run ` + "`auto watch doctor`" + ` to check all prerequisites.
`

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Print an LLM-friendly guide to using autowatch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), quickstartMarkdown)
			return nil
		},
	}
}
