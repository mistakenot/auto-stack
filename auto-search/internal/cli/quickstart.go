package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# autosearch quickstart

Search and inspect past coding agent sessions. Data comes from auto-etl parquet output.

## Before you start

Rebuild the index so recent sessions are searchable:

` + "```" + `bash
autosearch index
` + "```" + `

This reads parquet files from ~/.auto/etl/output and updates the local SQLite index.
Run this at the start of each session or after new ETL data lands.

## Core workflow

### 1. Search for sessions or messages

` + "```" + `bash
# Search messages (default) — finds individual messages matching your query
autosearch search "parquet EOF"

# Search sessions — finds sessions where the query appears anywhere in the transcript
autosearch search "parquet EOF" --scope sessions

# Filter by workspace and time
autosearch search "test failed" --cwd /home/vscode/src/my-project --since 7d

# Absolute window (inclusive --after, exclusive --before)
autosearch search "Exit code 0" --after 2024-03-21T08:00:00Z --before 2024-03-21T09:00:00Z

# Highlight matched terms in snippets
autosearch search "undefined symbol" --highlight

# Pagination: default page size is 20; set page size with --limit
autosearch search "undefined symbol" --limit 10

# Skip results with --offset (skip first 20)
autosearch search "undefined symbol" --offset 20

# Restrict search to specific content classes
autosearch search "contextual-commit" --field tool_input
autosearch search "Exit code 1" --field tool_output

# Filter by message role
autosearch search "database is busy" --role tool
autosearch search "undo that" --role user
` + "```" + `

Queries support AND, OR, NOT (uppercase), and quoted phrases:
` + "```" + `
autosearch search '"import cycle" OR "dependency cycle"'
autosearch search "timeout NOT config"
` + "```" + `

### 2. Read a full session transcript

` + "```" + `bash
# Get the session ID from search hits, then:
autosearch session get <session_id>
` + "```" + `

This renders the full conversation as markdown. Each message is tagged with its index,
e.g. ` + "`" + `<user index=0>` + "`" + `, ` + "`" + `<agent index=1>` + "`" + `, ` + "`" + `<tool name="Bash" index=26>` + "`" + `.

### 3. Drill into a specific message

Message IDs follow the format ` + "`" + `{sessionId}-{index}` + "`" + `. The index comes from
either a search hit (` + "`" + `messageId` + "`" + ` field) or the index shown in ` + "`" + `session get` + "`" + ` output.

` + "```" + `bash
# If session ID is abc123 and the message index is 26:
autosearch message get abc123-26

# Get metadata for a message
autosearch message describe abc123-26
` + "```" + `

### 4. Get session metadata

` + "```" + `bash
autosearch session describe <session_id>
` + "```" + `

Returns JSON with message counts, token usage, time range, workspace.

## Example: investigating a recurring bug

` + "```" + `bash
# 1. Index latest data
autosearch index

# 2. Search for the symptom across sessions
autosearch search "database is busy" --scope sessions --since 14d --highlight

# 3. Pick a high-scoring session and read the transcript
autosearch session get 6c71f534-8a37-4157-9ae2-cabe1ab541c9

# 4. Drill into a specific message from a search hit
autosearch search "database is busy" --scope messages --highlight
autosearch message get 6c71f534-8a37-4157-9ae2-cabe1ab541c9-98

# 5. Check surrounding messages for context
autosearch message get 6c71f534-8a37-4157-9ae2-cabe1ab541c9-97
autosearch message get 6c71f534-8a37-4157-9ae2-cabe1ab541c9-99
` + "```" + `

## Useful search patterns for bug hunting

These queries find common failure modes in coding agent sessions:

` + "```" + `bash
# Build failures — compilation errors, missing imports
autosearch search '"build failed" OR "does not compile" OR "undefined:"' --highlight

# Test failures — find which tests break and how often
autosearch search '"FAIL" OR "test failed" OR "panic:"' --scope sessions --highlight

# Wasted iteration — agent going in circles, retrying the same thing
autosearch search '"try again" OR "retry" OR "attempt"' --highlight

# Environment gaps — tools or commands missing from the dev container
autosearch search '"command not found" OR "not installed"' --highlight

# Agent guessing wrong — reverts, wrong approaches, undo
autosearch search '"revert" OR "undo" OR "wrong approach"' --highlight

# Flaky tests — tests that pass on retry, race conditions
autosearch search '"race" OR "flaky" OR "passes on retry"' --highlight

# Duplicate/conflict issues — name collisions, merge conflicts
autosearch search '"already exists" OR "duplicate" OR "redeclared"' --highlight

# Stuck processes — timeouts, hangs, infinite loops
autosearch search '"timeout" OR "hang" OR "stuck" OR "infinite loop"' --highlight

# Missing references — undefined symbols after code porting or refactoring
autosearch search '"not found" OR "missing" OR "undefined"' --cwd /path/to/project --highlight

# Session-level overview — broad error signal across all sessions in a project
autosearch search "error OR fail OR broken" --scope sessions --cwd /path/to/project --since 14d
` + "```" + `

Run ` + "`autosearch <command> --help`" + ` for full flag details on any command.
`

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Print an LLM-friendly guide to using autosearch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), quickstartMarkdown)
			return nil
		},
	}
}
