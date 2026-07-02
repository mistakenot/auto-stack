# Quint spike — ETL CRDT merge protocol

Behavioural-verification layer of the turtle-spike toolchain. Where the SHACL
shapes and SPARQL queries in `../shapes/` and `../tests/` check the *shape of a
single graph at rest*, this spike checks the *behaviour of the merge protocol
over time* — the properties that only exist across a sequence of sync/ingest
steps and multiple replicas.

**Task:** `auto-k5z`. **Tool:** [Quint](https://quint.sh) 0.32.0 + Apalache
0.56.1 (bundled SMT model checker). **Verdict:** Quint adds value that SHACL +
SPARQL structurally cannot — it verifies the *temporal/algebraic* contract of
the merge, not just the well-formedness of the data.

## What is modelled

`etl_sync.qnt` is the formal spec for the CRDT merge protocol implemented in
`auto-etl/internal/merge/merge.go`. The Go functions (`ResolveMessage`,
`MergeMessages`, `ResolveSession`, `MergeSessions`) are line-for-line ports of
the Quint `pure def`s, and the 500 `.itf.json` traces under
`auto-etl/internal/merge/testdata/` are model-based-testing traces exported from
this spec. This directory is the canonical home of that model within the
turtle-spike toolchain (provenance: `docs/experiments/quint-sync-protocol/`).

Six modules:

| Module | Purpose |
|---|---|
| `etl_merge` | The protocol: G-Set merge for messages, LWW-Register for sessions, tombstone dominance, schema-upgrade resolution, and the CRDT property predicates. |
| `etl_merge_test` | State machine — three replicas (A/B/C) that non-deterministically `sync` pairwise and `ingest` new records; `allProperties` asserts the CRDT laws hold in every reachable state. Used by `quint run`. |
| `etl_merge_verify` | Self-contained 2-replica model sized for exhaustive Apalache checking. Used by `quint verify`. |
| `concurrent_writers` | In-place-overwrite (unsafe) vs batch-merge (safe) shared-store writes; shows the no-data-loss property distinguishes them. |
| `partial_sync` | Re-syncing a subset of IDs must never shrink the store. |
| `buggy_merge` | **Negative control** — tombstone propagation deliberately removed. Model checking must find a counterexample. |

### Modelled semantics

- **G-Set merge for messages** — union by `id`; idempotent, commutative,
  associative. Records are only ever added, never dropped.
- **LWW-Register for session metadata** — highest `schema_version` wins; ties
  broken by latest `last_message_at`.
- **Tombstone dominance** — `deleted_at = max(left, right)` is propagated
  independently of which record wins the value merge, so a deletion on either
  replica survives (no resurrection).

### Safety invariants

- **Convergence** — all three replicas reach the same state regardless of sync
  order: `merge(merge(A,B),C) == merge(merge(A,C),B) == merge(merge(B,C),A)`.
- **No data loss / monotonicity** — every ID present in any input survives the
  merge; `concurrent_writers.noDataLoss` shows in-place writes violate this and
  batch merges don't.
- **Tombstone dominance** — a deleted record stays deleted after any merge.
- **Schema-upgrade correctness** — merged `schema_version` is `>=` both inputs.

## Running it

```bash
make typecheck   # parse + typecheck all six modules
make run         # randomised simulation of the sync state machine
make verify      # exhaustive SMT model check of the CRDT properties (Apalache)
make bughunt     # negative control: buggy merge MUST yield a counterexample
make all         # all of the above
```

### Results (reproduced 2026-07-02, quint 0.32.0 / apalache 0.56.1)

- **`quint run`** — 2000 traces, depth 6, ~1.9k traces/s: `[ok] No violation
  found`. The CRDT properties held in every simulated interleaving of sync +
  ingest.
- **`quint verify`** — Apalache exhaustively checked all six invariants
  (commutativity, idempotency, monotonicity, tombstone dominance, schema
  upgrade) over the bounded state space to depth 3: `The outcome is: NoError`
  (~6.5s). This is a proof over the whole state space, not sampling.
- **`make bughunt`** — with tombstone propagation removed, Apalache returned a
  counterexample in ~5.8s:

  ```
  storeA: Set({ id: "m1", schema_version: 2, deleted_at: 0   })
  storeB: Set({ id: "m1", schema_version: 1, deleted_at: 100 })
  ```

  The buggy merge picks the higher-schema record (`deleted_at: 0`) and drops the
  tombstone — a **deletion-resurrection** bug. This is the concrete failure the
  invariant exists to prevent, and the checker finds it automatically.

## Does Quint add value beyond SHACL + SPARQL?

**Yes — they verify orthogonal things and neither subsumes the other.**

SHACL and SPARQL operate on *one RDF graph, as it is right now*. They answer
"is this dataset well-formed?" — every dataset has an id, foreign keys resolve,
no duplicate field names, no dependency cycles (exactly the checks in
`../tests/*.rq`). They have no notion of an *operation*, a *replica*, or a
*sequence of states*, so they cannot express — let alone check — the properties
that define a CRDT.

Quint models the merge as a **state machine** and reasons about **all reachable
states**. That unlocks the class of properties the merge protocol actually lives
or dies by:

| Property | SHACL / SPARQL | Quint |
|---|---|---|
| Every dataset row has required fields / valid FKs | ✅ natural | ➖ out of scope |
| No dependency cycles in the graph | ✅ natural | ➖ out of scope |
| `merge(a,b) == merge(b,a)` (commutativity) | ❌ can't express | ✅ proven |
| `merge(a,a) == a` (idempotency) | ❌ can't express | ✅ proven |
| Convergence regardless of sync order | ❌ can't express | ✅ proven |
| Deleted records never resurrect across a merge | ❌ can't express | ✅ proven + counterexample when broken |
| Concurrent-writer data loss (in-place vs batch) | ❌ no notion of writers/time | ✅ distinguished |

The reason is structural, not a tooling gap: SHACL/SPARQL quantify over the
*triples in a graph*; the CRDT laws quantify over *pairs and triples of possible
store states and the merge function applied to them*. There is no graph you can
write a shape against that says "for all A, B: merging them both ways gives the
same result" — the universal is over inputs to a function, which is precisely
what a model checker enumerates and SHACL cannot.

**Cost.** Quint needs a JVM + Apalache for `verify` (present here as
`~/.quint/apalache-dist-0.56.1`), and the exhaustive check is bounded — you
choose a small finite universe (here 1–2 IDs, 2 schema versions, 2 delete
states) and trust that the properties generalise. `quint run` needs no JVM and
is a fast first line of defence. SHACL/SPARQL run in-process (rdflib, no JVM)
and scale to the whole real dataset. So the toolchain split is:

- **SHACL + SPARQL** — validate the *data* (structure, referential integrity) at
  full scale, in CI, on every real graph.
- **Quint** — validate the *protocol* (the merge algebra) once, exhaustively,
  before/while implementing it; then keep the Go implementation honest via the
  exported ITF traces (`auto-etl/internal/merge/`).

**Recommendation:** keep Quint as the behavioural layer of the verification
toolchain. It is the only tool here that can catch a deletion-resurrection or a
non-convergent-merge bug, and it caught exactly that in the negative control.
Its scope is the merge protocol specifically — not general data validation,
which SHACL + SPARQL already cover well.

## Follow-on

`auto-q0q` (blocked on this task) — replay the exported ITF traces through a Go
test harness so the `merge.go` implementation is continuously checked against the
model, not just at spec-authoring time. The ITF parser
(`auto-etl/internal/merge/itf.go`) and 500 testdata traces already exist; that
task wires them into the merge conformance tests.
