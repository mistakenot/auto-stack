---
name: Unit Testing
summary: Verifies individual functions/methods return correct outputs for given inputs
oracle: exact
archetypes: algorithmic-core, crud-surface
criticality-min: C1
volatility-fit: both
harness: ci
pairs-with: differential-testing, mutation-testing
upgrade-looser: none
upgrade-stricter: property-based-testing
cost-author: low
cost-maintain: low
cost-run: fast
---

## What it is & what it catches/misses

Unit testing exercises individual functions, methods, or small modules in isolation, asserting that given inputs produce expected outputs. The oracle is exact: the test author specifies the correct answer.

**Catches:** logic errors in pure transformations, off-by-one errors, boundary violations, regressions in isolated units, incorrect return values, exception-handling bugs.

**Misses:** integration failures between components, state-dependent bugs across call sequences, concurrency issues, emergent behavior from component interaction, UI rendering problems, and any bug class that requires observing the system as a whole. Unit tests also cannot catch specification errors — they verify the code does what the author *intended*, not what the user *needed*.

## When to prescribe / when not

**Prescribe when:**
- The subsystem has pure or near-pure functions with well-defined inputs and outputs.
- Criticality is C1 or above (unit tests are cheap enough for any criticality level).
- You need fast feedback during development — unit tests run in milliseconds.
- The codebase has algorithmic logic, data transformations, parsers, validators, or business rules.

**Do not prescribe when:**
- The value is entirely in the integration (e.g., a thin API route that delegates to a service — use contract testing instead).
- The function under test is a trivial wrapper with no logic.
- The bug class you are targeting involves state across multiple components (use integration or property-based testing instead).
- You need to verify UI behavior (use visual regression, ARIA snapshots, or semantic E2E instead).

## Prerequisites

**Artifacts:** a codebase with identifiable units — functions, methods, or modules with clear boundaries and deterministic behavior (or behavior that can be made deterministic via dependency injection/mocking).

**Infrastructure:** a test runner compatible with the project's language (pytest, vitest, jest, go test, etc.). CI integration is recommended but not required at adoption time — the walking skeleton proves it works locally first.

## Design decisions

The architect must decide:

- **Scope boundary:** what counts as a "unit" in this codebase. In a functional codebase, it is a single function. In an OO codebase, it may be a class or a small cluster of collaborating objects. Define this explicitly in the testing strategy doc so implementing agents apply it consistently.
- **Isolation strategy:** pure functions need no mocking. Impure units need a decision: dependency injection, test doubles, or thin wrappers. Over-mocking is the primary failure mode — if the test mocks everything the unit talks to, it tests the mocks, not the code.
- **Naming and organization:** tests mirror the source tree, or tests live alongside source files. Pick one convention and state it.
- **Assertion style:** exact equality for deterministic outputs; approximate/tolerance-based for floating point; structural matching for complex objects. State the default.

## Derivation guidance

Heuristics the architect embeds in the prescription so implementing agents can find what to test:

1. **Public API surface:** every exported function/method gets at least one happy-path test and one error-path test.
2. **Branch coverage scan:** each conditional branch is a candidate test case. Look for `if/else`, `switch/case`, ternary expressions, early returns, and guard clauses.
3. **Boundary values:** for numeric inputs, test at 0, 1, -1, max, min, and just outside valid ranges. For strings, test empty, single-char, and maximum-length. For collections, test empty, single-element, and large. For **partitioned domains** — functions with multiple thresholds that switch behavior (tax bands, pricing tiers, rate brackets, permission levels) — the rule extends: test at threshold−1, threshold, threshold+1 for *each* threshold, not just the outer bounds. A domain with N thresholds needs at least 2N+1 boundary probes. Collect these as a named constant list (e.g. `BOUNDARY_INCOMES`, `BAND_EDGES`) and reuse that list across every invariant test — this ensures all invariants are verified at every boundary with no duplication of the enumeration.
4. **Error paths:** every `throw`, `raise`, or error return is a test case. Verify the error type/message, not just that "an error occurs."
5. **Regression anchoring:** every bug fix gets a test that reproduces the bug before the fix and passes after. This test is the unit's memory — it prevents the exact recurrence.
6. **Data transformation chains:** if a function transforms data through multiple steps, test the end-to-end transformation and at least one intermediate step where logic is non-trivial.

## Minimum viable instance vs full rigor

Choose the rung that matches the four axes; do not prescribe a full suite when a smaller evidence artifact would catch the likely failure mode.

**Light / minimum viable (20 minutes):** write tests for the 3-5 most critical functions -- the ones where a bug would cause the most damage. Use the simplest assertion style. Run locally. This already pays rent by catching regressions in the hot path.

**Standard:** cover changed public functions plus their boundary and error paths. Add shared boundary-value lists for partitioned domains, wire the tests into the existing local verification command, and record runner output as evidence. This is the default rung for normal product work.

**Full rigor:** systematic coverage of the public API surface, boundary values, and error paths. Mutation-tested to verify detection power. Integrated into CI with failure blocking merge. Coverage metrics tracked (as a floor, not a target -- mutation score is the real quality signal).

## Harness changes

| Component | Delta |
|---|---|
| `docs/testing.md` | Add a "Unit Testing" section: scope boundary definition, naming convention, isolation strategy, derivation heuristics for this project |
| `make verify` | Add `verify-unit` subtarget (e.g., `pytest tests/unit/`, `vitest run --project unit`); wire into the top-level `make verify` |
| `AGENTS.md` / `CLAUDE.md` | Add line: "All function/method changes require unit tests — see docs/testing.md for conventions" |
| CI workflow | `make verify` already runs in CI; no additional wiring needed if `verify-unit` is composed into it |
| Evidence conventions | Test runner output (pass/fail counts, failure details) captured in `.evidence/` or PR comment |

## How to get to a walking skeleton

1. **Scaffold the harness:** create the test directory structure and configure the test runner (e.g., `tests/unit/`, vitest config, pytest discovery).
2. **Write the fail-fake:** a test that asserts `false == true` (or equivalent). This proves failure propagation — the test runner reports failure, `make verify` exits non-zero, CI goes red.
3. **Write the pass-fake:** a test that asserts `true == true`. This proves the green path and that evidence artifacts are emitted on success.
4. **Run locally:** `make verify-unit` exits non-zero with the fail-fake, exits zero with only the pass-fake.
5. **Run via `make verify`:** confirm the unit subtarget is composed into the top-level target and failures propagate.
6. **Confirm CI:** push both fakes, observe CI red; remove the fail-fake, observe CI green. Screenshot or link both runs as evidence.
7. **Remove fakes:** delete both fake tests (or park the fail-fake behind `make verify-selftest` for ongoing harness validation).

## Acceptance criteria to embed

The architect writes these into the generated artifacts so implementing agents can self-grade:

- [ ] Every public function/method has at least one test exercising the happy path.
- [ ] Every error/exception path has a test verifying the error type and message.
- [ ] Boundary values are tested for numeric and collection inputs; for partitioned domains, ±1 around each threshold is verified for every invariant test, collected into a shared named list.
- [ ] No test mocks the unit under test — only its dependencies.
- [ ] Every test has a descriptive name stating the scenario and expected outcome.
- [ ] Every assertion includes a failure message or uses a framework that provides one automatically.
- [ ] `make verify-unit` passes locally and in CI.
- [ ] **Kill test:** introduce a deliberate bug (e.g., off-by-one, swapped conditional) in a critical function. At least one existing test must fail. Record the mutation and the catching test as evidence.

## Composition

**Upstream feeds:**
- **Type-driven assurance** narrows the input space before unit tests run — branded types and exhaustiveness checks eliminate entire categories of invalid inputs, letting unit tests focus on logic rather than type validation.

**Downstream consumers:**
- **Mutation testing** audits unit test quality — a high-pass-rate unit suite with low mutation score is a false floor. Prescribe mutation testing as the quality gate once the unit suite is established.
- **Differential testing** reuses unit test infrastructure — when a reference implementation exists, unit tests become comparison tests (same inputs, assert outputs match).
- **Property-based testing** is the strict upgrade — when exact oracles become brittle or boundary enumeration is insufficient, graduate to property-based tests that generate inputs and assert invariants.

## Failure modes & retirement triggers

| Sign | Diagnosis | Correction |
|---|---|---|
| Tests break on every refactor without catching bugs | Assertions are too tightly coupled to implementation details (testing *how*, not *what*) | Rewrite assertions against the public interface; mock less |
| High coverage, bugs still escape | Coverage measures lines executed, not behavior verified; likely missing boundary values and error paths | Add mutation testing to audit detection power |
| Test suite takes minutes to run | Units are not isolated — they hit databases, file systems, or network | Enforce isolation: inject dependencies, use in-memory fakes |
| Tests pass but the feature is broken | Unit tests verify components in isolation; the integration is untested | Add integration or contract tests for the interaction layer |
| Snapshot/golden tests churn on every change | Snapshots are capturing unstable output (timestamps, random IDs, formatting) | Normalize output before snapshotting; or switch to structural assertions |

**Retirement triggers:** unit tests for a module are candidates for removal when the module is deprecated, replaced, or when a higher-level technique (property-based testing, formal verification) subsumes the same invariants with stronger guarantees. Never retire without confirming the replacement covers the same bug classes.

## Tool pointers

- **Python:** pytest (with pytest-cov for coverage, pytest-xdist for parallel runs)
- **TypeScript/JavaScript:** vitest (fast, ESM-native), jest (mature ecosystem), node:test (zero-dependency)
- **Go:** `go test` (built-in), testify (assertions + mocks)
- **Rust:** `cargo test` (built-in), proptest (if graduating to PBT)
- **Java/Kotlin:** JUnit 5, AssertJ (fluent assertions)
- **Mutation testing:** Stryker (JS/TS), mutmut (Python), cargo-mutants (Rust)
