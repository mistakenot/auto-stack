---
name: feature-miner
description: This skill should be used when the user asks to "mine sessions for feature ideas", "find usage patterns in coding sessions", "discover autodoc improvements from real usage", "run feature miner", "mine cass for ideas", "what are agents struggling with in docs", or mentions mining coding history for autodoc improvements.
version: 0.1.0
---

# Feature Miner

Mine coding agent session history (via `cass`) to discover how AI agents interact with documentation in real projects. Extract patterns, pain points, and feature ideas for improving autodoc.

## Purpose

Autodoc manages documentation for AI coding agents. Real-world session data reveals how agents actually use (or fail to use) docs — surfacing gaps, friction, and opportunities that code review alone cannot.

## Prerequisites

- `cass` CLI installed and indexed (`cass health` returns healthy)
- If index is stale, run `cass index` first

## State Tracking

Track the last run timestamp in `.autodoc/feature-miner-state.json`:

```json
{
  "last_run": "2026-03-12T00:00:00",
  "last_session_ts": 1773323229563
}
```

On first run, initialize this file. On subsequent runs, use `--since` with the `last_run` date to avoid re-scanning old sessions. After completing a mining run, update `last_run` to the current date and `last_session_ts` to the highest `created_at` value seen.

Read this state file at the start of every run. If the file exists and `last_run` is recent (within 1 day), inform the user and ask whether to re-scan or only look at new sessions.

## Mining Workflow

### Phase 1: Search for Patterns

Run the search script to gather raw results across multiple query categories:

```bash
bash ${SKILL_DIR}/scripts/mine-sessions.sh [--since YYYY-MM-DD]
```

The script runs parallel cass searches across these categories:
1. **Doc discovery** — agents trying to find, locate, or navigate documentation
2. **Doc frustration** — agents failing to find docs, outdated content, missing info
3. **Doc usage** — agents reading CLAUDE.md, AGENTS.md, doc indexes
4. **Search behavior** — keyword/semantic search attempts, grep for docs
5. **Doc maintenance** — stale docs, broken links, frontmatter issues
6. **Autodoc CLI** — direct usage of autodoc/docm commands

Output goes to `.tmp/feature-miner-results.json`.

### Phase 2: Deep Dive

For high-scoring or interesting results from Phase 1, use `cass expand` to see surrounding conversation context:

```bash
cass expand --line <LINE> --context 5 --json <SOURCE_PATH>
```

This reveals the full user intent and agent behavior around each hit — critical for understanding whether the agent succeeded or struggled.

### Phase 3: Analysis

Analyze expanded results looking for these signal categories:

| Signal | What to Look For | Autodoc Implication |
|--------|-----------------|---------------------|
| **Discovery failure** | Agent grep/glob for docs, reads wrong files | Better doc index, search, or CLAUDE.md pointers |
| **Stale content** | Agent follows outdated instructions | Better staleness detection or alerts |
| **Missing docs** | Agent asks user or guesses about undocumented areas | Suggest new doc topics |
| **Search miss** | Agent searches but gets irrelevant results | BM25 tuning, semantic search |
| **Repeated patterns** | Same doc lookup across multiple sessions | Frequently-needed docs should be more prominent |
| **Tool friction** | Difficulty running autodoc commands | CLI UX improvements |
| **Cross-project** | Same doc pattern across different workspaces | Universal feature opportunity |

### Phase 4: Report

Generate a structured report saved to `.tmp/feature-miner-report.md` with:

1. **Run metadata** — date range scanned, total sessions/hits analyzed
2. **Top findings** — ranked list of observations with evidence (session snippets)
3. **Feature ideas** — concrete suggestions with priority (high/medium/low)
4. **Pain points** — documented friction points with frequency
5. **Recommended next steps** — actionable items for the autodoc backlog

Present a summary to the user and offer to create issues or update the todo.md.

## Key cass Commands Reference

```bash
# Basic search with date filter
cass search "query" --since 2026-03-01 --limit 20 --json --max-content-length 500

# Filter to specific workspace
cass search "query" --workspace /path/to/project --limit 10 --json

# View context around a search hit
cass expand --line 42 --context 5 --json /path/to/session.jsonl

# Filter by agent type
cass search "query" --agent claude_code --limit 10 --json

# Chain searches (find sessions matching query1, then search within those for query2)
cass search "query1" --robot-format sessions | cass search "query2" --sessions-from -

# Stats overview
cass stats --json
```

## Important Notes

- Truncate content with `--max-content-length 500` to avoid context bloat
- Use `--json` for machine-readable output in all cass commands
- The `--sessions-from -` pipe pattern is powerful for narrowing results
- Check `cass health` before starting; run `cass index` if stale
- All temporary output goes to `.tmp/` (gitignored)

## Additional Resources

### Scripts
- **`scripts/mine-sessions.sh`** — Runs parallel cass searches across all query categories, outputs consolidated JSON

### Reference Files
- **`references/query-patterns.md`** — Detailed search queries organized by signal category, with rationale for each
