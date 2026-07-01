---
hash: "8f02c388"
id: "1c01932d"
read_when: "investigating auto-etl data quality, Session/Message schema invariants, or considering formal model verification for the ETL pipeline"
summary: "Alloy 6.2.0 structural conformance spike validating Session/Message invariants in auto-etl, with five concrete edge cases found and ETL-probe evidence."
title: "Tech Spike Report: Alloy Core Data Model Conformance"
---

# Tech Spike Report: Alloy Core Data Model Conformance

**Date:** 2026-07-01
**Scope:** Lightweight Alloy model of the normalized `Session` and `Message` data shape used by `auto-etl`.
**Verdict:** CONDITIONAL - Alloy was useful and found concrete edge cases worth hardening, but this should stay a small validation/doctor effort rather than a full formal spec.

## Context

The spike used the HASLab structural Alloy style: model the domain with signatures, facts, generated instances, and assertion checks. Alloy 6.2.0 was run from the self-contained jar in `.tmp/`; no global installation was needed.

References:

- HASLab, [Structural design with Alloy](https://haslab.github.io/formal-software-design/structural-design/index.html)
- AlloyTools, [download page](https://alloytools.org/download.html)
- AlloyTools 6.2.0 Maven metadata, [org.alloytools.alloy.dist](https://central.sonatype.com/artifact/org.alloytools/org.alloytools.alloy.dist)

## Assumptions Tested

| # | Assumption | Result | Confidence |
|---|------------|--------|------------|
| 1 | The current `Session`/`Message` schema can be expressed as a small structural Alloy model. | VALIDATED | High |
| 2 | Every persisted `Session` has at least one normalized `Message`. | INVALIDATED | High |
| 3 | Every subagent `Session` parent points to an existing parent `Session`. | INVALIDATED | High |
| 4 | Every tool-result `Message` has exactly one matching assistant tool-use `Message`. | INVALIDATED | High |
| 5 | Tool-use IDs are unique within a `Session`. | INVALIDATED | High |
| 6 | Persisted `Message` rows always have non-zero timestamps and valid time partitions. | INVALIDATED | High |
| 7 | The role vocabulary is closed to `user`, `assistant`, `tool`, `system`, `thinking`. | VALIDATED in model | Medium |

## Detailed Observations

### 1. Alloy fit the problem

**Result:** VALIDATED

The model in `.tmp/spike-001-alloy-core-model/core_model.als` encoded the core normalized shape:

- `Session` fields: `sid`, `host`, `workspace`, `gitRemote`, `model`, `isSubagent`, `parent`, `firstAt`, `lastAt`
- `Message` fields: `session`, denormalized `host/workspace/gitRemote/model`, `role`, `timestamp`, `idx`, `toolUseID`, and simplified content/tool metadata flags
- facts for implemented behavior such as denormalized fields and `Session.ID + index` uniqueness
- assertions for desirable consumer-facing invariants

Command:

```bash
java -jar .tmp/spike-001-alloy-core-model/org.alloytools.alloy.dist-6.2.0.jar \
  exec -f -t json \
  -o .tmp/spike-001-alloy-core-model/alloy-output \
  .tmp/spike-001-alloy-core-model/core_model.als
```

Alloy output:

```text
run example                              SAT
check EverySessionHasAMessage            SAT
check EverySubagentParentExists          SAT
check EveryToolResultHasOneUse           SAT
check ToolUseIDsUniqueWithinSession      SAT
check MessagesHaveNonZeroTimestamps      SAT
check RoleVocabularyClosed               UNSAT
```

For Alloy `check`, `SAT` means a counterexample exists. `UNSAT` means no counterexample was found in the scope.

### 2. Persisted Sessions can have zero Messages

**Result:** INVALIDATED

Alloy found a counterexample to `EverySessionHasAMessage`. The ETL probe confirmed it.

Probe fixture: `.tmp/spike-001-alloy-core-model/fixtures/projects/empty-normalized/session.jsonl`

ETL output:

```text
transformed: 6 messages, 5 sessions
```

DuckDB evidence:

```text
id                message_count
empty-normalized  0
```

Relevant code:

- `auto-etl/internal/transform/transform.go:88` skips only when `FirstMessageAt == 0`
- `auto-etl/internal/transform/transform.go:214` skips empty message content
- `auto-etl/internal/transform/transform.go:406` derives `FirstMessageAt` from any raw line timestamp

Implication: downstream analytics that treat `Session has many Messages` as non-empty can silently get a `Session` row with no joinable `Message` rows.

### 3. Subagent parent links can be orphaned

**Result:** INVALIDATED

Alloy found a subagent `Session` whose `parent` value does not match any parent `Session`. The ETL probe confirmed this can happen when the input contains a subagent file without the parent file.

Probe fixture: `.tmp/spike-001-alloy-core-model/fixtures/projects/orphan-subagent/subagents/agent-child123.jsonl`

DuckDB evidence:

```text
id        parent_session_id
child123  parent-missing
```

Relevant code:

- `auto-etl/internal/parser/parser.go:278` sets subagent `ID = agentId`
- `auto-etl/internal/parser/parser.go:281` sets `ParentSessionID = rawSessionID`
- there is no batch-level check that `rawSessionID` exists as a parent `Session` row

Implication: session-tree queries can produce partial trees without a diagnostic.

### 4. Tool results can be unpaired

**Result:** INVALIDATED

Alloy found a `tool` Message with a `toolUseID` but no matching assistant tool-use Message. The ETL probe confirmed this shape.

Probe fixture: `.tmp/spike-001-alloy-core-model/fixtures/projects/unpaired-tool/session.jsonl`

DuckDB evidence:

```text
id              session_id    tool_use_id  tool_name
tool-session-0  tool-session  missing-use
```

Relevant code:

- `auto-etl/internal/transform/transform.go:322` handles `tool_result`
- `auto-etl/internal/transform/transform.go:324` reads `meta := toolUseIdx[block.ToolUseID]`
- if the key is absent, Go returns a zero-value `toolUseMeta`; the row is still emitted

Implication: consumers joining tool calls by `tool_use_id` can silently drop results, and `tool_name`/file metadata can be blank even when `role = tool`.

### 5. Duplicate tool-use IDs are last-wins

**Result:** INVALIDATED

Alloy found duplicate assistant tool-use Messages in one `Session`. The ETL probe confirmed duplicate keys are emitted and the result metadata uses the last originator.

Probe fixture: `.tmp/spike-001-alloy-core-model/fixtures/projects/duplicate-tool-use/session.jsonl`

DuckDB evidence:

```text
session_id          tool_use_id  assistant_tool_use_rows
duplicate-tool-use  dup-use      2
```

Detailed rows:

```text
id                    role       index  tool_use_id  tool_file_path  content
duplicate-tool-use-0  assistant  0      dup-use      /first.txt
duplicate-tool-use-1  assistant  1      dup-use      /second.txt
duplicate-tool-use-2  tool       2      dup-use      /second.txt     duplicate result
```

Relevant code:

- `auto-etl/internal/transform/transform.go:108` builds a `map[string]toolUseMeta`
- `auto-etl/internal/transform/transform.go:142` assigns `idx[b.ID] = m`, so duplicate IDs overwrite earlier metadata

Implication: duplicate source IDs cause deterministic but silent last-wins metadata. This is exactly the kind of conformance problem a model/check plus a small fixture catches well.

### 6. Timestamp-zero Messages can create `year=0/week=00` partitions

**Result:** INVALIDATED

Alloy found a `Message` with `ZeroTimestamp`. The ETL probe confirmed it can be written when one raw line has a real timestamp and another normalized message line has no timestamp.

Probe fixture: `.tmp/spike-001-alloy-core-model/fixtures/projects/zero-message-timestamp/session.jsonl`

ETL write output:

```text
wrote .../messages/year=0/week=00/messages.parquet (1 rows)
```

DuckDB evidence:

```text
id                        session_id              role  timestamp  year  week
zero-message-timestamp-0  zero-message-timestamp  user  0          0     00
```

Relevant code:

- `auto-etl/internal/transform/transform.go:202` converts the line timestamp to milliseconds
- `auto-etl/internal/transform/transform.go:406` lets a different raw line establish the `Session` time
- `auto-etl/internal/writer/writer.go:69` partitions `Message` rows directly from `Message.Year` and `Message.Week`

Implication: bad or partial source timestamps can create invalid time partitions that look like historical immutable data.

## Aggregate Probe Evidence

The synthetic ETL probe produced one concrete row for each risky shape:

```text
check_name              rows
orphan_subagent         1
empty_session           1
unpaired_tool_result    1
zero_timestamp_message  1
duplicate_tool_use_id   1
```

## Risks Identified

- Session-tree reads can silently omit parent context for subagents.
- Message analytics can miss zero-message Sessions unless every query uses an outer join.
- Tool-call analytics can miscount or drop rows when source pairing is malformed.
- Duplicate tool-use IDs are accepted and resolved by map overwrite, not surfaced as data-quality errors.
- Timestamp-zero Messages can create `year=0/week=00` partitions and confuse incremental partition semantics.

## Recommendations

1. Add a small `auto etl doctor` or transform validation pass that emits structured validation errors for these five invariants.
2. Treat zero normalized Messages as either a skipped Session or an explicit validation error; do not silently write the `Session` row.
3. Validate tool pairing with a `map[string][]toolUseMeta` so missing originators and duplicate originators are distinguishable.
4. Prevent `year=0/week=00` writes. Either skip timestamp-zero Messages with a validation error, or deliberately inherit a safe timestamp with a provenance flag.
5. For subagent parent links, decide whether partial ingestion is allowed. If allowed, surface orphan parents in `doctor`; if not, fail fast or quarantine the subagent row.
6. Keep the Alloy model as a spike artifact for now. If this becomes useful, move a cleaned-up version into `docs/experiments/` and wire a small reproduction script.

## Appendix: Reproduction

Prerequisites used:

```bash
java -version
duckdb --version
```

Alloy version:

```bash
java -jar .tmp/spike-001-alloy-core-model/org.alloytools.alloy.dist-6.2.0.jar version
# 6.2.0
```

Run Alloy:

```bash
java -jar .tmp/spike-001-alloy-core-model/org.alloytools.alloy.dist-6.2.0.jar \
  exec -f -t json \
  -o .tmp/spike-001-alloy-core-model/alloy-output \
  .tmp/spike-001-alloy-core-model/core_model.als
```

Run the ETL probe:

```bash
cd auto-etl
go run . run \
  --input ../.tmp/spike-001-alloy-core-model/fixtures/projects \
  --output ../.tmp/spike-001-alloy-core-model/etl-output \
  --only sessions \
  --full
```

Summarize problematic rows:

```bash
duckdb -c "
WITH
  sessions AS (
    SELECT * FROM read_parquet('../.tmp/spike-001-alloy-core-model/etl-output/sessions/year=*/month=*/sessions.parquet')
  ),
  messages AS (
    SELECT * FROM read_parquet('../.tmp/spike-001-alloy-core-model/etl-output/messages/year=*/week=*/messages.parquet')
  )
SELECT 'orphan_subagent' AS check_name,
       (SELECT count(*) FROM sessions s LEFT JOIN sessions p ON s.parent_session_id = p.id WHERE s.is_subagent AND p.id IS NULL) AS rows
UNION ALL SELECT 'empty_session',
       (SELECT count(*) FROM (SELECT s.id FROM sessions s LEFT JOIN messages m ON m.session_id = s.id GROUP BY s.id HAVING count(m.id) = 0))
UNION ALL SELECT 'unpaired_tool_result',
       (SELECT count(*) FROM messages r LEFT JOIN messages u ON u.session_id = r.session_id AND u.role = 'assistant' AND u.tool_use_id = r.tool_use_id WHERE r.role = 'tool' AND u.id IS NULL)
UNION ALL SELECT 'zero_timestamp_message',
       (SELECT count(*) FROM messages WHERE timestamp = 0 OR year = 0 OR week = 0)
UNION ALL SELECT 'duplicate_tool_use_id',
       (SELECT count(*) FROM (SELECT session_id, tool_use_id FROM messages WHERE role = 'assistant' AND tool_use_id != '' GROUP BY session_id, tool_use_id HAVING count(*) > 1));
"
```
