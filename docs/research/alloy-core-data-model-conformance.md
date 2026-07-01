---
hash: "019ee142"
id: "c6f4d02b"
read_when: "designing auto-etl validation, hardening Session/Message integrity, choosing when to use Alloy, or evaluating lightweight formal models for auto-stack data contracts"
summary: "Research artifact on using Alloy to model the core auto-etl Session and Message data contract, generate counterexamples, confirm five concrete conformance gaps with synthetic ETL fixtures, and identify Project areas where Alloy is or is not a good fit."
title: "Alloy Conformance Model for auto-etl Session Data"
---

# Alloy Conformance Model for auto-etl Session Data

**Date:** 2026-07-01
**Status:** Research artifact from a bounded tech spike
**Related artifact:** [`SPIKE-REPORT.md`](../../SPIKE-REPORT.md)

## Purpose

This artifact records a lightweight experiment using Alloy to check the structural integrity of the
core `auto-etl` normalized data model. The goal was not to create a complete formal specification.
The goal was to test whether a small relational model could expose invalid or surprising shapes in
the `Session` and `Message` datasets before those shapes become downstream assumptions in
`auto-search`, `auto-reflect`, or future analytics.

The experiment focused on the current normalized `Session` and `Message` schema:

- `Session`: one row per parsed coding agent Session file.
- `Message`: one row per normalized content block inside a Session.
- Subagent linkage through `parent_session_id`.
- Tool call linkage through `tool_use_id`.
- Time partitioning through `year`, `week`, and `month`.
- Denormalized fields copied from `Session` onto each `Message`.

## Why Alloy Was a Good Fit

Alloy is a relational modeling language. It is useful when the question is not "does this function
return the right value for this one input?" but "can any small universe of data satisfy these facts
while violating this invariant?"

That maps directly onto `auto-etl` data contracts. The normalized parquet datasets are relational:
`Message.session_id` points at `Session.id`, subagent Sessions point at parent Sessions, and
tool-result Messages point at tool-use Messages. Alloy can cheaply generate small example worlds
where those relationships hold or fail.

The spike followed the structural design approach from the HASLab Alloy material:

- define signatures for the core domain objects,
- add facts for behavior the system promises or currently implements,
- write assertions for properties consumers probably rely on,
- ask Alloy to find counterexamples.

References:

- HASLab, [Structural design with Alloy](https://haslab.github.io/formal-software-design/structural-design/index.html)
- AlloyTools, [download page](https://alloytools.org/download.html)
- AlloyTools 6.2.0, [Maven artifact](https://central.sonatype.com/artifact/org.alloytools/org.alloytools.alloy.dist)

## Method

The spike used Alloy 6.2.0 from a self-contained jar stored under `.tmp/`. The model has now been
promoted into `auto-etl` as the first product-owned Alloy model:

```text
auto-etl/alloy/session_message_conformance.als
```

The original spike copy remains under `.tmp/spike-001-alloy-core-model/core_model.als` only as
scratch evidence. The promoted model deliberately stays small. Strings, timestamps, and IDs are
modeled as opaque atoms rather than parsed scalar values. This keeps the model focused on
relationships and multiplicities:

- a `Message` belongs to exactly one `Session`;
- a `Message` has one role, with known roles modeled explicitly and future roles left open;
- a subagent `Session` has a parent ID;
- tool-use and tool-result Messages share a `tool_use_id`;
- each `Message` carries denormalized fields copied from its owning `Session`;
- each `Message` index is unique within a `Session`.

The original spike model checked assertions that downstream consumers would reasonably expect:

1. Every persisted `Session` has at least one `Message`.
2. Every subagent `Session` parent points to an existing parent `Session`.
3. Every tool-result `Message` has exactly one matching assistant tool-use `Message`.
4. Tool-use IDs are unique within a `Session`.
5. Persisted `Message` rows have non-zero timestamps.
6. The role vocabulary is closed to `user`, `assistant`, `tool`, `system`, and `thinking`.

After review, the promoted model was updated to capture the intended ETL policy rather than only
the original spike counterexamples:

- empty Sessions are allowed in canonical output;
- orphan Subagents should be ignored from canonical output and captured as a `DataError`;
- missing tool-use originators should be captured as a `DataError`;
- duplicate assistant tool-use IDs should be captured as a `DataError`;
- zero-timestamp Messages should not produce canonical `year=0/week=00` partitions;
- role vocabulary should remain open because agent roles may change.

This means the ETL run should continue through recoverable malformed source data. Valid
`Session`/`Message` rows should still be written, and recoverable data problems should be written to
a separate diagnostic collection rather than crashing the whole run.

Promoted model command:

```bash
cd auto-etl
./alloy/run.sh
```

Alloy output:

```text
run example                                      SAT
run emptySessionAllowed                          SAT
run emptySessionCanBeNoticed                     SAT
run orphanSubagentCapturedAsError                SAT
run missingToolUseCapturedAsError                SAT
run duplicateToolUseCapturedAsError              SAT
run zeroTimestampCapturedAsError                 SAT
run futureRoleAllowed                            SAT
check CanonicalSubagentsHaveParents              UNSAT
check CanonicalToolResultsHaveExactlyOneUse      UNSAT
check CanonicalToolUseIDsUniqueWithinSession     UNSAT
check CanonicalMessagesHaveNonZeroTimestamps     UNSAT
check DataErrorsHaveExpectedSeverity             UNSAT
```

For an Alloy `check`, `SAT` means Alloy found a counterexample. `UNSAT` means no counterexample was
found in the selected finite scope.

For an Alloy `run`, `SAT` means Alloy found an example world. In the promoted model, `SAT` run
commands show that the policy can represent allowed cases and recoverable errors. `UNSAT` check
commands show that Alloy could not find a counterexample to the canonical output contract within
the selected finite scope.

The Alloy counterexamples were then tested against the real Go ETL by creating small raw Session
fixtures under:

```text
.tmp/spike-001-alloy-core-model/fixtures/projects/
```

The ETL probe command was:

```bash
cd auto-etl
go run . run \
  --input ../.tmp/spike-001-alloy-core-model/fixtures/projects \
  --output ../.tmp/spike-001-alloy-core-model/etl-output \
  --only sessions \
  --full
```

The probe produced:

```text
transformed: 6 messages, 5 sessions
wrote .../messages/year=0/week=00/messages.parquet (1 rows)
```

DuckDB queries against the generated parquet confirmed that all five risky shapes were accepted by
the real transform.

## Observations

### Observation 1: A Session Can Be Persisted Without Messages

Alloy generated a counterexample where a `Session` exists with no `Message` rows. The ETL probe
confirmed this with a raw file containing only a timestamped `system / turn_duration` line.

Concrete output:

```text
id                message_count
empty-normalized  0
```

Relevant implementation behavior:

- `auto-etl/internal/transform/transform.go` skips a transformed Session only when
  `FirstMessageAt == 0`.
- `FirstMessageAt` is derived from any timestamped raw line, including metadata lines.
- Empty or non-message content can produce no normalized Messages while still leaving a timestamped
  Session row.

Risk: downstream consumers that assume `Session has many Messages` means "one or more" will silently
miss these Sessions unless every query uses an outer join.

### Observation 2: Subagent Parent Links Can Be Orphaned

Alloy generated a subagent `Session` whose parent ID did not resolve to any parent `Session`. The
ETL probe confirmed this with a subagent fixture whose parent file was absent.

Concrete output:

```text
id        parent_session_id
child123  parent-missing
```

Relevant implementation behavior:

- The parser assigns subagent `Session.ID` from `agentId`.
- The parser assigns `ParentSessionID` from the raw `sessionId`.
- There is no batch-level conformance check that the parent `Session` exists in the transformed
  output.

Risk: session-tree queries can return partial trees without any diagnostic, making subagent analysis
look complete when parent context is missing.

### Observation 3: Tool Results Can Be Emitted Without Tool Uses

Alloy generated a tool-result `Message` with a `tool_use_id` but no matching assistant tool-use
`Message`. The ETL probe confirmed this with a raw user line containing only a `tool_result` block.

Concrete output:

```text
id              session_id    tool_use_id  tool_name
tool-session-0  tool-session  missing-use
```

Relevant implementation behavior:

- The transform builds a map from tool-use ID to metadata.
- A tool-result block looks up metadata by `tool_use_id`.
- If no key exists, Go returns a zero-value metadata struct and the row is still emitted.

Risk: tool-call analytics can silently lose rows or produce blank tool metadata when joining by
`tool_use_id`.

### Observation 4: Duplicate Tool-Use IDs Are Accepted With Last-Wins Metadata

Alloy generated two assistant tool-use Messages in one Session with the same `tool_use_id`. The ETL
probe confirmed that the normalized output keeps both tool-use rows and attaches the result to the
last metadata entry.

Concrete output:

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

Relevant implementation behavior:

- The transform stores tool-use metadata in `map[string]toolUseMeta`.
- A repeated key overwrites the earlier entry.
- The normalized rows do not record that a duplicate occurred.

Risk: duplicate source IDs produce deterministic but silent last-wins metadata. A downstream query
sees plausible data, but the provenance is ambiguous.

### Observation 5: Timestamp-Zero Messages Can Create `year=0/week=00`

Alloy generated a `Message` with a zero timestamp. The ETL probe confirmed this can happen when one
raw line supplies a real Session timestamp and another normalized message line has no timestamp.

Concrete output:

```text
id                        session_id              role  timestamp  year  week
zero-message-timestamp-0  zero-message-timestamp  user  0          0     00
```

The writer then created:

```text
messages/year=0/week=00/messages.parquet
```

Relevant implementation behavior:

- The Session timestamp is computed from all raw lines.
- Each Message partition is computed from that Message line's timestamp.
- The writer groups Message rows directly by `Message.Year` and `Message.Week`.

Risk: partial or malformed timestamps can create invalid time partitions that look like normal
historical immutable data.

### Observation 6: The Role Vocabulary Is Structurally Closed

The Alloy assertion for role closure returned `UNSAT` in the finite scope because the model used an
abstract `Role` signature with exactly five concrete roles:

```text
user, assistant, tool, system, thinking
```

This validates the model structure, not the Go implementation. In Go, `AgentMessage.Role` is stored
as a raw string, so role validation would still need an implementation-level check if the pipeline
must reject unknown role strings.

## Aggregate Evidence

The ETL probe produced one concrete row for each risky shape:

```text
check_name              rows
orphan_subagent         1
empty_session           1
unpaired_tool_result    1
zero_timestamp_message  1
duplicate_tool_use_id   1
```

## Interpretation

The important result is not that the real source data currently contains these exact edge cases. The
important result is that the normalized data contract does not prevent them, and the transform emits
them without structured diagnostics when given small plausible inputs.

This is the useful role for Alloy in this Project:

- express the intended relational shape cheaply;
- let the analyzer generate small counterexamples;
- convert the counterexamples into synthetic ETL fixtures;
- decide which shapes should be allowed, warned about, quarantined, or rejected.

The Alloy model should not become the only source of truth. The Go schema and transform remain
canonical. Alloy is best used here as a design and validation aid that helps discover missing
integrity checks.

## ETL Error Policy

The product policy coming out of the spike is resilient ingestion:

- `auto etl run` should continue through recoverable source-data problems.
- Canonical `sessions` and `messages` parquet should remain safe for normal downstream queries.
- Recoverable problems should be written to a separate diagnostic collection, tentatively
  `data_errors`.
- Unrecoverable operational failures still fail the run: unreadable input root, unwritable output
  root, corrupt parquet write, invalid flags, or configuration errors.

The proposed `data_errors` dataset should be appendable/queryable like the other ETL outputs and
should carry enough context to diagnose the source shape without making downstream readers parse raw
agent files again.

Recommended fields:

- `id`: deterministic error ID.
- `severity`: `warning` or `error`.
- `code`: stable machine-readable code, for example `empty_session`,
  `orphan_subagent_ignored`, `missing_tool_use`, `duplicate_tool_use_id`,
  `zero_timestamp_message`.
- `message`: human-readable explanation.
- `source_path`: raw source file path.
- `source_line_index`: source line index when known.
- `session_id`: affected Session ID when known.
- `parent_session_id`: parent Session ID when relevant.
- `message_index`: affected Message index when known.
- `tool_use_id`: affected tool-use ID when relevant.
- `action`: what ETL did, for example `kept`, `ignored`, `quarantined`, or `fallback_timestamp`.
- `schema_version`, `year`, `month`: normal dataset bookkeeping.

The current policy by shape:

| Shape | Canonical output policy | Diagnostic policy |
| --- | --- | --- |
| Session with no Messages | Allow; downstream code must not crash | Optional `warning` with code `empty_session` |
| Orphan Subagent | Do not emit as canonical Session/Message data | Emit `error` with code `orphan_subagent_ignored` |
| Tool result without tool use | Do not emit as a canonical paired tool result | Emit `error` with code `missing_tool_use` |
| Duplicate tool-use ID | Do not apply silent last-wins metadata | Emit `error` with code `duplicate_tool_use_id` |
| Zero timestamp Message | Do not write `year=0/week=00` partitions | Emit `error` with code `zero_timestamp_message` unless a deliberate fallback is chosen |
| Future role | Allow as a role string unless role-specific behavior is required | No error by default |

## Application Integration

The promoted Alloy model now defines what the rest of the application should be able to assume
about canonical ETL output.

`auto-search` should be able to query `sessions` and `messages` without guarding every join against
orphan Subagents, unpaired tool results, duplicate tool-use IDs, or invalid time partitions. It can
optionally expose `data_errors` through a separate command or filter.

`auto-reflect` should use canonical `Session` and `Message` data for Observations and Rules. It
should either ignore `data_errors` by default or retrieve them explicitly when analyzing pipeline
quality.

`auto-ui` should treat `data_errors` as Project health data: show counts by code/severity and link
back to affected source files or Sessions where possible.

`auto-graph` and future file-access heat maps should trust canonical tool metadata only after
duplicate and missing tool-use cases are removed from the canonical dataset or represented with an
explicit fallback.

`auto etl doctor` should summarize `data_errors` and can exit non-zero for error-severity rows. In
contrast, `auto etl run` should not fail the whole run for recoverable data errors if it can still
write valid canonical rows and diagnostics.

## Decision Predicates for Alloy

Alloy is most valuable when the target is a small structural model with relationships that must
always stay coherent. It is much less valuable when the main challenge is throughput, streaming,
timeouts, parsing, UI behavior, or long-running process control.

Use Alloy when these predicates are true:

- `FiniteUniverse(target)`: the target can be represented as a bounded set of named objects, such as
  Sessions, Messages, Events, Rules, TaskDefs, Triggers, Skills, or Context Pack files.
- `RelationalIntegrity(target)`: correctness depends on references between those objects, such as
  parent links, ownership links, version lineage, tool-use pairing, graph edges, or manifest entries.
- `ConsumerAssumption(target)`: downstream code already assumes invariants that are not locally
  enforced, such as "every Message has a Session" or "every managed Skill is represented in the
  manifest".
- `CounterexampleUseful(target)`: a tiny synthetic world would be actionable. If a two-Session or
  three-Rule counterexample can become a fixture or a failing unit test, Alloy is a good fit.
- `BehaviorIsMostlyStructural(target)`: ordering, lifecycle, or state changes can be reduced to
  small snapshots or short traces without modeling real time, network scheduling, or concurrency.
- `VocabularyIsStable(target)`: the domain names are settled enough that a model will clarify the
  design instead of freezing churn.
- `ImplementationBridgeExists(target)`: there is a practical path from Alloy output to product code:
  generated fixture, table-driven test, `doctor` check, validation rule, or schema constraint.

Do not use Alloy when these predicates dominate:

- `MostlyTemporal(target)`: the important question is liveness, timeout behavior, retries,
  backpressure, daemon scheduling, or whether something eventually happens. Prefer state-machine
  tests, stress tests, or TLA+/Quint-style modeling.
- `MostlyAlgorithmic(target)`: the important question is ranking quality, token selection,
  search scoring, AST parsing, hashing, rendering, or numeric optimization. Prefer unit tests,
  golden tests, fuzzing, or benchmark fixtures.
- `SingleInputOutputCase(target)`: a normal table-driven test can express the behavior more clearly
  than a relational model.
- `UnboundedDataShape(target)`: correctness depends on large text bodies, arbitrary JSON payloads,
  real timestamps, file contents, or external services rather than object relationships.
- `NoOwnedInvariant(target)`: the team is not ready to say which shapes are valid, invalid,
  tolerated, or quarantined. Alloy will expose ambiguity, but it cannot decide policy.
- `NoImplementationBridge(target)`: the model cannot be connected back to code through validation,
  tests, fixtures, or generated diagnostics. In that case it becomes documentation that drifts.
- `ModelWouldDuplicateCode(target)`: the proposed model would reimplement parsing or business logic
  instead of stating the relationships the implementation must preserve.

The practical rule is:

```text
Use Alloy when FiniteUniverse + RelationalIntegrity + CounterexampleUseful are true,
and no "do not use" predicate is the central risk.
```

## Project-Wide Alloy Candidates

The same decision predicates point to several better candidates than a blanket "model everything"
approach. The highest-value areas are places where multiple files describe one logical state and
where invalid states would look plausible to downstream commands.

### 1. Reflect Event Log to Playbook Fold

Candidate files:

- [`auto-reflect/internal/events/model.go`](../../auto-reflect/internal/events/model.go)
- [`auto-reflect/internal/rules/model.go`](../../auto-reflect/internal/rules/model.go)
- [`auto-reflect/internal/rules/projection.go`](../../auto-reflect/internal/rules/projection.go)

Why Alloy fits:

- `FiniteUniverse`: Events, Rules, Observations, and Playbooks are naturally finite in a bounded
  model.
- `RelationalIntegrity`: Rule creation, Rule edits, version numbers, predecessor/successor links,
  and Observation links must remain coherent.
- `CounterexampleUseful`: a small sequence of create/edit Events can become a fixture for the fold.

Useful Alloy assertions:

- every Rule in a Playbook has exactly one first creation Event;
- edits apply only to existing Rules;
- Rule version equals the creation Event plus the applied edit Events, subject to explicit conflict
  policy;
- predecessor and successor links do not point to missing Rules or to the same Rule;
- non-Rule Events do not accidentally advance Rule fold state.

This is the strongest next Alloy candidate. The fold is pure enough to model, and a counterexample
would likely map directly to a failing projection test.

### 2. Skill Sync State

Candidate files:

- [`auto-skill/internal/skill/skillsyaml.go`](../../auto-skill/internal/skill/skillsyaml.go)
- [`auto-skill/internal/skill/lock.go`](../../auto-skill/internal/skill/lock.go)
- [`auto-skill/internal/skill/manifest.go`](../../auto-skill/internal/skill/manifest.go)
- [`auto-skill/internal/sync/plan.go`](../../auto-skill/internal/sync/plan.go)
- [`auto-skill/internal/sync/process.go`](../../auto-skill/internal/sync/process.go)

Why Alloy fits:

- `FiniteUniverse`: Skills, targets, lock entries, manifest entries, and managed output directories
  are bounded objects.
- `RelationalIntegrity`: desired Skill state is split across `skills.yaml`, lock state, manifest
  state, target ownership, and rendered files.
- `ConsumerAssumption`: sync and prune behavior assumes that ownership records cannot become
  ambiguous.

Useful Alloy assertions:

- every manifest-managed Skill refers to a known manifest Skill;
- a target cannot own two versions of the same Skill at once;
- authored Skills shadow vendored Skills deterministically;
- locked and check-only modes cannot mutate lock or target state;
- prune removes only managed orphan output and never foreign target content.

This is high-value because a bad model here could produce destructive writes. The model should stay
focused on ownership, state transitions, and target membership, not on rendering template contents.

### 3. Watch Trigger, TaskDef, and RunRecord Lifecycle

Candidate files:

- [`auto-watch/internal/model/types.go`](../../auto-watch/internal/model/types.go)
- [`auto-watch/internal/config/validate.go`](../../auto-watch/internal/config/validate.go)

Why Alloy fits:

- `FiniteUniverse`: Project configs contain a bounded set of Triggers and TaskDefs.
- `RelationalIntegrity`: Triggers reference TaskDefs, and RunRecords reference both.
- `BehaviorIsMostlyStructural`: RunRecord state can be modeled as a small lifecycle snapshot.

Useful Alloy assertions:

- every Trigger references at least one existing TaskDef;
- a bash TaskDef has a command and no prompt;
- a Claude TaskDef has a prompt and no command;
- pending, running, completed, and failed RunRecords have coherent timestamp, exit, and error
  fields;
- duplicate RunRecords for the same Trigger, TaskDef, branch, and resource key are either impossible
  or explicitly allowed by policy.

Alloy is useful for the config and lifecycle contract. It is not the right primary tool for daemon
scheduling, backoff, or long-running process behavior.

### 4. Graph and Context Pack Integrity

Candidate files:

- [`auto-graph/internal/graph/model.go`](../../auto-graph/internal/graph/model.go)
- [`auto-graph/internal/codegraph/build.go`](../../auto-graph/internal/codegraph/build.go)
- [`auto-graph/internal/contextpack/model.go`](../../auto-graph/internal/contextpack/model.go)

Why Alloy fits:

- `FiniteUniverse`: files, graph nodes, graph edges, and Context Pack entries are bounded.
- `RelationalIntegrity`: edges and Context Pack relationships must point to known files.
- `CounterexampleUseful`: small two-file or three-file examples are enough to expose broken graph
  assumptions.

Useful Alloy assertions:

- every edge source and target exists as a node;
- duplicate edges are merged according to the selected identity rule;
- ReadingOrder references only included Context Pack files;
- no file is both included and omitted;
- relationships mention only included files or intentionally omitted candidates.

Avoid modeling token-budget arithmetic in Alloy beyond simple bounds. That belongs in product tests.

### 5. Doc Freshness and Autodoc Link Status

Candidate files:

- [`auto-doc/internal/linkscan/linkscan.go`](../../auto-doc/internal/linkscan/linkscan.go)
- [`auto-doc/internal/linkcheck/linkcheck.go`](../../auto-doc/internal/linkcheck/linkcheck.go)

Why Alloy fits:

- `FiniteUniverse`: docs, tags, source scopes, hashes, and statuses are small named objects.
- `RelationalIntegrity`: each tag points to a doc identity and a source scope.
- `SingleInputOutputCase` is close to true, so this should stay small.

Useful Alloy assertions:

- every autodoc tag receives exactly one status;
- `OK` is possible only when both doc hash and scope hash match;
- orphan, malformed, and self-referencing tags have explicit precedence;
- duplicate doc IDs cannot create ambiguous status classification unless that ambiguity is accepted
  as policy.

This could also be covered by a table-driven truth table. Alloy only earns its keep if status
precedence keeps growing.

### 6. Bus Event Envelope and Hook Mapping

Candidate files:

- [`auto-shared/bus/event.go`](../../auto-shared/bus/event.go)
- [`auto-cli/cmd/auto/hookscmd_test.go`](../../auto-cli/cmd/auto/hookscmd_test.go)

Why Alloy fits:

- `FiniteUniverse`: Event types, Host context, Project context, Session context, and path references
  can be bounded.
- `RelationalIntegrity`: Event context fields must not contradict each other.
- `ImplementationBridgeExists`: assertions can become conformance tests for hook mapping.

Useful Alloy assertions:

- Event type names follow the registry shape;
- required context is present for each Event family;
- hook-derived Events preserve Host, Project, Session, branch, worktree, and commit provenance;
- JSON-RPC notification method names match Bus Event types where that mapping is required.

This is a good candidate once the Bus Event registry is treated as a product contract.

### 7. Env Registry and Port Allocation

Candidate files:

- [`auto-env/internal/registry/registry.go`](../../auto-env/internal/registry/registry.go)
- [`auto-env/internal/port/port.go`](../../auto-env/internal/port/port.go)

Why Alloy partly fits:

- `FiniteUniverse`: registry entries, slots, branches, and ports are small.
- `RelationalIntegrity`: registry uniqueness and generated file ownership can be stated clearly.

Useful Alloy assertions:

- there is at most one active registry entry per Project root;
- slot and branch slug ownership are unique if policy requires that;
- generated port numbers do not overlap across slots for the configured stride;
- manifest files correspond to generated environment files.

This is lower priority because the most important part is arithmetic. Property tests may be simpler
than Alloy unless registry ownership policy becomes more complex.

### 8. Future Eval Schema

The `auto-eval` requirements are a good future Alloy candidate once the schema settles. A model
could check that eval runs reference valid scenarios, cases, fixtures, agents, evaluators, and
scores, and that each run receives exactly the required score set.

This should wait until the vocabulary and storage format are stable enough for
`VocabularyIsStable(target)` to be true.

## Poor Alloy Candidates

Do not make Alloy the primary tool for these areas:

- RPC stream behavior, timeouts, keepalive, dropped responses, or slow consumers. These are
  `MostlyTemporal` and need state-machine or protocol tests.
- Search scoring, ranking, embedding behavior, or token selection. These are `MostlyAlgorithmic`
  and need golden tests, evaluation fixtures, and benchmarks.
- UI rendering or interaction state. Use browser tests and screenshot checks.
- Parser correctness for large raw Session files. Use fixtures, fuzzing, and round-trip tests.
- S3 signing, external service behavior, or credential handling. Use integration tests and focused
  cryptographic test vectors.

## Recommendations

1. Keep `auto-etl/alloy/session_message_conformance.als` as the owned Session/Message conformance
   model.
2. Add a `data_errors` parquet dataset for recoverable malformed source data.
3. Keep empty Sessions valid, but ensure all downstream Session readers tolerate zero Messages.
4. Ignore orphan Subagents from canonical output and emit `orphan_subagent_ignored`.
5. Validate tool pairing with `map[string][]toolUseMeta` so missing originators and duplicate
   originators are distinguishable and emitted as `missing_tool_use` or `duplicate_tool_use_id`.
6. Prevent `messages/year=0/week=00` writes by rejecting timestamp-zero Messages from canonical
   output or assigning a deliberate fallback with explicit provenance.
7. Keep roles open-ended. Only validate role-specific behavior when a role participates in a known
   contract such as assistant tool use or tool result.
8. Add `auto etl doctor` support for summarizing `data_errors`; let `doctor` fail on error severity
   while `run` continues through recoverable data errors.
9. Run the next Alloy spike against the Reflect Event to Playbook fold before modeling broader
   Project surfaces.

## Reproduction

Run the promoted Alloy model:

```bash
cd auto-etl
./alloy/run.sh
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

Summarize the conformance violations:

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
