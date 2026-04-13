# Explorer Problems

kind: output
service: explorer

---

## Problem 1: --role flag silently accepts invalid values

**Severity:** degraded

**What happened:** Passing an invalid role value like `--role invalid` returns 0 results with no error, instead of rejecting the input. The `--field` flag correctly rejects invalid values with a clear error message. This inconsistency means a typo in `--role` (e.g., `--role tools` instead of `--role tool`) silently returns empty results, wasting the user's time debugging a non-existent data gap.

**Reproduction:**
```bash
autosearch search "test" --role invalid
# Returns: {"_meta": {..., "total_hits": 0, ...}, "hits": []}
# Expected: error like --field gives: 'invalid --role value "invalid" (use user, assistant, tool)'

autosearch search "test" --field invalid
# Returns: error: invalid --field value "invalid" (use all, content, tool_input, tool_output)
```

---

## Problem 2: --limit accepts invalid values (0 and negative numbers) silently

**Severity:** degraded

**What happened:** Passing `--limit 0` or `--limit -1` does not produce an error. Instead, both silently fall back to the default page size of 20. This is confusing because the user thinks they asked for 0 or -1 results but gets 20.

**Reproduction:**
```bash
autosearch search "test" --limit 0
# Returns 20 results, page_size: 20 in _meta

autosearch search "test" --limit -1
# Returns 20 results, page_size: 20 in _meta
```

---

## Problem 3: --mode flag silently accepts invalid values

**Severity:** minor

**What happened:** Passing `--mode invalid` does not produce an error. The search runs successfully using the default bm25 mode, but `_meta.mode` still reports `bm25`. The user gets no feedback that their requested mode was ignored.

**Reproduction:**
```bash
autosearch search "test" --mode invalid
# Returns results normally with mode: "bm25" in _meta
# Expected: error like 'invalid --mode value "invalid" (use bm25)'
```

---

## Problem 4: --cwd and --remote filters cause 30-80x performance degradation

**Severity:** degraded

**What happened:** Adding `--cwd` or `--remote` to a search query causes dramatic slowdowns. Without workspace filtering, searches complete in 60-80ms. With `--cwd`, the same query takes 2,000-6,000ms. This makes workspace-scoped searches painfully slow, despite the quickstart recommending workspace scoping as the default workflow.

**Reproduction:**
```bash
# Without --cwd: 64ms
autosearch search "test" --since 7d --limit 3
# elapsed_ms: 64

# With --cwd: 5549ms (86x slower)
autosearch search "test" --since 7d --cwd /home/vscode/src/auto-stack --limit 3
# elapsed_ms: 5549

# With --remote: 6282ms (98x slower)
autosearch search "test" --since 7d --remote https://github.com/mistakenot/auto-stack --limit 3
# elapsed_ms: 6282

# With --cwd and --role: 2172ms (33x slower)
autosearch search "test" --since 7d --role tool --cwd /home/vscode/src/auto-stack --limit 3
# elapsed_ms: 2172
```

---

## Problem 5: No upper bound on --limit flag

**Severity:** minor

**What happened:** Passing an extremely large `--limit` value (e.g., 100000) is accepted and actually returns all matching results in a single response. With the current dataset this returned 518 results, but on larger datasets this could cause memory issues or produce enormous JSON responses that overwhelm the consumer.

**Reproduction:**
```bash
autosearch search "error" --limit 100000 --since 7d
# Returns all 518 matching results in a single JSON response
# No cap or warning
```

---

## Problem 6: Empty query produces unhelpful error message

**Severity:** minor

**What happened:** Running `autosearch search ""` produces `parse query: expected term or phrase, got ""` on stderr and exits 1. The error message is technically correct but reads like an internal parser error rather than a user-facing message. It does not suggest what to do instead.

**Reproduction:**
```bash
autosearch search ""
# stderr: parse query: expected term or phrase, got ""
# exit status 1
```

---

## Problem 7: Session-scope search returns no snippets, --highlight is silently ignored

**Severity:** degraded

**What happened:** When searching with `--scope sessions`, the result hits contain no `snippet` field at all. The `--highlight` flag is accepted without error but has no effect on session-scope results. The quickstart docs show `--highlight` with session scope: `autosearch search "database is busy" --scope sessions --since 14d --highlight` but this combination produces no visible highlighting because there are no snippets.

**Reproduction:**
```bash
autosearch search "database" --scope sessions --highlight --limit 2
# Returns hits with: sessionId, score, workspace, firstMessageAt, lastMessageAt, totalMessages
# No snippet field present
# --highlight silently ignored
```

---

## Problem 8: Timestamps in JSON output are epoch milliseconds, not human-readable

**Severity:** degraded

**What happened:** All timestamps in search results and session describe output are raw epoch milliseconds (e.g., `1776023172125`). For a JSON-default CLI consumed by agents, this requires every consumer to implement ms-to-date conversion. The quickstart examples use ISO dates for `--after`/`--before` input but the output uses a different format.

**Manifestations:**
- `session describe` output: `"firstMessageAt": 1776023172125`
- Session-scope search hits: `"firstMessageAt": 1776023172125, "lastMessageAt": 1776025757779`
- `message describe` output: `"timestamp": 1775669578077`

---

## Problem 9: Missing `docs` and `doctor` commands

**Severity:** minor

**What happened:** The project CLAUDE.md specifies that most tools should support `docs` (full doc string) and `doctor` (configuration health check) commands. autosearch has neither. It has `quickstart` but not `docs`. There is no way to verify index health or configuration validity through the CLI.

**Reproduction:**
```bash
autosearch docs
# unknown command "docs" for "autosearch"

autosearch doctor
# unknown command "doctor" for "autosearch"
```

---

## Problem 10: `skills` command returns empty array despite sessions using skills

**Severity:** degraded

**What happened:** `autosearch skills` returns `{"skills": []}` even though `session describe` shows sessions with `"model": "claude-opus-4-6"` that clearly used skills (the session transcripts show contextual-commit, subagent dispatching, etc.). The skills tracking appears to not be populating from the ETL data.

**Reproduction:**
```bash
autosearch skills
# Returns: {"skills": []}

# But sessions clearly use skills:
autosearch session describe 0e160d60-06ee-4e5b-8bff-fccdc0138c9a
# Shows: "skillMessages": 0, "skillsUsed": null
# Despite the transcript containing skill invocations
```

---

## Problem 11: `total_hits`, `total_matches`, and `distinct_messages` are always identical in message-scope search

**Severity:** minor

**What happened:** In message-scope search results, the `_meta` object contains three fields that always have the same value: `total_hits`, `total_matches`, and `distinct_messages`. This suggests at least two of these fields are redundant. The quickstart docs describe `total_matches` as an "alias of total_hits for analytics workflows" confirming the redundancy.

**Reproduction:**
```bash
autosearch search "test" --since 7d --limit 3
# _meta: total_hits: 857, total_matches: 857, distinct_messages: 857
```

---

## Problem 12: Meta-search pollution in results

**Severity:** degraded

**What happened:** Searching for error patterns often returns results that are actually previous autosearch queries or quickstart documentation output rather than real errors. For example, searching for `"import cycle" OR "dependency cycle"` returns 6 hits, but all top results are from sessions where the quickstart guide was printed or where a previous agent ran the same search query. The `--role tool` filter helps but doesn't fully solve this because the quickstart output is also a tool message.

**Reproduction:**
```bash
autosearch search '"import cycle" OR "dependency cycle"' --role tool --since 14d --limit 3
# Top 3 results are all snippets from the quickstart guide being displayed in tool output,
# not actual import cycle errors in code
```

---

## Problem 13: No `session list` command

**Severity:** minor

**What happened:** There is no `session list` command to browse recent sessions. The quickstart explicitly documents this gap: "There is no `session list` command. To see recent sessions in a project, use a broad session-scope search." The workaround (`autosearch search "user" --scope sessions`) is unintuitive -- you have to search for a term you know exists in every session.

**Reproduction:**
```bash
autosearch session list
# Prints help text for session command, showing only "describe" and "get" subcommands

# Documented workaround:
autosearch search "user" --scope sessions --cwd /path/to/project --since 7d
```

---

## Problem 14: Error messages go to stderr but JSON output goes to stdout -- inconsistent for empty result sets

**Severity:** minor

**What happened:** When a search returns 0 results (e.g., querying a date range with no data, or using a nonexistent cwd path), the output is valid JSON with empty hits on stdout and exit code 0. There is no indication that the date range or workspace might not have data. A user searching `--after 2026-02-01 --before 2026-03-01` gets a clean empty result with no warning that February data does not exist in the index.

**Reproduction:**
```bash
autosearch search "test" --after 2026-02-01 --before 2026-03-01 --limit 3
# Returns: {"_meta": {..., "total_hits": 0, ...}, "hits": []}
# Exit code: 0
# No warning that February 2026 data is not in the index

autosearch search "test" --cwd /nonexistent/path --limit 3
# Returns: {"_meta": {..., "total_hits": 0, ...}, "hits": []}
# Exit code: 0
# No warning that the path doesn't match any indexed sessions
```
