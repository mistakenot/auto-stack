---
name: Property-Based Testing
summary: Generates random inputs and asserts invariants hold; shrinking isolates minimal counterexamples
oracle: relational
archetypes: algorithmic-core
criticality-min: C3
volatility-fit: both
harness: ci
pairs-with: differential-testing, mutation-testing
upgrade-looser: unit-testing
upgrade-stricter: formal-spec-model-checking
cost-author: medium
cost-maintain: low
cost-run: fast
---

## What it is & what it catches/misses

Property-based testing generates random inputs according to a specification (a generator) and asserts that stated invariants hold for every generated case. When an invariant fails, the framework *shrinks* the counterexample to the minimal reproducing input. The oracle is relational: instead of "f(3) == 7" you assert "for all valid x, decode(encode(x)) == x" (round-trip) or "for all xs, len(sort(xs)) == len(xs)" (invariant preservation).

**Catches:** bug classes that evade hand-picked examples — boundary violations at unexpected input shapes, missed edge cases in combinatorial input spaces, broken invariants under rare input combinations, encoding/decoding asymmetries, state-dependent failures that only emerge with certain input sequences. Particularly effective for: parsers, serializers, codec round-trips, sorting/filtering, mathematical transformations, state machine transitions, and any function where the input space is large but the invariants are expressible.

**Misses:** PBT cannot catch bugs where no property is stated — it tests the code against *your* invariants, not against the user's intentions. It misses integration failures across service boundaries, UI rendering problems, performance regressions, and any bug class requiring system-level observation. PBT also cannot replace exact-oracle tests where you need specific known-answer verification (use unit tests for those). If the property is wrong or vacuous ("for all x, true"), PBT gives false confidence — mutation testing is the audit for this.

**Key distinction from unit testing:** asserting `add(2, 3) == add(3, 2)` is a unit test that checks one example of commutativity. Asserting "for all a, b: add(a, b) == add(b, a)" across hundreds of *generated* pairs is PBT. The generator is what turns a known property into a bug-finder across the input space — without it, you are testing a property at a few hand-picked points, which is exactly the gap PBT exists to close.

## When to prescribe / when not

**Prescribe when:**
- The subsystem has functions whose correctness can be expressed as invariants, round-trips, or relational properties rather than (or in addition to) exact input-output pairs.
- Criticality is C3 or above — the subsystem justifies going beyond enumerated examples.
- The input space is large, combinatorial, or has non-obvious boundaries that hand-enumeration would miss.
- Existing unit tests are becoming brittle due to boundary proliferation — each new edge case adds another hand-written test, and you are not confident the enumeration is complete.
- The unit-testing card's `upgrade-stricter` pointer led here.

**Do not prescribe when:**
- The function under test has a small, fully enumerable input space (a 3-case enum does not need random generation — unit tests suffice).
- No meaningful property can be stated — the function's correctness is defined by specific expected outputs that cannot be generalized (use exact-oracle unit tests).
- The primary bug class is integration or cross-service failure (use contract testing or integration tests).
- The subsystem is a thin CRUD layer where schema-contract testing covers the relevant invariants more naturally.
- You need to verify visual/UI behavior (use visual regression or ARIA snapshots).

## Prerequisites

**Artifacts:** a codebase with identifiable pure or near-pure functions whose inputs can be described by generators. The functions must have expressible invariants — if the implementing agent cannot state what property should hold, PBT cannot be applied. The derivation guidance section provides heuristics for finding properties.

**Infrastructure:** a PBT library compatible with the project's language and test runner (Hypothesis for Python, fast-check for TypeScript/JavaScript, proptest for Rust, gopter for Go, jqwik for Java/Kotlin). The library must support shrinking — random testing without shrinking is dramatically less useful for agents because the counterexamples are noisy. CI integration is strongly recommended; PBT tests should be part of `make verify`.

## Design decisions

The architect must decide:

- **Property inventory scope:** which functions/modules get PBT coverage. The default: every function where unit testing flagged "boundary enumeration is insufficient" plus every function with a round-trip, encode/decode, or idempotency characteristic. State the scope explicitly in the testing strategy doc.
- **Generator strategy:** whether to use library built-in generators (for primitives, containers) or write custom generators (for domain types). Custom generators are needed when the input space has validity constraints (e.g., "valid email address", "balanced binary tree"). Decision: custom generators for domain types; built-in for everything else. Document the generator inventory.
- **Constrained inputs — construct vs reject:** when inputs must satisfy a validity constraint, prefer generators that *construct* only valid values (build the value so it is valid by definition) over generators that produce arbitrary values and then *reject* the invalid ones. Reject-based generation wastes work and, when the constraint is tight, most frameworks abandon the property after too many discarded attempts and report it as unable to find inputs. Reserve rejection for cheap, rarely-failing constraints. State the convention so implementing agents do not reach for reject-filtering by default.
- **Number of examples per property:** the default (100) is usually sufficient for fast feedback. Increase (1000+) for high-criticality subsystems (C4). State the default in the testing strategy doc. CI runs may use a higher count than local runs.
- **Determinism policy:** PBT is randomized, so decide where randomness is allowed. Recommended: local runs use a fresh random seed each time (broader exploration as the suite is re-run during development); CI pins a fixed seed (a failure is reproducible from the logs alone, and a green CI means green for that seed). Maintain a regression seed set — every seed that ever produced a failure is added to a checked-in list and replayed on every run, so a fixed bug never silently regresses. State which mode each environment uses in the testing strategy doc.
- **Seed management:** PBT frameworks produce a seed on failure for reproducibility. The harness must capture and store the seed — a failure without a seed is not reproducible. Seeds for failed runs are recorded in evidence artifacts and added to the regression seed set. CI failure logs must include the seed.
- **Shrinking configuration:** default shrinking is almost always correct. Only disable if shrinking is prohibitively slow (rare, and usually indicates the test setup is too expensive — fix the setup, not the shrinking).

## Derivation guidance

Heuristics the architect embeds in the prescription so implementing agents can find the properties:

1. **Round-trip / encode-decode:** if the system has `encode` and `decode` (or serialize/parse, compress/decompress, encrypt/decrypt, format/parse), the round-trip property is: `decode(encode(x)) == x` for all valid `x`. This is the single highest-yield PBT pattern. Look for any pair of inverse functions.
2. **Invariant preservation:** the output preserves a measurable property of the input. Examples: `len(sort(xs)) == len(xs)` (length preservation), `set(sort(xs)) == set(xs)` (element preservation), `sum(split(amounts)) == sum(amounts)` (quantity conservation). Ask: "what quantity or structure must be unchanged by this transformation?"
3. **Idempotence:** applying the function twice yields the same result as applying it once: `f(f(x)) == f(x)`. Common in: normalization, formatting, deduplication, cache warming, sanitization. If a function's name contains "normalize", "clean", "format", "deduplicate" — test idempotence.
4. **Commutativity / associativity / other algebraic laws:** if the function combines inputs, test whether order matters when it should not (`merge(a, b) == merge(b, a)`) or that grouping is irrelevant (`combine(combine(a, b), c) == combine(a, combine(b, c))`).
5. **Oracle comparison (test oracle):** when a simple but slow reference implementation exists alongside an optimized one, generate random inputs and assert both produce the same output. This is differential testing powered by PBT generators. Works for: optimized algorithms vs brute-force, new implementation vs legacy, compiled vs interpreted paths.
6. **Hard to prove, easy to verify:** some functions produce outputs that are hard to compute but easy to check. Example: a solver finds `x` such that `f(x) == target` — generate random targets, run the solver, verify `f(result) == target`. The checker is simpler than the solver.
7. **Stateful / model-based:** for stateful APIs, define a simplified model (e.g., a Python dict standing in for a database), generate random sequences of operations, apply them to both the real implementation and the model, and assert the states match after each step. This is the advanced form; prescribe only at C3+ for stateful subsystems.

## Minimum viable instance vs full rigor

**Minimum viable (30 minutes):** identify the 2–3 most obvious round-trip or invariant-preservation properties in the codebase. Write one property test per property using the framework's built-in generators. Run locally. This already catches boundary bugs that unit tests miss and proves the PBT infrastructure works. The time is dominated by *finding the properties*, not writing the tests.

**Full rigor:** systematic property inventory across the public API surface using all applicable derivation heuristics. Custom generators for domain types. Stateful/model-based testing for stateful APIs. Seed management integrated into CI evidence. Mutation-tested to verify detection power — a PBT suite with low mutation score means the properties are too weak (vacuous or tautological). Number-of-examples tuned per criticality tier.

## Harness changes

| Component | Delta |
|---|---|
| `docs/testing.md` | Add a "Property-Based Testing" section: property inventory, generator conventions (built-in vs custom), derivation heuristics for this project, seed-management convention, number-of-examples defaults |
| `make verify` | Property tests are a *style* of test, not a separate layer — by default they live alongside unit tests and run under the existing `verify-unit` subtarget (most PBT libraries are just decorators/wrappers the unit runner already discovers). Add a dedicated `verify-properties` subtarget only when the property inventory is large enough to warrant running or reporting it separately (e.g., it dominates suite runtime). Either way, it must be composed into the top-level `make verify`. |
| `AGENTS.md` / `CLAUDE.md` | Add line: "Changes to [scoped modules] require property tests — see docs/testing.md for property derivation heuristics and generator conventions" |
| CI workflow | `make verify` already runs in CI; no additional wiring needed if `verify-properties` is composed into it |
| Evidence conventions | On failure: seed, shrunk counterexample, and property name captured in `.evidence/` or CI log. On success: property count and example count in test runner output |

## How to get to a walking skeleton

1. **Install the PBT library:** add the PBT dependency to the project (Hypothesis, fast-check, proptest, etc.) and configure it with the test runner.
2. **Place the property tests:** add them alongside the unit tests so the existing runner discovers them (only carve out a separate directory/subtarget if the inventory is large — see Harness changes).
3. **Write the fail-fake property:** a property that asserts a known-false invariant (e.g., `for all x: int, x + 1 == x`). This proves the PBT framework is wired correctly AND that failures propagate through `make verify` with a shrunk counterexample and seed in the output.
4. **Write the pass-fake property:** a property that asserts a tautology (e.g., `for all x: int, x == x`). This proves the green path and that evidence artifacts (example count, property name) are emitted on success.
5. **Run locally:** the test subtarget that owns the properties exits non-zero with the fail-fake, exits zero with only the pass-fake.
6. **Run via `make verify`:** confirm the subtarget is composed into the top-level target and failures propagate.
7. **Confirm CI:** push both fakes, observe CI red; remove the fail-fake, observe CI green. Verify the failure output includes the shrunk counterexample and the seed.
8. **Remove fakes:** delete both fake properties. Replace with the first real property from the minimum-viable-instance list.

## Acceptance criteria to embed

The architect writes these into the generated artifacts so implementing agents can self-grade:

- [ ] Every function in the property inventory has at least one property test.
- [ ] Properties use the derivation heuristics from the testing strategy doc — round-trips, invariants, idempotence, commutativity, oracle comparison as applicable.
- [ ] No property is vacuous: each property has at least one assertion that can fail. (Test: commenting out the function body or returning a constant should cause at least one property to fail.)
- [ ] Custom generators exist for domain types with validity constraints; generators produce only valid inputs.
- [ ] Every property test uses shrinking (framework default; do not disable).
- [ ] Failed runs record the seed and shrunk counterexample in the evidence output; the seed is added to the regression seed set.
- [ ] `make verify` (including whichever subtarget owns the property tests) passes locally and in CI.
- [ ] **Kill test:** introduce a deliberate bug (e.g., off-by-one in a boundary, swap encode/decode order, break idempotence by mutating state). At least one existing property must fail. Record the mutation, the property that caught it, and the shrunk counterexample as evidence.

## Composition

**Upstream feeds:**
- **Unit testing** is the loose predecessor. When unit tests become brittle due to boundary enumeration or the architect recognizes that hand-picked examples cannot cover the input space, PBT is the upgrade. Existing unit test infrastructure (runners, CI integration) is reused.
- **Type-driven assurance** narrows the generator space — branded types and validation constraints become generator specifications, eliminating the need to generate and filter invalid inputs.

**Downstream consumers:**
- **Differential testing** reuses PBT generators directly. When a reference implementation exists (naive vs optimized, old vs new), the same generated inputs feed both implementations and the test asserts output equality. PBT + differential is the canonical bundle for algorithmic-core subsystems.
- **Mutation testing** audits PBT quality. A property suite with a low mutation score means the properties are too weak — they pass for the real code but also pass for mutants. Prescribe mutation testing as the quality gate once the property suite is established.

## Failure modes & retirement triggers

| Sign | Diagnosis | Correction |
|---|---|---|
| Properties pass but bugs still escape in covered functions | Properties are too weak or too generic — they assert true for broken code too | Add mutation testing to audit detection power; strengthen properties or add more specific ones |
| PBT tests are slow (seconds to minutes per property) | Generator produces complex inputs; setup/teardown is expensive; too many examples configured | Reduce example count for local runs (keep high count in CI); simplify generators; optimize test setup |
| Shrunk counterexamples are large or opaque | Custom generators are producing complex structures that the shrinker cannot simplify | Write custom shrinkers or simplify the generator; ensure domain types have meaningful string representations |
| Properties break on every refactor | Properties are asserting implementation details (internal state, specific error messages) rather than behavioral invariants | Rewrite properties to assert against the public interface; test *what* invariants hold, not *how* the code achieves them |
| Generator produces only trivial inputs (all zeros, empty collections) | Generator is under-constrained or the framework's default distribution skews toward degenerate cases | Construct generators that build meaningful inputs directly (see the construct-vs-reject design decision); give them explicit distributions or ranges; verify the spread with the framework's example statistics |
| Framework reports it cannot find / gave up generating inputs | A reject-based generator is discarding most candidates because the validity constraint is too tight | Switch to a constructive generator that builds valid values by construction instead of filtering arbitrary ones |
| High property count but low bug-finding rate | Diminishing returns — the easy properties are written, the remaining invariants are hard to express | Graduate to model-based/stateful PBT or formal spec (upgrade-stricter path); accept the current property set as maintenance-mode |

**Retirement triggers:** PBT for a module is a candidate for retirement when: (1) a formal spec with model checking (the upgrade-stricter path) subsumes the same invariants with exhaustive guarantees; (2) the module is deprecated or replaced; (3) the module's input space collapses to a small enumerable set (revert to unit tests). Never retire without confirming the replacement covers the same bug classes and the mutation score does not regress.

## Tool pointers

- **Python:** Hypothesis (the gold standard; built-in strategies, stateful testing, shrinking, Django/NumPy integration, pytest plugin)
- **TypeScript/JavaScript:** fast-check (mature, well-documented, arbitrary combinators, model-based testing support, integrates with vitest/jest)
- **Rust:** proptest (Hypothesis-inspired, procedural macro strategies, shrinking, integrates with cargo test)
- **Go:** gopter (generators + properties + shrinking), rapid (simpler API, less featureful)
- **Java/Kotlin:** jqwik (JUnit 5 platform, lifecycle integration, statistics, domain-specific generators)
- **Mutation testing (audit companion):** Stryker (JS/TS), mutmut (Python), cargo-mutants (Rust) — use to verify properties have actual detection power
