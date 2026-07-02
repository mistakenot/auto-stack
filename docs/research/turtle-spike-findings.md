---
hash: "785d01e3"
read_when: "extending the turtle verification pipeline, deciding between SHACL/Alloy/Quint for a verification task, planning struct conformance checking, or designing constraint-to-test traceability"
summary: "Findings from building and running the RDF/Turtle verification pipeline spike, plus the emerging design for a three-layer verification stack (structural, behavioral, traceability) covering spec-to-code conformance."
title: "Turtle Spike Findings: Verification Toolchain for Auto-Stack"
---

# Turtle Spike Findings: Verification Toolchain for Auto-Stack

Companion to `turtle-spec-research.md` (the pre-spike design). This document
captures what we learned building the spike, where the value actually landed,
and the emerging design for closing the gap between spec and implementation.

---

## 1. What we built

A working 4-stage verification pipeline under `turtle-spike/`, validated
end-to-end with 3 deliberate breakage modes.

### Pipeline stages

```
parse  →  lint  →  validate (SHACL)  →  test (SPARQL)
```

| Stage | Tool | What it catches | Runtime |
|-------|------|-----------------|---------|
| Parse | rdflib | Syntax errors (unclosed IRIs, missing periods) | ~ms |
| Lint | rdflib + custom | Unknown namespaces, unknown predicates | ~ms |
| Validate | pyshacl | Constraint violations (missing required fields, wrong types, cardinality) | ~100ms |
| Test | rdflib SPARQL | Graph invariants (cycles, orphans, connectivity, duplicates) | ~100ms |

### Spec files

Two TTL specs totalling 724 triples:

- **`spec/system.ttl`** (143 triples) — 8 auto-stack components, 5 architectural
  layers, 3 statuses, dependency graph, system root. Uses `auto:` namespace.
- **`spec/auto-etl.ttl`** (581 triples) — detailed auto-etl internals: 2 datasets
  (sessions, messages) with all 55 fields from the real `AgentSession`/`AgentMessage`
  Go structs, 4 pipeline stages with ordering and `feedsInto` links, 2 parsers,
  3 CLI commands, foreign keys, message role vocabulary. Uses `etl:` namespace.

### Shapes

- **`shapes/shapes.ttl`** — ComponentShape (name, owner, layer, status required),
  SystemShape (at least one component).
- **`shapes/etl-shapes.ttl`** — DatasetShape (fields, partition, schema version,
  output path), FieldShape (name, type, required flag), PipelineStageShape
  (order), ParserShape (parses a format), CLICommandShape (name),
  SourceFormatShape (name).

### SPARQL tests (8 total)

System-level:
- No dependency cycles (via `dependsOn+` property path)
- All components reachable from system root
- No self-dependencies

ETL-specific:
- Pipeline stages connected (every non-first stage fed by another)
- Foreign keys point at real fields
- Every dataset has a required `id` field
- No duplicate field names within a dataset
- CLI commands only invoke real pipeline stages

---

## 2. What we learned

### Which stages earned their keep

**SHACL validation — highest value, most design effort.** Writing shapes forces
you to answer "what does correct mean." The validation reports are specific
enough for an agent to act on: `Focus Node: auto:AutoETL / Result Path:
auto:hasOwner / Message: Less than 1 values`. This is the stage that justifies
the whole pipeline.

**SPARQL tests — high value for graph invariants.** The cycle detection query
(`?x auto:dependsOn+ ?x`) is elegant and caught a real planted bug, returning
both nodes in the cycle. These should grow like a test suite — add one every
time a spec bug is found.

**Parse — near-zero cost, catches dumb errors.** Milliseconds, line numbers in
errors. Prevents malformed TTL from reaching heavier stages.

**Lint — moderate value, keep it lean.** Catches typos and rogue namespaces that
SHACL can't flag. Most valuable for unknown predicates (misspellings that
silently create new triples).

### Runtime decision

**Python-only wins for structural specs.** rdflib + pyshacl in a venv, no JVM.
Sub-2-second full pipeline on 724 triples. The tradeoff: rdflib's Turtle
serializer doesn't produce deterministic sorted output like `rdf-toolkit`
(JVM), so the format stage is weaker. Acceptable for a spike; production spec
management would need a canonical formatter.

### OWL reasoning: skipped, correctly

The auto-stack spec is purely structural. No disjointness axioms, no property
transitivity, no OWL cardinality restrictions. SHACL + SPARQL covered every
validation needed. If the spec later grew real OWL semantics, Stage 4 would
earn its keep — but not before.

### Surprising findings

1. **SHACL `sh:class` beats `sh:in` for enums.** `sh:class auto:Layer` works
   better than `sh:in` with RDF lists — adding a new layer only requires a new
   instance, not editing the shapes file. `sh:in` is syntactically fragile.

2. **Property paths work for cycle detection without a reasoner.** rdflib's
   SPARQL engine handles `dependsOn+` natively and correctly identifies all
   nodes in a cycle.

3. **The pipeline caught a real spec bug during construction.** The
   `etl-pipeline-connected` test caught that `DiscoverStage` was missing its
   `feedsInto ParseStage` link — exactly the kind of gap between mental model
   and written spec that this toolchain exists to find.

---

## 3. SHACL vs Alloy: where the overlap is

The existing Alloy conformance model for auto-etl (`alloy/session_message_conformance.als`)
found 5 concrete gaps: orphan subagents, missing tool_use pairs, zero-timestamp
sessions, duplicate tool_use IDs, empty sessions.

**Every one of those is now expressible as a SHACL shape or SPARQL test.** "If
`is_subagent` is true then `parent_session_id` must be non-empty" is a SHACL
conditional shape. "No two messages share a `tool_use_id` within a session" is
a SPARQL `GROUP BY / HAVING` query.

The difference: SHACL validates **a specific graph you wrote**. Alloy explores
**all possible graphs that could exist** within bounds. SHACL answers "is this
spec correct?", Alloy answers "could any spec pass my rules and still be
broken?"

**Practical conclusion:** for CI, SHACL + SPARQL cover the same ground as Alloy
with less friction. Alloy's value is at design time — when writing new shapes,
use Alloy to explore what you might have forgotten to constrain. It becomes an
exploratory tool, not a CI gate.

---

## 4. The three-layer verification stack

Three tools form a layered stack, each answering questions the layers below
cannot:

```
┌───────────────────────────────────────────────────────────┐
│  Quint                                                    │
│  "What can happen over time?"                             │
│  Temporal properties, concurrency, protocol correctness   │
│  e.g. "Can concurrent ETL runs corrupt a partition?"      │
├───────────────────────────────────────────────────────────┤
│  Alloy (exploratory, design-time)                         │
│  "What did I forget to constrain?"                        │
│  Bounded counterexample generation                        │
├───────────────────────────────────────────────────────────┤
│  RDF/Turtle + SHACL + SPARQL                              │
│  "Is the spec well-formed and rule-compliant?"            │
│  Syntax, structural constraints, graph invariants         │
│  CI gate: every commit                                    │
└───────────────────────────────────────────────────────────┘
```

### Quint for behavioral verification

Quint is TLA+ with TypeScript-like syntax, static types, a built-in simulator,
and a REPL. Created by Informal Systems (Cosmos/Tendermint verification team).
It compiles to TLA+ and uses Apalache for exhaustive model checking.

**Why Quint over raw TLA+:** it's an `npm install`, not a Java GUI. Write specs
that look like TypeScript, `quint run` to simulate immediately, `quint verify`
for exhaustive checking. The REPL lets you explore state transitions
interactively.

**What Quint would answer for auto-etl:**
- Can two concurrent `run --only sessions` invocations corrupt the same partition?
- Does the CRDT merge protocol always converge regardless of message ordering?
- Can the GitHub sync high-water mark skip a PR merged during a fetch?
- Does the incremental sync state always advance (liveness)?

**Relationship to Alloy:** complementary, not competing. Alloy checks relational
invariants on data structures. Quint checks temporal properties on protocols.
Alloy asks "can this data exist?", Quint asks "can this sequence of events
happen?"

---

## 5. Closing the spec-to-code gap

The spec verifies itself. It cannot verify that the real Go code matches. Three
approaches close this gap, ordered by when to build them:

### 5a. Go struct → TTL extractor (build next)

Parse Go structs via `go/ast`, extract fields, types, struct tags, generate TTL
triples, and merge them into the ontology. Run SHACL shapes against the merged
graph. If someone adds a field to `AgentSession` and forgets to update
`auto-etl.ttl`, the shapes catch it.

The mapping is mechanical and unambiguous:

```
Go struct AgentSession        →  owl:Class etl:AgentSession
Field ID string               →  owl:DatatypeProperty, range xsd:string
parquet:"id"                  →  etl:fieldName "id"
parquet:"host_id,dict"        →  etl:isDictEncoded true
```

Go's rigid type system means the extraction has no guesswork. Struct tags give
the parquet schema for free.

**What it can extract:** classes, properties, types, cardinalities, dict-encoding
flags, field names.

**What it cannot extract:** business rules ("if `IsSubagent` then
`ParentSessionID` must be non-empty"). Those stay in hand-written SHACL shapes.

The split:

| Auto-generated (go/ast) | Hand-written (human knowledge) |
|---|---|
| Classes, properties, types | SHACL shapes with business rules |
| Field names, parquet tags | Cross-entity constraints |
| Dict-encoding, type mapping | Pipeline stage relationships |
| FK hints (by naming convention) | Temporal invariants (Quint) |

### 5b. Constraint-to-test traceability (build second)

Model both spec constraints and Go test functions in the ontology, then link
them:

```
Constraint (SHACL shape / SPARQL test)
    ↕ verifiedBy / verifies
Go test function
    ↕ testsField / testsInvariant
Struct field / dataset / pipeline stage
```

The queries that matter:

- **"Which constraints have no Go test?"** — coverage gaps, machine-found.
- **"Which Go tests don't trace to any constraint?"** — tests verifying
  implementation details but no spec property; candidates for deletion or
  promotion to spec constraints.
- **"I changed this SHACL shape — which Go tests are affected?"** — impact
  analysis.
- **"What percentage of the auto-etl spec is verified by real tests?"** — a
  number you can report and ratchet.

Linking approaches (cheapest to most precise):

1. **Naming convention** — `no-dependency-cycles.rq` ↔ `TestNoDependencyCycles`.
   Fuzzy string match gets you surprisingly far.
2. **Go test comments** — `// verifies: etl:FieldShape` annotations parsed by
   the extractor.
3. **Explicit mapping file** — hand-maintained TTL linking constraints to tests.
   Most precise, most maintenance.

This plays to RDF's actual strength: **cross-artifact joins** that no single
tool can do. Go tools know about Go tests. SHACL knows about shapes. Neither
knows about the other. The graph connects them and SPARQL queries the
connection.

The payoff compounds: every new SHACL shape without a corresponding Go test
gets flagged by CI. The traceability matrix stays live instead of rotting in a
spreadsheet.

### 5c. Quint trace replay (build third)

Quint's simulator exports execution traces (ITF format). Feed those traces as
test inputs to the real Go code and assert the implementation produces the same
state transitions. The plumbing already exists: `auto-etl/internal/merge/itf.go`
is an ITF trace parser from the Alloy spike. Quint generates traces, Go tests
replay them against `merge.go` or `writer.go`, any divergence is a conformance
failure.

Longer term, instrument the pipeline to emit structured events via the bus,
then replay real production event logs against the Quint spec to check that
actual runtime behavior satisfies temporal properties.

---

## 6. What's missing for production use

From the spike:
- **Canonical formatting** — rdflib's serializer doesn't produce stable sorted
  output. Need `rdf-toolkit` (JVM) or a custom normalizer for clean diffs.
- **CI workflow** — Makefile is ready, needs a `verify.yml` GitHub Actions job.
- **Pre-commit hooks** — parse + lint should run on save.
- **More shapes** — inter-component constraints (e.g. "PresentationLayer must
  not dependsOn AutomationLayer directly").
- **Spec coverage** — only auto-etl modeled in detail. Other components need
  the same treatment as the spec grows.

From the broader design:
- **Go struct extractor** — the highest-value next step.
- **Quint spike** — model one protocol (partition write or CRDT merge) to
  validate the approach.
- **Traceability prototype** — extract Go test names, match to SPARQL test
  names, report coverage.

---

## 7. Schema viewer

An interactive vis.js network graph of the full ontology (97 nodes, 173 edges)
is hosted at:

https://auto-artifact-datadyne.s3.eu-west-1.amazonaws.com/365d/04947e05-231a-4ca2-a828-6d8a16e512e4/schema-viewer.html

Features: type filtering, legend toggles, search, hierarchical layout,
click-for-detail panel. Retained for 365 days.

---

## 8. Decision log

| Decision | Rationale |
|---|---|
| Python-only toolchain | Lighter than polyglot JVM+Python; sufficient for structural validation |
| Skip OWL reasoning | Spec is purely structural; SHACL + SPARQL cover all current needs |
| SHACL over Alloy for CI | Same coverage, less friction; Alloy kept for design-time exploration |
| Quint over raw TLA+ | npm install, TypeScript syntax, built-in simulator, same TLA+ semantics |
| Struct extractor before traceability | Closes the most common drift scenario (field added, spec not updated) |
| Traceability via graph, not spreadsheet | Machine-queryable, CI-enforceable, compounds over time |

---

*End of document.*
