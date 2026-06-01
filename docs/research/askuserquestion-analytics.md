---
hash: ""
id: "d3da9d0c"
read_when: "investigating AskUserQuestion analytics, planning ETL schema changes for structured tool envelopes, or scoping autosearch CLI work around tool filtering"
summary: "How AskUserQuestion data flows through the auto-etl / auto-search pipeline, where the structured payload is lost, and a phased plan to surface the five target analytics metrics (frequency, question text, options, recommended option, picked option) for tuning Claude's question-asking against latent user intent."
title: "AskUserQuestion Analytics — Pipeline Investigation"
---

# AskUserQuestion Analytics — Pipeline Investigation

## Motivation

Claude's `AskUserQuestion` tool is the canonical mechanism for the agent to escalate ambiguity to the human. Each call carries a list of questions, a list of options per question (one of which may be flagged `(Recommended)`), and the user's eventual pick. Optimizing the agent's use of this tool — making the recommended option converge on latent user intent — requires good observability.

Five target metrics define the analytics surface:

1. **Frequency** — how often is `AskUserQuestion` invoked, per session and over time?
2. **Question text** — what does the agent actually ask?
3. **Options** — what choices does it offer?
4. **Recommended** — which option (if any) is flagged as the recommendation?
5. **Picked** — which option did the user choose? Did it match the recommendation?

This investigation traced those five metrics end-to-end through the `auto-etl` → `auto-search` pipeline to identify where the signal is preserved, where it's mangled, and where it's lost.

**Scope:** investigation only. No code changes were made; the deliverable is this report plus a prioritized phased plan.

---

## Methodology

A six-phase multi-agent self-improvement workflow:

| Phase | Role | Output |
|-------|------|--------|
| 0 | Preflight | Refreshed `autoetl run` + `autosearch index`, confirmed corpus state (787 sessions, 77 065 messages, 141 sessions touching AUQ) |
| 1 | Explorer | Hands-on tool exploration + session-history mining → 7 structural findings (S1–S7) + 6 tactical findings (T1–T6) with file:line evidence |
| 2 | Analyst | Read cited source, traced root causes, collapsed the 7 structural findings into 3 underlying decisions; proposed fix directions and tactical effort estimates |
| 3 | Reviewer | Independently re-ran all queries, verified every file:line citation, surfaced critical analyst errors (see [Reviewer's corrections](#what-the-reviewer-caught-that-the-analyst-missed) below) |
| 4 | Consolidator | Merged into a unified findings report with phased plan |
| 5 | Implementation | **Skipped per scope** — investigation only |

Each phase ran as an isolated subagent with no shared context beyond the previous phase's written artifact, forcing independent verification.

---

## Findings

### Quantitative baseline

Measured corpus-wide on HEAD `f1171ca` against `~/.auto/etl/output/messages/year=2026/week=*/messages.parquet`:

| Metric | Value |
|--------|-------|
| Sessions in corpus | 787 |
| Messages in corpus | 77 065 |
| Sessions using `AskUserQuestion` | 76 |
| `AskUserQuestion` tool_use rows | 262 |
| Result rows (adjacency-matched) | 261 (1 orphan) |
| Result template — `User has answered…` | 205 |
| Result template — `Your questions have been answered…` | 43 |
| Result template — rejected (`doesn't want to proceed`) | 12 |
| Result template — `tool_use_error` | 2 |
| Calls offering a `(Recommended)` option | 61 |
| Recommended option picked | 34 |
| **Recommended-acceptance rate (corpus-wide)** | **55.7%** |

The corpus-wide 55.7% is the baseline to track. A separate local-project subset (the `auto-stack` repo's own sessions) measured 45/63 = 71%, reflecting this repo's authoring style; it is not representative.

### What works today (zero changes)

All five target metrics are *technically computable* on the current binary using DuckDB regex over parquet. This is the cookbook that should ship as immediate documentation.

```bash
# Q1 — usage frequency (already first-class)
autosearch stats --tool-name AskUserQuestion --group-by session_id
autosearch stats --tool-name AskUserQuestion --group-by week

# Q2 — question text (autosearch search filtered to AUQ requires Phase A; today: DuckDB)
duckdb -c "
SELECT session_id, index, content_truncated
FROM read_parquet('/home/vscode/.auto/etl/output/messages/year=2026/week=*/messages.parquet')
WHERE tool_name='AskUserQuestion' AND role='assistant' LIMIT 20"

# Q3 — options offered (JSON in tool_input)
duckdb -c "
SELECT json_extract_string(tool_input, '\$.questions[0].options[*].label') AS labels
FROM read_parquet('...messages.parquet')
WHERE tool_name='AskUserQuestion' AND role='assistant' LIMIT 5"

# Q4 — recommended option (label suffixed with literal ' (Recommended)')
duckdb -c "
SELECT regexp_extract(tool_input, '\"label\":\"([^\"]+) \\(Recommended\\)\"', 1) AS recommended
FROM read_parquet('...messages.parquet')
WHERE tool_name='AskUserQuestion' AND tool_input LIKE '%(Recommended)%'"

# Q5 — picked + match-rate (adjacency join: result is at use_idx + 1)
duckdb -c "
WITH uses AS (
  SELECT session_id, index AS u_idx,
    regexp_extract(tool_input, '\"label\":\"([^\"]+) \\(Recommended\\)\"', 1) AS recommended
  FROM read_parquet('...messages.parquet')
  WHERE tool_name='AskUserQuestion' AND role='assistant'),
results AS (
  SELECT session_id, index AS r_idx, content
  FROM read_parquet('...messages.parquet')
  WHERE tool_name='AskUserQuestion' AND role='tool'
    AND content LIKE 'User has answered%')
SELECT
  COUNT(*) FILTER (WHERE recommended <> '') AS calls_with_rec,
  COUNT(*) FILTER (WHERE recommended <> '' AND content LIKE '%=\"' || recommended || ' (Recommended)\"%') AS rec_picked
FROM uses u JOIN results r ON u.session_id=r.session_id AND r.r_idx = u.u_idx+1"
```

**Brittleness notes** for anyone shipping these as production queries:

- Result text comes in three prose templates plus error/abort variants. Q5 above only catches `User has answered…` (205 of 262 rows). A complete query must union all four templates.
- Quotes inside picked answers will break a naive `="..."` regex; results carry literal `\"` only in the JSON form, not the flat `content` field.
- The flat text appends ` user notes: <concatenated free text>` when the user annotated, but **loses the per-question key**. To get per-question notes, the raw JSONL `toolUseResult.annotations[question].notes` would be needed — which is precisely what F1 below proposes to capture.

### Structural findings (what's missing in the pipeline)

The 7 explorer findings collapse cleanly to **three underlying decisions in the code**.

#### F1 — `toolUseResult` envelope dropped at parser

**Severity: high. The single structural change worth making.**

Each Claude JSONL line carries a fully structured `toolUseResult` sibling alongside `message`. For `AskUserQuestion`, that envelope contains:

```json
{
  "toolUseResult": {
    "questions": [{ "question": "...", "options": [...] }],
    "answers": { "<question text>": "<picked label>" },
    "annotations": { "<question text>": { "notes": "<free text>" } }
  }
}
```

`auto-etl/internal/parser/parser.go:67-84` defines `rawLine` without any `ToolUseResult` field. The structured payload — including the only authoritative source for *per-question* free-text annotations — is discarded at the parse step. Every downstream consumer has to regex the locale-sensitive prose in `tool_result.content` to recover what the agent already had structured.

Code trace:
- `auto-etl/internal/parser/parser.go:67-84` — `rawLine` struct missing `ToolUseResult`
- `auto-etl/internal/parser/parser.go:177-192` — `ParsedLine` only carries `Message.*`
- `auto-etl/internal/transform/transform.go:269-294` — `tool_result` case reads `block.Content` only
- `auto-etl/internal/model/model.go:23-63` — parquet schema has generic `tool_input`/`content`; per-tool typed columns exist for Bash/Read/Write/Skill but not for AUQ

Evidence the payload exists in the raw JSONL but is absent from parquet:

```bash
grep -h '"toolUseResult".*"answers"' ~/.claude/projects/-home-vscode-src-auto-stack/*.jsonl | wc -l
# 111

duckdb -c "DESCRIBE SELECT * FROM read_parquet('...week=23/messages.parquet')" \
  | grep -iE 'answer|annotation|question'
# (no rows)
```

**Fix direction (lean):** add **one** new column `tool_use_result_json TEXT` to `AgentMessage`, populated verbatim from the JSONL envelope. DuckDB `json_extract` handles all five target metrics from that column at read time. Roughly 30 LoC across parser + transform + model + a `SchemaVersion` bump, and the change unlocks every deferred tool — not just AUQ.

The alternative (seven typed AUQ columns — `auq_recommended_label`, `auq_picked_label`, `auq_outcome`, `auq_user_notes`, etc.) is deferrable until a measured DuckDB-bottleneck appears.

#### F2 — `tool_use_id` parsed but not stored

**Severity: medium.**

`block.ToolUseID` is parsed at `parser.go:63` and used transiently by `buildToolUseIndex` (`transform.go:103-143`) but is never written to the parquet row. Downstream cannot rejoin use→result by canonical id; only an adjacency heuristic (`index = use_idx + 1`) is available. Misses 1 in 262 today, would silently mispair under any parallel-dispatch scenario.

**Fix direction:** add `tool_use_id` to `AgentMessage`. The abandoned branch `improve/duration-tooling/*` (commit `c20d354`) already wrote this column to older partitions; forward-port the column-write piece.

#### F3 — Parquet schema drift across partitions

**Severity: medium.**

Older partitions still carry `tool_use_id`, `duration_ms`, `interrupted` columns written by the un-merged `improve/duration-tooling/*` binary; newer partitions do not. DuckDB unions by name and inserts NULLs on cross-partition reads, so the regression is silent.

```bash
duckdb -c "DESCRIBE SELECT * FROM read_parquet('...week=11/messages.parquet')" | grep -E 'tool_use_id|duration|interrupted'
# all three present
duckdb -c "DESCRIBE SELECT * FROM read_parquet('...week=23/messages.parquet')" | grep -E 'tool_use_id|duration|interrupted'
# none
```

**Fix direction:** `autoetl doctor` partition-drift scan + documented schema-bump policy in `auto-etl/docs/reference/normalized-schema.md`. Immediate remediation is `rm` orphan partitions and rerun `autoetl --full`.

#### F4 — `--field tool_input` is a misnamed row filter

**Severity: medium.**

`messages_fts` indexes only `content_truncated, workspace, git_remote, model` (`auto-search/internal/indexdb/schema.go:107-125`). The CLI flag `--field tool_input` advertises field-targeted search but actually adds a SQL `WHERE tool_input != ''` pre-filter while leaving the FTS5 `MATCH` clause against `content_truncated` (`auto-search/internal/search/messages.go:200-211`). Users get matches inside Bash command bodies that mention the query string.

**Fix direction:** rename the flag values to make row-filter intent explicit (`--has-tool-input` / `--has-tool-output`). The large fix (FTS-indexing `tool_input`) is unnecessary because `renderAskUserQuestion` already populates `content_truncated` with the question/options markdown.

#### F5 — `MessageHit` narrow projection drops `tool_name`

**Severity: medium.**

`MessageHit` has no `ToolName` field (`auto-search/internal/search/messages.go:14-25`) and the SELECT (`messages.go:245-247`) doesn't fetch it. Search hits arrive with `tool_name: null` even though SQLite has the value. `message describe` also omits `toolInput` from its summary map (`auto-search/internal/cli/message.go:107-125`).

**Fix direction:** add `ToolName` + capped `ToolInputPreview` to the struct, SELECT, and construction. The abandoned branch `improve/autosearch/3` (commit `a7564af`) already does the `tool_name` half.

#### F6 — `autosearch search` missing `--tool-name` that `stats` has

**Severity: medium — already implemented on an unmerged branch.**

`--tool-name` exists end-to-end on `stats` (`auto-search/internal/cli/stats.go:25,60,89`) but not on `search` (`auto-search/internal/cli/search.go:108-122`). The only ways to list AUQ rows today are DuckDB-direct or `stats --group-by session_id` → `session get` per session.

**Critical prior art:** commit `a7564af` on `origin/improve/autosearch/3` ("feat(autosearch): add --tool filter to search and stats") implements this with a canonical tool-name set, `IN (?, ?, ...)` predicate, and tests. The work is not merged. The branch also carries a SchemaVersion 3→4 bump for duration-tooling fields; the `--tool` change is forward-portable in isolation.

#### F7 — `message get` / `session get` rendering gaps for AUQ

**Severity: medium (ergonomics, not analytics-blocking).**

Three related defects:

1. `autosearch message get <auq-tool-use-id>` prints nothing because `auto-search/internal/cli/message.go:48-50` writes only `msg.Content`, which is empty for tool_use rows. The sibling `session get` at `session.go:295-303` already has the right fallback.
2. `session get` wraps AUQ tool_use rows in `<agent index=N>` and drops the tool name because `roleTag` (`session.go:306-340`) keys off `Role` only. Grep for `AskUserQuestion` in a transcript sees only the result side.
3. The tool_use block body is raw JSON of `tool_input`, even though `ContentTruncated` already holds the rendered `renderAskUserQuestion` markdown.

**Fix direction:** scope the `ContentTruncated` fallback to `session get` only; do not change the shared `messageContent()` helper, or analytics consumers calling `message get` lose the structured JSON they actually want.

### Dropped from this round

**I7 — ToolSearch → AskUserQuestion deferred-tool dance.** The 4-message sequence (ToolSearch lookup → schema fetch → AskUserQuestion call → result) is not modelled in the normalized schema. Research curiosity, not on the path to any of the 5 target metrics. Backlog.

---

## What the reviewer caught that the analyst missed

The phase-3 reviewer ran every query independently and found four important corrections that reshaped the plan:

1. **All 5 metrics are answerable today** via DuckDB regex over parquet. The analyst framed F1 as a blocker; it's actually a quality-of-life upgrade. The cookbook is the cheapest first deliverable.
2. **Recommended-acceptance is 55.7% corpus-wide, not 71%.** The analyst's number was the local `auto-stack` project subset (45/63), which over-represents this repo's authoring style.
3. **F6 is already implemented.** Commit `a7564af` on `origin/improve/autosearch/3` lands `--tool` for `search` and `stats` with tests. Forward-port, don't greenfield.
4. **F1's lean shape is one column, not seven.** The reviewer proposed a single raw `tool_use_result_json TEXT` column; DuckDB `json_extract` handles all five metrics. The analyst's seven typed AUQ columns become optional optimization, not necessary infrastructure.

### Meta-observation

Branches `improve/duration-tooling/{1,2,3}` and `improve/autosearch/3` are correlated abandoned work: someone tried to ship structural improvements to AUQ analytics, bumped `SchemaVersion 3→4`, and the work stalled before merge. **F6's tactical fix is already written, with tests, on the second branch.** Mining those branches for landable subsets is worth half a day before any greenfield work begins.

---

## Phased plan

Decisions registered with the user at the close of the investigation:

| Decision | Choice |
|----------|--------|
| Phase A scope | Single PR for T1+T2+T6+T7+T5-small |
| Phase C retroactivity | Full re-run on all partitions with `autoetl --full` |
| F1 column shape | One raw column: `tool_use_result_json TEXT` |
| Immediate next step | Stop at the findings report |

### Phase A — Forward-port abandoned-branch CLI wins (1–2 hours, no schema change)

Single PR landing:

- **T1** — `message get` falls back to `tool_input` when `content` empty (mirror `session get`'s `messageContent()` at `session.go:295-303`)
- **T2** — Add `ToolName` + capped `ToolInputPreview` to `MessageHit`, SELECT, and construction (`messages.go:14-25, 245-247, 283-298`)
- **T6** — Forward-port `--tool-name` on `autosearch search` from commit `a7564af`; drop the SchemaVersion 3→4 ride-along
- **T7** — `session get` tags AUQ tool_use rows with the tool name; update golden fixtures
- **T5-small** — Rewrite `--field` flag help text and `quickstart.go:59` examples to reflect row-filter (not search-target) intent

Outcome: Q1 fully first-class, Q2 first-class via search, hits filterable to AUQ, transcripts greppable.

### Phase B — DuckDB cookbook (zero code)

Publish `auto-etl/docs/auq-analytics.md` with the five copy-paste queries above plus the brittleness notes. First user-runnable deliverable; also documents the regex fragility that motivates Phase C.

### Phase C — F1 schema change (~1 day)

- Add `ToolUseResult json.RawMessage` to `rawLine` and `ParsedLine`
- Add `tool_use_result_json TEXT` to `AgentMessage`; propagate through transform
- Bump `SchemaVersion` (resolves the dangling 3→4 ambiguity)
- Mirror the column on the SQLite index but **do not FTS-index it**
- Full `autoetl --full` re-run on all partitions; reconcile week=11 drift while the door is open

Outcome: Q4 and Q5 graduate from regex-over-prose to one-line `json_extract` SQL. Per-question `annotations.notes` becomes queryable.

### Phase D — Optional polish (defer)

- **T3** (`toolInput` on `message describe`), **T4** (`session get` AUQ markdown, narrowly scoped)
- Typed AUQ columns: only if Phase C measurements show DuckDB `json_extract` is too slow

### Phase E — Governance (decoupled)

- F3: `autoetl doctor` partition-drift scan + documented schema-bump policy

---

## What we learned (beyond the specific findings)

1. **Multi-phase verification catches large errors cheaply.** The reviewer's 4 corrections — including a 15-point swing in the headline acceptance-rate number and the existence of pre-written prior art — reshaped the entire phased plan. Single-pass analysis would have shipped wrong recommendations.

2. **Investigate before designing.** The instinct on day one was "schema change is required to compute these metrics." The reality after one DuckDB session: every metric is already computable. The schema change becomes a UX upgrade for production queries, not infrastructure for the analytics itself.

3. **The lean fix beat the elegant fix.** One raw JSON column versus seven typed columns: same query power, 7x less migration surface, generalizes to every deferred tool. Reach for the lean form first; type-expand on measured need.

4. **Abandoned branches are a code reservoir.** `improve/autosearch/3` and `improve/duration-tooling/*` together contained roughly half of Phase A's work, already tested. The pattern of looking at abandoned `improve/*` branches before designing a fresh PR is worth institutionalizing.

5. **Adjacency joins are surprisingly robust on this corpus** (1 orphan in 262 AUQ pairs ≈ 0.4%), but they will fail silently under any parallel-tool-call scenario. `tool_use_id` propagation (F2) is cheap insurance.

6. **The result-side prose format is the single biggest source of brittleness** for current analytics. Three templates plus error/abort variants, all locale-sensitive, all losing per-question keys. F1 eliminates the entire category.

---

## Appendix — File reference index

Source locations cited above, grouped by area:

**auto-etl parsing & schema**
- `auto-etl/internal/parser/parser.go:63` — `block.ToolUseID` parsed
- `auto-etl/internal/parser/parser.go:67-84` — `rawLine` missing `ToolUseResult`
- `auto-etl/internal/parser/parser.go:177-192` — `ParsedLine` construction
- `auto-etl/internal/transform/transform.go:103-143` — `buildToolUseIndex`
- `auto-etl/internal/transform/transform.go:259-261` — `renderAskUserQuestion` populates `ContentTruncated`
- `auto-etl/internal/transform/transform.go:269-294` — `tool_result` case reads `block.Content` only
- `auto-etl/internal/transform/transform.go:481-533` — `renderAskUserQuestion` body
- `auto-etl/internal/model/model.go:5` — `SchemaVersion = 2`
- `auto-etl/internal/model/model.go:23-63` — `AgentMessage` columns

**auto-search index & query surface**
- `auto-search/internal/indexdb/schema.go:107-125` — FTS5 column set
- `auto-search/internal/search/messages.go:14-25` — `MessageHit` struct
- `auto-search/internal/search/messages.go:178-211` — `preFilterConds`
- `auto-search/internal/search/messages.go:200-211` — `--field tool_input` misnamed pre-filter
- `auto-search/internal/search/messages.go:245-247` — SELECT misses `tool_name`
- `auto-search/internal/search/messages.go:283-298` — `MessageHit` construction
- `auto-search/internal/cli/search.go:108-122` — flag block missing `--tool-name`
- `auto-search/internal/cli/stats.go:25,60,89` — `--tool-name` plumbed for `stats`
- `auto-search/internal/cli/message.go:48-50` — `message get` empty-content gap
- `auto-search/internal/cli/message.go:107-125` — `describe` map missing `toolInput`
- `auto-search/internal/cli/session.go:295-303` — `messageContent()` fallback
- `auto-search/internal/cli/session.go:306-340` — `roleTag` drops tool name
- `auto-search/internal/cli/quickstart.go:59` — misleading `--field` example

**Abandoned branches with reusable code**
- `origin/improve/autosearch/3` commit `a7564af` — `--tool` flag for `search`/`stats` with tests
- `origin/improve/duration-tooling/{1,2,3}` commit `c20d354` — `tool_use_id`, `duration_ms`, `interrupted` columns

**Pre-existing open-issue docs**
- `docs/better-questions.md:117-125` — AUQ gaps flagged
- `auto-etl/docs/claude-message-types-and-etl-mapping.md:120,375,439-457` — message-type mapping
