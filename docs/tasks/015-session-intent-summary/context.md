# Context: Task 015

Verified codebase facts grounding the intent-summary design. See [solution.md](solution.md).

## Key Files

### auto-etl (compute + canonical schema)
- `auto-etl/internal/model/model.go:5` — `const SchemaVersion = 4` (bump to 5).
- `auto-etl/internal/model/model.go:90-127` — `AgentSession` struct; precedent fields
  `TranscriptFull`/`TranscriptTruncated` at lines 121-122 show derived-field convention.
- `auto-etl/internal/model/model.go:13-21` — `MessageRole` consts; `RoleUser = "user"`.
- `auto-etl/internal/transform/transform.go:148` — `transformSession(...)`; `messages` slice
  is built in the loop at lines 184-353 (user text → `msg.Role = line.Message.Role`,
  `msg.Content = text` at lines 208-209).
- `auto-etl/internal/transform/transform.go:382-407` — `AgentSession` literal assembled here;
  add the two new fields alongside `TranscriptTruncated` (line 403). This is where
  `firstUserIntent(messages, ...)` gets called.
- `auto-etl/internal/transform/transform.go:516-528` — `MidTruncate(s, maxChars)` truncates
  the **middle** (`…[truncated N chars]…`). Not suitable for a leading preview — a new
  head-truncate helper is needed.
- `auto-etl/internal/transform/transform.go:483-514` — `buildTranscripts`/`rolePrefix` show
  the existing pattern of deriving session-level text from the `messages` slice.

### auto-search (index + query + CLI)
- `auto-search/internal/model/parquet.go:5-38` — `ParquetSessionRow` mirrors `AgentSession`
  parquet tags; add `FirstUserIntent` / `FirstUserIntentTruncated` matching the etl tags.
- `auto-search/internal/indexdb/schema.go:13` — `const SchemaVersion = 7` (bump to 8;
  rebuild is automatic on mismatch).
- `auto-search/internal/indexdb/schema.go:31-55` — `sessions` table DDL; add two
  `TEXT NOT NULL DEFAULT ''` columns. FTS for sessions is defined at lines 115-123
  (`sessions_fts`) with triggers at 138-153 — left untouched (display-only decision).
- `auto-search/internal/indexdb/sessions.go:9-44` — `InsertSession(...)` positional params +
  INSERT column list/placeholders; add two params + two columns.
- `auto-search/internal/indexdb/indexer.go:285-296` — `insertSessionFromParquet` maps
  `ParquetSessionRow` → `InsertSession`; pass the two new fields.
- `auto-search/internal/indexdb/query_sessions.go:10-34` — `SessionRow` (loaded by
  `GetSessionByID`, used only by `session describe`); add full intent field(s).
- `auto-search/internal/indexdb/query_sessions.go:67-83` — `SessionListRow` (JSON list
  payload); add `FirstUserIntentTruncated` (carrying the truncated value).
- `auto-search/internal/indexdb/query_sessions.go:229-269` — `ListSessions` SELECT + Scan;
  add `s.first_user_intent_truncated`.
- `auto-search/internal/indexdb/query_sessions.go:279-299` — `GetSessionByID` SELECT + Scan;
  add the intent column(s).
- `auto-search/internal/cli/session.go:34-189` — `newSessionListCmd`; output is **JSON-only**
  (`enc.Encode(out)` at line 166, no text/table mode). Truncated intent surfaces as a JSON field.
- `auto-search/internal/cli/session.go:191-230` — `newSessionGetCmd` renders a **message
  transcript** (no JSON, no `SessionRow`) — NOT a surface for session-level fields; leave untouched.
- `auto-search/internal/cli/session.go:232-305` — `newSessionDescribeCmd` is the session-level
  JSON command; builds an explicit `map[string]any` literal (lines 271-294) from `GetSessionByID`.
  Full intent is added here as `"firstUserIntent": sess.FirstUserIntent`.

### Tests / fixtures
- `auto-etl/internal/transform/transform_test.go` — home for heuristic unit tests.
- `auto-search/internal/testutil/fixtures.go` — fixture parquet rows; needs the new columns
  for index/CLI integration tests.
- `auto-search/internal/indexdb/indexer_integration_test.go`,
  `auto-search/internal/cli/cli_integration_test.go` — round-trip assertions.

## Patterns
- **Derived session text is computed once in ETL and carried through, not recomputed.**
  `transcript_truncated` is built in `buildTranscripts` (transform.go), stored on
  `AgentSession`, mirrored in `ParquetSessionRow`, indexed via `InsertSession`, and exposed
  in `SessionRow`. Intent follows this exact path.
- **Schema-version bumps force rebuilds.** `model.SchemaVersion` (etl) and
  `indexdb.SchemaVersion` (search) gate immutability; bumping `indexdb.SchemaVersion` makes
  `autosearch index` rebuild from scratch (schema.go:246-259 `ReadSchemaVersion`).
- **Tool-result messages are role `tool`, not `user`** (transform.go:297-298), so a
  `role == "user"` filter already excludes tool output — the heuristic only needs to filter
  command/caveat/reminder *text*.
- **New SQLite columns use `TEXT NOT NULL DEFAULT ''`** (see `tool_use_result_json`,
  schema.go:77) so older partitions index cleanly.

## Empirical findings (local dataset, `~/.auto/etl/output`, 1,039 sessions)
- Literal first user message is junk ~24%: 225 `<local-command-caveat>`, 22
  `<command-message>`/other XML tags, 1 `[Request interrupted]`; 791 already clean prose.
- Junk sequence in slash-started sessions: msg1 `<local-command-caveat>`, msg2
  `<command-name>/clear…`, msg3 = real intent (verified by inspecting a caveat session).
- Skip-list heuristic recovers clean intent for 1,022/1,039 (98.4%); only 15 still start
  with a tag (edge cases), ~2 have no clean user message.

## Related Commits / Tasks (verified via git log)
- **`042857e`** `feat(autoetl,autosearch): persist per-tool-call duration_ms, tool_use_id,
  interrupted (PR 2 of 3)` — **the closest template**: one PR adds fields to the etl
  `AgentSession`/`AgentMessage`, mirrors them in `ParquetSessionRow`, threads through
  `InsertSession`/indexer/queries, and bumps both schema versions. Copy this end-to-end shape.
- **`c11e5cb`** `feat(012): structured tool output (tool_use_result_json)` (Task 012) —
  added `tool_use_result_json` with `TEXT NOT NULL DEFAULT ''` columns; the additive-column
  migration pattern.
- **`65189bb`** / **`0f839ba`** `feat(autosearch): session duration, subagent filters,
  sort-by` / `--min-tool-duration` — recent `query_sessions.go` + `cli/session.go` changes in
  the exact areas the CLI step touches.
