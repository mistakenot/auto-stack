---
hash: "fc8c7a78"
id: "59ff2cd3"
read_when: "implementing structured tool output storage in the auto-etl parquet schema"
summary: "Design for capturing tool use result envelopes as one raw JSON parquet column, with a message describe surface to retrieve it without FTS or typed AUQ fields."
title: "Solution: Task 012 — Structured Tool Output"
---

# Solution: Task 012 — Structured Tool Output

## Approach

The change adds **one** new raw JSON column to the parquet messages dataset, mirrors it in the SQLite index, and surfaces it through one CLI command. No FTS, no typed AUQ columns, no `MessageHit` projection changes (deferred to a future Phase-A task per the requirements' Out of Scope list).

<!-- RESOLVED(P1): MessageHit deferral does not satisfy AC-6
REVIEW: `requirements.md` AC-6 explicitly requires `MessageHit` to gain the parsed envelope and points to `auto-search/internal/search/messages.go:14-25,245-247,283-298`; the requirements Out of Scope excludes `MessageHit` carrying `tool_name`, not `toolUseResult`. This solution defers all `MessageHit` projection changes, so search-hit JSON would not meet the accepted surface. Either revise AC-6 to remove search hits, or add the MessageHit/SELECT/construction/test work here.
AUTHOR: Resolved upstream by revising `requirements.md` AC-6 to scope the surface to `message describe` (lookup by ID), which is the natural fit for a structured-envelope projection. `MessageHit` is now explicitly excluded — adding the envelope to every BM25-ranked search hit would balloon hit payloads for a query path nobody asked for. `requirements.md` Out of Scope updated to enumerate this exclusion (`MessageHit` carrying `tool_name` *or* `toolUseResult` is deferred).
-->

1. **Parser** — capture the `toolUseResult` field verbatim from each JSONL line into `rawLine` and `ParsedLine`. Stays empty on lines that don't carry it.
2. **Transform** — when handling a `tool_result` content block, copy the parsed `ToolUseResult` raw JSON onto the corresponding `AgentMessage.ToolUseResultJSON` field. Tool_use rows do not get the envelope — see [Ground-truth correction](#ground-truth-correction-to-ac-1) below.
3. **Model** — add `ToolUseResultJSON string \`parquet:"tool_use_result_json"\`` to `auto-etl/internal/model/model.go`'s `AgentMessage`. Bump `model.SchemaVersion` from 2 → 3.
4. **autosearch indexer mirror** — mirror the field on `auto-search/internal/model/parquet.go`'s `ParquetMessageRow`, add `tool_use_result_json TEXT NOT NULL DEFAULT ''` to the SQLite `messages` table, plumb through `InsertMessage` and `insertMessageFromParquet`. Bump `indexdb.SchemaVersion` from 5 → 6 (forces a clean rebuild on next `autosearch index`).
5. **MessageRow** — add `ToolUseResultJSON string` to `auto-search/internal/indexdb/query_messages.go`'s `MessageRow`, extend the `GetMessageByID` SELECT and Scan.
6. **`message describe`** — add `"toolUseResult": <parsed JSON value>` to the JSON output map at `auto-search/internal/cli/message.go:107-125`. When the column is non-empty, parse it via `json.RawMessage` and let `encoding/json` re-emit it as structured JSON (not as a quoted string). When empty, omit the key entirely.

<!-- RESOLVED(P2): CLI surface conflicts with requirements
REVIEW: The requirements and AC-11(d) say `message get` emits JSON-mode output with `toolUseResult`, while the existing contract says `message get` returns full raw text (`auto-search/internal/cli/message.go:48-50`, `auto-search/docs/requirements.md:432-438`) and JSON metadata belongs to `message describe`. The solution's `message describe` target is probably the cleaner fit, but the requirements/test rows must be updated to match or implementation will fail the stated ACs.
AUTHOR: Resolved upstream: `requirements.md` AC-6 and AC-11(d) now name `message describe` as the surface. `message get` remains a raw-content dump (no change to its existing contract). The test coverage table here already named `message describe` correctly; no edit needed.
-->

7. **Backfill** — human-acceptance step run by the implementer before the task is closed (matches the requirements decision: "ship code + run --full as part of acceptance"). Two commands, in order: `autoetl run --full` against `~/.claude/projects` to reprocess all weekly partitions under the new schema (also reconciles the week=11 drift in one go), then `autosearch index` against `~/.auto/etl/output` — the autosearch schema-version bump (5 → 6) triggers a full rebuild automatically via the existing schema-mismatch path, so no extra flag is needed. AC-3 / AC-4 are verified by running the documented DuckDB queries against the live corpus after the backfill completes.

<!-- RESOLVED(P2): Backfill step is not executable as written
REVIEW: `autosearch index --rebuild` is not a supported flag in `auto-search/internal/cli/index.go`; the command only exposes `--name`, `--input`, and `--key`, and a schema-version mismatch triggers a full rebuild through plain `autosearch index`. This step also says AC-3/AC-4 are verified post-merge, while `requirements.md` marks live `autoetl run --full` on this machine as acceptance. Decide whether the implementer must run the live backfill before closing, and replace the nonexistent command.
AUTHOR: Both points fixed. `--rebuild` removed; plain `autosearch index` is correct because the SQLite `SchemaVersion` bump 5 → 6 already triggers the existing schema-mismatch rebuild path. Framing changed from "post-merge operator step" to "human-acceptance step run by the implementer before closing" to match the requirements decision.
-->

8. **Documentation** — update four docs: the normalized-schema reference, the claude-message-types-and-etl-mapping doc, `docs/better-questions.md`, and `docs/research/askuserquestion-analytics.md`. The research doc is a frozen investigation snapshot, so the update is **append-only**: add a `## Postscript — landed in task 012` section at the bottom pointing readers to the new `json_extract` query patterns and to the live reference doc, without rewriting the original narrative or query examples. The other three docs are updated in place (in the mapping doc, the Q5 regex examples are replaced with `json_extract` against `tool_use_result_json`).

<!-- RESOLVED(P2): Documentation list misses a required doc
REVIEW: `requirements.md` AC-10 includes `docs/research/askuserquestion-analytics.md` because it contains the regex workaround examples that this task supersedes. The solution's documentation step and Files list omit that file, so the doc closeout is incomplete unless AC-10 is revised to keep the research report immutable.
AUTHOR: Added `docs/research/askuserquestion-analytics.md` to the documentation step and Files list. To preserve the historical value of the investigation snapshot (it's a frozen point-in-time record with corpus baselines), the update is append-only — a Postscript section pointing to the new query patterns, no in-place rewrite of the original narrative. The other three docs (in-place updates) are unchanged from the original solution.
-->

### Ground-truth correction to AC-1

Inspection of live JSONL (`/home/vscode/.claude/projects/-home-vscode-src-auto-stack/011dfac8-*.jsonl`) confirms the `toolUseResult` field appears **only on JSONL lines with `type: "user"`** — i.e. the line carrying the `tool_result` content block. The assistant tool_use line does not carry it. AC-1's parenthetical "(assistant tool_use rows and tool result rows produced by deferred tools)" overstates the surface; in the normalized schema, only `role=tool` messages will have a non-empty `tool_use_result_json`. The structured request side (questions/options) is already present in `tool_input` on the assistant tool_use row, which is sufficient — no envelope duplication needed. AC-1 should be read with this clarification; the implementation populates only `role=tool` rows.

### Bare-bones code outlines

```go
// auto-etl/internal/parser/parser.go

type rawLine struct {
    // ...existing fields...
    Message        rawMessage      `json:"message"`
    ToolUseResult  json.RawMessage `json:"toolUseResult"` // NEW: sibling of message
}

type ParsedLine struct {
    // ...existing fields...
    Message       ParsedMessage
    ToolUseResult json.RawMessage // NEW
}

// In ParseSession's per-line loop:
parsed := ParsedLine{
    // ...
    Message:       ParsedMessage{ /* ...as before... */ },
    ToolUseResult: line.ToolUseResult,
}
```

```go
// auto-etl/internal/transform/transform.go (case "tool_result")

case "tool_result":
    msg.Role = string(model.RoleTool)
    // ...existing meta lookup...
    if len(line.ToolUseResult) > 0 {
        msg.ToolUseResultJSON = string(line.ToolUseResult) // NEW
    }
    // ...existing content handling...
```

```go
// auto-etl/internal/model/model.go

const SchemaVersion = 3 // bumped from 2

type AgentMessage struct {
    // ...existing fields, between SkillName and InputTokens...
    SkillName          string `parquet:"skill_name,dict"`
    ToolUseResultJSON  string `parquet:"tool_use_result_json"` // NEW
    InputTokens        int32  `parquet:"input_tokens"`
    // ...
}
```

<!-- RESOLVED(P2): Nullability contract is unresolved
REVIEW: AC-1 specifies a nullable parquet column that is null for lines without an envelope, but this outline uses a plain Go `string`, and the SQLite DDL below uses `TEXT NOT NULL DEFAULT ''`. Existing optional message fields often use empty strings, so that may be the right local pattern, but the AC/test expectations must be changed to empty-string/omitted-output semantics or the implementation must use nullable representations consistently.
AUTHOR: Resolved upstream. AC-1 now specifies empty-string sentinel (no nullability), which is the existing convention for every optional Go-string column in `AgentMessage` (`bash_command`, `skill_name`, `tool_file_path`, etc.). AC-11(b) updated to assert the assistant tool_use row's `tool_use_result_json` is the empty string. AC-11(d) updated to assert `message describe` omits the `toolUseResult` key entirely when the column is empty (so the JSON consumer sees a clean absence, not an empty string). Parquet `string`, SQLite `TEXT NOT NULL DEFAULT ''`, Go `string` — all aligned.
-->

```go
// auto-search/internal/model/parquet.go

type ParquetMessageRow struct {
    // ...existing fields...
    SkillName         string `parquet:"skill_name,dict"`
    ToolUseResultJSON string `parquet:"tool_use_result_json"` // NEW
    InputTokens       int32  `parquet:"input_tokens"`
    // ...
}
```

```sql
-- auto-search/internal/indexdb/schema.go (messages table)
CREATE TABLE IF NOT EXISTS messages (
  -- ...existing columns...
  skill_name TEXT NOT NULL,
  tool_use_result_json TEXT NOT NULL DEFAULT '',  -- NEW
  input_tokens INTEGER NOT NULL,
  -- ...
);
```

```go
// auto-search/internal/indexdb/schema.go
const SchemaVersion = 6 // bumped from 5
```

```go
// auto-search/internal/indexdb/messages.go
// InsertMessage signature gains one trailing param:
func InsertMessage(tx *sql.Tx, partitionSourcePath string,
    /* ...existing 28 params... */,
    sourceLineIndex, schemaVersion int,
    toolUseResultJSON string, // NEW (trailing)
) error { ... }
```

The signature is already long; this task adds one positional arg in the trailing position to keep the diff minimal. A refactor to a struct parameter is a separate cleanup (see Rejected Alternatives).

```go
// auto-search/internal/indexdb/query_messages.go
type MessageRow struct {
    // ...existing fields...
    ToolUseResultJSON string // NEW
}
// SELECT and Scan extended to include tool_use_result_json.
```

```go
// auto-search/internal/cli/message.go (in newMessageDescribeCmd)
msgMap := map[string]any{
    "id":              msg.MessageID,
    // ...existing keys...
    "skillName":       msg.SkillName,
    "preview":         preview,
    // NEW: include parsed envelope when present
}
if msg.ToolUseResultJSON != "" {
    msgMap["toolUseResult"] = json.RawMessage(msg.ToolUseResultJSON)
}
// ...prev/next/sessionFirstAt/sessionLastAt keys appended after...
```

## Files

```
~ auto-etl/internal/parser/parser.go                          # add ToolUseResult to rawLine and ParsedLine; populate in ParseSession
~ auto-etl/internal/parser/parser_test.go                     # cover envelope capture from a fixture line
~ auto-etl/internal/transform/transform.go                    # write ToolUseResultJSON in the tool_result case
~ auto-etl/internal/transform/transform_test.go               # cover envelope propagation to AgentMessage
~ auto-etl/internal/model/model.go                            # add ToolUseResultJSON field; SchemaVersion 2 → 3
~ auto-search/internal/model/parquet.go                       # mirror ToolUseResultJSON on ParquetMessageRow
~ auto-search/internal/indexdb/schema.go                      # add tool_use_result_json column; SchemaVersion 5 → 6
~ auto-search/internal/indexdb/messages.go                    # InsertMessage gains toolUseResultJSON trailing param
~ auto-search/internal/indexdb/indexer.go                     # plumb through insertMessageFromParquet
~ auto-search/internal/indexdb/query_messages.go              # MessageRow.ToolUseResultJSON; extend SELECT + Scan
~ auto-search/internal/indexdb/indexer_integration_test.go    # round-trip parquet → SQLite assertion for the new column
~ auto-search/internal/cli/message.go                         # message describe emits parsed toolUseResult when populated
+ auto-search/internal/cli/message_envelope_test.go           # cover describe JSON output with and without envelope
+ auto-search/internal/indexdb/auq_e2e_test.go                # SQL-only recommended-acceptance computation against fixture
~ auto-etl/docs/reference/normalized-schema.md                # document tool_use_result_json column and SchemaVersion 3
~ auto-etl/docs/claude-message-types-and-etl-mapping.md       # replace regex Q5 examples with json_extract over new column
~ docs/better-questions.md                                    # close out the AUQ envelope open item
~ docs/research/askuserquestion-analytics.md                  # append Postscript section noting json_extract patterns landed in task 012
```

## Test Coverage

| AC | Test Type | File |
|----|-----------|------|
| AC-1 | unit | `auto-etl/internal/parser/parser_test.go` — envelope captured verbatim from a fixture JSONL line carrying a `toolUseResult` with `answers` and `annotations` |
| AC-1 | unit | `auto-etl/internal/transform/transform_test.go` — envelope propagates to `AgentMessage.ToolUseResultJSON` on the `role=tool` row; byte-identical to source |
| AC-2 | unit | `auto-etl/internal/model/model_test.go` (existing or new) — `SchemaVersion == 3` |
| AC-2 | manual / doc | `auto-etl/docs/reference/normalized-schema.md` updated; verified by reviewer |
| AC-3 | manual (operator) | Post-merge: `autoetl run --full && duckdb -c "SELECT COUNT(*) FROM read_parquet('...messages/year=*/week=*/messages.parquet') WHERE tool_use_result_json != ''"` returns ≥ 261 |
| AC-4 | manual (operator) | Post-merge: `duckdb -c "DESCRIBE SELECT * FROM read_parquet('...week=*/messages.parquet')"` returns a single column set across all weeks |
| AC-5 | integration | `auto-search/internal/indexdb/indexer_integration_test.go` — extends existing fixture with a row that has `tool_use_result_json`; asserts SQLite round-trip; asserts FTS table unchanged (no new column in `messages_fts` PRAGMA) |
| AC-6 | integration | `auto-search/internal/cli/message_envelope_test.go` — `message describe <id>` JSON output contains a parsed `toolUseResult` object (not a quoted string) when the row has it; key absent when empty |
| AC-7 | end-to-end | `auto-search/internal/indexdb/auq_e2e_test.go` — hand-authored fixture with 5 AUQ pairs (3 with `(Recommended)` option, 2 of which user picked); SQL with `json_extract(tool_use_result_json, '$.answers')` plus a join against `tool_input` returns `(calls_with_rec=3, rec_picked=2)` |
| AC-8 | end-to-end | Same `auq_e2e_test.go` — one fixture row carries `annotations` with per-question notes; `json_extract_string(tool_use_result_json, '$.annotations."<question>".notes')` returns the expected string |
| AC-9 | integration | Existing `indexer_integration_test.go`, `query_messages_test.go`, search/CLI tests — all pass without assertion changes other than the documented column-set additions |
| AC-9 | unit | New regression test: golden snapshot of `content_truncated` for an AUQ tool_use row before/after this change is byte-identical (proves `renderAskUserQuestion` markdown unchanged) |
| AC-10 | manual | Doc diff: regex Q5 example in `claude-message-types-and-etl-mapping.md` replaced with `json_extract` example; `docs/better-questions.md` open-item line struck through |
| AC-11(a) | unit | Parser test — see AC-1 row |
| AC-11(b) | unit | Transform test — see AC-1 row |
| AC-11(c) | integration | Indexer integration — see AC-5 row |
| AC-11(d) | integration | `message describe` test — see AC-6 row |
| AC-11(e) | end-to-end | `auq_e2e_test.go` recommended-acceptance — see AC-7 row |
| AC-11(f) | regression | `content_truncated` golden snapshot — see AC-9 row |

## Out of Scope

(Copied from requirements; technical boundaries added.)

- **Typed AUQ columns.** Deferred per investigation decision.
- **CLI ergonomics from investigation Phase A** — `message get` empty-content fallback (T1), `--tool-name` on `autosearch search` (T6), `MessageHit` carrying `tool_name` (T2), `<agent>` tag rename (T7), `--field` flag rename (T5-small). Belongs in a separate task. **Technical implication:** this task does not modify `MessageHit` or its SELECT — AC-6 covers only `MessageRow` and `message describe`. Search-hit JSON output remains unchanged.
- **DuckDB cookbook doc.** Lives alongside but does not gate this task.
- **FTS-indexing of `tool_use_result_json`.** Column carries structured JSON.
- **Cross-tool typed columns.**
- **Live `--full` run in CI.** Operator step; PR ships code + fixture-backed tests.
- **`renderAskUserQuestion` markdown changes.** `content_truncated` rendering unchanged; AC-9 regression test enforces this.

## Rejected Alternatives

- **Refactor `InsertMessage` to take a struct parameter instead of 30 positional args.** Cleaner long-term but doubles the diff and touches every call site (including tests). Defer to a separate cleanup task; this task adds one trailing positional arg to keep the schema-change PR focused.
- **Store the envelope as a parquet struct/map type instead of a serialized JSON string.** Native typing would catch a class of bugs at write time, but it (a) locks the column schema to whatever Claude emits today — any envelope field addition triggers cross-partition column drift exactly like the `improve/duration-tooling/*` situation this task already has to clean up, and (b) DuckDB JSON functions still work better against a string column than a struct column with `STRUCT_EXTRACT`. STRING is the right default for opaque JSON.
- **Populate `tool_use_result_json` on the assistant tool_use row too, by copying it back from the matched tool_result.** Adds complexity to `buildToolUseIndex` and duplicates the same JSON on two rows. The assistant row already has the structured request in `tool_input`; the only value the envelope adds is the response side, which lives naturally on the `role=tool` row.
- **Add a `JSON` SQLite column type.** SQLite has no actual JSON column type — `JSON` is a type-affinity hint that stores as TEXT. No functional difference, slight risk that some clients treat it specially. Stick with `TEXT`.
- **FTS-index the envelope.** Tempting for searching across user annotation notes by text, but the FTS5 table already covers `content_truncated`, the column would balloon the FTS index for a niche query path, and the structured access (`json_extract`) is the better surface for the only known analytics workload. Revisit only if a text-search use case emerges.
- **Forgo the autoetl `SchemaVersion` bump and rely on detection by column presence.** Skipping the bump means partitions written by the old and new binaries can coexist undetected. The whole reason the week=11 drift hurts is that the abandoned `improve/duration-tooling/*` work never bumped the published version. Bumping to 3 makes mixed-version partitions a first-class signal.
- **Pick `SchemaVersion = 4 or 5` to leapfrog the abandoned bump.** The abandoned bump never landed in any released binary or merged partition. Treating it as if it didn't happen and using `3` keeps the numbering clean for future readers.
- **Skip the regression snapshot test for `renderAskUserQuestion` markdown.** It costs ~20 lines but prevents an entire class of silent breakage during this PR's transform.go changes. Cheap; keep it.
