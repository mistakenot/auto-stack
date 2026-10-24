---
name: Metamorphic Testing
summary: Asserts known relations between the outputs of related inputs when no oracle exists — transforms an input in a known way and checks the output changes as it must; catches inconsistency, not wrongness
oracle: metamorphic
archetypes: algorithmic-core
criticality-min: C2
volatility-fit: both
harness: ci
pairs-with: property-based-testing, differential-testing, fuzzing
upgrade-looser: property-based-testing
upgrade-stricter: differential-testing
cost-author: medium
cost-maintain: low
cost-run: medium
---

## What it is & what it catches/misses

Metamorphic testing is the technique for when you **cannot check whether an output is correct, but you can check whether two related outputs are consistent with each other.** It exists to solve the *oracle problem*: the situation where nobody has computed the right answer and the output is too complex to hand-verify — exactly where an autonomous agent lands when building novel software (search ranking, ML inference, simulations, compilers, complex numerics, query engines).

Instead of asserting anything about a *single* output, you assert a **metamorphic relation (MR)**: a relationship between the outputs of two (or more) *related* inputs. You take an input, transform it in a way whose effect on the output you *do* know, run the system on both the original and the transformed input, and check the relation holds. You never state what either output *is* — only how they must relate. The shape is always: *"if I change the input like this, the output must change (or stay the same) like that."*

Examples: a search must never *gain* results when you add a filter (`results(q + filter) ⊆ results(q)`); `sort(shuffle(x)) == sort(x)` (reordering can't change the sorted output); doubling income must never *decrease* tax (`tax(2·income) ≥ tax(income)`); rotating an image 2° must not flip its class label; compiling at `-O0` and `-O2` must yield identical observable behavior; `decode(encode(x)) == x`.

**Pros:**
- **Needs zero oracle** — the only no-ground-truth technique that still gives a real correctness signal; works precisely where unit, differential, and even reference-oracle testing can't be applied.
- **Catches broad, high-value bug classes:** order-dependence, broken filter/constraint logic, off-by-ones at boundaries, stale caches, illegitimate non-determinism, ML brittleness, optimizer bugs.
- **Domain-meaningful and stable:** relations express real semantics ("more constraints ⇒ fewer results"), so they survive refactors — they assert *what must be true*, not *how the code works*. Low maintenance cost.
- **Cheap to author once a relation is found and composes with everything** — the transformations plug straight into a PBT generator or a fuzz corpus, so one relation becomes thousands of checks.

**Cons / what it misses:**
- **Catches inconsistency, not wrongness.** A system that is *uniformly, self-consistently wrong* passes every MR — if it's wrong the *same way* for `x` and `shuffle(x)`, the permutation relation stays green. It finds *contradictions between runs*, never absolute correctness.
- **Only as good as your domain understanding.** A *wrong* relation produces false alarms; a *too-weak* relation ("output is non-empty") catches nothing. Finding strong, true, non-trivial relations is genuine intellectual work and the main authoring cost.
- **Weaker guarantee than an oracle.** When a real oracle (a spec, a competitor, a simple reference impl) is obtainable, differential/unit testing is strictly stronger — metamorphic is the *fallback*, not the first choice.
- **Poor fault localization.** A failed MR tells you two runs disagree; it usually can't tell you *which* run is wrong or *why*, so debugging is harder than a single-input assertion.
- **Runs the system multiple times per check**, so it's more expensive per assertion than a one-shot test, and the follow-up transformation must itself be trusted (a buggy transform gives false failures).

**Key distinction from property-based testing:** a PBT invariant constrains a *single* run (`len(sort(xs)) == len(xs)` — one input, one output, one assertion). A metamorphic relation constrains *the relationship between two or more runs* on deliberately-related inputs (`sort(xs) == sort(shuffle(xs))` — two runs, compared). Metamorphic testing is best understood as the *multi-execution* branch of property-based testing; it reuses PBT's generators and shrinking wholesale and differs only in that the property spans runs. When your invariant already holds on one run, use PBT; reach for metamorphic when the only thing you can assert requires *comparing* runs.

## When to prescribe / when not

**Prescribe when:**
- The subsystem has a genuine **oracle problem** — outputs are complex, novel, or subjective and there is no known-correct answer and no simpler/external implementation to diff against (the differential card was considered and rejected for lack of an obtainable oracle).
- You *can* state at least one **true, non-trivial relation** between related inputs — a filter that must narrow, a transformation that must preserve, a scaling that must move the output monotonically.
- The bug class you fear is **inconsistency under transformation** — order-dependence, filter/constraint errors, non-determinism, cache staleness, ML fragility.
- Criticality is C2+ and the subsystem is algorithmic (search, ranking, ML, simulation, compiler, numeric engine, query planner).

**Do not prescribe when:**
- A **real oracle is obtainable** — an exact known answer (use unit testing), a spec/competitor/simple reference implementation (use differential testing). Metamorphic is weaker; don't reach for it when something stronger is in range.
- You **cannot state any relation stronger than "it doesn't crash"** — fall back to fuzzing / single-input property invariants (`upgrade-looser`).
- The only relations you can think of are **vacuous or trivially true** ("output is a list", "output is non-empty") — they give false confidence and catch nothing. If you can't find a relation that a plausible bug would violate, metamorphic testing adds noise, not signal.
- The suspected bug is one both the input and its transform would exhibit **identically** — metamorphic is blind to consistent wrongness; a different technique is needed.
- The subsystem is stateless with a small enumerable input space (unit tests suffice) or the concern is UI/visual (use visual regression).

## Prerequisites

**Artifacts:** a subsystem whose output is hard to verify directly but has *known behavioral symmetries* — properties that a class of input transformations must preserve or predictably change. The implementing agent must be able to name (1) an **input transformation** (shuffle, add-a-filter, scale, rotate, re-order, add-data, recompile-at-different-opt), (2) the **required output relation** it induces (unchanged / subset / monotone-increase / identical-behavior), and (3) an **equivalence or comparison operator** for outputs once legitimate non-determinism is canonicalized. If no non-trivial (transformation, relation) pair can be stated, metamorphic testing does not apply.

**Infrastructure:** an input source — reuse the PBT generators or a fuzz corpus so each relation runs over many source inputs, not a few hand-picked ones. A harness that, per source input, applies the transformation, runs the system on both, canonicalizes outputs, and checks the relation, reporting the *minimal* violating source input (delegate shrinking to the PBT framework). CI integration is recommended; the metamorphic run belongs in `make verify`. Because the system is executed multiple times per check, deterministic setup and a comparable output projection are required.

## Design decisions

The architect must decide:

- **The metamorphic relation inventory (the load-bearing decision):** which (transformation, relation) pairs to assert. Each must be *true* (a real semantic law of the domain) and *falsifiable by a plausible bug* — a relation no realistic defect would violate is wasted. Document each MR as "transform T applied to input ⇒ output must satisfy R", and state why a likely bug would break it. This is where the technique's value is created or lost.
- **Relation strength vs. safety:** prefer the *strongest true* relation (equality > subset > monotonicity > non-emptiness). Stronger relations catch more, but an over-stated relation that isn't actually guaranteed produces false alarms. When unsure, assert the safe weaker relation and record the stronger one as a candidate.
- **Equivalence / tolerance:** exactly how outputs are compared after the transformation — set-equality for unordered results, a numeric tolerance for floats, canonicalized serialization, label-equality for classifiers. Under-specifying this is a primary false-positive source; over-canonicalizing hides real bugs. State it per relation.
- **Input source and volume:** reuse PBT generators / fuzz corpus / captured traces, and how many source inputs per relation per environment (local vs CI). Construct-valid generators over reject-filtering, same as the PBT card.
- **Transformation trust:** the follow-up transformation is itself code and can be buggy — decide how it's kept simple and (where possible) independently checked, so a failed MR indicts the system under test, not the transform.
- **Violation handling:** which run is presumed wrong (often undecidable — record both), how the minimal violating source input is produced (framework shrinking), and that violating inputs go into a checked-in regression corpus.

## Derivation guidance

Heuristics the architect embeds so implementing agents can find relations. This catalog *is* the technique — most of the work is recognizing which pattern applies.

1. **Permutation / reorder invariance:** reordering the input must not change the (logical) output. `sort(shuffle(xs)) == sort(xs)`; a query returns the same result set regardless of row insertion order; a set operation ignores element order. Catches order-dependence — one of the highest-yield MRs.
2. **Subset / superset monotonicity:** adding a constraint can only shrink the result; adding data can only grow it. `results(q + filter) ⊆ results(q)`; `count(data + row) ≥ count(data)`. Catches broken filter/constraint/pagination logic.
3. **Scaling / additive relations:** a known change to the input moves the output predictably. Doubling all quantities doubles the total; increasing income never decreases tax (monotone); `f(a + b)` relates to `f(a)` and `f(b)`. Catches sign errors, non-monotonic bugs, boundary miscalculations.
4. **Semantics-preserving mutation:** change the input in a way that *must not* change the output. Rename a variable and recompile → same behavior; rephrase a query into an equivalent form → same results; add whitespace to source → same parse tree; rotate/scale/adjust-brightness an image slightly → same classification. Catches over-sensitivity and optimizer/normalization bugs.
5. **Equivalent-path agreement:** two ways of producing the same result must agree. `-O0` vs `-O2` builds; streaming vs batch computation; the fast path vs the fallback; computing a total two different ways. (Where a *full second implementation* exists this becomes differential testing — its `upgrade-stricter` neighbor.)
6. **Inverse / round-trip:** a transform composed with its inverse returns the original. `decode(encode(x)) == x`, `deserialize(serialize(x)) == x`, undo-after-do. The special case that's also a PBT staple.
7. **Idempotence:** re-applying a transformation changes nothing after the first. `normalize(normalize(x)) == normalize(x)`; re-running a converged computation yields the same result. Common in normalization, dedup, formatting, cache warming.
8. **Decomposition / composition:** splitting the input and combining partial results equals processing it whole. `sum(map(f, split(xs))) == f(xs)` for a distributive `f`; MapReduce-style consistency. Catches aggregation and partitioning bugs.

## Minimum viable instance vs full rigor

Choose the rung that matches the four axes; metamorphic testing can start as one relation over a generator and only graduate to a systematic relation inventory when the system deserves it.

**Light / minimum viable (30–60 minutes):** identify the single strongest true relation for the most-suspect subsystem (usually permutation-invariance or filter-monotonicity), write one metamorphic test that applies the transform and checks the relation over a few hundred generated source inputs, run locally. This already catches order-dependence and filter bugs no single-input test would, and proves the harness works. Time is dominated by *finding a strong true relation*, not writing the test.

**Standard:** a small inventory (3–6) of relations covering the changed/high-risk surface using the derivation catalog, each driven by PBT generators, with per-relation equivalence/tolerance defined and shrinking producing minimal violating inputs. Violations captured to a checked-in regression corpus. CI integration once stable.

**Full rigor:** a systematic relation inventory across the algorithmic surface using every applicable derivation pattern, fed by fuzzing and captured production traces as well as generators, with documented justification per relation (why a plausible bug breaks it), transformation-independence arguments, delta-debugging for minimal reproductions, and mutation-tested detection power to prove the relations aren't vacuous. Permanent regression corpus replayed every run.

## Harness changes

| Component | Delta |
|---|---|
| `docs/testing.md` | Add a "Metamorphic Testing" section: chosen rung, the relation inventory (each as transform ⇒ relation, with why a bug breaks it), equivalence/tolerance per relation, input source (generators/fuzzer/traces), and the violation-corpus location |
| `make verify` | Metamorphic tests are a *style* of property test — by default they live alongside the property suite under `verify-properties` (they reuse PBT generators/shrinking). Add a dedicated `verify-metamorphic` subtarget only if the inventory is large enough to warrant separate reporting. Either way it must compose into the top-level `make verify` |
| `AGENTS.md` / `CLAUDE.md` | Add line: "Changes to [scoped modules] must preserve the metamorphic relations in docs/testing.md — see it for the relation inventory and transformations" |
| CI workflow | `make verify` already runs in CI; no extra wiring if the subtarget is composed in |
| Evidence conventions | On failure: the violated relation, the minimal source input, the transformed input, both outputs (canonicalized), and the added regression entry, in `.evidence/` or CI log. On success: relation count, source-input count per relation |

## How to get to a walking skeleton

1. **Pick one strong relation:** the single highest-yield MR for the subsystem (usually permutation-invariance or filter-monotonicity).
2. **Write the transformation + comparator:** the input transform (shuffle / add-filter / scale) and the output equivalence check (set-equality / tolerance). Get these right first — they're the hard part.
3. **Run one source input through both:** apply the system to an input and its transform; confirm the relation holds on a known-good case.
4. **Write the fail-fake:** deliberately assert a *false* relation (e.g., "adding a filter must *grow* results") and confirm the harness reports a violation *with the minimal source input* and exits non-zero via `make verify`.
5. **Write the pass-fake:** restore the true relation and confirm a satisfying case passes and emits success evidence (relation name, input count).
6. **Connect the generator/fuzzer:** replace the single input with the PBT generator; run a few hundred source inputs locally.
7. **Run via `make verify`:** confirm the subtarget composes in and violations propagate.
8. **Confirm CI + seed the corpus:** push, observe green; add the first real violation you find to the regression corpus.

## Acceptance criteria to embed

The architect writes these into the generated artifacts so implementing agents can self-grade:

- [ ] Every subsystem in the metamorphic inventory has at least one relation implemented as (transform → run both → check relation) over generated inputs.
- [ ] Each relation is documented with its transformation, its required output relation, and **why a plausible bug would violate it** — no vacuous relations.
- [ ] No relation is trivially true: commenting out the core logic (or returning a constant) must break at least one relation. (This is the vacuity guard, mirroring the PBT non-vacuity criterion.)
- [ ] Each relation states its **equivalence/tolerance** (set-equality, float tolerance, canonicalization) and legitimate non-determinism is canonicalized before comparison.
- [ ] Inputs come from a generator, fuzzer, or captured trace — not only hand-picked examples.
- [ ] On violation the harness reports the **minimal** source input (shrinking), the transformed input, and both outputs; the violating input is added to a checked-in regression corpus.
- [ ] `make verify` (including whichever subtarget owns the metamorphic tests) passes locally and in CI.
- [ ] **Kill test:** inject a deliberate bug the relation *should* catch (e.g., a filter that occasionally adds a result, an order-dependent code path). At least one relation must be violated; record the bug, the violated relation, and the minimal input. Then note the blind spot explicitly: a bug that corrupts *both* the input and its transform identically would pass — document that metamorphic catches inconsistency, not absolute correctness, so the team doesn't over-trust a green run.

## Composition

**Upstream feeds:**
- **Property-based testing** is the direct parent — metamorphic relations are multi-execution properties and reuse PBT generators and shrinking wholesale. If a single-run invariant suffices, that's PBT; when the only assertion available spans runs, it's metamorphic. They belong in the same suite.
- **Fuzzing** supplies adversarial source inputs; fuzz + metamorphic is a strong bundle for parsers, compilers, and format readers (feed weird inputs, apply a semantics-preserving transform, assert unchanged behavior).

**Downstream / escalation:**
- **Differential testing** is the `upgrade-stricter` neighbor: the moment a real oracle becomes obtainable — a simple reference implementation, a spec suite, a competitor — switch from "outputs must be *consistent*" to "output must *equal the oracle*", a strictly stronger check. The equivalent-path MR (heuristic 5) is the bridge: when the "other path" becomes a full independent implementation, it *is* differential testing.
- **Property-based testing / fuzzing** is the `upgrade-looser` fallback when even relations can't be stated — retreat to single-input invariants and "doesn't crash".
- **Mutation testing** audits relation strength: a metamorphic suite with a low mutation score has vacuous or too-weak relations that pass for broken code. Prescribe it once the inventory is established.

## Failure modes & retirement triggers

| Sign | Diagnosis | Correction |
|---|---|---|
| Green suite but real bugs escape | Relations are too weak/vacuous, or the bug is *consistent* across the transform (metamorphic's inherent blind spot) | Add mutation testing to expose weak relations; strengthen relations (equality > subset > non-emptiness); pair with differential/unit testing to cover consistent-wrongness |
| Constant false-positive violations | The asserted relation isn't actually guaranteed, or non-determinism wasn't canonicalized | Weaken to the safe true relation; strengthen canonicalization (sort, tolerance, strip timestamps); check the transformation itself isn't buggy |
| Violations are hard to debug | Poor fault localization — a failed MR shows two runs disagree but not which is wrong | Add finer-grained relations that isolate the stage; log both runs' intermediate outputs; reduce the transform to the smallest one that still triggers it |
| Violating inputs are huge/opaque | No shrinking on the source input | Delegate shrinking to the PBT framework; ensure inputs have readable representations |
| Suite is slow | Every check runs the system 2+ times over a large corpus | Split a fast local subset from a large CI corpus; cache the base run when many relations share a source input; parallelize |
| Relations keep breaking on refactor | Relations encode implementation detail, not domain semantics | Rewrite relations against the *behavioral contract* (what must stay true for a user), not internal structure |

**Retirement triggers:** metamorphic testing for a subsystem is a candidate for retirement when: (1) a real oracle becomes available and differential/unit testing subsumes the relations with a stronger guarantee (keep any relation that catches a class the oracle doesn't); (2) the subsystem is deprecated or its output space collapses to something directly verifiable; (3) mutation testing shows the relations have become vacuous and can't be strengthened. Never retire without confirming the replacement covers the same inconsistency bug classes.

## Tool pointers

- **Input generation + shrinking:** reuse the PBT libraries from the property-based-testing card (Hypothesis, fast-check, proptest, gopter, jqwik) — metamorphic relations are written as multi-execution properties in the same frameworks.
- **Fuzzing (source inputs for parsers/compilers/codecs):** libFuzzer / AFL++ (`cargo-fuzz`), Atheris (Python), Jazzer (JVM), native Go fuzzing — feed the corpus, apply a semantics-preserving transform, assert unchanged behavior.
- **ML / numeric transformations:** the deep-learning ecosystem's augmentation utilities (image rotate/scale/brightness, text paraphrase) double as metamorphic transformations; assert label/score stability under them.
- **Established practice/reference:** metamorphic testing has a mature research literature (Chen et al.) and is used in production for compilers (CSmith + EMI/equivalence-modulo-inputs), SQL engines, and ML systems — mine those domains for relation patterns.
- **Detection-power audit:** the mutation-testing tools (Stryker, mutmut, cargo-mutants) — run against the relation suite to prove the relations aren't vacuous.
