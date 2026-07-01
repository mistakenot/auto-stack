---
hash: "50797bbb"
id: "9e963c63"
read_when: "designing verification strategy for auto-skill, adding new test techniques, or diagnosing silent edge-case bug classes"
summary: "Four-axis assurance diagnosis and prescribed testing techniques (model-based, property-based, edge-case pinning) for auto-skill's sync pipeline edge cases."
title: "auto-skill Assurance Strategy"
---

# auto-skill Assurance Strategy

## Diagnosis

### Four-axis assessment

| Axis | Rating | Rationale |
|---|---|---|
| **Criticality** | **C3** — high | Tool manages file trees with deletion authority (receipt-gated prune). Silent edge cases cause data loss or confusion. Now depended on in real projects. |
| **Volatility** | **Medium** | Core state machine (plan→fetch→process→commit→prune) is stabilizing; new features (adopt, migrate, trust) still arriving at the edges. |
| **Longevity** | **High** | Core auto-stack infrastructure, maintained long-term. |
| **Accountability** | **Medium** | Primary user is the operator + autonomous agents. Agents can't visually verify results, so silent misbehavior compounds undetected. |

### Bug class analysis

The bugs escaping into production are **silent edge cases in stateful composition** — individually correct phases that compose into surprising behavior. Concrete examples:

1. **Silent precedence:** authored `./skills/<name>` shadows a vendored skill of the same name with no warning (process.go:249 — `gatherSources` overwrites vendored with authored in `byName`).
2. **Partial-op side effects:** `remove --vendored` when an authored copy exists leaves the authored copy taking over silently on next sync — the user thinks the skill is removed but it reappears.
3. **Scoped sync false orphans:** `sync --target X` stages only X, so every other managed skill classifies as a false orphan — the fixed `planPrune` scope gate prevents deletion, but the bug class (scoped-op leaking into full-set logic) recurs at every new feature.
4. **Missing diagnostics:** operations succeed but should warn about state the user didn't expect (shadowing, surviving copies after remove, intent drift).
5. **Cross-source interactions:** add then authored-create, authored-create then sync, remove-vendored with authored present, remove-local with vendored present — each combination has different semantics.

### What's already in place

| Layer | Coverage | Gap |
|---|---|---|
| **Model-based (stateful PBT)** | `syncStateMachine` in `sync_statemachine_test.go` — 9 operations, disk-presence invariant, using `rapid` | Tracks only **disk presence**, not warnings/diagnostics, source provenance, or artifact consistency (lock, manifest, receipts). Does not cover `add`, `remove`, or `adopt` operations. |
| **Property-based** | Lock round-trip, render determinism, URL canonicalization, skills.yaml round-trip (`skill_prop_test.go`, `render_prop_test.go`, `transport_prop_test.go`) | All stateless single-function. No cross-function composition properties. |
| **Unit/integration** | ~42 test files across all packages | Good functional coverage, but edge case scenarios (the multi-source interaction matrix) are sparse. No diagnostic/warning assertion tests. |

## Prescribed techniques

### T1. Model-Based Testing — extend the state machine (Standard rung)

**Why this technique.** The existing `syncStateMachine` already proved its worth (it caught the `--target` prune bug). The bugs escaping now are in the same class — stateful composition surprises — but the model doesn't observe enough to catch them. Extending the model is strictly cheaper than building new infrastructure.

**What to extend:**

1. **Diagnostic model.** Track expected warnings per operation in the model. After every `Run`, assert the result's `Warnings` field contains exactly the expected set. The canonical test: authored+vendored same name → sync → warning emitted about shadowing.

2. **Source-provenance model.** Track which source (authored vs vendored) "owns" each rendered skill in the model. After sync, assert that `ProcessResult.Staged[*].Source` matches the model's expected owner. This catches silent-precedence bugs.

3. **Artifact-consistency invariants.** After every `Run`, assert:
   - `manifest.Skills` keys == set of dirs on disk in every target
   - Lock entries are a subset of rendered vendored skills (no phantom entries)
   - Receipts cover every rendered skill (no unestablished gaps after a full sync)

4. **New operations to add to the state machine:**
   - `RemoveVendored(name)` — calls `Remove(env, name, SelVendored)` when the skill is vendored
   - `RemoveLocal(name)` — calls `Remove(env, name, SelLocal)` when the skill is authored
   - `RemoveAmbiguous(name)` — calls `Remove(env, name, SelUnset)` when both exist; asserts error
   - `AuthoredShadowsVendored` — creates an authored skill with the same name as an existing vendored skill, syncs, asserts warning
   - `AddSkillSameNameAsAuthored` — adds a vendored skill with same name as authored, syncs, asserts precedence

5. **Kill test for the diagnostic model:** inject a bug where `gatherSources` stops logging the shadow trace line. Assert the state machine catches the missing warning within a generated sequence of AddSkill→AddAuthoredSkill(same name)→FullSync.

**Coverage target:** transition coverage — every (state, operation) pair exercised at least once. The rapid framework's random walk provides this with enough steps; the `Check()` invariant runs after every step.

**Rung justification:** Standard, not full-rigor. The existing rapid infrastructure is already set up and working. The model stays simpler than the implementation (it's a set of maps tracking expected state). Full rigor (explicit FSM with coverage-directed generation) is not warranted because the rapid random walk with shrinking already found the scoped-prune bug class.

**Upgrade trigger:** If bugs continue escaping that involve concurrent sync operations or multi-process interactions, escalate to an explicit FSM model with coverage metrics.

### T2. Property-Based Testing — decision function invariants (Standard rung)

**Why this technique.** The ownership `Classify` function and the `gatherSources` precedence logic are pure (or near-pure) functions with complex input spaces. Their correctness is expressible as relational invariants that hand-picked examples cannot exhaustively cover.

**Properties to add:**

1. **ownership.Classify decision table exhaustiveness:**
   - `∀ inputs: Classify(inputs) produces exactly one State per (target, dir)` (totality)
   - `∀ inputs: PruneEligible(Classify(inputs)) ⊆ {d | d.State == StateManagedOrphan}` (prune safety)
   - `∀ inputs: if !managed[name] then state == StateForeign` (foreign exclusivity)
   - `∀ inputs: if desired[name] then state ∉ {StateManagedOrphan}` (desired never orphaned)
   - `∀ inputs: if !hasReceipt then state ∉ {StateManagedOrphan, StateModified}` (receipt necessity)

2. **gatherSources precedence invariant:**
   - `∀ (vendored, authored) with overlapping names: gatherSources returns authored[name], not vendored[name]` (authored-wins)
   - `∀ sources: len(output) == len(unique names in vendored ∪ authored)` (no drops, no dupes)

3. **classifySpec round-trip with refForSpec:**
   - `∀ spec: classifySpec(spec) == kindFloat ⟹ refForSpec(spec) ∈ {"HEAD", branch-name}` (float→HEAD)
   - `∀ spec: classifySpec(spec) == kindCommit ⟹ refForSpec(spec) is unchanged` (commit passthrough)

4. **planPrune scope safety:**
   - `∀ (verdicts, targets, scope): scope != nil ⟹ ∀ p in planPrune(...): scope[p.Skill]` (scoped prune never reaches outside scope)

**Generators:** Use the existing `rapid` generators in `skill_prop_test.go` (skillNameGen, lockEntryGen, etc.) as the base. Add:
- `ownershipInputsGen` — generates `ownership.Inputs` with random target scans, desired sets, manifests, and receipt maps
- `skillSourceSetGen` — generates a mix of authored and vendored `skillSource` entries with controlled name overlaps

**Rung justification:** Standard. These are the highest-yield relational invariants for the pure decision logic. The generators exist; the properties are expressible. Full-rigor (mutation-tested detection power) is overkill for this iteration.

**Upgrade trigger:** If the properties pass but bugs still escape in the classified functions, add mutation testing to audit detection power.

### T3. Unit Testing — edge case pinning and diagnostic assertions (Standard rung)

**Why this technique.** Each discovered-in-production edge case needs a pinned regression test that asserts both the behavior AND the diagnostic output. These are exact-oracle tests — the property tests above check invariants, but each specific surprise scenario needs a named, deterministic, fast test that says "this exact sequence produces this exact warning."

**Tests to add (the edge case matrix):**

| # | Scenario | Behavior assertion | Diagnostic assertion |
|---|---|---|---|
| 1 | Authored `./skills/foo` + vendored `foo` → sync | Authored content rendered, vendored ignored | Warning in result: "authored skill 'foo' shadows vendored" |
| 2 | `remove --vendored foo` when authored `foo` exists | Lock entry removed, authored copy survives | `RemoveResult.Reported` lists surviving authored copies |
| 3 | `remove --local foo` when vendored `foo` exists | Authored dir deleted, vendored copy rendered on next sync | No spurious warnings |
| 4 | `remove foo` (SelUnset) when both exist | Error returned, nothing mutated | Error message suggests `--local` or `--vendored` |
| 5 | `add` a skill with same name as existing authored | Lock entry created, but authored shadows on sync | Warning on sync about shadowing (the add itself may succeed) |
| 6 | `sync --target foo` when `foo` not in lock | No crash, empty plan for the target | Appropriate message that no matching skills found |
| 7 | `sync --check` with pending journal | Non-zero exit | Error message says "run auto skill sync to recover" |
| 8 | `sync` with empty lock + no authored skills | Clean zero exit | No crash, no empty manifest written |
| 9 | `sync --locked` after intent changed in skills.yaml | Intent-changed reported but lock untouched | `StaleItem` with reason `stale_by_intent` |
| 10 | Authored skill with invalid name → sync | Skipped with error | Error includes the invalid name and path |
| 11 | `remove` non-existent skill | Error returned, nothing mutated | Error says "no skill named ... found" |
| 12 | Foreign dir in target collides with desired skill → sync | Refused without `--force` | Conflict entry with remediation hint |

**Evidence:** Each test is named `TestEdgeCase_<scenario>` with a descriptive suffix. Each asserts both behavior (what happened on disk/in artifacts) and diagnostics (what the user/agent sees in the result).

**Rung justification:** Standard. These are pinned regression tests, not exploratory. Each takes 5-10 minutes to write using the existing `helpers_test.go` fixture infrastructure. The `newFixture` / `newEnv` / `approve` / `writeLock` helpers make setup fast.

**Upgrade trigger:** When the edge case list stabilizes and hand-enumeration feels brittle, graduate the interaction matrix to a property-based approach using T2's generators.

## Implementation priority

| Priority | Technique | Effort | Yield |
|---|---|---|---|
| **P0** | T3 — Pin the known edge cases (scenarios 1-5 first) | 2-3 hours | Immediate regression coverage for discovered bugs |
| **P1** | T1 — Extend state machine with diagnostic model + remove ops | 4-6 hours | Catches the next shadowing/precedence bug before production |
| **P2** | T2 — ownership.Classify properties + gatherSources invariants | 2-3 hours | Exhaustive coverage of the pure decision logic |

## What this strategy does NOT cover

- **Performance:** sync timing, cache hit rates, git operation latency — out of scope (separate concern).
- **Trust/security:** trust gate behavior, credential handling — covered by existing trust_test.go; extend if the trust surface grows.
- **UI/CLI formatting:** text-mode output rendering — not a correctness concern at this criticality level.
- **Concurrent access:** multiple sync processes running simultaneously — not a supported use case; if it becomes one, escalate to model checking.

## Diagnostic coverage contract

Every operation that silently overrides, shadows, or ignores user intent MUST emit a structured warning in the result. The assurance strategy enforces this via:
1. T1's diagnostic model (generative — catches missing warnings across random sequences)
2. T3's pinned edge cases (deterministic — ensures each known scenario warns)

A new edge case discovered in production is added as a T3 pinned test FIRST (immediate regression), then the T1 model is updated to cover the general case.

## Evidence artifacts

- `go test ./internal/sync/ -run TestSyncStateMachine -rapid.checks=200` — model-based conformance
- `go test ./internal/sync/ -run TestEdgeCase` — pinned edge case suite
- `go test ./internal/ownership/ -run TestClassify` — decision table properties
- All integrated into `go test ./...` and the pre-commit hook's `go vet` pass
