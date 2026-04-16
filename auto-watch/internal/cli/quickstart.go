package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# autowatch quickstart

Monitor repositories and run tasks automatically — on cron schedules or when new files appear.

## Setup

` + "```" + `bash
# 1. Initialize (global config + project registration)
cd /path/to/your/repo
autowatch init

# 2. Verify prerequisites
autowatch doctor
` + "```" + `

## Cron trigger — run tasks on a schedule

` + "```" + `bash
# Create a bash task
autowatch task create --id run-etl --bash "autoetl run"

# Create a claude task (starts a Claude Code session in a worktree)
autowatch task create --id regression-review --claude "/regression-review on commits from last 24 hours"

# Create a cron trigger — fires daily at midnight
autowatch trigger create --id daily --cron "0 0 * * *"

# Optionally skip if no new commits on a branch
autowatch trigger create --id weekly --cron "0 9 * * 1" --only-if-branch-changed main

# Link tasks to the trigger
autowatch trigger add-task --trigger daily --task run-etl
autowatch trigger add-task --trigger daily --task regression-review
` + "```" + `

Cron expressions are 5-field (minute hour dom month dow), evaluated in the host's local timezone.

## File-created trigger — react when new files appear

Fires when files matching a glob pattern are created in the project directory. Useful for
processing new docs, config files, or any file-based workflow trigger.

` + "```" + `bash
# Create a task that processes new documentation
autowatch task create --id process-doc --bash "echo 'new doc detected'"

# Create a file_created trigger watching for new markdown files
autowatch trigger create --type file_created --glob "docs/**/*.md" --id watch-docs

# Link the task
autowatch trigger add-task --trigger watch-docs --task process-doc
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
autowatch start

# Run a single tick then exit (useful for testing)
autowatch start --once

# Install as a systemd user service
autowatch daemon install
autowatch daemon restart
` + "```" + `

## Managing tasks and triggers

` + "```" + `bash
# List tasks and triggers
autowatch task list --json
autowatch trigger list --json

# Test a task immediately (foreground, no trigger needed)
autowatch task run --id run-etl

# Unlink a task from a trigger
autowatch trigger remove-task --trigger daily --task run-etl

# Remove a trigger or task
autowatch trigger remove --id daily
autowatch task remove --id run-etl
` + "```" + `

## Monitoring

` + "```" + `bash
# View recent events
autowatch logs
autowatch logs --since 24h --level error
autowatch logs --project my-project --task run-etl --json

# Daemon status and health
autowatch status
autowatch health

# Manual worktree cleanup
autowatch clean
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
      "command": "autoetl run"
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

Run ` + "`autowatch doctor`" + ` to check all prerequisites.
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
