# Phase 2: Model-Based Testing — Verifying Go Against Quint

Phase 1 (the [tech spike](SPIKE-REPORT.md)) proved that Quint can model our CRDT merge semantics and catch bugs like tombstone resurrection in <100ms. This phase answers the next question: **can we use Quint's model-based testing (MBT) to automatically verify that a real Go implementation faithfully matches the formal specification?**

> **Status: experiment only.** This is a research writeup. The Go merge package, ITF parser, and trace fixtures described below were built and run in a throwaway worktree to produce the findings; **none of that code is checked into the live tree.** The merge logic is reproduced here as illustrative examples so the findings stand on their own. Adopting a `merge` package into production is a separate, future decision (see [Implications](#implications-for-next-steps)).

## Hypothesis

Quint MBT traces can drive Go conformance tests that automatically detect semantic divergences between the formal merge specification and a Go merge implementation.

## Prerequisites

- Quint v0.32.0 (`quint` CLI)
- Existing Quint spec at [`etl_sync.qnt`](etl_sync.qnt)
- The production merge implementation this is measured against: `auto-etl/internal/writer/git.go` (`mergeByID`, `mergeGitRepos`) and `github.go` (`mergePRs`, `mergeComments`)
- Cost budget: $0 (no API calls — pure CPU work)

## Method

**Step 1: Generate ITF traces.** Use `quint run --mbt --out-itf` to generate 500+ diverse traces from the `etl_merge_test` module. Each trace is a sequence of states capturing all store variables after each action (init, syncAB, syncAC, syncBC, ingestToA). The `--mbt` flag adds `mbt::actionTaken` and `mbt::nondetPicks` metadata so a Go harness knows which action to replay.

```bash
quint run docs/experiments/quint-sync-protocol/etl_sync.qnt \
  --main=etl_merge_test --mbt --out-itf=trace_{}.itf.json \
  --max-samples=500 --max-steps=10
```

**Step 2: Parse ITF in Go.** ITF (Informal Trace Format) uses a specific JSON encoding: `#set` for sets, `#bigint` for integers, record types as plain objects. A small decoder turns ITF JSON into typed Go structs. The one quirk worth flagging: integers are wrapped, so `{"#bigint": "123"}` has to be unwrapped before use.

```go
// bigintVal unwraps the ITF {"#bigint":"123"} integer encoding.
func bigintVal(v any) (int, error) {
    switch x := v.(type) {
    case map[string]any:
        if s, ok := x["#bigint"].(string); ok {
            return strconv.Atoi(s)
        }
    case float64: // small ints may arrive bare
        return int(x), nil
    }
    return 0, fmt.Errorf("not an ITF int: %v", v)
}
```

**Step 3: Implement spec-aligned merge functions.** The production Go `mergeByID` uses simple "incoming wins" semantics. The Quint spec uses richer CRDT semantics (schema_version comparison, tombstone propagation). A small package of pure functions that match the Quint spec exactly gives a Go "reference implementation" to test against:

```go
// MessageRecord matches the Quint MessageRecord type exactly.
type MessageRecord struct {
    ID            string
    Content       string
    SchemaVersion int
    DeletedAt     int
}

// ResolveMessage resolves two MessageRecords with the same ID.
// Higher schema_version wins; max(deleted_at) is always propagated
// (tombstone dominance). Matches Quint resolveMessage(left, right).
func ResolveMessage(left, right MessageRecord) MessageRecord {
    winner := left
    if right.SchemaVersion > left.SchemaVersion {
        winner = right
    }
    tombstone := left.DeletedAt
    if right.DeletedAt > tombstone {
        tombstone = right.DeletedAt
    }
    winner.DeletedAt = tombstone // tombstone survives even if winner was alive
    return winner
}
```

`MergeMessages(a, b)` is a G-Set union by ID: records present in only one set pass through; records in both go through `ResolveMessage`; output is sorted by ID for deterministic comparison. `ResolveSession` adds an LWW tiebreaker on top — higher `schema_version` wins, then higher `last_message_at`, with the same tombstone dominance:

```go
// ResolveSession: schema_version wins, then last_message_at; tombstone dominates.
func ResolveSession(left, right SessionRecord) SessionRecord {
    var winner SessionRecord
    switch {
    case right.SchemaVersion > left.SchemaVersion:
        winner = right
    case left.SchemaVersion > right.SchemaVersion:
        winner = left
    case right.LastMessageAt > left.LastMessageAt:
        winner = right
    default:
        winner = left
    }
    tombstone := left.DeletedAt
    if right.DeletedAt > tombstone {
        tombstone = right.DeletedAt
    }
    winner.DeletedAt = tombstone
    return winner
}
```

**Step 4: Replay each trace.** For each ITF trace, the harness reads the initial state (storeA/B/C, sessA/B/C); then for each step reads `mbt::actionTaken` + `mbt::nondetPicks`, applies the corresponding Go merge function, and compares the resulting Go state against the expected Quint state. Any mismatch is reported as `(trace_file, step_index, variable, expected, got)`.

**Step 5: Run conformance against two implementations.**
- The **spec-aligned** merge functions — expected to pass all traces (validates the harness itself).
- A **naive** merge that mimics production `mergeByID` (incoming always wins, no schema/tombstone logic) — expected to diverge, quantifying the production gap:

```go
// naiveMergeMessages mimics current mergeByID: incoming always wins on ID
// collision. No schema_version comparison. No tombstone propagation.
func naiveMergeMessages(existing, incoming []MessageRecord) []MessageRecord {
    incomingIDs := make(map[string]bool, len(incoming))
    for _, m := range incoming {
        incomingIDs[m.ID] = true
    }
    result := make([]MessageRecord, 0, len(existing)+len(incoming))
    for _, m := range existing {
        if !incomingIDs[m.ID] { // keep existing only if incoming has no collision
            result = append(result, m)
        }
    }
    result = append(result, incoming...) // incoming wins, unconditionally
    sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
    return result
}
```

## Known semantic gap

The Quint spec and production Go implementation intentionally differ on conflict resolution:

| Behavior | Quint Spec | Go `mergeByID` |
|---|---|---|
| ID collision | Higher `schema_version` wins, then propagate max `deleted_at` | Incoming always wins |
| Tombstone propagation | `max(left.deleted_at, right.deleted_at)` always applied | No tombstone logic |
| Session LWW | `schema_version` → `last_message_at` tiebreaker | N/A (sessions not merged) |

The point of this experiment is NOT to find surprises — we know the gap exists. The point is to **build machinery** that makes the gap measurable and closable: generate traces, replay them, get a concrete divergence report, and use that report to drive a future Go implementation toward the spec.

## Success criteria

- **Metric 1: Trace generation.** Pass if ≥500 valid ITF traces with ≥3 distinct action types represented.
- **Metric 2: Harness correctness.** Pass if spec-aligned Go merge functions agree with Quint on 100% of trace steps. Fail if any step diverges (means the Go "reference" is wrong).
- **Metric 3: Divergence detection.** Pass if the harness detects ≥1 divergence when running the naive merge against traces with schema_version conflicts or tombstone scenarios.
- **Metric 4: Divergence report quality.** Pass if every divergence includes (trace_file, step_index, variable, expected_state, actual_state) — enough to reproduce the bug.

## Go/no-go decision matrix

| Traces | Harness vs Spec | Divergence Detection | Verdict |
|---|---|---|---|
| Pass | Pass (100%) | Pass (finds gaps) | **Full go.** MBT is viable for ongoing verification. Candidate for CI. |
| Pass | Pass (100%) | Fail (no gaps found) | **Partial.** Harness works but traces don't cover interesting divergences. Expand trace generation. |
| Pass | Fail (<100%) | N/A | **Debug harness.** ITF parsing or type mapping has a bug. Fix and re-run. |
| Fail | N/A | N/A | **Quint MBT unusable.** Fall back to property-based Go tests with `testing/quick`. |

## What this phase did NOT cover

- **Multi-host concurrent writer model** — the `concurrent_writers` Quint module requires interleaved actions across hosts. Deferred to Phase 3.
- **Parquet read/write roundtrip** — merge logic only, not IO.
- **Real `model.AgentMessage` / `model.AgentSession` types** — verified against simplified types that match the Quint spec directly. Mapping to production types is a separate step.
- **`mergeComments` (PR refresh semantics)** — the Quint spec doesn't model PR refresh; it's a distinct merge strategy needing its own module.

## Cost budget

$0.00. No API calls. Quint simulation runs in ~8 seconds for 500 traces; the Go replay is CPU-bound (~1.5s).

---

## Findings (run 2026-06-02)

### Headline verdict

**Pass** on all four metrics. Quint MBT traces successfully drive Go conformance tests that automatically detect semantic divergences between the formal spec and a real implementation.

### Headline numbers

| Metric | Threshold | Result | Pass? |
|---|---|---|---|
| Trace generation | ≥500 traces, ≥3 action types | 500 traces, 4 action types (ingestToA, syncAB, syncAC, syncBC) | **Pass** |
| Harness vs spec | 100% agreement | 0 divergences across 5200 steps | **Pass** |
| Divergence detection | ≥1 divergence with naive merge | 3889 divergences (100% of traces) | **Pass** |
| Report quality | trace/step/var/expected/actual | All 3889 include full context | **Pass** |

### What we found

**The spec-aligned Go merge functions match Quint perfectly.** Zero divergences across 520 traces (500 generated + 20 fixtures) and 5200 state transitions. The ITF replay harness validates every step of every trace — this is not sampling; it's exhaustive replay of each generated trace.

**The naive "incoming wins" merge diverges on every single trace.** 3889 total divergences: 2228 in message stores, 1661 in session stores. They fall into three categories:

1. **Schema version loss.** When the incoming record has a lower `schema_version` than existing, naive merge overwrites with the downgrade. Example: storeA has `{m2, sv:2, del:100}`, incoming from storeC has `{m2, sv:1, del:0}` — naive produces `{m2, sv:1, del:0}`, spec produces `{m2, sv:2, del:100}`.

2. **Tombstone resurrection.** When the existing record has `deleted_at > 0` but incoming has `deleted_at == 0`, naive merge resurrects the deleted record. This is exactly the bug the Phase 1 spike flagged — now reproduced mechanically against real Go code.

3. **Session LWW tiebreak missing.** Naive merge doesn't compare `schema_version` or `last_message_at` for sessions — it always takes incoming, losing the LWW semantics.

**The MBT approach found these automatically** — no manual test case authoring. The Quint simulator explores the state space via random traces, and the Go harness catches every disagreement mechanically.

### Caveats

1. **ITF encoding quirks.** Integers are encoded as `{"#bigint":"123"}` — needed the decoder shown above. The `vars` array also contains duplicate entries for `mbt::actionTaken` / `mbt::nondetPicks` — harmless since we parse by key name.

2. **Small universe.** The Quint test module uses 2 IDs × 2 schema_versions × 2 delete_states. Enough to cover all merge semantics (commutativity, tombstone propagation, schema wins), but it doesn't test scaling behavior.

3. **Resync-on-divergence.** When the naive merge diverges at step N, the harness resyncs Go state to the expected Quint state before continuing step N+1. So each divergence is evaluated independently rather than cascading — the right design for *measuring* the gap, but it means the 3889 count overcounts what a real pipeline would see (early divergences would make later ones moot).

4. **Session merge not yet in production.** The Go codebase doesn't currently merge sessions at all (past partitions are immutable). The naive session test models what *would* happen if the existing `mergeByID` pattern were applied to sessions.

### What would have changed our mind

If the spec-aligned Go functions had diverged from Quint even once, our translation from Quint semantics to Go would be wrong — we'd debug the refinement mapping before trusting any conformance result. It didn't happen.

If the naive merge had zero divergences, the traces wouldn't be covering the interesting conflict scenarios (schema_version ties, tombstone propagation). We'd expand the Quint universe or add targeted trace modules.

### Implications for next steps

1. **The MBT pipeline works.** `quint run --mbt --out-itf` → a Go replay harness is a viable, automated spec-conformance path. If/when a CRDT merge is adopted in production, this is the verification harness for it — and a CI candidate so future changes to merge logic stay spec-aligned.

2. **The spec-aligned merge is a known-good reference.** The `ResolveMessage` / `MergeMessages` / `ResolveSession` / `MergeSessions` shapes above match Quint exactly and can seed a production implementation when the codebase is ready for CRDT-grade merge semantics. **Not adopted yet** — this remains experiment output.

3. **Phase 3 candidates:**
   - Extend the spec to the `concurrent_writers` module and generate traces for the event-batch / multi-host pattern.
   - Map the simplified `MessageRecord` / `SessionRecord` types to the real `model.AgentMessage` / `model.AgentSession` types.
   - Add Quint modules for PR and comment merge semantics (including the "full PR refresh" pattern).
   - An Apalache-friendly variant of the spec (bounded, exhaustive proofs rather than random sampling) was sketched during this phase; promoting it to a checked proof is future work.

### Cost

$0.00 actual. Quint trace generation: ~8 seconds for 500 traces. Go replay suite: ~1.5 seconds.
