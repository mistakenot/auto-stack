# Plan: Task 015

## Summary

Compute a session "intent" (first real user message) in the `auto-etl` transform, store it
as two derived parquet columns, and thread it through the `auto-search` index into `session
list` / `session describe` — following the `042857e` add-a-field-end-to-end pattern.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| ~ | auto-etl/internal/model/model.go | + `FirstUserIntent`, `FirstUserIntentTruncated` on `AgentSession`; + `IntentTruncateMaxChars=200`; bump `SchemaVersion` 4→5 |
| ~ | auto-etl/internal/transform/transform.go | + `firstUserIntent`/`isJunkIntent`/`parseSlashCommand`/`collapseWhitespace`/`headTruncate` + `junkPrefixes`; populate the two fields in `transformSession` |
| ~ | auto-etl/internal/transform/transform_test.go | unit tests for the heuristic (AC-2, AC-3) |
| ~ | auto-search/internal/model/parquet.go | + two fields on `ParquetSessionRow` (matching parquet tags) |
| ~ | auto-search/internal/indexdb/schema.go | + two `sessions` columns (`TEXT NOT NULL DEFAULT ''`); bump `SchemaVersion` 7→8 |
| ~ | auto-search/internal/indexdb/sessions.go | `InsertSession` + two params + two INSERT columns |
| ~ | auto-search/internal/indexdb/indexer.go | `insertSessionFromParquet` passes the two fields |
| ~ | auto-search/internal/indexdb/query_sessions.go | + fields on `SessionRow` & `SessionListRow`; `ListSessions` + `GetSessionByID` SELECT/Scan |
| ~ | auto-search/internal/cli/session.go | verify intent surfaces in list/get JSON (likely no code change) |
| ~ | auto-search/internal/testutil/fixtures.go | populate new columns in fixture rows |
| ~ | auto-search/internal/indexdb/indexer_integration_test.go, internal/cli/cli_integration_test.go | round-trip assertions (AC-4, AC-5) |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [ ] `auto-etl/internal/transform/transform_test.go` — unit: heuristic skips junk, falls back to slash command, head-truncates (AC-1/2/3)
- [ ] `auto-search/internal/indexdb/indexer_integration_test.go` — integration: intent columns round-trip parquet→index (AC-4)
- [ ] `auto-search/internal/cli/cli_integration_test.go` — integration: `session list` JSON carries `first_user_intent_truncated`, `session describe` carries full `firstUserIntent` (AC-5)
- [ ] Manual: `autoetl transform --full` then `autosearch session list` shows readable intents; `duckdb` confirms columns populated

## Execution Sequence
```
Phase 1 (auto-etl: schema + heuristic)
        --> Phase 2 (auto-search: index threading)
        --> Phase 3 (auto-search: CLI + fixtures + integration tests)
        --> Phase 4 (rollout + e2e verification, no commit)
```
Strictly linear: Phase 2's parquet column names must match Phase 1's; Phase 3 asserts the
columns Phase 2 added.

## Plan

### Phase 1: auto-etl — intent computation + schema
- [x] Step 1.1: In `model.go`, add `FirstUserIntent string \`parquet:"first_user_intent"\`` and `FirstUserIntentTruncated string \`parquet:"first_user_intent_truncated"\`` to `AgentSession`; add `const IntentTruncateMaxChars = 200`; bump `SchemaVersion` 4→5. **Verify**: `go build ./...` in `auto-etl`.
- [x] Step 1.2: In `transform.go`, add `junkPrefixes` (`<local-command-caveat>`, `<command-name>`, `<command-message>`, `<local-command-stdout>`, `<system-reminder>`, `[Request interrupted`) and helpers `isJunkIntent`, `parseSlashCommand` (extract `<command-name>` + non-empty `<command-args>` → e.g. `/execute-task 014`), `collapseWhitespace` (`strings.Fields`+join), `headTruncate` (first N chars + `…`), and `firstUserIntent(messages, maxChars) (full, truncated string)`. **Verify**: `go build ./...`.
- [x] Step 1.3: In `transformSession` (after the `messages` slice is built, before the `AgentSession` literal at ~line 382), call `firstUserIntent(messages, model.IntentTruncateMaxChars)` and set both fields on the `AgentSession` literal. **Verify**: `go build ./...`.
- [x] Step 1.4: Add table-driven unit tests in `transform_test.go`: (a) caveat-then-prose → prose; (b) command-then-prose → prose; (c) `<system-reminder>` then prose → prose; (d) empty/whitespace skipped; (e) slash-command-only → `/execute-task 014`; (f) prose-first → prose; (g) no user message → empty; (h) truncated is single-line ≤200 runes + `…`; (i) multibyte/emoji prose intent truncates on a rune boundary (no U+FFFD / split rune). **Verify**: `go test ./internal/transform/...` passes.
- [x] Step 1.5: Run real transform to a temp dir (`go run ./cmd/autoetl transform --full --output ./.tmp/output` or existing flags) and `duckdb` the sessions parquet to confirm `first_user_intent`/`first_user_intent_truncated` are populated and non-junk for caveat sessions. **Verify**: duckdb shows readable intents; junk-prefix rate ~0.
- [x] Step 1.6: Commit: `feat(015): auto-etl first-user-intent fields + heuristic`

### Phase 2: auto-search — index threading
- [ ] Step 2.1: In `parquet.go`, add `FirstUserIntent` / `FirstUserIntentTruncated` to `ParquetSessionRow` with matching parquet tags. **Verify**: `go build ./...` in `auto-search`.
- [ ] Step 2.2: In `schema.go`, add `first_user_intent TEXT NOT NULL DEFAULT ''` and `first_user_intent_truncated TEXT NOT NULL DEFAULT ''` to the `sessions` DDL; bump `SchemaVersion` 7→8. Leave `sessions_fts` untouched (display-only). **Verify**: `go build ./...`.
- [ ] Step 2.3: In `sessions.go`, add two params to `InsertSession` and to the INSERT column list + placeholders (keep positional arg order consistent). **Verify**: `go build ./...`.
- [ ] Step 2.4: In `indexer.go`, pass `r.FirstUserIntent`, `r.FirstUserIntentTruncated` from `insertSessionFromParquet`. **Verify**: `go build ./...`.
- [ ] Step 2.5: In `query_sessions.go`: add `FirstUserIntent` + `FirstUserIntentTruncated` to `SessionRow`; add `FirstUserIntentTruncated string \`json:"first_user_intent_truncated,omitempty"\`` to `SessionListRow` (distinct key from the full-intent `describe` surface); update `ListSessions` SELECT to include `s.first_user_intent_truncated` and add to `Scan`; update `GetSessionByID` SELECT + `Scan` for both columns. **Verify**: `go build ./...`.
- [ ] Step 2.6: Run existing index unit/integration tests to confirm no regressions from the schema bump. **Verify**: `go test ./internal/indexdb/...` passes.
- [ ] Step 2.7: Commit: `feat(015): thread first-user-intent through auto-search index`

### Phase 3: auto-search — CLI exposure + fixtures + integration tests
- [ ] Step 3.1: Two surfaces in `cli/session.go`: (a) `session list` — verify `SessionListRow` JSON carries `first_user_intent_truncated` (marshals directly; no edit needed after Step 2.5); (b) `session describe` (`newSessionDescribeCmd`, ~lines 232-305) — add `"firstUserIntent": sess.FirstUserIntent` to the `"session"` `map[string]any` literal (~lines 271-294). Do NOT touch `session get` (it is a transcript renderer with no JSON surface). **Verify**: build; `autosearch session describe <id>` JSON includes `firstUserIntent`.

<!-- RESOLVED(P1): `session get` does not emit SessionRow — this step needs a real edit
REVIEW: Verified against cli/session.go: `session get` (newSessionGetCmd, lines 191-230)
renders a message transcript and never touches SessionRow, so "session get emits SessionRow
with the full intent" is false and "add code only if a field is dropped in marshalling" will
silently leave the full intent unsurfaced. The session-level JSON command is `session
describe` (lines 232-305), which builds an explicit `map[string]any` — adding FirstUserIntent
to SessionRow is NOT enough there either; you must add `"firstUserIntent": sess.FirstUserIntent`
to that map. Rewrite this step to: (a) list — verify SessionListRow JSON carries the
truncated intent (marshals directly, OK after the SELECT/Scan change in Step 2.5); (b)
describe — add GetSessionByID's new field(s) to the describe map literal. Update Step 3.3 and
AC-5's integration test target (`cli_integration_test.go`) to assert on `session describe`,
not `session get`.
AUTHOR: Verified (get=transcript text 191-230; describe=JSON map literal 232-305). Rewrote
Step 3.1 to require the explicit map-literal edit in `session describe` and leave `get`
untouched. Step 3.3 and the AC-5 success criterion now assert on `session describe`. Step 2.5
field renamed to `first_user_intent_truncated` to match.
-->

- [ ] Step 3.2: Update `testutil/fixtures.go` so generated session fixtures set the two new columns (at least one caveat-derived intent + one slash-command fallback row). **Verify**: `go build ./...`.
- [ ] Step 3.3: Add/extend integration tests: in `indexer_integration_test.go` assert intent round-trips parquet→index; in `cli_integration_test.go` assert `session list` JSON carries `first_user_intent_truncated` and `session describe` JSON carries the full `firstUserIntent`. **Verify**: `go test ./internal/indexdb/... ./internal/cli/...` passes.
- [ ] Step 3.4: Run the full module test suite. **Verify**: `go test ./...` passes in `auto-search`.
- [ ] Step 3.5: Commit: `feat(015): expose first-user-intent in session list + describe`

### Phase 4: Rollout + e2e verification (no commit)
- [ ] Step 4.1: `autoetl transform --full` against `~/.claude/projects` → `~/.auto/etl/output`. **Verify**: completes; sessions parquet has the new columns (duckdb `DESCRIBE`).
- [ ] Step 4.2: `autosearch index` (auto-rebuilds on schema-version 8 mismatch). **Verify**: rebuild runs; no errors.
- [ ] Step 4.3: `autosearch session list --limit 20` and `autosearch session describe <id>` on a known caveat-started session. **Verify**: list shows readable truncated intents (no `<local-command-caveat>`/`<command-name>` leakage); describe shows the full `firstUserIntent`; slash-only session shows `/cmd args`.
- [ ] Step 4.4: duckdb sanity: junk-prefix rate among `first_user_intent_truncated` ≈ 0; non-empty rate ≥ ~98%. **Verify**: matches the investigation baseline.

## Success Criteria
- [ ] `go build ./...` passes in both `auto-etl` and `auto-search`
- [ ] AC-1: sessions parquet contains populated `first_user_intent` + `first_user_intent_truncated` (Phase 1 unit + Step 1.5 duckdb)
- [ ] AC-2: junk first-messages skipped — unit tests for caveat/command/reminder/empty pass
- [ ] AC-3: slash-only session yields `/cmd args`; no-message session yields empty — unit tests pass
- [ ] AC-4: intent round-trips parquet→SQLite — `indexer_integration_test.go` passes
- [ ] AC-5: `session list` JSON carries `first_user_intent_truncated`, `session describe` JSON carries full `firstUserIntent` — `cli_integration_test.go` passes
- [ ] e2e: real `autosearch session list` shows readable intents with ~0 junk leakage (Phase 4)

## Open Questions
- (none — placement, storage shape, subagent handling, FTS, and slash-fallback format all resolved in requirements.md / solution.md)
