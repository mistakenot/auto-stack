---
hash: "44764cf6"
id: "2714cfdb"
read_when: "implementing thinking block preservation or any of the dropped ETL signal fields in auto-etl and auto-search"
summary: "Phased implementation plan for preserving thinking blocks, stop_reason, is_error, cache token split, permission_mode, skill_name attribution, and other dropped ETL signals, with schema bumps and autosearch CLI opt-in for thinking content."
title: "Plan: ETL Preserve Session Signal (Task 016)"
---

# Plan: Task 016

## Summary

Stop the auto-etl transform from dropping session signal — preserve assistant
thinking blocks (new `thinking` role) plus `stop_reason`, `is_error`, the cache
split, session `permission_mode`/`version`, and `skill_name` from
`attributionSkill` — then surface thinking end-to-end in autosearch (opt-in),
following the dual-SchemaVersion-bump pattern of tasks 012/015.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| ~ | `auto-etl/internal/parser/parser.go` | Add `Thinking`/`Signature`/`Data` (ContentBlock), `StopReason` (rawMessage), `Version`/`PermissionMode`/`AttributionSkill` (rawLine); mirror to `Parsed*` + constructors |
| ~ | `auto-etl/internal/model/model.go` | `SchemaVersion 5→6`; `RoleThinking`; +5 message cols, +2 session cols |
| ~ | `auto-etl/internal/transform/transform.go` | `thinking` case; `is_error`; cache split; `stop_reason`; `skill_name` fallback; permission_mode/version pre-pass; exclude thinking from transcript |
| ~ | `auto-etl/internal/transform/transform_test.go` | Unit tests for each new field/role |
| ~ | `auto-etl/internal/parser/parser_test.go` | Unit tests for new parsed fields |
| ~ | `auto-etl/e2e_test.go` | `expectedMessages()` includes thinking; fixture with thinking + attributionSkill |
| ~ | `auto-search/internal/model/parquet.go` | Mirror new fields on `ParquetMessageRow`/`ParquetSessionRow` |
| ~ | `auto-search/internal/indexdb/schema.go` | `SchemaVersion 8→9`; messages + sessions columns |
| ~ | `auto-search/internal/indexdb/messages.go` | `InsertMessage` signature/columns/placeholders/args |
| ~ | `auto-search/internal/indexdb/sessions.go` | `InsertSession` signature/columns/placeholders/args |
| ~ | `auto-search/internal/indexdb/indexer.go` | `insert{Message,Session}FromParquet` wiring |
| ~ | `auto-search/internal/indexdb/query_sessions.go` | `SessionMessages` new-column scan + optional `role!='thinking'` |
| ~ | `auto-search/internal/search/messages.go` | `normalizeRole` accept `thinking`; `IncludeThinking` opt; default `role!='thinking'` (both builders) |
| ~ | `auto-search/internal/stats/validate.go` | `normalizeRole` (2nd copy): accept `thinking` so `stats --role thinking` works |
| ~ | `auto-search/internal/cli/search.go` | `--include-thinking` flag; `--role` help text |
| ~ | `auto-search/internal/cli/session.go` | `--include-thinking` flag on `session get` |
| ~ | `auto-search/internal/cli/quickstart.go` | Document `--role thinking` + `--include-thinking` |
| ~ | `auto-search/internal/search/messages_test.go` | `--role thinking`, opt-in default, `--skill` hit |
| ~ | `auto-search/internal/cli/session_test.go` | `--include-thinking` render of `<thinking>` |
| ~ | `auto-etl/docs/claude-message-types-and-etl-mapping.md` | Fix thinking claim; add new fields |
| ~ | `auto-etl/docs/reference/normalized-schema.md` | New columns + `thinking` role; fix stale version |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [ ] `auto-etl/internal/parser/parser_test.go` — parses thinking/signature/version/permissionMode/attributionSkill/stop_reason
- [ ] `auto-etl/internal/transform/transform_test.go` — thinking→`role=thinking`; attributionSkill→skill_name; cache split; stop_reason; is_error; thinking excluded from transcript
- [ ] `auto-etl/e2e_test.go` — `--full` transform of a thinking+attributionSkill fixture; role-count sum holds
- [ ] `auto-search/internal/search/messages_test.go` — `--role thinking` returns only thinking; default search excludes thinking; `--include-thinking` includes it; `--skill <name>` now hits
- [ ] `auto-search/internal/cli/session_test.go` — `session get --include-thinking` renders `<thinking index=N>`; default omits it; `message get` returns full thinking content

## Execution Sequence
```
Phase 1 (auto-etl) --> Phase 2 (auto-search index) --> Phase 3 (auto-search CLI + tests) --> Phase 4 (docs + rollout)
```
Strictly linear: Phase 2 mirrors the parquet columns Phase 1 defines; Phase 3 queries
what Phase 2 indexes; Phase 4 documents/backfills the finished pipeline. (Docs in Phase 4
have no code dependency and may overlap Phase 3 if parallelized during execution.)

## Plan

### Phase 1: auto-etl capture + transform + schema bump
- [x] Step 1.1: `parser.go` — add `Thinking`,`Signature`,`Data` to `ContentBlock`; `StopReason` to `rawMessage`; `Version`,`PermissionMode`,`AttributionSkill` to `rawLine`; mirror each onto `Parsed*` structs and copy in their constructors. Verify: `go build ./...` in auto-etl passes.
- [x] Step 1.2: `model.go` — bump `SchemaVersion 5→6`; add `RoleThinking MessageRole = "thinking"`; add message cols `thinking_signature`,`stop_reason`,`is_error`,`cache_creation_input_tokens`,`cache_read_input_tokens`; session cols `permission_mode`,`version`. Verify: `go build ./...` passes; `grep 'SchemaVersion = 6' model.go`.
- [x] Step 1.3: `transform.go` — replace `default: continue` with `case "thinking","redacted_thinking":` emitting `Role=RoleThinking`, `ContentTruncated`, output-byte accounting. For `thinking`: `Content=block.Thinking`, `ThinkingSignature=block.Signature`. For `redacted_thinking`: `Content="[redacted]"`, `ThinkingSignature=block.Data` (the column doubles as the opaque per-block blob). Verify: unit tests assert (a) a thinking block yields one `role="thinking"` message with reasoning text + signature, and (b) a `redacted_thinking` block yields one `role="thinking"` marker row with `[redacted]` content AND `thinking_signature` carrying the data.
- [x] Step 1.4: `transform.go` — in `tool_result` case set `msg.IsError = block.IsError`; beside both cache-sum lines (~212,230) set the two split columns (keep the sum); set `msg.StopReason` on assistant-line rows. Verify: unit tests assert is_error on an error tool_result, both cache split cols + preserved sum, and stop_reason on an assistant row.
- [x] Step 1.5: `transform.go` — `skill_name` fallback: on assistant-line rows with `attributionSkill` and no Skill-tool skill set, `msg.SkillName = line.AttributionSkill`. Verify: unit test — a `/review-task`-style line yields rows with `skill_name="review-task"`.
- [x] Step 1.6: `transform.go` — add a session-level pre-pass (mirroring `system/turn_duration` at ~174) reading the **top-level `rawLine.PermissionMode` / `rawLine.Version` fields off lines** (the scan loop decodes every line into `rawLine`, so this captures both message-line fields and any standalone `type:"permission-mode"` line — no `type==` switch); last-seen wins; **no `IsSubagent` gate** (env-level). Set `PermissionMode`/`Version` on the `AgentSession` literal. Exclude `role="thinking"` rows from `transcript_full`/`transcript_truncated`. Verify: unit test — session has `permission_mode`/`version` set and transcript contains no thinking text.
- [x] Step 1.7: Add/extend `parser_test.go` and `transform_test.go` for all of the above (raw-JSON `Message.Content` exercises the thinking switch case). Verify: `go test ./... ` in auto-etl passes.
- [x] Step 1.8: Update `e2e_test.go` — add a fixture session (runtime `t.TempDir()` preferred over checked-in testdata) containing a populated `thinking` block + an `attributionSkill` line; update `expectedMessages()` to count `ContentBlocksByType["thinking"]`; keep the role-count-sum invariant. Verify: `go test ./...` in auto-etl passes; thinking role appears in counts.
- [x] Step 1.9: Commit: `feat(016): phase 1 - auto-etl preserves thinking + dropped signal (schema 5→6)`

### Phase 2: auto-search index plumbing + schema bump
- [x] Step 2.1: `model/parquet.go` — add the 5 message + 2 session mirror fields with matching parquet tags to `ParquetMessageRow`/`ParquetSessionRow`. Verify: `go build ./...` in auto-search passes.
- [x] Step 2.2: `indexdb/schema.go` — bump `SchemaVersion 8→9`; add `thinking_signature`,`stop_reason`,`is_error`,`cache_creation_input_tokens`,`cache_read_input_tokens` to the `messages` DDL and `permission_mode`,`version` to `sessions` DDL (all `NOT NULL DEFAULT`). Verify: `grep 'SchemaVersion = 9'`; build passes.
- [x] Step 2.3: `indexdb/messages.go` + `sessions.go` — extend `InsertMessage`/`InsertSession` signatures, INSERT column lists, `?` placeholders, and Exec args for the new columns. **Verify: placeholder count == column count == arg count for each (state the numbers); `grep -rn 'InsertMessage(' && grep -rn 'InsertSession('` shows every call-site updated.**
- [x] Step 2.4: `indexdb/indexer.go` — pass the new parquet fields through `insertMessageFromParquet`/`insertSessionFromParquet`. Update the `SessionMessages` SELECT/scan in `query_sessions.go` for the new columns. Verify: `go build ./...` passes; both Insert call-sites (incl. integration test) compile.
- [x] Step 2.5: Round-trip test — index a fixture parquet (regenerate fixtures so they carry the new columns) and read a row back with the new fields populated (not empty). Verify: `go test ./internal/indexdb/...` passes; a thinking row is retrievable with non-empty `thinking_signature`.
- [x] Step 2.6: Commit: `feat(016): phase 2 - auto-search indexes new columns (index schema 8→9)`

### Phase 3: auto-search CLI surface + integration tests
- [x] Step 3.1: Add `thinking` to **both** `normalizeRole` copies —
  `search/messages.go:647` and `stats/validate.go:177` (switch + error text in each;
  `grep -rn normalizeRole auto-search/internal/` to confirm both). In
  `search/messages.go` also add `IncludeThinking` to `MessageSearchOpts`; when not
  include-thinking and not `--role thinking`, append `role != 'thinking'` in both the
  FTS and noFTS builders (and the session subquery). Verify: unit test — default
  message search excludes thinking; `--role thinking` returns only thinking;
  `--include-thinking` includes it; **`stats --role thinking` is accepted (not rejected)**.

<!-- RESOLVED(P2): Update BOTH normalizeRole copies (see solution.md P2)
REVIEW: There is a second independent `normalizeRole` at `internal/stats/validate.go:177`
(caller stats/validate.go:37) that also whitelists user|assistant|tool. This step only edits
the search/messages.go copy. Add `thinking` to the stats copy too (or scope stats out explicitly),
else `autosearch stats --role thinking` rejects the value. Run `grep -rn normalizeRole auto-search/internal/`
before editing to confirm both sites.
AUTHOR: Resolved — Step 3.1 now names both `normalizeRole` copies and adds a verify that
`stats --role thinking` is accepted; `stats/validate.go` is added to the Changes table. (Mirrors the
solution.md P2 resolution.)
-->

- [x] Step 3.2: `cli/search.go` — add `--include-thinking` bool, thread to opts; update `--role` help to include `thinking`. `cli/session.go` — add `--include-thinking` to `session get`, pass to `SessionMessages` (optional `role!='thinking'`). Verify: `autosearch search --role thinking` and `autosearch session get <id> --include-thinking` run; default `session get` omits `<thinking>`.
- [x] Step 3.3: Integration tests — `messages_test.go` (`--role thinking`, opt-in default, `--skill <name>` now returns the skill-attributed session), `session_test.go` (`--include-thinking` renders `<thinking index=N>…</thinking>`; default omits; `message get` returns full thinking content). Verify: `go test ./...` in auto-search passes.
- [x] Step 3.4: Run `make check` (fmt+vet+lint) + `go build ./...` + `go test ./...` across both modules. Verify: all exit 0.
- [x] Step 3.5: Commit: `feat(016): phase 3 - autosearch thinking surface (--role thinking, --include-thinking)`

### Phase 4: docs + rollout (rollout = no-commit human acceptance)
- [x] Step 4.1: `auto-etl/docs/claude-message-types-and-etl-mapping.md` — drop the disproven "Claude Code redacts it" claim; mark thinking actually preserved; add rows for `thinking_signature`,`stop_reason`,`permission_mode`,`version`,`is_error`,cache-split,`skill_name`. Verify: doc no longer lists these as "lost".
- [x] Step 4.2: `auto-etl/docs/reference/normalized-schema.md` — add new message/session columns, add `thinking` to the role enum, fix the stale `Current value`. `cli/quickstart.go` — document `--role thinking`, `--include-thinking`, and the opt-in default. Verify: `autosearch quickstart` output shows the new flags.
- [x] Step 4.3: Commit: `docs(016): phase 4 - update ETL mapping, schema reference, quickstart`
- [ ] Step 4.4: **Rollout (no commit)** — `cd auto-etl && go run . run --full --only sessions` then `cd auto-search && go run ./cmd/autosearch index`. Verify: index log reports a full rebuild (v9 mismatch); `autosearch search --role thinking --since 14d` returns thinking rows; `autosearch search --skill review-task` returns the `c9124cdf` session; `autosearch message get <thinking-id>` returns full reasoning.

## Success Criteria
- [ ] `go build ./...` passes in both `auto-etl` and `auto-search`
- [ ] `make check` (fmt+vet+lint) passes; `make test` passes
- [ ] AC-1: a session's thinking blocks become `role="thinking"` messages with full reasoning text + `thinking_signature`
- [ ] AC-2: thinking is excluded from default `search`/`session get`; reachable via `--role thinking`, `session get --include-thinking` (`<thinking index=N>`), and `message get` (full)
- [ ] AC-3: `skill_name` populated from `attributionSkill`; `autosearch search --skill review-task` returns the `c9124cdf` session (was 0 hits)
- [ ] AC-4: `stop_reason`, `is_error`, `cache_creation_input_tokens` + `cache_read_input_tokens` (combined sum kept), session `permission_mode`/`version` all populated
- [ ] AC-5: `SchemaVersion` etl 6 / index 9; `autoetl run --full --only sessions` + `autosearch index` rebuild cleanly
- [ ] AC-6: mapping doc, schema reference, and quickstart updated and consistent with code

## Open Questions
- (none — all four resolved in requirements.md)
