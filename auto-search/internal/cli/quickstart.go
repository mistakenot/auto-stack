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
If sessions are missing from results, run ` + "`" + `autoetl run` + "`" + ` first to transform raw logs into parquet.

## Core workflow

### 1. Search for sessions or messages

Most searches should be scoped to a workspace and time window to avoid noise:

` + "```" + `bash
# Recommended: always scope to project and time range
autosearch search "test failed" --cwd /home/vscode/src/my-project --since 7d --role tool
` + "```" + `

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

Tip: use ` + "`" + `--role tool` + "`" + ` for error hunting — errors and build output live in tool messages.
Without it, results include agent commentary and prior search queries that mention the same terms.

Queries support AND, OR, NOT (uppercase), and quoted phrases:
` + "```" + `
autosearch search '"import cycle" OR "dependency cycle"'
autosearch search "timeout NOT config"
` + "```" + `

` + "```" + `bash
# search = examples; stats = grouped counts and dominant patterns
autosearch stats --scope messages --group-by session_id --query '"Exit code 1"' --role tool --since 14d --limit 10
autosearch stats --scope messages --group-by bash_command --query '"Exit code 1"' --role tool --since 14d --limit 10
` + "```" + `

### 2. Use stats to rank dominant patterns

Use ` + "`" + `stats` + "`" + ` when you need frequencies and top buckets instead of individual examples.
` + "```" + `bash
# Top sessions for a failure signature
autosearch stats --scope messages --group-by session_id --query '"Exit code 1" OR "FAIL"' --role tool --since 14d --limit 10

# Top command families (normalized)
autosearch stats --scope messages --group-by bash_command --query '"Exit code 1"' --role tool --since 14d --limit 10

# Time trend (sparse day buckets)
autosearch stats --scope messages --group-by day --query '"cannot find main module"' --role tool --since 14d
` + "```" + `

### 3. Read a full session transcript

` + "```" + `bash
# Get the session ID from search hits, then:
autosearch session get <session_id>
` + "```" + `

This renders the full conversation as markdown. Each message is tagged with its index,
e.g. ` + "`" + `<user index=0>` + "`" + `, ` + "`" + `<agent index=1>` + "`" + `, ` + "`" + `<tool name="Bash" index=26>` + "`" + `.

### 4. Drill into a specific message

Message IDs follow the format ` + "`" + `{sessionId}-{index}` + "`" + `. The index comes from
either a search hit (` + "`" + `messageId` + "`" + ` field) or the index shown in ` + "`" + `session get` + "`" + ` output.

` + "```" + `bash
# If session ID is abc123 and the message index is 26:
autosearch message get abc123-26

# Get metadata for a message
autosearch message describe abc123-26
` + "```" + `

### 5. List and filter sessions

` + "```" + `bash
# List recent sessions
autosearch session list

# Filter by workspace
autosearch session list --cwd /home/vscode/src/my-project --since 7d

# Show only sub-agent sessions, sorted by duration
autosearch session list --subagent --sort-by duration

# Show only parent (non-sub-agent) sessions
autosearch session list --no-subagent

# Find long-running sessions (useful for diagnosing stuck agents)
autosearch session list --min-duration 10m --sort-by duration

# Combine: find long autonomous sub-agents — the "spinning agent" smell
autosearch session list --subagent --min-duration 10m --sort-by duration

# Sort by token usage to find expensive sessions
autosearch session list --sort-by tokens --limit 10

# Find sessions that burned a lot of tokens (spinning agents)
autosearch session list --min-tokens 1000000 --sort-by tokens

# Find sessions with many messages (long conversations)
autosearch session list --min-messages 200 --sort-by messages

# Find sessions with bash errors (non-zero exit codes)
autosearch session list --min-errors 3 --sort-by errors

# Drill into a specific parent session's sub-agents
autosearch session list --parent-session <parent-session-id>

# Combined: find expensive, error-prone sub-agents
autosearch session list --subagent --min-errors 5 --sort-by errors --min-duration 5m
` + "```" + `

Output includes ` + "`" + `duration_ms` + "`" + `, ` + "`" + `is_subagent` + "`" + `, ` + "`" + `parent_session_id` + "`" + `, ` + "`" + `subagent_name` + "`" + `,
` + "`" + `message_count` + "`" + `, and ` + "`" + `error_count` + "`" + ` for each session.

### 6. Get session metadata

` + "```" + `bash
autosearch session describe <session_id>
` + "```" + `

Returns JSON with message counts (including ` + "`" + `userMessages` + "`" + ` for interactivity analysis),
token usage, ` + "`" + `durationMs` + "`" + `, sub-agent relationship fields, and a transcript summary.

### 7. List skills used across sessions

` + "```" + `bash
autosearch skills

# Filter by time window or workspace
autosearch skills --since 7d
autosearch skills --cwd /home/vscode/src/my-project
autosearch skills --after 2026-01-01 --before 2026-02-01
` + "```" + `

Returns JSON with each skill name, usage count, and distinct session count.

### 8. Analyze skill adoption patterns with stats

` + "```" + `bash
# Rank skills by usage
autosearch stats --scope messages --group-by skill_name --since 30d

# Time trend for a specific skill
autosearch stats --scope messages --group-by day --skill contextual-commit --since 14d

# Which workspaces use skills most
autosearch stats --scope messages --group-by workspace --skill release
` + "```" + `

### 9. Find files that change together (co-change)

Given a file, find the other files that historically change with it — a fast
onboarding heuristic and a refactor-safety signal. Reads git parquet produced by
` + "`" + `autoetl run --only git` + "`" + `; resolves the repo from the input path's git toplevel.

` + "```" + `bash
# What else tends to change when I touch this file?
autosearch co-change auto-etl/internal/git/extract.go

# Limit results and disable time decay
autosearch co-change internal/cli/root.go --limit 10 --no-decay

# Override the decay constant (units m|h|d|w)
autosearch co-change internal/cli/root.go --decay-tau 26w

# Select the repo explicitly (no origin remote, or multiple matches)
autosearch co-change path/to/file.go --repo-id <repo-id>
` + "```" + `

Output is JSON: a ` + "`" + `metadata` + "`" + ` header (the input file's history, authors,
sessions, renames) and a ` + "`" + `related_files` + "`" + ` list ranked by coupling score, each
with ` + "`" + `co_commits` + "`" + `, ` + "`" + `confidence_a_to_b` + "`" + `, ` + "`" + `lift` + "`" + `, and ` + "`" + `top_sessions` + "`" + ` you can
pivot into ` + "`" + `autosearch session get` + "`" + `.

## Understanding search output

Search results are JSON with two top-level keys: ` + "`" + `_meta` + "`" + ` and ` + "`" + `hits` + "`" + `.

` + "```" + `json
{
  "_meta": {
    "scope": "messages",        // "messages" or "sessions"
    "query": "exit code 1",     // your query as parsed
    "total_hits": 42,           // total matches across the index
    "total_matches": 42,        // alias of total_hits for analytics workflows
    "distinct_sessions": 7,     // unique session IDs matched
    "distinct_messages": 42,    // unique message IDs across matched scope
    "returned_hits": 20,        // hits in this page
    "page_size": 20,            // current --limit value
    "offset": 0,                // current --offset value
    "has_more": true,           // more pages available
    "next_offset": 20,          // use as --offset for next page
    "is_capped": false,         // true when totals are lower bounds
    "wildcard_fallback": false  // true if prefix expansion was applied
  },
  "hits": [
    {
      "sessionId": "abc123-...",  // session this hit belongs to
      "messageId": "abc123-...-26", // full message ID (message scope only)
      "messageType": "tool",      // user, assistant, or tool
      "score": -5.23,            // BM25 relevance (see below)
      "snippet": "Exit code 1..."  // matched text with context
    }
  ]
}
` + "```" + `

**Scores:** BM25 scores are negative floats from SQLite FTS5. More negative = more relevant.
A score of ` + "`" + `-17.0` + "`" + ` is a much stronger match than ` + "`" + `-0.001` + "`" + `. Results are already sorted
by relevance, so you can usually just read them top-to-bottom.

**Browsing sessions:** Use ` + "`" + `session list` + "`" + ` for structured filtering and sorting:

` + "```" + `bash
autosearch session list --cwd /path/to/project --since 7d
autosearch session list --subagent --min-duration 10m --sort-by duration
` + "```" + `

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

These queries find common failure modes in coding agent sessions.
Use ` + "`" + `--role tool` + "`" + ` to target actual errors (not agent commentary about errors):

` + "```" + `bash
# Build failures — compilation errors, missing imports
autosearch search '"build failed" OR "does not compile" OR "undefined:"' --role tool --highlight

# Test failures — find which tests break and how often
autosearch search '"FAIL" OR "test failed" OR "panic:"' --role tool --scope sessions --highlight

# Wasted iteration — agent going in circles, retrying the same thing
autosearch search '"try again" OR "let me fix" OR "retry"' --role assistant --highlight

# Environment gaps — tools or commands missing from the dev container
autosearch search '"command not found" OR "not installed"' --role tool --highlight

# User corrections — the user redirecting the agent after drift
autosearch search '"no," OR "wrong" OR "undo" OR "not what I"' --role user --highlight

# Flaky tests — tests that pass on retry, race conditions
autosearch search '"race" OR "flaky" OR "passes on retry"' --role tool --highlight

# Duplicate/conflict issues — name collisions, merge conflicts
autosearch search '"already exists" OR "duplicate" OR "redeclared"' --role tool --highlight

# Stuck processes — timeouts, hangs, infinite loops
autosearch search '"timeout" OR "hang" OR "stuck" OR "infinite loop"' --role tool --highlight

# Edit tool failures — stale reads, replace mismatches
autosearch search '"File has not been read yet" OR "modified since read" OR "String to replace not found"' --role tool --highlight

# Session-level overview — broad error signal across all sessions in a project
autosearch search "error OR fail OR broken" --scope sessions --cwd /path/to/project --since 14d
` + "```" + `

## Session list flags

` + "```" + `
--subagent         show only sub-agent sessions
--no-subagent      show only parent (non-sub-agent) sessions
--parent-session   filter by parent session ID (exact match)
--min-duration     minimum session duration: 10m, 1h, 5d, 1w
--min-tokens       minimum total tokens (e.g. 1000000)
--min-messages     minimum message count
--min-errors       minimum bash error count (non-zero exit codes)
--sort-by          recency (default), duration, tokens, messages, errors
--cwd            filter by workspace path (mutually exclusive with --remote)
--remote         filter by git remote URL
--since          relative time: 5m, 7d, 2w
--after          absolute lower bound (ISO 8601, inclusive)
--before         absolute upper bound (ISO 8601, exclusive)
--limit          max results per page (default 50)
--offset         skip N results for pagination
--index          named index to query (default: "default")
--request-id     echo an ID back in _meta
` + "```" + `

## All search flags

` + "```" + `
--scope       messages (default) or sessions
--role        user, assistant, or tool
--field       all (default), content, tool_input, tool_output
--cwd         filter by workspace path (mutually exclusive with --remote)
--remote      filter by git remote URL
--since       relative time: 5m, 7d, 2w
--after       absolute lower bound (ISO 8601, inclusive)
--before      absolute upper bound (ISO 8601, exclusive)
--limit       max results per page (default 20)
--offset      skip N results for pagination
--highlight   bold matched terms in snippets (message scope only)
--skill       filter by skill name
--mode        bm25 (default)
--index       named index to query (default: "default")
--request-id  echo an ID back in _meta
` + "```" + `

## Key stats flags

` + "```" + `
--scope       messages (default) or sessions
--group-by    required grouping key (for example: session_id, bash_command, role, day)
--query       optional FTS prefilter before grouping
--measure     count (default), distinct_sessions, distinct_messages
--min-count   minimum threshold for selected measure
--limit       max buckets per page (default 20)
--offset      skip N buckets for pagination
--role        role filter
--field       all (default), content, tool_input, tool_output
--cwd         filter by workspace path (mutually exclusive with --remote)
--remote      filter by git remote URL
--since       relative time: 5m, 7d, 2w
--after       absolute lower bound (ISO 8601, inclusive)
--before      absolute upper bound (ISO 8601, exclusive)
--skill       filter by skill name
--index       named index to query (default: "default")
--request-id  echo an ID back in _meta
` + "```" + `

## Related tools

- ` + "`" + `autoetl run` + "`" + ` — transforms raw session logs into the parquet files autosearch indexes.
  If sessions are missing, run this first.
- ` + "`" + `autoreflect` + "`" + ` — analyzes patterns found via autosearch to generate improvement rules.
- ` + "`" + `autoskill` + "`" + ` — manages agent skills, which can be created from autoreflect insights.

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
