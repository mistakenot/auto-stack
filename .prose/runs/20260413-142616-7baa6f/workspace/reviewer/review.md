# Independent Review of Autosearch Improvement Suggestions

kind: output
service: reviewer

---

## Suggestion 1: Add input validation for `--role`, `--mode`, and `--limit` flags

**Confidence: HIGH**

**Root cause verified.** I independently confirmed:

- `--role`: Passed directly to SQL filters at `messages.go:195-197` and `sessions.go:142-144` without any validation. The analyst is correct that any string is accepted and silently produces zero results.
- `--mode`: Declared at `search.go:16` and registered at `search.go:104`, but the variable `mode` is never referenced in the `RunE` function body (lines 35-101). Confirmed: it is a dead flag. The `Meta.Mode` field is hardcoded to `"bm25"` at `messages.go:157` and `sessions.go:103`.
- `--limit`: `normalizePagination` at `messages.go:332-339` treats `pageSize <= 0` as "use default". This means `--limit -5` silently becomes 20. There is no upper bound.

**Feedback:**

1. The `normalizeRole()` function is appropriate and follows the existing pattern. However, the analyst's proposed values (`user`, `assistant`, `tool`) should be verified against actual data. The schema at `schema.go:63` stores `role TEXT NOT NULL` but does not constrain values. I checked the ETL model reference -- Claude sessions produce `user`, `assistant`, and `tool` roles, so these are correct.

2. For `--limit`, the analyst's proposal has an internal contradiction: the code comment says `--limit 0` means "use default" (which is the current behavior and correct for Cobra's zero-value convention), but the error message says `--limit must be >= 0`, implying 0 is valid. This is fine -- keeping 0 as "use default" is correct since Cobra flags default to zero. The key fix is rejecting negatives and adding the upper bound. The 1000 cap is reasonable.

3. For `--mode`: I agree the simplest fix is validating in `RunE` or removing the flag entirely. Since only `bm25` exists and the flag has never been functional, **removing it is cleaner** than adding validation for a single allowed value. However, removing a flag is technically a breaking change for scripts that pass `--mode bm25`. The analyst's validation approach is safer.

4. **Missed issue:** The analyst did not mention that `--scope` also has no validation -- it falls through to the `default` case in the switch at `search.go:98-99` which does return an error. So scope is already handled correctly.

**Regression risk: LOW.** These are additive validations that reject previously-silent bad input. Existing valid usage is unaffected. Tests in `cli_integration_test.go` and `search_integration_test.go` use valid inputs.

---

## Suggestion 2: Fix `--cwd`/`--remote` performance

**Confidence: MEDIUM**

**Root cause verified.** I confirmed the 4 separate queries in `execMessageSearch` at `messages.go:220-251`:
- Line 220: `COUNT(*)` query
- Line 226: `COUNT(DISTINCT m.session_id)` query
- Line 232: `COUNT(DISTINCT m.message_id)` query
- Line 238-244: Paginated result query

Each re-evaluates the full FTS MATCH + filter. For `execSessionSearch` at `sessions.go:167-193`, I count 3 queries: one COUNT, one distinct messages count (with a subquery), and the paginated result query. Plus the per-row `COUNT(*)` at line 216 inside the row loop.

**Feedback:**

1. **Combining count queries is correct and low-risk.** The suggestion to merge the 3 count queries into `SELECT COUNT(*), COUNT(DISTINCT m.session_id), COUNT(DISTINCT m.message_id)` is straightforward. This is a clear win.

2. **The per-row message count in sessions (line 216) is a genuine N+1 query problem.** For a page of 20 results, this adds 20 extra queries. The analyst correctly identifies this. A JOIN or window function would fix it, but since `messages` and `sessions` are separate tables (not FTS), a simple `LEFT JOIN (SELECT session_id, COUNT(*) ...) GROUP BY session_id` on the paginated results would work. This is the bigger performance win for session search.

3. **The composite index suggestion (`idx_messages_workspace_timestamp`) is speculative.** SQLite's query planner may not use it effectively alongside FTS. The existing `idx_messages_workspace` and `idx_messages_timestamp` are separate; whether a composite helps depends on query patterns. I would benchmark before adding.

4. **The CTE approach is the right long-term fix** but the analyst correctly flags it as needing more testing. Materializing FTS rowids once avoids re-evaluation.

5. **The "30-80x degradation" claim is not verified in this review** -- there is no benchmark data in the codebase. The architectural analysis of why it would be slow is correct, but the magnitude is a guess. The analyst should have been explicit about this.

6. **Effort estimate "Medium" seems right.** Combining counts is small, but fixing the N+1 and testing the CTE approach adds work.

**Regression risk: MEDIUM.** Changing SQL queries requires careful testing. The existing `search_integration_test.go` provides a baseline but only tests basic scenarios. The workspace-filtered path is not covered by tests (no test fixture with workspace data set up).

---

## Suggestion 3: Add `session list` command

**Confidence: HIGH**

**Root cause verified.** `session.go:24-28` registers only `get` and `describe`. There is no `list` subcommand. `query_sessions.go` has `GetSessionByID` and `SessionMessages` but no `ListSessions` function.

**Feedback:**

1. This is a **legitimate feature request, not a bug fix.** The analyst frames it as addressing problem P13, which itself is a missing feature. This is fine -- it is a valid and useful addition.

2. The implementation approach is correct: a direct table query against `sessions` with standard filters. The existing `query_sessions.go` provides the pattern for scanning `SessionRow`.

3. **Effort estimate "Small" is accurate.** The query function is straightforward, and the CLI subcommand follows existing patterns.

4. **One concern:** The analyst suggests ordering by `first_message_at DESC`. This is correct for "most recent first" but consider also supporting `--sort` for flexibility, or at minimum documenting the default sort order.

**Regression risk: LOW.** Purely additive -- new command, new function.

---

## Suggestion 4: Improve empty-query and empty-result error messages

**Confidence: HIGH**

**Root cause verified.** The parser at `parser.go:110` returns `expected term or phrase, got ""` for empty input. This error bubbles up through `fmt.Errorf("parse query: %w", err)` at `messages.go:117`, producing: `parse query: expected term or phrase, ""`. Not helpful for users.

For empty results, the search functions return empty `hits` arrays with exit 0 and no diagnostic output.

**Feedback:**

1. The early empty-query check is correct and trivial. Adding it before `query.Parse()` avoids the cryptic parser error.

2. The empty-result hints are a nice UX improvement. However, the analyst's suggestion to thread `stderr io.Writer` through search functions is more invasive than necessary. **Better approach:** Return hints as part of the result metadata (e.g., `Meta.Hints []string`) and let the CLI layer decide how to display them. This keeps the search functions pure and testable.

3. **The hint content suggestions are good:** checking if `--cwd` or date filters narrowed results to zero is useful diagnostic information.

4. **Effort estimate "Small" is accurate** for the empty-query check. The hints plumbing is small-to-medium depending on approach.

**Regression risk: LOW.** The empty-query check is a new error for previously-broken input. The hints are additive.

---

## Suggestion 5: Add ISO 8601 timestamps alongside epoch milliseconds

**Confidence: HIGH**

**Root cause verified.** `SessionHit` at `sessions.go:13-21` has `FirstMessageAt int64` and `LastMessageAt int64`. The `session describe` output at `session.go:112-114` also emits raw epoch values. No human-readable formatting exists anywhere in the output.

**Feedback:**

1. This is a **valid usability improvement**. Epoch milliseconds are machine-friendly but agents and humans both benefit from ISO timestamps.

2. The approach of adding `*ISO` fields alongside existing fields is correct -- no breaking change.

3. **Minor concern about field naming:** `firstMessageAtISO` uses camelCase + "ISO" suffix, which is slightly awkward. Consider `firstMessageAtFormatted` or `firstMessageAtRfc3339`. But this is bikeshedding -- the analyst's suggestion is fine.

4. The analyst correctly notes that `MessageHit` has no timestamp fields currently. This is a separate observation, not part of this fix.

5. **Effort estimate "Small" is accurate.** Purely additive field population.

**Regression risk: LOW.** Additive fields. No existing fields change.

---

## Suggestion 6: Add snippet support for session-scope search

**Confidence: MEDIUM**

**Root cause verified.** `SessionSearchOpts` at `sessions.go:30-45` has no `Highlight` field. `SessionHit` at `sessions.go:13-21` has no `Snippet` field. The CLI at `search.go:78-96` does not pass `highlight` to `SearchSessions`. The `Snippet()` function in `snippets.go` works on arbitrary text and could be applied to `transcript_truncated`.

**Feedback:**

1. The analysis is correct that `--highlight` is silently ignored for session scope. This is a real bug (flag accepted but has no effect).

2. However, **session transcripts are structurally different from message content.** The `transcript_truncated` field contains the full session transcript, which may be very large. The `Snippet()` function at `snippets.go:16-63` uses a 300-character window, which works for message content but may not produce meaningful results for full transcripts. The snippet might land in the middle of a tool output dump.

3. **The `execSessionSearch` query at sessions.go:186-192 does not currently SELECT `s.transcript_truncated`**, so adding it to the query is needed. This increases data transfer from SQLite for every hit, even when snippets are not requested. Consider only selecting it when `highlight` or snippet is enabled.

4. The `ExtractTerms(ast)` call requires the parsed AST, which is available in `SearchSessions` (line 76-79) but not passed to `execSessionSearch`. Threading it through adds parameter count to an already-long function signature.

5. **Effort estimate "Medium" is accurate.** The plumbing is straightforward but testing transcript snippets requires thought about edge cases.

**Regression risk: LOW-MEDIUM.** Adding a column to the SELECT changes the query plan. Adding the Snippet field is additive. But transcript snippets may produce confusing output that looks worse than no snippet.

---

## Suggestion 7: Remove redundant meta fields in message-scope search

**Confidence: HIGH**

**Root cause verified.** At `messages.go:155-159`:
```go
TotalHits:        stats.TotalMatches,
TotalMatches:     stats.TotalMatches,
```
These are set to the same value. `DistinctMessages` comes from the separate `COUNT(DISTINCT m.message_id)` query at line 232-235, but since FTS indexes at the message row level (one FTS document per message row), `COUNT(DISTINCT m.message_id)` equals `COUNT(*)` for non-duplicate message IDs. The `messages.message_id` column has a `UNIQUE` constraint (schema.go:59), confirming they must be equal.

**Feedback:**

1. The analysis is correct. `total_hits`, `total_matches`, and `distinct_messages` are always identical for message scope.

2. **Removing the `distinctMessagesQuery` is a clear performance win** (one fewer SQL query per search) and aligns with Suggestion 2.

3. **Breaking change concern is valid.** If any external scripts parse `total_matches` or `distinct_messages`, removing them breaks those scripts. The analyst's suggestion to keep `total_matches` as an alias is pragmatic. For `distinct_messages`, I agree it should be removed since it is actively confusing.

4. An alternative: keep all three fields but populate them from the same `COUNT(*)` result, eliminating the extra query without changing the JSON schema. This is zero-risk.

5. **Effort estimate "Small" is accurate.**

**Regression risk: LOW** if keeping fields as aliases; **MEDIUM** if removing fields from JSON output.

---

## Suggestion 8: Add `docs` and `doctor` commands

**Confidence: HIGH**

**Root cause verified.** `root.go:54-64` does not register `docs` or `doctor`. The project CLAUDE.md lists these as standard CLI patterns that "most tools will support."

**Feedback:**

1. This is a **feature request aligned with project conventions**, not a bug fix. Valid and expected per project standards.

2. The `doctor` checks proposed are sensible:
   - Index exists: check `config.IndexPath()` and `os.Stat()`
   - Schema version: already available via `SchemaVersion` constant at `schema.go:14`
   - ETL output directory: check configured input path
   - Staleness: compare mtimes

3. The `docs` command is essentially a static string dump -- very low effort.

4. **Effort estimate "Medium" is accurate.** `docs` is tiny; `doctor` needs the health check logic but nothing complex.

5. **Consider prioritization:** `quickstart` already exists and serves a similar purpose to `docs`. The `doctor` command is more valuable because there is currently no way to diagnose index staleness or configuration issues programmatically.

**Regression risk: LOW.** Purely additive commands.

---

## Suggestion 9: Fix skills extraction in ETL pipeline

**Confidence: MEDIUM**

**Root cause verified.** The `query_skills.go` query is correct -- it queries `WHERE skill_name != ''`. The schema has the column. The indexer copies parquet data. The problem is upstream in `auto-etl/`.

**Feedback:**

1. The analyst correctly identifies this as an ETL issue, not a search issue. **The focus area was autosearch**, so this suggestion is partially out of scope.

2. The root cause hypothesis (ETL not parsing tool-use blocks for skill invocations) is plausible but **not verified**. The analyst did not read the ETL code. This is the weakest root cause analysis in the document.

3. **Effort estimate "Large" is appropriate** given the cross-project scope and need to understand Claude session log format.

4. **This is the lowest-confidence suggestion** because the actual root cause in the ETL pipeline is not confirmed.

**Regression risk: HIGH** (if implemented incorrectly, could corrupt ETL output for all downstream consumers).

---

## Suggestion 10: Address meta-search pollution

**Confidence: LOW**

**Feedback:**

1. This is a **real problem** but the analyst acknowledges it is a design-level issue. All three proposed approaches have significant trade-offs.

2. The `--exclude-tool` flag is the simplest incremental step, and I agree with this assessment. However, it puts the burden on the user to remember to exclude tools, which is poor UX.

3. **ETL-level filtering is risky** because it could accidentally exclude legitimate search results that happen to mention autosearch. Heuristic-based filtering is fragile.

4. **The analyst missed a simpler approach:** add a `source_tool` or `is_tool_output` tag during indexing (not ETL) that marks messages which are autosearch/auto-stack tool outputs. Then default search to exclude these unless `--include-meta` is passed. This inverts the default to clean results while preserving data.

5. **Effort estimate "Large" is correct** for any robust solution. The `--exclude-tool` flag alone is "Small" but does not really solve the problem.

**Regression risk: HIGH** for ETL-level approaches; **LOW** for the `--exclude-tool` flag.

---

## Additional Problems Discovered

### Problem A: N+1 query pattern in `CountSessionMessages`

`indexdb/query_sessions.go:119-161` runs **7 separate SQL queries** to count messages by category for a single session. This is called from `session describe`. While not on the hot path (it is per-session, not per-search-result), it could be combined into a single query with `CASE WHEN` aggregation:

```sql
SELECT
  COUNT(*),
  SUM(CASE WHEN role = 'tool' THEN 1 ELSE 0 END),
  SUM(CASE WHEN tool_name = 'Bash' THEN 1 ELSE 0 END),
  ...
FROM messages WHERE session_id = ?
```

This is a small optimization but follows the same pattern as Suggestion 2.

### Problem B: `neighborMessageIDs` is an N+1 inside the search loop

At `messages.go:269`, `neighborMessageIDs` runs 2 queries per search hit (previous and next message). For a page of 20 results, that is 40 extra queries. This was not called out by the analyst but is arguably a bigger performance concern than the count queries, especially since it runs inside the paginated results loop. A window function or batch query would be more efficient.

### Problem C: No index on `(session_id, message_index)` pair for neighbor lookups

Wait -- I checked `schema.go:94`: `CREATE INDEX IF NOT EXISTS idx_messages_session_id_message_index ON messages(session_id, message_index)`. This index exists, so the neighbor lookups are at least indexed. But 40 indexed queries is still worse than 1 batch query.

---

## Summary Assessment

| # | Suggestion | Analyst Correct? | Confidence | Key Concern |
|---|-----------|-----------------|------------|-------------|
| 1 | Input validation | Yes | High | Minor contradiction in limit=0 handling (resolved in text) |
| 2 | Performance fix | Yes | Medium | 30-80x claim unverified; direction is right |
| 3 | Session list | Yes | High | Feature request, not bug fix |
| 4 | Error messages | Yes | High | Prefer metadata hints over stderr threading |
| 5 | ISO timestamps | Yes | High | None |
| 6 | Session snippets | Yes | Medium | Transcript snippets may be low quality |
| 7 | Remove redundant meta | Yes | High | Keep as aliases to avoid breaking changes |
| 8 | docs/doctor commands | Yes | High | Feature request per project convention |
| 9 | Skills ETL fix | Partially | Medium | Root cause not verified in ETL code |
| 10 | Meta-search pollution | Partially | Low | Simpler indexer-level approach missed |

**Top 3 highest-impact suggestions by my independent assessment:**
1. **Suggestion 2** (performance) -- real architectural issue with clear fix path
2. **Suggestion 1** (input validation) -- low effort, high defensive value
3. **Suggestion 3** (session list) -- fills a genuine usability gap

**Analyst quality:** Generally strong root cause analysis with accurate line references. The analyst read the actual code and provided specific file/line citations. Weakest on Suggestion 9 (did not verify ETL code) and Suggestion 10 (missed a simpler approach). The performance magnitude claim in Suggestion 2 should have been flagged as estimated rather than measured.
