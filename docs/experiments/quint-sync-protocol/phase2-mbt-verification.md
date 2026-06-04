# Phase 2: Model-Based Testing — Verifying Go Against Quint

Phase 1 (the tech spike) proved that Quint can model our CRDT merge semantics and catch bugs like tombstone resurrection in <100ms. This phase answers the next question: **can we use Quint's model-based testing (MBT) to automatically verify that the real Go implementation faithfully matches the formal specification?**

## Hypothesis

Quint MBT traces can drive Go conformance tests that automatically detect semantic divergences between the formal merge specification and the real Go merge implementation.

## Prerequisites

- Quint v0.32.0 (`quint` CLI installed at `~/.local/bin/quint`)
- Existing Quint spec at `docs/experiments/quint-sync-protocol/etl_sync.qnt`
- Go merge implementation at `auto-etl/internal/writer/git.go` (`mergeByID`, `mergeGitRepos`)
- Go merge implementation at `auto-etl/internal/writer/github.go` (`mergePRs`, `mergeComments`)
- Cost budget: $0 (no API calls — pure CPU work)

## What we're going to do

**Step 1: Generate ITF traces.** Use `quint run --mbt --out-itf` to generate 500+ diverse traces from the `etl_merge_test` module. Each trace is a sequence of states capturing all store variables after each action (init, syncAB, syncAC, syncBC, ingestToA). The `--mbt` flag adds `mbt::actionTaken` and `mbt::nondetPicks` metadata so the Go harness knows which action to replay.

**Step 2: Build an ITF parser in Go.** ITF (Informal Trace Format) uses a specific JSON encoding: `#set` for sets, `#bigint` for integers, record types as plain objects. Write a Go package that parses ITF JSON into typed Go structs.

**Step 3: Implement spec-aligned merge functions.** The current Go `mergeByID` uses simple "incoming wins" semantics. The Quint spec uses richer CRDT semantics (schema_version comparison, tombstone propagation). Write a new `merge` package with pure functions that match the Quint spec exactly. This gives us a Go "reference implementation" to test against.

**Step 4: Build the trace replay harness.** For each ITF trace, the harness:
1. Reads the initial state (storeA, storeB, storeC, sessA, sessB, sessC)
2. For each subsequent step, reads `mbt::actionTaken` and `mbt::nondetPicks`
3. Applies the corresponding Go merge function
4. Compares the resulting Go state against the expected Quint state
5. Reports any divergence as (trace_index, step_index, variable, expected, got)

**Step 5: Run conformance tests.** Execute the harness against both:
- The spec-aligned merge functions (should pass all traces — validates the harness)
- The current `mergeByID` function (will likely diverge — identifies spec gaps in production code)

## Known semantic gap

The Quint spec and Go implementation intentionally differ on conflict resolution:

| Behavior | Quint Spec | Go `mergeByID` |
|---|---|---|
| ID collision | Higher `schema_version` wins, then propagate max `deleted_at` | Incoming always wins |
| Tombstone propagation | `max(left.deleted_at, right.deleted_at)` always applied | No tombstone logic |
| Session LWW | `schema_version` → `last_message_at` tiebreaker | N/A (sessions not merged) |

The point of this experiment is NOT to find surprises — we know the gap exists. The point is to **build machinery** that makes the gap measurable and closable: generate traces, replay them, get a concrete divergence report, and use that report to drive incremental Go implementation toward the spec.

## Success criteria

- **Metric 1: Trace generation.** Pass if ≥500 valid ITF traces with ≥3 distinct action types represented.
- **Metric 2: Harness correctness.** Pass if spec-aligned Go merge functions agree with Quint on 100% of trace steps. Fail if any step diverges (means our Go "reference" is wrong).
- **Metric 3: Divergence detection.** Pass if the harness detects ≥1 divergence when running current `mergeByID` against traces with schema_version conflicts or tombstone scenarios.
- **Metric 4: Divergence report quality.** Pass if every divergence includes (trace_file, step_index, variable, expected_state, actual_state) — enough to reproduce the bug.

## Go/no-go decision matrix

| Traces | Harness vs Spec | Divergence Detection | Verdict |
|---|---|---|---|
| Pass | Pass (100%) | Pass (finds gaps) | **Full go.** MBT is viable for ongoing verification. Integrate into CI. |
| Pass | Pass (100%) | Fail (no gaps found) | **Partial.** Harness works but traces don't cover interesting divergences. Need to expand trace generation with targeted scenarios. |
| Pass | Fail (<100%) | N/A | **Debug harness.** ITF parsing or type mapping has a bug. Fix and re-run. |
| Fail | N/A | N/A | **Quint MBT unusable.** Fall back to property-based Go tests with `testing/quick`. |

## Deliverables

1. `auto-etl/internal/merge/merge.go` — spec-aligned pure merge functions (no IO)
2. `auto-etl/internal/merge/merge_test.go` — unit tests for spec-aligned functions
3. `auto-etl/internal/merge/itf.go` — ITF JSON trace parser
4. `auto-etl/internal/merge/itf_test.go` — trace replay conformance tests
5. `auto-etl/internal/merge/testdata/` — checked-in ITF trace files (subset)
6. `phase2_results.json` — metrics per success criteria
7. `phase2_notes.md` — human-readable findings

## What we will NOT test in this phase

- **Multi-host concurrent writer model** — the `concurrent_writers` Quint module requires a different replay strategy (interleaved actions across hosts). Defer to Phase 3.
- **Parquet read/write roundtrip** — this phase tests merge logic only, not IO.
- **Real `model.AgentMessage` / `model.AgentSession` types** — we test against simplified types that match the Quint spec directly. Mapping to production types is a separate step.

## Open questions (resolved)

**Q: Should the ITF parser be a reusable library or test-only code?**
A: Test-only for now. Put it in the `merge` package as unexported helpers. If it proves useful, extract later.

**Q: How many traces are enough?**
A: 500 traces × 10 steps = 5000 state transitions. The Quint universe is small (2 IDs × 2 versions × 2 delete states), so 500 traces should cover the state space well. We can verify coverage by checking how many distinct initial states appear.

**Q: Should we test `mergeComments` (PR refresh semantics)?**
A: No. The Quint spec doesn't model PR refresh. That's a distinct merge strategy that would need its own Quint module. Out of scope for this phase.

## Cost budget

$0.00. No API calls. Quint simulation runs in ~7 seconds for 1000 traces. Go tests are CPU-bound.

---

## Findings (run 2026-06-02)

### Headline verdict

**Pass** on all four metrics. Quint MBT traces successfully drive Go conformance tests that automatically detect semantic divergences between the formal spec and the real implementation.

### Headline numbers

| Metric | Threshold | Result | Pass? |
|---|---|---|---|
| Trace generation | ≥500 traces, ≥3 action types | 500 traces, 4 action types (ingestToA, syncAB, syncAC, syncBC) | **Pass** |
| Harness vs spec | 100% agreement | 0 divergences across 5200 steps | **Pass** |
| Divergence detection | ≥1 divergence with naive merge | 3889 divergences (100% of traces) | **Pass** |
| Report quality | trace/step/var/expected/actual | All 3889 include full context | **Pass** |

### What we found

**The spec-aligned Go merge functions match Quint perfectly.** Zero divergences across 520 traces (500 generated + 20 checked into testdata) and 5200 state transitions. The ITF trace replay harness validates every step of every trace — this is not sampling; it's exhaustive replay.

**The naive "incoming wins" merge diverges on every single trace.** 3889 total divergences: 2228 in message stores, 1661 in session stores. The divergences fall into three categories:

1. **Schema version loss.** When the incoming record has a lower `schema_version` than existing, naive merge overwrites with the downgrade. Example: storeA has `{m2, sv:2, del:100}`, incoming from storeC has `{m2, sv:1, del:0}` — naive produces `{m2, sv:1, del:0}`, spec produces `{m2, sv:2, del:100}`.

2. **Tombstone resurrection.** When the existing record has `deleted_at > 0` but incoming has `deleted_at == 0`, naive merge resurrects the deleted record. This is exactly the bug the Phase 1 spike flagged.

3. **Session LWW tiebreak missing.** Naive merge doesn't compare `schema_version` or `last_message_at` for sessions — it always takes incoming, losing the LWW semantics.

**The MBT approach found these automatically** — no manual test case authoring needed. The Quint simulator explores the state space via random traces, and the Go harness catches every disagreement mechanically.

### Caveats

1. **ITF encoding quirks.** Integers are encoded as `{"#bigint": "123"}` — needed a decoder. The `vars` array in ITF JSON contains duplicate entries for `mbt::actionTaken` and `mbt::nondetPicks` — harmless since we parse by key name.

2. **Small universe.** The Quint test module uses 2 IDs × 2 schema_versions × 2 delete_states. This is enough to cover all merge semantics (commutativity, tombstone propagation, schema wins), but doesn't test scaling behavior.

3. **Resync-on-divergence.** When the naive merge diverges at step N, the harness resyncs Go state to the expected Quint state before continuing step N+1. This means each divergence is independently evaluated rather than cascading — which is the right design for measuring the gap, but means the 3889 count overcounts versus what a real-world pipeline would see (early divergences would make later ones moot).

4. **Session merge not yet in production.** The Go codebase doesn't currently merge sessions at all (past partitions are immutable). The naive test validates what would happen if the existing `mergeByID` pattern were applied to sessions.

### What would have changed our mind

If the spec-aligned Go merge functions had diverged from Quint even once, it would mean our translation from Quint semantics to Go is wrong — we'd need to debug the refinement mapping before trusting any conformance results. This didn't happen.

If the naive merge had zero divergences, the Quint traces wouldn't be covering the interesting conflict scenarios (schema_version ties, tombstone propagation). We'd need to expand the Quint universe or add targeted trace modules.

### Implications for next steps

1. **The MBT pipeline works.** `quint run --mbt --out-itf` → Go test harness is a viable, automated spec-conformance path. It should be integrated into CI so any future changes to merge logic are verified against the Quint spec.

2. **The `merge` package is ready as a reference.** The spec-aligned `ResolveMessage`, `MergeMessages`, `ResolveSession`, `MergeSessions` functions can be adopted into the production writer package when the codebase is ready for CRDT-grade merge semantics.

3. **Phase 3 candidates:**
   - Extend the Quint spec to model the `concurrent_writers` module and generate traces for the event-batch pattern
   - Map the simplified `MessageRecord`/`SessionRecord` types to the real `model.AgentMessage`/`model.AgentSession` types
   - Add Quint modules for PR and comment merge semantics (including the "full PR refresh" pattern)

### Cost

$0.00 actual. Quint trace generation: ~8 seconds for 500 traces. Go test suite: ~1.5 seconds for all 17 tests.
