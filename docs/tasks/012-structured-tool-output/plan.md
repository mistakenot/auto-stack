---
hash: "809a30da"
id: "9d1de815"
read_when: "implementing structured tool output in auto-etl/auto-search or adding tool_use_result_json to the pipeline"
summary: "Implementation plan for threading a new tool_use_result_json column from JSONL through auto-etl parquet, autosearch SQLite, and message describe JSON output, with dual schema version bumps and corpus backfill."
title: "Plan: Task 012 — Structured Tool Output"
---

# Plan: Task 012 — Structured Tool Output

## Summary

Thread a single new `tool_use_result_json` column from JSONL through auto-etl parquet, autosearch SQLite, and the `message describe` JSON output; bump both schema versions in lockstep; backfill the live corpus.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| ~ | `auto-etl/internal/parser/parser.go` | Add `ToolUseResult` to `rawLine` and `ParsedLine`; populate in `ParseSession` loop |
| ~ | `auto-etl/internal/parser/parser_test.go` | Add `TestParseSession_ToolUseResultEnvelope` reading a `testdata/` fixture |
| + | `auto-etl/internal/parser/testdata/auq_envelope.jsonl` | Hand-authored fixture with a `toolUseResult` envelope carrying `answers` + `annotations` |
| ~ | `auto-etl/internal/transform/transform.go` | Write `msg.ToolUseResultJSON` in `case "tool_result":` |
| ~ | `auto-etl/internal/transform/transform_test.go` | Add transform test asserting envelope lands on `role=tool` row byte-identical; assistant row's column empty |
| ~ | `auto-etl/internal/model/model.go` | Add `ToolUseResultJSON string` to `AgentMessage`; bump `SchemaVersion 2 → 3` |
| ~ | `auto-search/internal/model/parquet.go` | Mirror `ToolUseResultJSON` on `ParquetMessageRow` |
| ~ | `auto-search/internal/indexdb/schema.go` | Add `tool_use_result_json TEXT NOT NULL DEFAULT ''` column; bump `SchemaVersion 5 → 6` |
| ~ | `auto-search/internal/indexdb/messages.go` | Add `toolUseResultJSON string` as trailing param to `InsertMessage`; update INSERT + Exec args |
| ~ | `auto-search/internal/indexdb/indexer.go` | Pass `r.ToolUseResultJSON` to `InsertMessage` in `insertMessageFromParquet` |
| ~ | `auto-search/internal/indexdb/query_messages.go` | Add `ToolUseResultJSON` to `MessageRow`; extend SELECT + Scan in `GetMessageByID` |
| ~ | `auto-search/internal/indexdb/indexer_integration_test.go` | Round-trip parquet → SQLite assertion for the new column |
| ~ | `auto-search/internal/cli/message.go` | Conditionally add `toolUseResult` (parsed JSON) to `message describe` output map |
| + | `auto-search/internal/cli/message_envelope_test.go` | `message describe` JSON output with and without envelope |
| + | `auto-search/internal/indexdb/auq_e2e_test.go` | SQL-only recommended-acceptance fixture test |
| ~ | `auto-etl/docs/reference/normalized-schema.md` | Document new column + SchemaVersion 3 |
| ~ | `auto-etl/docs/claude-message-types-and-etl-mapping.md` | Replace Q5 regex examples with `json_extract` over new column; update searchability summary |
| ~ | `docs/better-questions.md` | Close out AUQ envelope open item (strike or rewrite the line about per-question Q&A pairs) |
| ~ | `docs/research/askuserquestion-analytics.md` | Append `## Postscript — landed in task 012` section pointing to the new query patterns (append-only) |

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test

- [x] `auto-etl/internal/parser/parser_test.go::TestParseSession_ToolUseResultEnvelope` — AC-1 (parser captures envelope verbatim)
- [x] `auto-etl/internal/transform/transform_test.go::TestTransform_ToolUseResultEnvelope` — AC-1, AC-11(b) (envelope on `role=tool` row; assistant row empty)
- [x] `auto-search/internal/indexdb/indexer_integration_test.go` (extended) — AC-5, AC-11(c) (parquet → SQLite round-trip)
- [x] `auto-search/internal/cli/message_envelope_test.go::TestMessageDescribe_ToolUseResult` — AC-6, AC-11(d) (parsed JSON when populated; key omitted when empty)
- [x] `auto-search/internal/indexdb/auq_e2e_test.go::TestRecommendedAcceptanceFromFixture` — AC-7, AC-8, AC-11(e) (SQL recommended-acceptance + per-question notes)
- [x] Regression: existing transform golden snapshot for `content_truncated` on an AUQ tool_use row — AC-9, AC-11(f)
- [x] Human acceptance: `autoetl run --full && autosearch index && duckdb <Q5 query>` returns ≥ 261 rows with `tool_use_result_json != ''` on the live corpus — AC-3
- [x] Human acceptance: `duckdb DESCRIBE` across all weekly partitions returns a single consistent column set — AC-4

## Execution Sequence

```
Phase 1 (auto-etl envelope) ─┐
                             ├─→ Phase 2 (autosearch schema + plumbing + CLI) ─→ Phase 3 (E2E test + live backfill) ─→ Phase 4 (docs)
                             │                                                                                          ↑
                             └──────────────────────────────────────────────────────────────────────────────────────────┘
                             (Phase 4 may overlap Phase 3 since the docs touch different files)
```

Phases are sequential because each writer depends on the prior writer's schema. Phase 4 (docs) has no code dependency and may run in parallel with Phase 3 if executed by separate subagents.

## Plan

### Phase 1: auto-etl envelope capture

- [x] Step 1.1: Add `ToolUseResult json.RawMessage \`json:"toolUseResult"\`` to `rawLine` (sibling of `Message`) in `auto-etl/internal/parser/parser.go:67-77`. **Verify**: `go build ./auto-etl/...` succeeds.
- [x] Step 1.2: Add `ToolUseResult json.RawMessage` to `ParsedLine` in `auto-etl/internal/parser/parser.go:27-37`. Wire it into the `ParsedLine{...}` literal at lines 177-192. **Verify**: `go build ./auto-etl/...` succeeds.
- [x] Step 1.3: Add `ToolUseResultJSON string \`parquet:"tool_use_result_json"\`` to `AgentMessage` in `auto-etl/internal/model/model.go:23-63` (between `SkillName` and `InputTokens`). Bump `SchemaVersion 2 → 3` at line 5. **Verify**: `go build ./auto-etl/...` succeeds.
- [x] Step 1.4: In `auto-etl/internal/transform/transform.go:269-294` (the `case "tool_result":` block), add `if len(line.ToolUseResult) > 0 { msg.ToolUseResultJSON = string(line.ToolUseResult) }` after the metadata lookup. **Verify**: `go build ./auto-etl/...` succeeds and `go vet ./auto-etl/...` passes.
- [x] Step 1.5: Create fixture `auto-etl/internal/parser/testdata/auq_envelope.jsonl` — a hand-authored JSONL with one assistant `tool_use` line carrying an `AskUserQuestion` block, and one `type:"user"` line carrying the matching `tool_result` block plus a `toolUseResult` envelope (must include at least one question, one option flagged `(Recommended)`, an `answers` map, and one `annotations` entry with `notes`). **Verify**: `jq .` validates the file; manual read shows the envelope structure is realistic.
- [x] Step 1.6: Add `TestParseSession_ToolUseResultEnvelope` to `auto-etl/internal/parser/parser_test.go` asserting (a) `len(s.Lines[1].ToolUseResult) > 0` for the tool_result line and (b) `len(s.Lines[0].ToolUseResult) == 0` for the assistant line. **Verify**: `go test ./auto-etl/internal/parser/... -run ToolUseResultEnvelope` passes.
- [x] Step 1.7: Add `TestTransform_ToolUseResultEnvelope` to `auto-etl/internal/transform/transform_test.go` asserting (a) the produced `AgentMessage` with `Role == "tool"` has `ToolUseResultJSON` equal to the raw JSON from the fixture (byte-identical via `string(rawBytes) == msg.ToolUseResultJSON`), (b) the `AgentMessage` with `Role == "assistant"` has `ToolUseResultJSON == ""`. **Verify**: `go test ./auto-etl/internal/transform/... -run ToolUseResultEnvelope` passes.
- [x] Step 1.8: Add a regression assertion to existing `TestTransform_*AskUserQuestion*` golden test (or create one if absent) asserting `content_truncated` on the assistant AUQ row is byte-identical to a pre-change snapshot. **Verify**: `go test ./auto-etl/internal/transform/...` passes; the markdown rendering is unchanged.
- [x] Step 1.9: Run full module tests + vet. **Verify**: `go test ./auto-etl/... && go vet ./auto-etl/...` exit 0.
- [x] Step 1.10: Commit: `feat(012): phase 1 - auto-etl captures toolUseResult envelope`

### Phase 2: autosearch schema + plumbing + CLI

- [x] Step 2.1: Add `ToolUseResultJSON string \`parquet:"tool_use_result_json"\`` to `ParquetMessageRow` in `auto-search/internal/model/parquet.go:36-73` (between `SkillName` and `InputTokens`, mirroring `AgentMessage`). **Verify**: `go build ./auto-search/...` succeeds.
- [x] Step 2.2: In `auto-search/internal/indexdb/schema.go`, bump `SchemaVersion 5 → 6` at line 13. Add `tool_use_result_json TEXT NOT NULL DEFAULT '',` to the `messages` table DDL (line 75 area, after `skill_name`). Do not modify `messages_fts` or its triggers. **Verify**: `go build ./auto-search/...` succeeds.
- [x] Step 2.3: Extend `InsertMessage` in `auto-search/internal/indexdb/messages.go`: add `toolUseResultJSON string,` as trailing parameter; add `tool_use_result_json` to the INSERT column list; add a `?` placeholder; add `toolUseResultJSON` to Exec args. **Verify**: count VALUES placeholders matches Exec args (should be 30 of each); `go build ./auto-search/...` succeeds.
- [x] Step 2.4: Update the single call-site in `auto-search/internal/indexdb/indexer.go:298-315` (`insertMessageFromParquet`) — add `r.ToolUseResultJSON,` as the final argument. **Verify**: `go build ./auto-search/...` succeeds; `grep -rn "InsertMessage(" auto-search/` returns only this one call-site (plus the definition).
- [x] Step 2.5: Extend `MessageRow` in `auto-search/internal/indexdb/query_messages.go:9-40` with `ToolUseResultJSON string` (after `SkillName`). Extend the `GetMessageByID` SELECT list with `tool_use_result_json,` (after `skill_name,`) and the Scan target list with `&m.ToolUseResultJSON,` (in matching position). **Verify**: `go build ./auto-search/...` succeeds; column order in SELECT matches Scan order exactly.
- [x] Step 2.6: Modify `newMessageDescribeCmd` in `auto-search/internal/cli/message.go:57-136` — after the existing map literal is built, conditionally insert the new key: `if msg.ToolUseResultJSON != "" { msgMap["toolUseResult"] = json.RawMessage(msg.ToolUseResultJSON) }`. (Refactor the map literal into a named variable `msgMap` if the existing structure is inline.) **Verify**: `go build ./auto-search/...` succeeds and `go vet ./auto-search/...` passes.
- [x] Step 2.7: Extend `auto-search/internal/indexdb/indexer_integration_test.go` — assert the SQLite messages table has the `tool_use_result_json` column via `PRAGMA table_info('messages')` or equivalent, and round-trip a fixture parquet row with envelope through to a `GetMessageByID` read. **Verify**: `go test ./auto-search/internal/indexdb/... -run TestFullBuildFromFixtures` passes (existing test should not break) and the new assertion passes.
- [x] Step 2.8: Create `auto-search/internal/cli/message_envelope_test.go` — index a small fixture with two AUQ messages (one envelope-bearing tool_result, one envelope-empty assistant tool_use), invoke `message describe` for each, assert the populated row's JSON output contains a structured `toolUseResult` object (not a quoted string) and the empty row's JSON output **does not** contain the `toolUseResult` key at all. **Verify**: `go test ./auto-search/internal/cli/... -run TestMessageDescribe_ToolUseResult` passes.
- [x] Step 2.9: Run full module tests + vet. **Verify**: `go test ./auto-search/... && go vet ./auto-search/...` exit 0.
- [x] Step 2.10: Commit: `feat(012): phase 2 - autosearch surfaces tool_use_result_json via message describe`

### Phase 3: E2E SQL test + live backfill (human acceptance)

- [x] Step 3.1: Create `auto-search/internal/indexdb/auq_e2e_test.go` — hand-author a fixture with 5 AUQ pairs (3 with `(Recommended)` options, 2 of which user picked; 1 row with `annotations.notes`). Build the parquet + SQLite index in `t.TempDir()`. Run a SQL query using `json_extract(tool_use_result_json, '$.answers')` joined with `json_extract` over `tool_input.questions[*].options[*].label`-with-suffix-match, and assert `(calls_with_rec=3, rec_picked=2)`. Also assert `json_extract_string(tool_use_result_json, '$.annotations."<Q>".notes')` returns the expected string for the annotated row. **Verify**: `go test ./auto-search/internal/indexdb/... -run TestRecommendedAcceptanceFromFixture` passes.
- [x] Step 3.2: Run full repo build + tests + vet across all sub-modules. **Verify**: `make build && make test && make lint` (or the project-specific equivalent) all exit 0.
- [x] Step 3.3: Human acceptance — backfill the live corpus. Run `autoetl run --full` against `~/.claude/projects`. Capture the row count from stderr. **Verify**: command exits 0; logged sessions count > 700 (matches preflight baseline).
- [x] Step 3.4: Human acceptance — re-index. Run `autosearch index`. The autosearch SchemaVersion mismatch (5 → 6) should auto-trigger a clean rebuild via the existing `isDirty` path. **Verify**: command exits 0; logged messages count ≥ 77,000.
- [x] Step 3.5: Human acceptance — AC-3 query. Run `duckdb -c "SELECT COUNT(*) FROM read_parquet('~/.auto/etl/output/messages/year=*/week=*/messages.parquet') WHERE tool_use_result_json != ''"`. **Verify**: returns ≥ 261.
- [x] Step 3.6: Human acceptance — AC-4 query. Run `duckdb -c "DESCRIBE SELECT * FROM read_parquet('~/.auto/etl/output/messages/year=2026/week=*/messages.parquet')"`. **Verify**: column list is identical across all weeks (no NULL-filled missing columns from week=11 drift).
- [x] Step 3.7: Human acceptance — AC-7 SQL query. Run the corpus-wide recommended-acceptance query from `docs/research/askuserquestion-analytics.md` against the SQLite index using `json_extract(tool_use_result_json, '$.answers')`. **Verify**: returns numbers consistent with the 55.7% baseline (within ±5pp; the corpus may have grown by the time backfill runs).
- [x] Step 3.8: Commit: `test(012): phase 3 - e2e SQL test for recommended-acceptance via tool_use_result_json` (human-acceptance verification results captured in commit body).

### Phase 4: Documentation closeout

- [x] Step 4.1: Update `auto-etl/docs/reference/normalized-schema.md` — add a row for `tool_use_result_json` (type STRING, populated on `role=tool` rows when JSONL carries the envelope) and update the documented `SchemaVersion` to `3`. **Verify**: doc renders cleanly (no broken table layout); the column row sits next to `skill_name`.
- [x] Step 4.2: Update `auto-etl/docs/claude-message-types-and-etl-mapping.md` around lines 120, 375, 439-457 — replace the regex Q5 example with a `json_extract(tool_use_result_json, '$.answers')` example; update the "Not searchable" entry to clarify that AUQ answers are now queryable via SQL `json_extract` over the new column (not via FTS — that's still unchanged). **Verify**: every Q5 regex example referenced in the investigation report has been replaced; Q1–Q4 examples remain untouched.
- [x] Step 4.3: Update `docs/better-questions.md:117-125` — close out the "AUQ doesn't surface Q&A pairs" bullet by striking it through or rewriting it to reference the new `tool_use_result_json` column and `autosearch message describe`. **Verify**: the resolved item is clearly marked; the remaining open items in that section are untouched.
- [x] Step 4.4: Append a `## Postscript — landed in task 012` section to `docs/research/askuserquestion-analytics.md` (after line 342). Body: 5–10 lines pointing at (a) the new `tool_use_result_json` column on `messages` parquet, (b) the SQL `json_extract` pattern that supersedes the Q5 regex, (c) the `message describe` surface for per-row envelope inspection, (d) note that the investigation narrative above remains the frozen point-in-time record. **Verify**: original investigation body is byte-unchanged above the Postscript; new section uses the same heading style as the rest of the doc.
- [x] Step 4.5: Run `autodoc fix` (or whatever doc-index regenerator the project uses) so the doc index entries reflect any edits. **Verify**: `git diff` shows no unexpected changes to docs not in this task's scope.
- [x] Step 4.6: Commit: `docs(012): phase 4 - close out AUQ envelope docs after schema change`

## Success Criteria

- [x] All Phase 1–4 commits land in order; each is independently buildable and testable.
- [x] `go build ./...` succeeds at every commit boundary.
- [x] `go test ./...` passes at every commit boundary.
- [x] `go vet ./...` passes at every commit boundary.
- [x] All AC-1 through AC-11 test rows in [solution.md](./solution.md#test-coverage) have a passing test or a recorded human-acceptance result.
- [x] Corpus-wide `tool_use_result_json` row count ≥ 261 on the live corpus after backfill (AC-3).
- [x] Single column set across all weekly partitions after backfill — no week=11 drift (AC-4).
- [x] `autosearch message describe <id>` returns a parsed JSON envelope under the `toolUseResult` key for a known AUQ tool_result row, and omits the key for a row without the envelope (AC-6).
- [x] The 4 docs in Phase 4 are updated and committed together; `docs/research/askuserquestion-analytics.md` original narrative is preserved verbatim above the Postscript.

## Open Questions

(All resolved during requirements + solution + codex-review stages. See `requirements.md` Open Questions and the RESOLVED comment trails in both planning docs.)
