# Task 012: Structured Tool Output

<!-- REJECTED(P1): Planning docs are incomplete
REVIEW: This task folder currently contains only `requirements.md` and `solution.md`; `context.md` and `plan.md` are absent. The review workflow expects all four planning docs, and without `plan.md` there is no executable phase sequence, command list, or success-criteria mapping for AC-1 through AC-11. Add the missing docs before implementation starts.
AUTHOR: Expected at this stage. The workflow is `/new-task` → `/new-solution` → `/new-plan` → `/commit-task`. `context.md` and `plan.md` are produced by `/new-plan`, which is the next step after this review. Codex's `review-task` was invoked on the post-solution state; the missing docs are not a defect, they're the next stage's output.
-->

## Problem

`auto-etl` reads only the `message` field of each Claude JSONL line and discards the sibling `toolUseResult` envelope at `auto-etl/internal/parser/parser.go:67-84`. For deferred tools like `AskUserQuestion`, that envelope is the only structured source of the user's actual answers, per-question annotations/notes, and the full original questions/options array. Downstream consumers (autosearch, ad-hoc DuckDB analytics) currently have to regex-parse three different locale-sensitive prose templates inside `tool_result.content` to recover what the agent already had structured — and the per-question annotation key is lost entirely in that flat-text representation.

See `docs/research/askuserquestion-analytics.md` for the full investigation that motivated this task.

## Why This Matters

The five target metrics for AskUserQuestion analytics (call frequency, question text, options offered, recommended option, picked option) need to be cheap to query so we can iterate on Claude's question-asking behaviour against latent user intent. Today, four of the five are queryable via `json_extract` over the existing `tool_input` column; only the picked/recommended-match join requires regex over prose, and per-question free-text notes are not queryable at all. Capturing the raw `toolUseResult` envelope in one column makes every deferred-tool result structured and unblocks the analytics surface for AUQ and every future deferred tool — without committing to AUQ-specific typed columns until measured query pressure warrants them.

## Goals

- Capture the JSONL `toolUseResult` envelope verbatim into the normalized `messages` parquet as a single new column.
- Mirror the column on the SQLite index so autosearch can read it without parsing raw parquet.
- Make AUQ picked/recommended-match and per-question annotation notes answerable from autosearch — without changing the existing `tool_input` schema or breaking JSON-mode consumers.
- Reprocess the full historical corpus under the new schema so analytics work end-to-end across the existing 6 months of data.
- Resolve the week=11 partition schema drift (`tool_use_id`, `duration_ms`, `interrupted` columns from abandoned branch) opportunistically while the door is open.
- Net effect: a one-line DuckDB or autosearch query returns picked-vs-recommended for every AUQ call across the corpus; future deferred tools get free analytics surface from the same column.

## Acceptance Criteria

**AC-1**: `toolUseResult` envelope is parsed and stored verbatim
- Given: a Claude JSONL line of `type: "user"` containing a `toolUseResult` field (sibling of `message`) — i.e. the line that carries a `tool_result` content block
- When: `autoetl run` transforms that line
- Then: the raw envelope is preserved as a single JSON-text column on the corresponding `role=tool` row of the messages parquet (column name `tool_use_result_json`, type `STRING`). The column is populated for every `role=tool` row whose source JSONL line carries the envelope, and is the empty string for rows whose source line does not. The stored bytes are byte-for-byte the JSON found in the JSONL — no re-marshaling, no key reordering, no field whitelisting. Assistant `tool_use` rows do not carry the envelope (the JSONL does not include it there); their structured request side is already exposed via the existing `tool_input` column.

<!-- RESOLVED(P1): AC-1 requires rows the solution will not populate
REVIEW: AC-1, AC-3, and AC-11 require `tool_use_result_json` on assistant `tool_use` rows, but `solution.md` says only `role=tool` rows are populated because live JSONL carries `toolUseResult` on `type:"user"` tool-result lines. I verified sampled `toolUseResult` lines under `/home/vscode/.claude/projects/-home-vscode-src-auto-stack/` are `type:"user"` lines. Resolve by either changing the acceptance/test expectations to tool-result rows only, or explicitly requiring copy-back onto the assistant row.
AUTHOR: Confirmed against live JSONL — envelope only appears on `type: "user"` lines. AC-1 narrowed to populate envelope on `role=tool` rows only. AC-3 and AC-11(a) updated to match. Out of Scope additionally calls out that copying the envelope onto assistant `tool_use` rows is explicitly rejected (separate Rejected Alternative in solution.md).
-->

**AC-2**: Schema version bump
- Given: `auto-etl/internal/model/model.go` defines `SchemaVersion`
- When: this task lands
- Then: `SchemaVersion` is bumped from 2 → 3 (decision recorded in Open Questions). The reference doc `auto-etl/docs/reference/normalized-schema.md` (or its current equivalent) is updated to describe `tool_use_result_json` and the bump. The autosearch index `SchemaVersion` (currently 5) is bumped in lockstep so the existing schema-version-mismatch path triggers a clean index rebuild on next `autosearch index`. Drift detection / reporting on legacy autoetl partitions (e.g. a `autoetl doctor` partition-drift scan) is **not** part of this task — see `docs/research/askuserquestion-analytics.md` Phase E for the decoupled governance work; here, the operator's `autoetl run --full` re-transform (AC-3) is the migration path.

<!-- RESOLVED(P1): Old partition detection is not designed
REVIEW: AC-2 requires old-schema partitions to be detected and visibly reported by `autoetl run`, but the solution only bumps `model.SchemaVersion` and updates docs. Current session output skips existing historical partitions silently in `auto-etl/internal/writer/writer.go:20,33-35,50-52` unless `--full` removes the output directory in `auto-etl/cmd/run.go:73-78`, so a normal run after this schema bump can leave old partitions in place with no report. Add an explicit scan/report step or remove this acceptance requirement.
AUTHOR: Softened AC-2. Drift detection was overreach for this task; the research doc explicitly lists `autoetl doctor` partition-drift scan as Phase E governance, decoupled from the schema change (F3 in the findings report). The schema-version bump alone is the signal; operator-driven `autoetl run --full` (AC-3) is the documented migration path. Partition detection becomes its own future task.
-->

**AC-3**: Full re-transform repopulates historical corpus
- Given: 6 months of partitions exist under `~/.auto/etl/output/messages/year=YYYY/week=WW/`
- When: a user runs `autoetl run --full` after this task lands
- Then: every partition is reprocessed under the new schema and ends up with `tool_use_result_json` populated wherever the raw JSONL carries it. The expected backfill on the current corpus is ≥ 261 `role=tool` rows for AskUserQuestion alone (one per matched AUQ call); the exact number is informational, but a zero-count outcome must fail an end-to-end test.

**AC-4**: Week=11 partition schema drift is resolved
- Given: weekly partitions under `year=2026/week=11/` (and any other partitions identified during implementation) carry `tool_use_id`, `duration_ms`, `interrupted` columns from the abandoned `improve/duration-tooling/*` work that newer partitions lack
- When: the Phase C full re-transform runs
- Then: those drift partitions are either reprocessed cleanly under the new schema (preferred — they end up with the same column set as every other partition) or explicitly removed and rebuilt. After the task lands, `duckdb -c "DESCRIBE SELECT * FROM read_parquet('.../messages/year=2026/week=*/messages.parquet')"` returns a single consistent column set across every week.

**AC-5**: Autosearch index mirrors the new column
- Given: `autosearch index` reads parquet and writes the SQLite index defined in `auto-search/internal/indexdb/schema.go`
- When: this task lands
- Then: `messages.tool_use_result_json TEXT` exists on the SQLite messages table and is populated by the indexer from the parquet column of the same name. The column is **not** added to `messages_fts` — it is a structured-data column, not a search-target. Existing FTS behaviour is unchanged.

**AC-6**: `message describe` exposes the new column
- Given: `autosearch message describe <id>` is invoked against a row that has `tool_use_result_json` populated
- When: results are emitted in JSON mode (the only mode `message describe` supports)
- Then: the output JSON includes a `toolUseResult` key whose value is the parsed JSON envelope (not a quoted string). When the column is empty (no envelope on the source line), the key is omitted entirely. The change is scoped to `MessageRow` (`auto-search/internal/indexdb/query_messages.go`) and the `message describe` command (`auto-search/internal/cli/message.go:107-125`). `MessageHit` (search-hit projection at `auto-search/internal/search/messages.go:14-25`) is **not** modified — adding envelope JSON to every search hit is high-cost / low-value (a hit is a BM25-ranked snippet, not a full record fetch), and stays out of scope per the Out of Scope list. `message get` remains a raw-content dump (`message.go:48-50`) and is not modified here either.

**AC-7**: AUQ picked/recommended is queryable end-to-end from autosearch
- Given: the SQLite index is populated with `tool_use_result_json`
- When: a consumer wants to compute the corpus-wide recommended-acceptance rate
- Then: it can be done with a single SQL statement against the SQLite index (no DuckDB, no regex) — for example by `json_extract(tool_use_result_json, '$.answers')` and comparing against `tool_input.questions[*].options[*].label` where label ends with " (Recommended)". The exact query is a solution-stage concern; the AC is that it works and returns numbers consistent with the corpus-wide baseline in `docs/research/askuserquestion-analytics.md` (55.7% on the current corpus, ≈ 34/61 AUQ calls offering a recommendation).

**AC-8**: Per-question annotation notes are queryable
- Given: an AUQ result whose user added a free-text annotation via the `Other` / notes field on at least one question
- When: a consumer queries `tool_use_result_json` for that row
- Then: `$.annotations[<question text>].notes` returns the user's free text, keyed by the original question text. This is the signal that was lost entirely in the flat-text prose representation; it MUST be recoverable from the indexed data after this task.

**AC-9**: No regression in existing parquet / SQLite / CLI consumers
- Given: every existing `autoetl` and `autosearch` test
- When: this task lands
- Then: all of them pass without modification of the assertions other than (a) tests that explicitly check the parquet/SQLite column set may add `tool_use_result_json` as an expected column, and (b) tests that snapshot full search-hit JSON may include the new field. No removal or rename of any existing column, no change to `tool_input` shape, no change to `content`/`content_truncated` behaviour, no change to FTS query semantics.

**AC-10**: Documentation closeout
- Given: existing docs flag the AUQ envelope gap as open (`docs/better-questions.md:117-125`, `auto-etl/docs/claude-message-types-and-etl-mapping.md:120,375,439-457`) and `docs/research/askuserquestion-analytics.md` describes the regex workarounds
- When: this task lands
- Then: those docs are updated to (a) describe the new column and its shape, (b) replace the regex-over-prose query examples for Q5 / per-question notes with `json_extract` examples against `tool_use_result_json`, (c) leave Q1–Q4 query examples unchanged where they already use the existing `tool_input` JSON. The DuckDB cookbook (planned as a separate doc in `auto-etl/docs/`) is out of scope for this task but the underlying queries must work against the new column.

**AC-11**: Test coverage
- Given: the new column, schema bump, and CLI surface
- When: this task lands
- Then: (a) a parser unit test asserts `toolUseResult` is captured verbatim from a representative JSONL line (a `type: "user"` line carrying a `tool_result` content block); (b) a transform unit test asserts the envelope ends up on the corresponding `role=tool` parquet row with byte-identical JSON, and asserts the assistant `tool_use` row in the same fixture has an empty `tool_use_result_json`; (c) an indexer unit or integration test asserts the column round-trips parquet → SQLite without loss; (d) a `message describe` integration test asserts the JSON-mode output includes the parsed envelope under the `toolUseResult` key when populated, and omits the key when empty; (e) an end-to-end test computes the recommended-acceptance rate via SQL over the SQLite index against a fixture and asserts a known value (uses a small hand-authored fixture, not the live corpus); (f) a regression test confirms `tool_input` / `content` / `content_truncated` / FTS behaviour is unchanged for AUQ rows (compare against pre-task golden output).

## Out of Scope

- **Typed AUQ columns** (`auq_recommended_label`, `auq_picked_label`, `auq_outcome`, `auq_user_notes`, `auq_question_count`, `auq_multi_select`, `auq_picked_matches_recommended`). Deferred per investigation decision (#3 in `docs/research/askuserquestion-analytics.md`). Re-evaluate only if measured DuckDB / SQLite `json_extract` query latency on the analytics workload proves insufficient.
- **CLI ergonomics from investigation Phase A** (`message get` empty-content fallback, `--tool-name` on `autosearch search`, `MessageHit` carrying `tool_name` *or* `toolUseResult`, `<agent>`-tag rename for AUQ rows, `--field` flag rename). These are valuable independently and should be a separate task; bundling them here muddies the schema-change PR. Specifically: surfacing the new envelope on search hits (`MessageHit`) is out of scope — `message describe` (lookup by ID, AC-6) is the surface this task delivers.
- **Copying the envelope onto assistant `tool_use` rows.** The JSONL carries `toolUseResult` only on `type:"user"` lines. The structured request side (questions/options) is already in `tool_input` on the assistant row, so no copy-back is needed; see Rejected Alternatives in `solution.md`.
- **DuckDB cookbook doc** (`auto-etl/docs/auq-analytics.md`). Phase B in the investigation plan; lives alongside this task but doesn't gate it.
- **FTS-indexing of `tool_use_result_json`**. The column carries structured JSON, not natural-language prose; full-text-indexing it would bloat the FTS table with no analytics benefit. If a future need emerges (e.g. searching across user annotation notes by text), it's a follow-on task.
- **Cross-tool schema typing.** The new column is universal across deferred tools (ToolSearch, AskUserQuestion, future tools); per-tool typed columns are not introduced.
- **Live re-run of `autoetl run --full` as part of CI / release.** The task ships the code; the operator decides when to run the full backfill. The end-to-end test in AC-11 uses a fixture, not the live corpus.
- **Rewriting `transform.go`'s `renderAskUserQuestion` markdown output** (`content_truncated` rendering). The existing markdown is unchanged; consumers continue to read it where they read it today.

## Open Questions

- [x] **Column name.** `tool_use_result_json` (explicit; matches existing `tool_input` convention of "raw JSON in a TEXT/STRING column").
- [x] **Parquet column type.** `STRING` carrying serialized JSON. Robust against future field additions in Claude's envelope; matches what DuckDB `json_extract` expects.
- [x] **SQLite column type.** `TEXT`. Use JSON1 extension functions at query time.
- [x] **Schema version.** Bump from `2` to `3`. Treat the abandoned `improve/duration-tooling/*` branches as not having landed (they didn't).
- [x] **JSON output field name on `MessageHit` / `message get`.** `toolUseResult` (camelCase, matches the JSONL source field). Parsed JSON value (not raw string) in the output.
- [x] **Backfill scope.** Acceptance includes running `autoetl run --full` on this machine end-to-end. AC-3 covers the live corpus assertion; the week=11 schema drift is resolved by the same run (AC-4).
- [x] **Text-mode rendering of envelope.** JSON-only for now. No new renderer; existing markdown rendering in `content_truncated` remains the human-readable surface.
