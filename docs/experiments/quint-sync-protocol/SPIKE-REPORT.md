# Tech Spike Report: Quint for ETL Sync Protocol Verification

**Date:** 2026-06-02
**Scope:** Can Quint model and verify the CRDT-based merge protocol from the pipeline integrity research doc?
**Verdict:** GO — Quint is a strong fit for specifying and verifying the ETL sync protocol.

## How this was generated

This spike was executed by Claude Code using the `/tech-spike` skill:
```
/tech-spike read @auto-etl/docs/pipeline-integrity-research.md and https://quint.sh/llms.txt,
  lets plan and implement an exploration spike to see if quint can be a good tool
  to help us build a sync protocol that is robust, considering the full requirements.
  the quint cli is installed.
```

## Context

The auto-etl pipeline needs a formally sound merge protocol to support multi-host sync, schema evolution, and tombstone-based deletion. The [pipeline integrity research doc](auto-etl/docs/pipeline-integrity-research.md) defines CRDT-based merge semantics (G-Set for messages, LWW-Register for sessions) with specific algebraic properties that must hold. We explored whether Quint — a TypeScript-like specification language with built-in simulation and model checking — can verify these properties before implementation.

## Assumptions Tested

| # | Assumption | Result | Confidence |
|---|-----------|--------|------------|
| 1 | Quint can model the CRDT merge operations | VALIDATED | High |
| 2 | Quint can verify the four CRDT properties | VALIDATED | High |
| 3 | Quint can model concurrent writers and find conflicts | VALIDATED | High |
| 4 | Quint can model schema evolution merge rules | VALIDATED | High |
| 5 | Quint catches real edge cases (tombstone resurrection) | VALIDATED | High |
| 6 | Literate specs work for combined documentation + verification | VALIDATED | Medium |

## Detailed Findings

### Assumption 1: Quint can model the CRDT merge operations

**Result:** VALIDATED

**What we did:**
Modeled `MessageRecord` and `SessionRecord` as Quint record types. Implemented `mergeMessages()` as G-Set union with schema-version-aware conflict resolution and tombstone propagation. Implemented `mergeSessions()` as LWW-Register merge (latest `last_message_at` wins, higher `schema_version` trumps).

**What we found:**
The Quint type system maps naturally to the domain. Record types with spread syntax (`{ ...winner, deleted_at: tombstone }`) make conflict resolution readable. Set operations (`filter`, `map`, `union`, `forall`, `exists`) express merge logic directly. The spec reads almost like pseudocode from the research doc.

**Key learning:** `nondet` bindings can only appear in actions (boolean context), not inside pure `map()` calls. Building test universes requires pre-computing all valid configurations as sets and sampling from them with `nondet x = SET.oneOf()`. This is a language constraint worth knowing upfront.

**Evidence:** `etl_sync.qnt` — `etl_merge` module (lines 7-126). Parses, typechecks, and simulates correctly.

---

### Assumption 2: Quint can verify the four CRDT properties

**Result:** VALIDATED

**What we did:**
Defined commutativity, associativity, idempotency, and monotonicity as pure boolean functions. Ran `quint run` with 3 hosts, random initial states (2 IDs × 2 schema versions × 2 delete states), sync actions, and ingestion.

**What we found:**
All 11 invariants (4 message properties + 4 session properties + tombstone dominance + schema upgrade + convergence) pass with 1000 random traces in 741ms (~1350 traces/sec). The Rust evaluator is fast.

**Evidence:**
```
quint run etl_sync.qnt --main=etl_merge_test --invariant=allProperties --max-steps=5 --max-samples=1000
[ok] No violation found (741ms at 1350 traces/second).
```

---

### Assumption 3: Quint can model concurrent writers and find conflicts

**Result:** VALIDATED

**What we did:**
Modeled two hosts with local state, pending writes, and a shared store. Created two step actions: `stepUnsafe` (in-place overwrite) and `stepSafe` (merge-based batch append). Checked the invariant "after both hosts flush, shared store contains all ingested IDs."

**What we found:**
- **Unsafe model:** Quint found the data loss violation in **59ms** with a clear counterexample trace: Host B writes m3 → Host A overwrites shared store with its empty local state → m3 is lost.
- **Safe model:** 5000 traces, all pass, 188ms (~26K traces/sec).

This is the most compelling result — Quint automatically discovered the exact concurrent write hazard the research doc describes, and confirmed the event-batch approach eliminates it.

**Evidence:**
```
# UNSAFE: violation found
quint run etl_sync.qnt --main=concurrent_writers --step=stepUnsafe --invariant=noDataLoss
[violation] Found an issue (59ms at 254 traces/second).

# SAFE: no violation
quint run etl_sync.qnt --main=concurrent_writers --step=stepSafe --invariant=noDataLoss
[ok] No violation found (188ms at 26596 traces/second).
```

---

### Assumption 4: Quint can model schema evolution merge rules

**Result:** VALIDATED

**What we did:**
Added `schema_version` to all records. Merge rule: higher schema version wins on ID collision, same version → left wins. Defined `schemaUpgradeCorrectness` invariant: merged record's schema_version ≥ both inputs.

**What we found:**
Property passes across all simulation traces. The `max(schema_version)` merge rule composes correctly with the CRDT properties because `max` is commutative, associative, and idempotent.

**Evidence:** Included in the `allProperties` invariant check above (741ms, 1000 traces).

---

### Assumption 5: Quint catches real edge cases (tombstone resurrection)

**Result:** VALIDATED

**What we did:**
Created a `buggy_merge` module that intentionally omits tombstone propagation — the resolve function picks the winner by schema version but doesn't carry over `max(deleted_at)`. Checked `tombstoneSafe` invariant.

**What we found:**
Quint found the deletion resurrection bug in **63ms** with a precise counterexample:
- Store A: `{id: "m1", schema_version: 2, deleted_at: 0}` (alive)
- Store B: `{id: "m1", schema_version: 2, deleted_at: 100}` (tombstoned)
- Buggy merge picks Store A (same schema version → left wins), resurrecting the deleted record.

This is exactly the bug pattern warned about in the research doc. The correct merge propagates `max(deleted_at)` and passes the same invariant.

**Evidence:**
```
quint run etl_sync.qnt --main=buggy_merge --invariant=tombstoneSafe
[violation] Found an issue (63ms at 810 traces/second).
```

---

### Assumption 6: Literate specs work for combined documentation + verification

**Result:** VALIDATED (with caveats)

**What we did:**
Wrote the initial spec as a markdown file with embedded `quint` code blocks using `lmt` syntax. Extracted `.qnt` files with `lmt`.

**What we found:**
The extraction works and produces valid `.qnt` files. However:
- `lmt` (Go tool) needed separate installation (`go install`)
- Multi-file extraction (`etl_merge.qnt` + `etl_merge_test.qnt`) requires careful import handling — Quint's `parse` command doesn't auto-discover sibling files. We ended up consolidating into a single `.qnt` file.
- For iterative development, editing `.qnt` directly is much faster than editing markdown and re-extracting.

**Recommendation:** Literate specs are better suited for final documentation than for active development. Write the spec in `.qnt` during development, then wrap it in literate markdown as a publishable artifact.

## Surprises & Secondary Findings

1. **`quint run` (simulation) is the primary workflow, not `quint verify`.** The simulator found every bug we planted in under 100ms. The Apalache model checker (`quint verify`) works for focused, simple models (verified partial sync exhaustively in 35s) but chokes on compound invariants with nested set operations across 3 stores. For our use case, simulation with 1000+ random traces provides strong confidence without the model checker's computational cost.

2. **Rust evaluator is fast.** Quint auto-downloads a Rust-based evaluator on first run, which handles thousands of traces per second. No performance concerns for the spec sizes we're working with.

3. **State space management is key.** The powerset of all possible records grows fast. With 2 IDs × 2 versions × 2 delete states = 8 records, the valid store space is manageable. Scaling to 3+ IDs would require more careful state space design or moving to `quint verify` for focused properties.

4. **Quint's `nondet` is action-only.** You can't use `nondet` inside pure functions like `map()`. Test data generation requires pre-computing the universe of valid inputs as a set and sampling from it. This is a design constraint, not a bug — it ensures reproducibility.

5. **Multi-file modules need explicit wiring.** Unlike TLA+ toolbox, `quint parse` operates on a single file. Cross-file imports need the `-r/--require` flag (only available on `quint run/test`, not `quint parse`). Consolidating into a single file with multiple modules is simpler.

## Risks Identified

1. **Model checker scalability.** `quint verify` may not scale to the full protocol model (3+ hosts, compaction, S3 backend). But `quint run` with statistical sampling is sufficient for our confidence needs.

2. **Spec-to-implementation gap.** The Quint spec proves the merge *logic* is correct, but the Go implementation must faithfully reproduce that logic. Quint's model-based testing feature could bridge this gap by generating test traces from the spec, but we haven't explored that yet.

3. **Quint is a young tool.** Version 0.32.0, spun out of Informal Systems in 2025. Documentation exists but community/examples are still growing. The language is stable enough for our needs.

## Recommendations

**Proceed with Quint as the formal specification tool for the ETL sync protocol.**

Suggested next steps:
1. **Evolve the spec** alongside the Go implementation — add compaction model, S3 backend model, batch ingestion log.
2. **Use `quint run` as the primary verification workflow** — fast, finds bugs, no Java dependency.
3. **Keep `quint verify` for focused proofs** — individual properties on simplified models where exhaustive checking adds value.
4. **Explore model-based testing** — use Quint's `--mbt` flag to generate test traces that can validate the Go implementation matches the spec.
5. **Store the spec in `auto-etl/spec/`** — version it alongside the code. The spec is compact (~300 lines) and self-documenting.

## Appendix: Reproduction

```bash
cd docs/experiments/quint-sync-protocol/

# All CRDT properties (simulation, ~1s)
quint run etl_sync.qnt --main=etl_merge_test --invariant=allProperties --max-steps=5 --max-samples=1000

# Partial sync safety
quint run etl_sync.qnt --main=partial_sync --invariant=neverShrinks --max-steps=20 --max-samples=1000

# Concurrent writers: unsafe (finds violation)
quint run etl_sync.qnt --main=concurrent_writers --init=init --step=stepUnsafe --invariant=noDataLoss --max-steps=20 --max-samples=5000

# Concurrent writers: safe (passes)
quint run etl_sync.qnt --main=concurrent_writers --init=init --step=stepSafe --invariant=noDataLoss --max-steps=20 --max-samples=5000

# Buggy merge: finds tombstone resurrection
quint run etl_sync.qnt --main=buggy_merge --invariant=tombstoneSafe --max-steps=1 --max-samples=1000

# Exhaustive verification (partial sync, ~35s)
quint verify etl_sync.qnt --main=partial_sync --invariant=neverShrinks --max-steps=3
```

Files (in this directory):
- `etl_sync.qnt` — the full Quint spec (all modules, runnable)
- `etl_merge.md` — literate spec (initial version, shows the markdown+code approach)
