---
name: Differential Testing
summary: Runs a second independent implementation — a deliberately-simple reference oracle, or an external spec/competitor/legacy build — on the same inputs and asserts identical outputs, manufacturing an oracle where none exists
oracle: differential
archetypes: algorithmic-core
criticality-min: C2
volatility-fit: both
harness: ci
pairs-with: property-based-testing, fuzzing, model-based-testing
upgrade-looser: property-based-testing
upgrade-stricter: formal-spec-model-checking
cost-author: medium
cost-maintain: medium
cost-run: medium
---

## What it is & what it catches/misses

Differential testing runs two independent implementations of the same behavior on the same input and asserts they produce the same output. The second implementation is the **oracle**: any disagreement is a bug in one of them (almost always the one under test). It solves the hardest problem an autonomous agent faces — verifying code when nobody has computed the right answer — by *manufacturing* an oracle instead of hand-writing expected values.

There are two flavors of the oracle, same mechanic:

- **Reference oracle (home-grown):** you write the functionality *twice* — the real way (fast, clever, optimized, concurrent) and the stupidest possible way (slow, single-threaded, naive, obviously correct by inspection). The dumb twin exists only to judge; it never ships. Example: a query planner with 13 index types checked against a linear table scan; a SIMD JSON parser checked against a naive recursive-descent one; a custom concurrent B-tree checked against a plain sorted map.
- **External oracle (already exists):** the trusted twin is not yours — an official conformance suite (openCypher TCK, a Wasm/SQL spec), a competitor product (parity-test against Neo4j/Memgraph/SQLite), a legacy implementation being replaced, or a second code path of your own (interpreted vs compiled, `-O0` vs `-O2`). You diff your output against theirs.

The defining move is that **the oracle is a full-fidelity, concrete implementation** — it computes the *actual answer*, byte-for-byte, and is simpler only in *mechanism*, not in *what it computes*. This is what separates it from the abstract, lossy models used by model-based testing (see the distinction below). Differential testing is at its best fed by a generator — random or fuzzed inputs through both implementations — so it is the canonical bundle partner for property-based testing and fuzzing.

**Catches:** any output discrepancy on a single call, including pure-computation bugs that have no sequencing at all — a codec that mishandles one Unicode escape, an optimized path that returns a row the brute-force scan doesn't, a rounding divergence, a cache that serves a stale value, an off-by-one the naive version gets right. Because the oracle is total (predicts every bit of the answer), it catches classes that invariant-only tests (PBT) miss: PBT tells you *a property held*; differential tells you *the exact answer was right*. Especially strong for reimplementations, optimizations, ports, parsers/serializers, compilers, query engines, numeric kernels, and codecs — anywhere a slow-but-obvious twin is cheap to write or already exists.

**Misses:** it cannot catch a bug both implementations share. If the agent makes the same wrong assumption in the reference and the real code (a *correlated* fault), they agree on the wrong answer and the test passes green — the single most important limitation, and the reason the reference must be built so simply that whole fault classes (concurrency, caching, optimization) *cannot occur* in it. It says nothing about behavior neither implementation is asked to produce (performance, UI, security properties). It needs the two outputs to be *comparable* — nondeterminism (unordered results, timestamps, hash-seeded iteration) must be canonicalized first or every run is a false alarm. And it is the wrong tool when no independent implementation can exist at less-than-the-cost-of-the-real-one — if the reference would be as complex as the code, it duplicates any conceptual bug and tests nothing.

**Key distinction from model-based testing:** MBT's twin is an *abstract model* — deliberately lossy, predicting equivalence-class state (`empty/full`, `connected/authenticated`) and *which operations are legal*, across *sequences* of operations. A reference oracle is a *concrete full implementation* — lossless, predicting the *exact answer* on a *single input*, and it works on stateless/pure code where MBT has no sequences to explore. Use MBT when the bug is in the *ordering* of operations and the oracle is a set of *rules*; use differential when the bug is in the *computed answer* and the oracle is a second *implementation*. They merge at one point: a concrete stateful reference driven by random operation sequences, comparing full state after each step, is simultaneously the stateful rung of MBT and differential testing — when you're there, prescribe MBT and reuse its harness.

## When to prescribe / when not

**Prescribe when:**
- The code has **no natural exact-oracle** — nobody has the "right answer" written down, and outputs are too complex to hand-verify (novel algorithms, engines, planners, solvers, codecs).
- A **simpler correct implementation is cheap to write** (a naive O(n²) version, a single-threaded version, a reference algorithm from a paper) — the reference's whole value is being *obviously* correct.
- A **trusted external oracle already exists** — a conformance suite, spec, competitor, or legacy build you can diff against for near-zero authoring cost.
- You are **reimplementing, optimizing, porting, or refactoring** something that already works — keep the old path (or a golden capture of it) as the oracle and assert output equivalence across the change.
- Criticality is C2+ and the correctness of the *concrete output* (not just an invariant) matters. The light external-oracle rung is cheap enough to justify even at C2.

**Do not prescribe when:**
- The reference implementation would be **as complex as the code under test** — it will share the same conceptual bugs and test nothing. This is the primary disqualifier; if you cannot make the twin meaningfully simpler or independent, reject differential testing and record why.
- The bug class is in the **sequence/ordering** of stateful operations and the oracle is a set of legality rules, not a second implementation → use model-based testing.
- **No independent oracle is obtainable at all** — you can't build a simpler twin and none exists externally. Fall back to metamorphic relations or property invariants (the `upgrade-looser` path).
- Outputs are **irreducibly nondeterministic** and cannot be canonicalized into a comparable form (and you're unwilling to pin determinism) — differential will drown in false positives.
- The oracle is **subjective or experiential** (visual layout, UX) — no second implementation adjudicates "looks right."

## Prerequisites

**Artifacts:** a unit of behavior with a well-defined input→output contract that a *second* implementation can reproduce — a pure function, a stateless transform, or a stateful subsystem with a projectable state. The implementing agent must be able to name (1) the input space and how to generate/collect inputs, (2) the source of the oracle (a twin they will write, or an external suite/competitor/legacy path they will call), and (3) an **equivalence relation** — what "same output" means once nondeterminism is normalized (sorted result sets, rounded floats, canonicalized JSON). If no simpler-or-external oracle can exist, differential testing does not apply.

**Infrastructure:** an input source — reuse the PBT generators or a fuzzer wherever possible so inputs are numerous and adversarial rather than hand-picked. A comparison harness that runs both implementations and diffs canonicalized outputs, reporting the *minimal* diverging input (delegate shrinking to the PBT framework, or add delta-debugging for fuzzed corpora). For external oracles: a client/adapter to the spec suite or competitor, and a version pin so the oracle itself doesn't drift silently. CI integration is strongly recommended; the differential run belongs in `make verify`. The reference oracle, if home-grown, is compiled/gated for test builds only so it never ships.

## Design decisions

The architect must decide:

- **Oracle source — build vs borrow:** a home-grown reference (full control, but authoring cost and correlated-fault risk) versus an external suite/competitor/legacy path (near-free, but you inherit its semantics and version drift). Default: borrow when a credible external oracle exists; build a naive reference otherwise. Document which, and for a built reference, *what it deliberately omits* (concurrency, caching, optimization) so the independence argument is explicit.
- **Independence discipline (the load-bearing decision):** the reference must be independent enough that it cannot share the real code's bug. State the mechanism: a different algorithm, a different language/library, a radically simpler structure, or a captured golden output from a version known-good. If the only correct reference is a copy of the implementation, record that differential was rejected.
- **Equivalence relation and canonicalization:** exactly what "outputs agree" means. Define the normalization applied before comparison — sort unordered collections, quantize floats to a tolerance, strip/rewrite timestamps and non-deterministic IDs, canonicalize serialization. Under-specifying this is the #1 source of false positives. State it as the pass/fail definition.
- **Input source and volume:** reuse PBT generators, a fuzzer's corpus, captured production traces, or a curated seed set — and how many cases per run (local vs CI). Prefer construct-valid generators over reject-filtering, same as the PBT card.
- **Divergence handling:** when they disagree, which is presumed wrong (usually the system under test, but a spec-conformance failure may indict your reading of the spec), how the minimal counterexample is produced (framework shrinking / delta-debug), and that the diverging input is added to a checked-in regression corpus.
- **Oracle lifecycle:** for a home-grown reference, how it stays correct as the contract evolves (it's code that needs its own review); for an external oracle, the pinned version and the policy for bumping it. A silently-drifting oracle turns green into meaningless.

## Derivation guidance

Heuristics the architect embeds so implementing agents can find the oracle:

1. **Optimized ↔ naive:** any performance-motivated implementation (SIMD, caching, parallelism, index, incremental) has an obvious slow twin — the brute-force loop, the recompute-from-scratch path. This is the highest-yield reference oracle. Write the slow one; diff.
2. **New ↔ legacy (reimplementation/port):** when replacing or porting code, the thing you're replacing *is* the oracle. Run both on the same inputs during the migration; retire the old path only when they've agreed across a large corpus. For a port, the source-language implementation is the oracle for the target-language one.
3. **Spec/conformance suite exists:** if the domain has a standard (openCypher TCK, SQL logic tests, Wasm test suite, CommonMark spec, Unicode data files), the suite is a ready-made external oracle — wire it up before writing bespoke tests.
4. **Competitor/parity:** a mature product implementing the same contract (SQLite for a SQL engine, Neo4j for Cypher, reference `libc` for a math routine) is an oracle for the subset of behavior you intend to match. Scope the matched subset explicitly.
5. **Two internal paths:** interpreted vs compiled, `-O0` vs `-O2`, streaming vs batch, the fast path vs the fallback — these must agree by construction and diff against each other for free.
6. **Golden capture as frozen oracle:** when no live twin is affordable, capture the current known-good output for a corpus of inputs and freeze it; the change under test must reproduce the golden set (this is the snapshot/approval degenerate case — the oracle is *past you*).
7. **Hard-to-compute, easy-to-check inverse:** if a full twin is too costly, sometimes the cheap oracle is the *inverse* (see the PBT "hard to prove, easy to verify" heuristic) — run the solver, feed the result back through the forward function, diff against the target. This shades into property-based testing.

## Minimum viable instance vs full rigor

Choose the rung that matches the four axes; differential testing can start as a handful of assertions against an existing suite and only graduate to a full continuously-run reference-oracle harness when the system deserves it.

**Light / minimum viable (30–60 minutes):** either (a) wire up an existing external oracle (conformance suite, competitor, or a golden capture of the current output) and diff a dozen representative inputs, or (b) write one naive reference for the single most-suspect function and diff it against the real one over a few hundred generated inputs. Run locally. This already catches computation bugs no invariant test would, and proves the comparison harness works. Time is dominated by *defining the equivalence relation*, not by writing the twin.

**Standard:** a home-grown reference oracle (or wired external oracle) for the changed/high-risk surface, driven by PBT generators so inputs are numerous, with canonicalization and shrinking producing minimal counterexamples. Diverging inputs captured to a checked-in regression corpus. CI integration once the suite is stable. For a migration, run new-vs-legacy in shadow across a real workload.

**Full rigor:** reference oracles (or pinned external suites) across the public contract, fed by fuzzing and captured production traces as well as generators, with delta-debugging for minimal reproductions, an independence argument documented per oracle, oracle-version pinning with a bump policy, mutation-tested detection power on the comparison harness, and (for stateful surfaces) escalation to model-based conformance. Divergence corpus is permanent and replayed every run.

## Harness changes

| Component | Delta |
|---|---|
| `docs/testing.md` | Add a "Differential Testing" section: chosen rung, oracle source(s) and their independence argument, what the home-grown reference deliberately omits, the equivalence relation + canonicalization rules, input source (generators/fuzzer/traces), external-oracle version pins, and the divergence-corpus location |
| `make verify` | Add a `verify-differential` subtarget (running the comparison harness) and compose it into the top-level `make verify`. If inputs come from PBT generators, it may live alongside the property suite instead — either way it must be reachable from `make verify` |
| Reference oracle code | Home-grown references are gated to test builds only (feature flag / test module) so they never ship; they get code review like any other correctness-critical code |
| `AGENTS.md` / `CLAUDE.md` | Add line: "Changes to [scoped modules] must pass differential tests against [oracle] — see docs/testing.md for the equivalence relation and oracle source" |
| CI workflow | `make verify` already runs in CI; pin external-oracle versions in the lockfile/CI image so the oracle doesn't drift under you |
| Evidence conventions | On failure: the minimal diverging input, both outputs (canonicalized and raw), which side is presumed wrong, and the added regression entry, in `.evidence/` or CI log. On success: input count, oracle identity + version, and canonicalization applied |

## How to get to a walking skeleton

1. **Pick the oracle:** choose the cheapest credible one — an existing suite, a golden capture, or the single naive twin you can write in minutes.
2. **Define the equivalence relation:** write the canonicalization + comparison function first (sort, round, strip timestamps). This is the part that's actually hard; nail it before wiring inputs.
3. **Wire one input through both:** run a single hand-picked input through the system and the oracle; confirm the comparison passes on agreement.
4. **Write the fail-fake:** deliberately break the equivalence check (or point it at a knowingly-wrong constant) and confirm the harness reports a divergence *with the offending input* and exits non-zero through `make verify`.
5. **Write the pass-fake:** restore it and confirm a matching pair passes and emits success evidence (input count, oracle identity).
6. **Connect the generator/fuzzer:** replace the single input with the PBT generator or fuzz corpus; run a few hundred cases locally.
7. **Run via `make verify`:** confirm the differential subtarget is composed in and failures propagate.
8. **Confirm CI + pin the oracle:** push, observe green; for external oracles, pin the version in CI so a silent upstream change can't flip the result. Add the first real divergence you find to the regression corpus.

## Acceptance criteria to embed

The architect writes these into the generated artifacts so implementing agents can self-grade:

- [ ] Every function/subsystem in the differential inventory has an oracle (home-grown reference or external suite) and a diff harness that runs it against the system on shared inputs.
- [ ] The oracle's **independence** is documented: what it does differently (algorithm/language/structure) or, for external oracles, its identity and pinned version. A reference that merely restates the implementation is rejected.
- [ ] The **equivalence relation is explicit**: canonicalization rules (ordering, float tolerance, timestamp/ID stripping) are written down and applied before comparison.
- [ ] Inputs come from a generator, fuzzer, or captured trace — not only hand-picked examples — and volume is stated per environment.
- [ ] On divergence the harness reports the **minimal** offending input (shrinking / delta-debug) plus both outputs, and the input is added to a checked-in regression corpus.
- [ ] Home-grown reference oracles are gated to test builds and cannot ship.
- [ ] External oracles are version-pinned so the oracle cannot drift silently.
- [ ] `make verify` (including the differential subtarget) passes locally and in CI.
- [ ] **Kill test:** introduce a deliberate bug in the real implementation (off-by-one, wrong rounding, dropped edge case). The differential harness must diverge and surface the minimal input that exposes it. Record the mutation, the diverging input, and both outputs as evidence. Then confirm the *correlated-fault* limitation is understood: the same bug injected into *both* implementations would pass — note this in the strategy so the independence discipline is taken seriously.

## Composition

**Upstream feeds:**
- **Property-based testing / fuzzing** supply the inputs. Differential testing is what you assert *with* those inputs when a full oracle exists rather than only an invariant — the PBT "oracle comparison" heuristic *is* the light rung of this card. PBT + differential is the canonical bundle for algorithmic-core subsystems; fuzzing + differential is the canonical bundle for parsers/codecs/format readers.
- **Unit testing** covers the exact known-answer cases; differential extends coverage to the vast space where you *don't* know the answer but a twin does.

**Downstream consumers / escalation:**
- **Model-based testing** is the `upgrade-stricter` neighbor when the subsystem is stateful and the bug is in operation *ordering* — promote the concrete reference into a stateful conformance harness that compares full state across generated sequences.
- **Metamorphic testing / property-based testing** is the `upgrade-looser` fallback when no independent oracle can be built at all — give up on exact-output equality and assert relations between related inputs (`upgrade-looser` path).
- **Mutation testing** audits the comparison harness's detection power — a differential suite whose comparison is too lax (over-canonicalized) passes for mutants; mutation score is the check.
- **Formal spec / model checking** is the strongest neighbor: when even a trusted twin isn't enough (the twin can be wrong), a machine-checked proof of the core invariants supersedes differential for that tiny critical core.

## Failure modes & retirement triggers

| Sign | Diagnosis | Correction |
|---|---|---|
| Green suite but real bugs still escape | Correlated fault — the reference shares the implementation's bug, or the equivalence relation is over-canonicalized and hides the difference | Make the reference more independent (different algorithm/language/structure); tighten canonicalization; add mutation testing to audit the harness |
| Constant false-positive divergences | Nondeterminism not canonicalized — ordering, timestamps, float noise, hash-seed iteration differ legitimately | Strengthen the canonicalization/equivalence relation; pin determinism (fixed seeds, stable sort) in both implementations |
| Divergences point at the oracle, not the system | The external oracle drifted (version bump) or your reading of the spec is wrong | Pin the oracle version; when the spec genuinely disagrees with your intent, record the intentional deviation and exclude it from the diff |
| Reference oracle keeps needing changes | The reference is tracking implementation detail rather than the stable contract, or it was written too close to the real code | Rewrite the reference against the *contract* at the simplest possible abstraction; if it can't be kept simpler than the code, retire differential for this unit |
| Diverging inputs are huge/opaque | No shrinking/delta-debugging on the input | Delegate shrinking to the PBT framework or add delta-debugging over the fuzz corpus; ensure inputs have readable representations |
| Suite is slow | Running two full implementations over a large corpus every run | Split a fast local subset from a large CI corpus; cache oracle outputs for a fixed input set (golden mode); parallelize |

**Retirement triggers:** differential testing for a unit is a candidate for retirement when: (1) a formal spec with model checking subsumes the same guarantees exhaustively for that core; (2) a migration completes and the legacy oracle is decommissioned — freeze a golden corpus before deleting the old path; (3) the reference can no longer be kept meaningfully simpler or more independent than the implementation (it has become a correlated copy). Never retire a *migration* oracle before the new and old paths have agreed across a representative corpus; never retire without confirming the replacement covers the same output-discrepancy bug classes.

## Tool pointers

- **Input generation:** reuse the PBT libraries from the property-based-testing card (Hypothesis, fast-check, proptest, gopter, jqwik) — their generators and shrinking feed the diff harness directly.
- **Fuzzing (for parsers/codecs/format readers):** libFuzzer / AFL++ (C/C++/Rust via `cargo-fuzz`), Atheris (Python), Jazzer (JVM), go-fuzz / native Go fuzzing — pair coverage-guided corpora with a reference oracle.
- **Conformance/external oracles:** openCypher TCK and GQL feature matrices (graph), SQL Logic Test / sqllogictest (SQL engines), the CommonMark spec suite (Markdown), the Wasm test suite, Unicode/ICU data (text) — wire the standard suite as the oracle before hand-rolling one.
- **Snapshot/golden (frozen-oracle rung):** insta (Rust), jest/vitest snapshots (JS/TS), syrupy (Python), approvaltests (multi-language) — for the captured-golden-output degenerate case.
- **Minimization:** the PBT framework's shrinker for generated inputs; `C-Reduce` / `halfempty` / delta-debugging for reducing fuzzed corpus counterexamples.
- **Detection-power audit:** the mutation-testing tools (Stryker, mutmut, cargo-mutants) — run against the comparison harness to prove the equivalence relation isn't too lax.
