# Context: Task 012 — Structured Tool Output

Code-level context for implementing the `tool_use_result_json` column. See [solution.md](./solution.md) for the design rationale and [requirements.md](./requirements.md) for ACs.

## Key Files

### auto-etl parser (envelope capture)
- `auto-etl/internal/parser/parser.go:56-65` — `ContentBlock` struct (context only; unchanged)
- `auto-etl/internal/parser/parser.go:67-77` — `rawLine` struct. **Add** `ToolUseResult json.RawMessage \`json:"toolUseResult"\`` as sibling of `Message`.
- `auto-etl/internal/parser/parser.go:79-84` — `rawMessage` struct (unchanged).
- `auto-etl/internal/parser/parser.go:27-37` — `ParsedLine` struct. **Add** `ToolUseResult json.RawMessage` after `Message`.
- `auto-etl/internal/parser/parser.go:177-192` — `ParsedLine{...}` construction in `ParseSession` loop. **Add** `ToolUseResult: line.ToolUseResult,` to the literal.

### auto-etl transform (envelope propagation)
- `auto-etl/internal/transform/transform.go:105-143` — `buildToolUseIndex` (unchanged; pre-scans tool_use blocks).
- `auto-etl/internal/transform/transform.go:160-161` — loop binds `line := &raw.Lines[i]`, so `line.ToolUseResult` is in scope throughout the per-block switch.
- `auto-etl/internal/transform/transform.go:269-294` — `case "tool_result":` block. **Add** `if len(line.ToolUseResult) > 0 { msg.ToolUseResultJSON = string(line.ToolUseResult) }` after the metadata lookup (around line 277).
- `auto-etl/internal/transform/transform.go:259-261, 481-533` — `renderAskUserQuestion` (untouched; AC-9 regression test enforces unchanged `content_truncated` output).

### auto-etl model (schema)
- `auto-etl/internal/model/model.go:5` — `const SchemaVersion = 2`. **Bump** to `3`.
- `auto-etl/internal/model/model.go:23-63` — `AgentMessage` struct. **Insert** `ToolUseResultJSON string \`parquet:"tool_use_result_json"\`` between `SkillName` (line 44) and `InputTokens` (line 46).

### autosearch parquet mirror
- `auto-search/internal/model/parquet.go:36-73` — `ParquetMessageRow`. **Insert** `ToolUseResultJSON string \`parquet:"tool_use_result_json"\`` between `SkillName` (line 55) and `InputTokens` (line 57). Field order must match `AgentMessage`.

### autosearch SQLite schema
- `auto-search/internal/indexdb/schema.go:13` — `const SchemaVersion = 5`. **Bump** to `6` (triggers existing schema-mismatch rebuild path in `indexer.go`).
- `auto-search/internal/indexdb/schema.go:56-87` — `CREATE TABLE messages` DDL. **Add** `tool_use_result_json TEXT NOT NULL DEFAULT '',` after `skill_name TEXT NOT NULL,` (line 75).
- `auto-search/internal/indexdb/schema.go:117-125, 147-162` — `messages_fts` virtual table + FTS triggers. **Unchanged** — column is structured JSON, not natural-language prose (AC-5).

### autosearch InsertMessage (signature + INSERT statement)
- `auto-search/internal/indexdb/messages.go:9-24` — `InsertMessage` signature. **Add** `toolUseResultJSON string,` as the trailing parameter (after `schemaVersion int`).
- `auto-search/internal/indexdb/messages.go:29-40` — INSERT statement. **Add** `tool_use_result_json` to the column list (after `skill_name`) and `?` to the VALUES list. Total: 30 columns / 30 placeholders.
- `auto-search/internal/indexdb/messages.go:41-49` — Exec args. **Add** `toolUseResultJSON` as the final argument.

### autosearch indexer plumbing
- `auto-search/internal/indexdb/indexer.go:298-315` — `insertMessageFromParquet`. **Add** `r.ToolUseResultJSON,` as the final argument to the `InsertMessage` call.

### autosearch MessageRow (read path)
- `auto-search/internal/indexdb/query_messages.go:9-40` — `MessageRow` struct. **Add** `ToolUseResultJSON string` after `SkillName` (line 28).
- `auto-search/internal/indexdb/query_messages.go:43-74` — `GetMessageByID` SELECT + Scan. **Extend** the SELECT list with `tool_use_result_json,` (after `skill_name,`) and the Scan target list with `&m.ToolUseResultJSON,` (after `&m.SkillName,`). Must preserve column ↔ Scan ordering.

### autosearch CLI surface
- `auto-search/internal/cli/message.go:48-50` — `message get` (raw-content dump). **Unchanged** — task does not modify `message get`.
- `auto-search/internal/cli/message.go:57-136` — `newMessageDescribeCmd`. **Modify** the output map at lines 107-125: after `"skillName": msg.SkillName,`, conditionally insert `msgMap["toolUseResult"] = json.RawMessage(msg.ToolUseResultJSON)` only when the column is non-empty (empty → key omitted entirely).
- `auto-search/internal/search/messages.go:14-25,245-247,283-298` — `MessageHit` projection. **Unchanged** — adding the envelope to BM25 hits is explicit out-of-scope (AC-6 + Out of Scope).

### Live JSONL ground truth
- `~/.claude/projects/-home-vscode-src-auto-stack/011dfac8-*.jsonl` carries lines of the shape:
  ```json
  {"type":"user","message":{"role":"user","content":[{"type":"tool_result",...}]},"toolUseResult":{"questions":[...],"answers":{"<q text>":"<picked>"},"annotations":{"<q text>":{"notes":"..."}}},...}
  ```
- `toolUseResult` only appears on `type:"user"` lines (the tool_result-carrying line). Assistant `tool_use` lines do not carry it. Empty string is the sentinel on rows whose source line has no envelope.

### `--full` flag handling
- `auto-etl/cmd/run.go:73-78` — `--full` calls `os.RemoveAll(outputDir)` before re-running the pipeline. The implementer runs `autoetl run --full` after merging the code to backfill historical partitions (AC-3) and reconcile the week=11 drift (AC-4).

### Test files
- `auto-etl/internal/parser/parser_test.go` — extend with `TestParseSession_ToolUseResultEnvelope` reading a `testdata/` JSONL fixture carrying a `toolUseResult` envelope.
- `auto-etl/internal/transform/transform_test.go` — extend with a transform test asserting envelope lands on `role=tool` row byte-identically and assistant `tool_use` row has empty `ToolUseResultJSON`.
- `auto-search/internal/indexdb/indexer_integration_test.go` — extend with a parquet → SQLite round-trip assertion for the new column.
- `auto-search/internal/cli/message_envelope_test.go` — **new file** asserting `message describe` JSON output includes parsed `toolUseResult` when populated and omits the key when empty.
- `auto-search/internal/indexdb/auq_e2e_test.go` — **new file** computing recommended-acceptance via SQL `json_extract` against a hand-authored fixture.

## Patterns

### Column-addition end-to-end sequence (precedent: `skill_name` commit `ffc0df9`; `bash_exit_code` commit `41bf300`)

Files touched in order:
1. `auto-etl/internal/model/model.go` — add field + bump SchemaVersion
2. `auto-etl/internal/parser/parser.go` (only if the source field needs capturing)
3. `auto-etl/internal/transform/transform.go` — populate field
4. `auto-etl/internal/transform/transform_test.go` (and parser_test.go) — unit tests
5. `auto-search/internal/model/parquet.go` — mirror parquet struct
6. `auto-search/internal/indexdb/schema.go` — add SQLite column + bump SchemaVersion
7. `auto-search/internal/indexdb/messages.go` — extend `InsertMessage` signature + INSERT
8. `auto-search/internal/indexdb/indexer.go` — pass field to InsertMessage
9. `auto-search/internal/indexdb/query_messages.go` — extend `MessageRow`, SELECT, Scan
10. `auto-search/internal/cli/message.go` — surface in JSON output

This task follows the same sequence but **trails the new InsertMessage parameter** rather than inserting mid-signature (the precedents inserted mid-signature, which produces a noisier diff; the solution chose trailing per `solution.md:122-131`).

### Schema version bumps in lockstep
- autoetl `model.SchemaVersion 2 → 3` and autosearch `indexdb.SchemaVersion 5 → 6` are bumped in the **same change set** so that operators see exactly one rebuild trigger per task. The autosearch indexer's `isDirty` path (`auto-search/internal/indexdb/indexer.go` around the schema-mismatch check) handles the rebuild automatically once the bump lands.

### Optional Go-string columns use empty-string sentinel
- Every existing optional string column on `AgentMessage` (`bash_command`, `skill_name`, `tool_file_path`, …) uses `string` (Go zero value = `""`) with SQLite `TEXT NOT NULL DEFAULT ''`. The empty string is the "absent" signal. `tool_use_result_json` follows this convention (decision recorded in requirements Open Questions; conflict-resolution AUTHOR reply on the nullability comment).

### `message describe` output convention
- Add new JSON keys as conditional map inserts after the main map literal is built (precedent: `c20d354` added `toolUseId`/`durationMs`/`interrupted` to the same map). For optional fields, omit the key entirely when the source column is empty so JSON consumers see absence, not an empty placeholder.

### Documentation closeout convention
- The research investigation doc (`docs/research/askuserquestion-analytics.md`) is treated as a frozen snapshot — append-only `## Postscript` section, no in-place rewrite. Live reference docs (`auto-etl/docs/reference/normalized-schema.md`, `auto-etl/docs/claude-message-types-and-etl-mapping.md`, `docs/better-questions.md`) are updated in place.

### Project rules (from CLAUDE.md files)
- **Go build discipline** (root CLAUDE.md): run `go build ./...` after each Go edit; don't accumulate unbuilt files.
- **Immutable original data** (auto-etl/CLAUDE.md): store raw JSON verbatim; create new fields rather than transforming existing ones.
- **JSON default output** (root CLAUDE.md): `message describe` already emits JSON; new key carries parsed JSON (via `json.RawMessage`), not a quoted string.
- **Conventional commits**: `<type>(<scope>): <message>`; scope is task number for plan phases (e.g., `feat(012): phase 1 - …`).

## Related Tasks

- **`c20d354` — abandoned `improve/duration-tooling/*`**: tried to capture `duration_ms`/`interrupted`/`tool_use_id` from the same `toolUseResult` envelope but never landed. Bumped `model.SchemaVersion 3 → 4` and `indexdb.SchemaVersion 6 → 7`. Left orphaned columns in week=11 partitions — the schema drift task 012 has to reconcile (AC-4). Task 012's lean approach (one raw column, one schema bump) supersedes that branch's per-field approach.
- **`a7564af` — abandoned `improve/autosearch/3`**: added `--tool` filter to `search` and `stats` with tests. Adjacent to this task (out of scope for 012; planned as separate Phase A forward-port per the investigation doc).
- **Task 010 — autosearch co-change**: precedent for plan structure with multi-phase commit cadence and hand-authored fixture-backed E2E tests. Showed that `--full` against the real `~/.auto` is acceptable as a human-acceptance step (not a CI step).
- **`ffc0df9` — skill_name column**: closest analogue to this task's threading shape (parquet model → SQLite schema → InsertMessage → MessageRow → describe). Inserted the new parameter mid-signature; task 012 deliberately chooses trailing position to minimize diff.
- **`41bf300` — bash_exit_code**: same pattern as `skill_name`, with the addition of a regex-based extraction in parser/transform. Task 012 needs no extraction — it stores raw JSON.
