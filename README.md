# Auto Stack

**Intelligence layer for AI coding agents.**

Auto Stack is a set of CLI tools that capture, index, analyze, and learn from your coding agent sessions. The tools form a feedback loop: your agents code, the stack watches, and the next session is smarter than the last.

```
Code with agents  -->  autoetl  -->  autosearch  -->  autoreflect  -->  autoskill
       ^                                                                    |
       +--------------------------------------------------------------------+
                              autowatch orchestrates the loop
```

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/mistakenot/auto-stack/main/install.sh | bash
```

Custom install directory:

```bash
INSTALL_DIR=/usr/local/bin curl -fsSL https://raw.githubusercontent.com/mistakenot/auto-stack/main/install.sh | bash
```

Requires Go 1.22+.

## The Tools

| Tool | What it does |
|------|-------------|
| **autoetl** | Extracts session logs from Claude Code, Codex, and other agents. Normalizes them into partitioned Parquet datasets (messages + sessions). |
| **autosearch** | Indexes the Parquet output into SQLite FTS. Full-text search across messages and sessions with date, workspace, and role filters. |
| **autodoc** | Doc management with freshness tracking. Links code blocks to docs via `[autodoc()]` comments and detects drift. |
| **autoreflect** | Analyzes past sessions to find patterns, extract rules, and build project playbooks. |
| **autoskill** | Turns reflect outputs into reusable agent skills. |
| **autowatch** | Daemon that monitors repos and triggers ETL, search indexing, and reflection jobs on a schedule. |

## Quick Start: The Full Loop

Here's the happy path from raw coding sessions to self-improving agents.

### 1. Capture session history

After coding with Claude Code, Codex, or other agents, extract and normalize the session logs:

```bash
autoetl run
```

This reads raw session logs (default: `~/.claude/projects`), normalizes them into two Parquet datasets, and writes incremental output to `~/.auto/etl/output/`:

```
~/.auto/etl/output/
  messages/year=2026/week=15/messages.parquet
  sessions/year=2026/month=04/sessions.parquet
```

### 2. Index for search

Build a full-text search index over the normalized data:

```bash
autosearch index
```

### 3. Search your history

Find what your agents have been doing:

```bash
# Find error patterns across all sessions
autosearch search "Exit code 1" --since 1w

# Which sessions touched auth code?
autosearch search "auth middleware" --scope sessions

# Drill into a specific session
autosearch session get $SESSION_ID

# List recent sessions
autosearch session list --limit 10

# Filter by workspace
autosearch search "flaky test" --cwd /home/dev/my-project
```

All commands output JSON by default. Use `--highlight` to add bold markers to matching terms in snippets.

### 4. Reflect and improve

Use autosearch as the foundation for finding patterns. A reflection agent can:

```bash
# Find sessions with repeated failures
autosearch search "Exit code 1 AND retry" --scope sessions --since 2w

# Inspect the worst session
autosearch session get $SESSION_ID

# Search for a specific recurring mistake
autosearch search "playwright mcp" --scope messages --since 1w
```

Then feed those findings into a rule or a skill that prevents the same mistake next session.

### 5. Self-improve (automated)

The self-improve pipeline ties it all together. It uses autosearch to find problems in your coding sessions, analyzes them against your codebase, and opens PRs to fix the top issues:

```bash
prose run scripts/self-improve/index.md -- focus: "autosearch"
```

This runs a multi-agent pipeline:
1. **Preflight** -- refreshes ETL and search index
2. **Explorer** -- uses the tools as a real user, captures every friction point
3. **Analyst** -- reads the codebase to find root causes
4. **Reviewer** -- independently verifies the analysis
5. **Consolidator** -- picks the top 3 improvements by impact-to-effort ratio
6. **Implementers** -- 3 parallel agents, each on an isolated worktree, each opening a PR

The PR bodies capture the full workflow narrative (problem, plan, decisions, test results), which gets indexed by the next ETL run -- closing the feedback loop.

## Example: Finding and Fixing a Real Problem

```bash
# 1. Run ETL to capture recent sessions
autoetl run

# 2. Index them
autosearch index

# 3. Search for sessions where agents got stuck in loops
autosearch search "retry AND failed" --scope sessions --since 2w

# 4. Find the 3 worst sessions by token usage
autosearch session list --since 2w --limit 20
# (pick the ones with highest total_tokens)

# 5. Read the transcript of the worst one
autosearch session get abc123-def456

# 6. Discover the agent kept re-running a test that was timing out
#    because verbose passing-test output filled the context window

# 7. Add a rule to prevent this next time
#    (via CLAUDE.md, autoreflect, or a skill)
```

## Data Architecture

All tools share a common data format. They communicate through files, not APIs.

```
Raw session logs (Claude, Codex, etc.)
    |
    v
autoetl --> Partitioned Parquet (messages + sessions)
    |           ~/.auto/etl/output/
    v
autosearch --> SQLite FTS index
    |              ~/.auto/search/default.sqlite
    v
autoreflect --> Rules and patterns
    |
    v
autoskill --> Reusable agent skills
```

The canonical schema is defined in `auto-etl/internal/model/model.go`. Two datasets:

- **messages** -- one row per message. Includes role, content, tool name, file paths, token counts, timestamps. Content is stored inline (Parquet is columnar, so large text columns don't slow down metadata queries).
- **sessions** -- one row per session. Includes workspace, git remote, model, token totals, and full/truncated transcripts.

You can query the Parquet files directly with DuckDB:

```bash
# Most-edited files across all sessions
duckdb -c "
  SELECT tool_file_path, count(*) as edits
  FROM '~/.auto/etl/output/messages/**/*.parquet'
  WHERE tool_name = 'Write'
  GROUP BY tool_file_path
  ORDER BY edits DESC
  LIMIT 20
"

# Token usage by session
duckdb -c "
  SELECT workspace, total_tokens,
    epoch_ms(first_message_at) as started
  FROM '~/.auto/etl/output/sessions/**/*.parquet'
  WHERE is_subagent = false
  ORDER BY total_tokens DESC
  LIMIT 10
"
```

## Configuration

Global config lives in `~/.auto/`. Each tool has its own subdirectory:

```
~/.auto/
  settings.json          # shared host-level defaults
  etl/                   # autoetl settings and raw data
  search/                # autosearch indexes
  docs/                  # autodoc settings
  watch/                 # autowatch schedules
```

Project-local config lives in `.auto/` within your repo.

## Development

```bash
git clone https://github.com/mistakenot/auto-stack.git
cd auto-stack
make install-hooks   # pre-commit hooks (gofmt, go vet, beads sync)
make build           # build all binaries to ./bin/
make install         # build and install to ~/.local/bin/
make test            # run all tests
```

Each sub-project is an independent Go module under its own directory (`auto-etl/`, `auto-search/`, etc.) with its own `go.mod`. See each sub-project's `CLAUDE.md` for specific build and test instructions.

## Status

| Tool | Status | Description |
|------|--------|-------------|
| autoetl | Active | Session ETL from multiple agent tools |
| autosearch | Active | Full-text search over normalized sessions |
| autodoc | Active | Doc management with freshness tracking |
| autoskill | Active | Agent skill management |
| autowatch | Early | Repo monitoring and job scheduling |
| autoreflect | Early | Pattern extraction from session history |
| autograph | Early | Context graphs built from coding sessions |

## License

Private repository. See LICENSE for details.
