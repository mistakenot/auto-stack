# Autosearch Improvement Suggestions

kind: output
service: analyst

---

## Summary Table

| # | Suggestion | Problems | Impact | Effort | Ratio |
|---|-----------|----------|--------|--------|-------|
| 1 | Add input validation for `--role`, `--mode`, and `--limit` flags | P1, P2, P3, P5 | High | Small | Best |
| 2 | Fix `--cwd`/`--remote` performance: reduce query count and optimize FTS+filter pattern | P4 | High | Medium | High |
| 3 | Add `session list` command | P13 | Medium | Small | High |
| 4 | Improve empty-query and empty-result error messages | P6, P14 | Medium | Small | High |
| 5 | Add ISO 8601 timestamps alongside epoch milliseconds in JSON output | P8 | Medium | Small | Good |
| 6 | Add snippet support for session-scope search and fix `--highlight` passthrough | P7 | Medium | Medium | Good |
| 7 | Remove redundant `total_matches`/`distinct_messages` from message-scope meta | P11 | Low | Small | Good |
| 8 | Add `docs` and `doctor` commands | P9 | Low | Medium | Moderate |
| 9 | Fix skills extraction in ETL pipeline | P10 | Medium | Large | Moderate |
| 10 | Address meta-search pollution with self-referential result filtering | P12 | Low | Large | Low |

---

## Suggestion 1: Add input validation for `--role`, `--mode`, and `--limit` flags

**Problems:** P1 (--role silently accepts invalid values), P2 (--limit accepts 0 and negatives), P3 (--mode silently accepts invalid values), P5 (no upper bound on --limit)

**Root cause:** The `--field` flag has proper validation via `normalizeField()` in `auto-search/internal/search/messages.go:342-353`, but `--role` and `--mode` have no equivalent validation functions. The `--limit` normalization at `messages.go:332-339` treats `<= 0` as "use default" instead of erroring. The `--mode` flag is declared in `auto-search/internal/cli/search.go:104` but never read or validated -- it is not passed to `SearchMessages` or `SearchSessions` at all.

**Changes needed:**

1. **`auto-search/internal/search/messages.go`** -- Add a `normalizeRole()` function analogous to `normalizeField()`:
   ```go
   func normalizeRole(role string) (string, error) {
       if role == "" { return "", nil }
       normalized := strings.ToLower(strings.TrimSpace(role))
       switch normalized {
       case "user", "assistant", "tool":
           return normalized, nil
       default:
           return "", fmt.Errorf("invalid --role value %q (use user, assistant, tool)", role)
       }
   }
   ```
   Call it in both `SearchMessages()` (line 91) and `SearchSessions()` (line 48).

2. **`auto-search/internal/search/messages.go:332-339`** -- Change `normalizePagination` to reject `pageSize` of 0 or negative:
   ```go
   func normalizePagination(offset, pageSize int) (int, int, error) {
       if offset < 0 { return 0, 0, errors.New("--offset must be >= 0") }
       if pageSize < 0 { return 0, 0, errors.New("--limit must be >= 0") }
       if pageSize == 0 { pageSize = defaultPageSize }
       const maxPageSize = 1000
       if pageSize > maxPageSize { return 0, 0, fmt.Errorf("--limit must be <= %d", maxPageSize) }
       return offset, pageSize, nil
   }
   ```
   Note: `--limit 0` currently means "use default" from the CLI declaration (`IntVar` with default 0), so treating 0 as "default" is acceptable. The fix is specifically for negative values (error) and adding an upper bound (cap at 1000).

3. **`auto-search/internal/cli/search.go`** -- Either validate `--mode` before passing to search functions, or remove the flag entirely since only `bm25` is supported. Simplest: validate in the `RunE`:
   ```go
   if mode != "bm25" {
       return &ExitError{Code: 1, Err: fmt.Errorf("invalid --mode value %q (use bm25)", mode)}
   }
   ```

**Effort:** Small -- three validation functions, all following the existing `normalizeField` pattern.

---

## Suggestion 2: Fix `--cwd`/`--remote` performance

**Problems:** P4 (30-80x performance degradation with workspace filters)

**Root cause:** In `auto-search/internal/search/messages.go:173-295`, the `execMessageSearch` function runs **4 separate SQL queries** against the FTS table, each joining `messages_fts` with `messages` and applying the workspace filter:
- Line 220-223: `COUNT(*)` for total hits
- Line 226-229: `COUNT(DISTINCT m.session_id)` for distinct sessions
- Line 232-235: `COUNT(DISTINCT m.message_id)` for distinct messages
- Line 238-251: The actual paginated result query

Each query re-evaluates the full FTS MATCH + filter combination. When `--cwd` is applied, SQLite cannot use both the FTS index and the `idx_messages_workspace` B-tree index simultaneously -- it does a full FTS scan then filters, running this expensive operation 4 times. The `execSessionSearch` in `sessions.go:120-239` has the same problem with 3 queries plus per-hit message count queries (line 216-217).

**Changes needed:**

1. **`auto-search/internal/search/messages.go`** -- Combine the count queries into a single query:
   ```sql
   SELECT COUNT(*), COUNT(DISTINCT m.session_id), COUNT(DISTINCT m.message_id)
   FROM messages_fts
   JOIN messages m ON m.doc_id = messages_fts.rowid
   WHERE messages_fts MATCH ? AND m.workspace = ? ...
   ```
   This cuts 3 queries down to 1, reducing FTS evaluation from 4x to 2x.

2. **`auto-search/internal/search/sessions.go:216-217`** -- The per-hit `SELECT COUNT(*) FROM messages WHERE session_id = ?` runs inside the row loop. Batch this into a single query or use a `JOIN` / window function in the main query.

3. **`auto-search/internal/indexdb/schema.go`** -- Consider adding a composite index:
   ```sql
   CREATE INDEX IF NOT EXISTS idx_messages_workspace_timestamp ON messages(workspace, timestamp);
   ```

4. **Long-term:** For workspace-filtered searches, consider a CTE approach that materializes matching FTS rowids once, then joins for both counting and pagination.

**Effort:** Medium -- the single-count-query refactor is straightforward; the CTE approach requires more testing.

---

## Suggestion 3: Add `session list` command

**Problems:** P13 (no session list command)

**Root cause:** The session command in `auto-search/internal/cli/session.go:19-29` only registers `get` and `describe` subcommands. There is no `list` subcommand. The workaround (searching for "user" in session scope) is documented in the quickstart but is unintuitive.

**Changes needed:**

1. **`auto-search/internal/cli/session.go`** -- Add a `newSessionListCmd()` function that queries the `sessions` table directly (no FTS needed) with optional `--cwd`, `--remote`, `--since`, `--after`, `--before` filters, ordered by `first_message_at DESC`, with `--limit` pagination.

2. **`auto-search/internal/indexdb/query_sessions.go`** -- Add a `ListSessions()` function that builds a filtered query against the `sessions` table.

3. **`auto-search/internal/cli/session.go:24-28`** -- Register the new subcommand:
   ```go
   cmd.AddCommand(
       newSessionListCmd(),
       newSessionGetCmd(),
       newSessionDescribeCmd(),
   )
   ```

**Effort:** Small -- this is a direct table query with standard filters, following existing patterns.

---

## Suggestion 4: Improve empty-query and empty-result error messages

**Problems:** P6 (empty query error is unhelpful), P14 (empty results give no diagnostic hints)

**Root cause:** The parser at `auto-search/internal/query/parser.go:110` returns `fmt.Errorf("expected term or phrase, got %q", tok.Value)` for empty input. This is a parser-internal message. For empty results, there is no diagnostic layer -- `SearchMessages` just returns empty hits with exit 0.

**Changes needed:**

1. **`auto-search/internal/search/messages.go`** (and `sessions.go`) -- Add an early check before parsing:
   ```go
   if strings.TrimSpace(opts.Query) == "" {
       return nil, errors.New("search query is required; provide one or more terms (e.g., autosearch search \"database error\")")
   }
   ```

2. **`auto-search/internal/search/messages.go`** -- After the search completes with 0 hits, emit a diagnostic to stderr (not in the JSON) noting possible causes:
   - If `--cwd` was provided: "hint: no messages found for workspace %q; verify the path matches indexed sessions"
   - If date filters were used: "hint: no messages found in the specified date range"

   This requires threading a `stderr io.Writer` through to the search functions or returning hints in the result for the CLI layer to print.

**Effort:** Small -- validation check is trivial; stderr hints require a small plumbing change.

---

## Suggestion 5: Add ISO 8601 timestamps alongside epoch milliseconds

**Problems:** P8 (timestamps are epoch milliseconds, not human-readable)

**Root cause:** `SessionHit` in `auto-search/internal/search/sessions.go:13-21` stores `FirstMessageAt` and `LastMessageAt` as `int64` (epoch ms). The session describe output at `auto-search/internal/cli/session.go:112-115` also emits raw epoch values. There is no formatting layer.

**Changes needed:**

1. **`auto-search/internal/search/sessions.go`** -- Add formatted timestamp fields to `SessionHit`:
   ```go
   FirstMessageAtISO string `json:"firstMessageAtISO"`
   LastMessageAtISO  string `json:"lastMessageAtISO"`
   ```
   Populate them with `time.UnixMilli(firstMessageAt).UTC().Format(time.RFC3339)` in the row scan loop (line 218-226).

2. **`auto-search/internal/cli/session.go:112-115`** -- Add ISO fields to the describe output alongside the epoch values.

3. **`auto-search/internal/search/messages.go`** -- No timestamp fields exist on `MessageHit` currently, but if added in future, follow the same pattern.

**Effort:** Small -- purely additive, no breaking changes to existing fields.

---

## Suggestion 6: Add snippet support for session-scope search

**Problems:** P7 (session search has no snippets, --highlight silently ignored)

**Root cause:** `SearchSessions` in `auto-search/internal/search/sessions.go:48-118` does not extract snippets from `transcript_truncated`. The `SessionSearchOpts` struct (line 30-45) has no `Highlight` field. The CLI at `auto-search/internal/cli/search.go:78-96` does not pass `highlight` to `SearchSessions`.

**Changes needed:**

1. **`auto-search/internal/search/sessions.go:30-45`** -- Add `Highlight bool` to `SessionSearchOpts`.

2. **`auto-search/internal/search/sessions.go:13-21`** -- Add `Snippet string` field to `SessionHit`.

3. **`auto-search/internal/search/sessions.go:186-226`** -- In the select query, also retrieve `s.transcript_truncated`. Extract a snippet using the existing `Snippet()` function from `snippets.go`, passing `ExtractTerms(ast)`.

4. **`auto-search/internal/cli/search.go:78-96`** -- Pass `Highlight: highlight` to `SearchSessions`.

**Effort:** Medium -- requires threading terms through session search and adapting the snippet extraction for transcript content.

---

## Suggestion 7: Remove redundant meta fields in message-scope search

**Problems:** P11 (total_hits, total_matches, distinct_messages always identical)

**Root cause:** In `auto-search/internal/search/messages.go:155-160`, `TotalHits` and `TotalMatches` are both set to `stats.TotalMatches`. `DistinctMessages` is computed via a separate `COUNT(DISTINCT m.message_id)` query (line 232-235) but in practice equals `total_hits` because FTS operates at the message row level.

**Changes needed:**

1. **`auto-search/internal/search/messages.go:232-235`** -- Remove the separate `distinctMessagesQuery` -- it adds latency for no new information.

2. **`auto-search/internal/search/messages.go:34-50`** -- Remove `TotalMatches` from `Meta` (or keep as documented alias but set from the same value). Remove `DistinctMessages` or document clearly that it is always equal to `TotalHits` for message scope.

   **Breaking change consideration:** If external consumers rely on `total_matches`, keep it as an alias but add a comment. Remove `distinct_messages` as it adds confusion.

**Effort:** Small -- delete one query, remove or alias one field.

---

## Suggestion 8: Add `docs` and `doctor` commands

**Problems:** P9 (missing docs and doctor commands)

**Root cause:** `auto-search/internal/cli/root.go:54-64` does not register `docs` or `doctor` commands. The project CLAUDE.md specifies these as standard CLI patterns.

**Changes needed:**

1. **`auto-search/internal/cli/docs.go`** (new file) -- Create a `newDocsCmd()` that prints comprehensive usage documentation as markdown to stdout.

2. **`auto-search/internal/cli/doctor.go`** (new file) -- Create a `newDoctorCmd()` that checks:
   - Index database exists and is readable
   - Schema version matches expected
   - ETL output directory exists and has parquet files
   - Index is not stale (compare parquet mtime vs index mtime)
   - Output as JSON with `status`, `checks[]` array per the project conventions.

3. **`auto-search/internal/cli/root.go:54-64`** -- Register both commands.

**Effort:** Medium -- `docs` is a string dump; `doctor` requires health check logic.

---

## Suggestion 9: Fix skills extraction in ETL pipeline

**Problems:** P10 (skills command returns empty array)

**Root cause:** `auto-search/internal/indexdb/query_skills.go:17-28` queries `WHERE skill_name != ''` from the `messages` table. The table schema at `schema.go:74` has a `skill_name TEXT NOT NULL` column. The issue is upstream: the ETL pipeline (in `auto-etl/`) is not populating `skill_name` when extracting messages from Claude session logs. The indexer at `auto-search/internal/indexdb/indexer.go` copies whatever the parquet data contains.

**Changes needed:**

1. Investigate `auto-etl/` -- specifically how skill invocations in Claude sessions are detected and mapped to `skill_name` in the parquet output. The skill detection logic likely needs to parse tool-use blocks where `tool_name == "Skill"` and extract the skill name from the tool input.

2. This is primarily an `auto-etl` fix, not an `auto-search` fix. The search indexer and skills query are correct -- they just receive empty data.

**Effort:** Large -- requires understanding the ETL transformation pipeline and Claude session log format.

---

## Suggestion 10: Address meta-search pollution

**Problems:** P12 (search results contain previous search queries and quickstart output)

**Root cause:** When agents run `autosearch quickstart` or `autosearch search`, the output becomes part of the session transcript, which gets indexed by the next ETL + index cycle. This creates self-referential results where searching for error patterns surfaces previous searches for those same patterns.

**Changes needed:**

This is a design-level issue with multiple possible approaches:

1. **ETL-level filtering:** In `auto-etl/`, add heuristics to tag or exclude messages that are autosearch output (e.g., messages containing `_meta` JSON with `scope` and `mode` fields).

2. **Search-level filtering:** Add a `--exclude-tool` flag to filter out messages where `tool_name` matches specified tools (e.g., `--exclude-tool autosearch`).

3. **Indexer-level tagging:** During indexing, detect and tag messages that are tool output from auto-stack tools, allowing post-hoc filtering.

**Effort:** Large -- any approach requires careful heuristics to avoid false positives. The `--exclude-tool` flag is the simplest incremental step.
