---
name: Model-Based Testing
summary: Builds an abstract model of expected behavior, generates operation sequences from it, and checks the running system conforms — finds ordering bugs hand-picked tests miss
oracle: model
archetypes: stateful-protocol, crud-surface
criticality-min: C3
volatility-fit: both
harness: ci
pairs-with: property-based-testing, differential-testing
upgrade-looser: property-based-testing
upgrade-stricter: formal-spec-model-checking
cost-author: high
cost-maintain: medium
cost-run: medium
---

## What it is & what it catches/misses

Model-based testing (MBT) builds an explicit, abstract model of *how the system is supposed to behave* — typically a state machine: a set of abstract states, an alphabet of operations, and transitions saying "in state S, operation O is legal, produces output X, and moves to state S'." From that model the harness automatically does two things: **generates** sequences of operations (by traversing the model's paths) and **checks conformance** by applying each generated sequence to the real system and comparing the real result against what the model predicted. The oracle is the model itself: pass means the running system agreed with the model on every step; fail means a divergence between model and reality.

The defining move is that MBT tests *sequences*, not single calls. A unit test fixes one ordering of operations by hand. PBT generates random *inputs* to one function and checks an invariant. MBT generates random *paths through a state space* and checks that the system's state and outputs track the model's at every step. The bug it is built to find is the one that only appears in a particular *order* of operations — the fourth `withdraw` after two `deposit`s and a `freeze`, the `read` after `close` after `reopen`.

**Catches:** ordering and lifecycle bugs that example-based suites never reach — illegal operations that should be rejected but aren't, state that silently corrupts after a rare command sequence, transitions that work in isolation but not in combination, "stuck" or unreachable states, resource leaks across open/use/close cycles, protocol violations (operations accepted in the wrong phase), and regressions in any of these once a model exists. It systematically explores the combinatorial space of operation orderings that defeats hand-enumeration. Strong fit for: protocols and session lifecycles, workflow/approval state machines, parsers with modes, connection pools and caches, allocators, APIs where calls have a required order, and UI navigation flows.

**Misses:** MBT can only catch divergence between the system and *your model* — it cannot find a bug the model also gets wrong (if model and implementation share the same misunderstanding, both agree and the test passes). It says nothing about behavior you chose not to model (performance, visual correctness, security properties outside the state machine). It is the wrong tool for stateless/pure logic (no sequences to explore — use unit tests or PBT invariants) and for behavior whose oracle is subjective (a model cannot encode "the layout looks right"). And a model as complicated as the code under test catches almost nothing while costing a great deal — the value is entirely in the model being *simpler* than the implementation.

**Key distinction from stateful PBT:** stateful/model-based PBT (a `RuleBasedStateMachine`, fast-check `commands`) *is* the lightweight rung of MBT — the model is a tiny reference implementation and sequences are random. Full MBT adds an explicit state-machine model you can measure coverage against (state, transition, transition-pair), coverage-directed sequence generation rather than pure random, and online/adaptive execution for nondeterministic systems. Reach past stateful PBT to full MBT when you need those — otherwise the PBT rung is the right amount of MBT.

## When to prescribe / when not

**Prescribe when:**
- The subsystem is **stateful** and the suspected bug class is in the *sequence* of operations, not any single call — you keep finding defects in "weird orderings."
- The state space is too large to enumerate by hand, but the *rules* governing it are compact enough to write down as a small model.
- There is a meaningful notion of illegal/forbidden operations per state (a lifecycle, a protocol phase, a status workflow) that must be enforced.
- Criticality is C3+ and the subsystem's correctness across long operation sequences genuinely matters (a payment ledger, an allocator, a session manager, a state-machine-driven workflow engine).
- The property-based-testing card's stateful rung was tried and you now need explicit model-coverage metrics, coverage-directed generation, or online testing for nondeterminism.

**Do not prescribe when:**
- The code is stateless or a pure function — there are no sequences to explore (use unit testing for exact cases, PBT for invariants).
- You cannot write a model meaningfully *simpler* than the implementation. If the model would just restate the code, MBT tests nothing and duplicates any conceptual bug. This is the single most important disqualifier.
- The subsystem is throwaway, a spike, or low-criticality CRUD where schema-contract or example tests already cover the lifecycle adequately.
- Behavior is so volatile that the model would need rewriting every iteration — the maintenance anchor outweighs the bug-finding (model the *stable* behavioral contract, or wait until it stabilizes).
- The real oracle is visual or experiential (use visual regression / E2E with human-meaningful assertions).
- A lighter technique already finds the bugs: if stateful PBT with a reference-implementation model is catching everything, you do not need the heavier explicit-model machinery.

## Prerequisites

**Artifacts:** a stateful subsystem with a discernible lifecycle — something with a `create`/`open`/`use`/`close` shape, a status field that transitions (draft → submitted → approved), or a session/connection that has phases. The implementing agent must be able to name (1) the operations (the command alphabet), (2) the abstract states that matter (equivalence classes, not raw memory), and (3) for each operation the expected effect — legal or rejected, what it returns, where it lands. If those three cannot be stated, there is no model and MBT does not apply.

**Infrastructure:** at the light rung, a PBT library with stateful support (Hypothesis `RuleBasedStateMachine`, fast-check `commands`, proptest state machine, PropEr `statem`) and its shrinking — reuses the PBT infra wholesale. At higher rungs, either the same library driven by an explicit model, or a dedicated MBT tool (GraphWalker, AltWalker, fMBT) that owns model traversal and coverage. A command adapter that maps abstract model operations to concrete system calls, and a projection that maps real system state into the model's comparable shape, are both required. CI integration is strongly recommended; conformance runs belong in `make verify`.

## Design decisions

The architect must decide:

- **What the model represents — and that it stays simpler than the code.** The model captures *intended behavior at the right abstraction*, not the implementation. Choose abstract states as equivalence classes (empty / non-empty / full; disconnected / connected / authenticated) rather than mirroring internal fields. State explicitly in the strategy doc that the model must be reviewable independently of the code; if the only correct model is a copy of the implementation, record that MBT was rejected and why.
- **The command alphabet and its scope:** which operations the model covers. Default: the subsystem's public/state-mutating API. Document the alphabet; operations left out are explicitly out of scope for conformance.
- **Legal vs forbidden transitions:** for every (state, operation) pair, decide whether it is allowed, and what an illegal call must do (raise, no-op, error code). Forbidden transitions are among the highest-yield assertions — model them deliberately, do not leave them implicit.
- **Conformance relation — what "agrees" means:** state parity (project real state, compare to model state after each step), output parity (compare returned values/emitted events), or both. For labeled-transition systems the standard relation is input-output conformance (ioco): every output the implementation can produce after a trace must be one the model allows. A third flavor is **trace validation** — instead of the harness driving the system, you replay an *already-recorded* trace (from a test run or from production telemetry) and check it is a path the model permits; this turns the same model into a runtime/offline monitor without a command adapter. Pick and document the relation; it is the pass/fail definition.
- **Generation strategy and coverage target:** random walk (cheapest, the stateful-PBT default) vs coverage-directed traversal aiming at a criterion — state coverage, **transition coverage** (every edge taken — the recommended default), transition-pair / switch coverage (every pair of consecutive transitions), or bounded-length path coverage. State the target so "done" is measurable; transition coverage is the usual standard-rung goal.
- **Offline vs online (on-the-fly) execution:** offline generates concrete sequences ahead of time and replays them (simple, great for deterministic systems, sequences are reproducible artifacts). Online generates the next operation from the model *as it observes the system's actual responses* (handles nondeterminism and large state spaces, but harder to reproduce). Default to offline; choose online only when the system is genuinely nondeterministic.
- **Nondeterminism handling:** if the system can legitimately produce more than one outcome, model the nondeterministic transitions explicitly (a set of allowed outcomes) and switch the conformance check to "real outcome ∈ model's allowed set." Do not paper over it with retries.
- **Sequence length and determinism policy:** bound the maximum operation-sequence length (unbounded paths explode). Pin a CI seed for reproducibility and keep a regression set of every sequence that ever failed, replayed each run — mirrors the PBT seed convention.
- **Where the model lives:** the model is a first-class spec artifact, versioned and reviewed when behavior changes — not buried in a test file. Decide its home (e.g. `docs/models/` or a dedicated module) and that behavior changes update the model in the same change.

## Derivation guidance

Heuristics the architect embeds so implementing agents can build the model:

1. **Find the entity with a lifecycle.** Look for anything with create/open/use/close, a `status`/`state` field that moves through values, a session, a connection, a handle, or a workflow with stages. That entity's lifecycle is the model's backbone.
2. **List the operations (the alphabet).** Enumerate the public, state-mutating calls. Each becomes a model command. Include the operations that *should* be rejected in some states — those are where the bugs hide.
3. **Abstract the states into equivalence classes.** Do not model raw memory. Ask "what distinctions actually change what an operation is allowed to do?" A queue is usefully {empty, non-empty, full} — three states, not 2³² counts. Fewer, meaningful states beat a faithful replica.
4. **Define each transition as (state, op) → {legal?, expected output, next state}.** Walk every cell of the state × operation grid. The empty and illegal cells (what must error, what must no-op) are as important as the happy path.
5. **Choose the conformance check.** Decide what you compare after each step: a projection of real state vs model state, the operation's return value vs the model's predicted output, or both. Keep the projection cheap and total.
6. **Pick the coverage criterion.** Start at transition coverage (every edge exercised at least once); graduate to transition-pair or bounded paths if bugs persist or the criterion saturates without finding them.
7. **Decide reproducibility vs reach.** Deterministic system → offline generation with stored, shrinkable sequences. Nondeterministic system → online generation with explicitly modeled nondeterministic transitions.
8. **Start with a reference-implementation model (the on-ramp).** Before building an explicit state machine, try the lightest form: the model is a trivial in-memory reference (a dict, a list) and stateful PBT generates random command sequences comparing real vs reference state. Promote to an explicit, coverage-measured model only when you need the metrics or online testing.

## Minimum viable instance vs full rigor

Choose the rung that matches the four axes. MBT is unusual in that its lightest rung overlaps the property-based-testing card — start there, and only build explicit-model machinery when the cheaper rung is provably insufficient.

**Light / minimum viable (reference-model stateful PBT, ~1 hour):** pick the one stateful subsystem most prone to ordering bugs. Write a trivial reference implementation as the model. Use the PBT library's stateful machine to generate random command sequences and assert real-vs-reference state parity after each step. This is real MBT — it already catches lifecycle and ordering bugs — and it reuses existing PBT infra with no new tooling. The cost is dominated by writing the reference model and the command adapter, not the harness.

**Standard:** build an explicit state-machine model (named states, an operation alphabet, legal and forbidden transitions with expected outputs). Generate sequences to a stated coverage target (transition coverage) rather than purely at random. Run offline against the real system through a command adapter, projecting real state for comparison and emitting a structured divergence report (the trace, the diverging step, model-expected vs actual). Seed/sequence capture required; CI integration recommended once stable.

**Full rigor:** a rich model (statechart / extended FSM carrying data variables) maintained as a versioned spec artifact. Coverage-directed generation to transition-pair or bounded-path coverage, online/on-the-fly execution to handle nondeterminism, an explicit conformance relation (ioco) documented as the pass/fail contract, failing-trace shrinking, and CI-integrated evidence (coverage achieved, divergences, shrunk counterexample sequences). Optionally pair with model checking of the model itself (see Composition) so the abstraction you test against is itself verified.

**When to escalate to a spec-language tool (Quint/TLA+) — and when that is overkill.** The in-language stateful-PBT and explicit-FSM rungs are the default; they cover most stateful subsystems and a single-process queue, parser, or session object rarely needs more. Escalating to a spec-language model checker is a deliberate jump, justified only when **any** of these triggers fires:
- The bugs live in **concurrency, distribution, or interleavings** — multiple actors, message orderings, replication, locking, distributed transactions. This is the sharpest discriminator: in-language stateful PBT explores *sequential* command sequences on *one* process and cannot exhaustively cover interleavings; model checking can.
- You need **temporal/liveness properties** ("eventually consistent", "no deadlock") or **exhaustive proof** over the abstraction rather than sampled paths — typically C4 "it's proven".
- The SUT spans **multiple services or languages**, so no single in-language library covers it and the model-outside-the-SUT property pays off.

Two counter-weights keep this from being overkill, and **both** must hold: the behavioral **contract must be stable** (a formal spec is a poor fit for churning behavior), and the **team must be able to sustain** a spec language (adoption and maintenance cost is a real decision input, not a technicality). When the triggers fire and the counter-weights hold, one model amortizes across proof, conformance, and trace validation — that is when a tool like Quint is ideal rather than overkill. When they do not, stay on the in-language rung and record *why* the spec tool was not warranted.

## Harness changes

| Component | Delta |
|---|---|
| `docs/testing.md` | Add a "Model-Based Testing" section: the chosen rung, the model's location, the abstract states and operation alphabet, legal/forbidden transition policy, the conformance relation (state/output/ioco), the coverage target, offline-vs-online choice, and nondeterminism handling |
| `docs/models/` (or equivalent) | The model as a first-class, reviewable spec artifact — the state machine definition, kept simpler than the implementation and updated in the same change as any behavior change |
| `make verify` | Conformance runs are a stateful test style. At the light rung they live under the existing property/unit subtarget the runner already discovers. Add a dedicated `verify-model` subtarget when generation/traversal is heavy enough to report or run separately. Either way it must compose into top-level `make verify` |
| `AGENTS.md` / `CLAUDE.md` | Add line: "Changes to [scoped stateful modules] must update the model in docs/models and pass conformance — see docs/testing.md for the state machine and transition policy" |
| CI workflow | `make verify` already runs in CI; pin the conformance seed and replay the regression sequence set so failures are reproducible from logs |
| Evidence conventions | On failure: the generated operation sequence, the diverging step, model-expected vs actual state/output, the shrunk minimal sequence, and the seed — captured in `.evidence/` or CI log, and the sequence added to the regression set. On success: coverage achieved (e.g. transitions covered / total), sequences run, and sequence-length distribution |

## How to get to a walking skeleton

1. **Install/choose the harness:** add the stateful-PBT library (or MBT tool) and wire it to the test runner.
2. **Define a trivial model:** two states and two transitions for the target subsystem (e.g. {closed, open} with `open`/`close`), plus one forbidden transition (`use` while closed must error).
3. **Write the command adapter and state projection:** map the abstract operations to real calls and project real state into the model's shape. Keep both minimal.
4. **Write the fail-fake:** deliberately make the model expect the *wrong* result for one transition (e.g. assert `use`-while-closed succeeds when the real system correctly rejects it), and confirm a generated sequence reports the divergence with a shrunk trace and seed through `make verify`.
5. **Write the pass-fake:** correct that transition so the model matches reality and confirm the conformance run goes green, emitting coverage and sequence-count evidence.
6. **Run via `make verify`:** confirm the conformance subtarget is composed into the top-level target and that divergences fail the build.
7. **Confirm CI:** push with the fail-fake, observe CI red with the diverging trace in the log; correct it, observe CI green; verify the seed and coverage appear.
8. **Remove fakes:** delete the fake transitions and replace with the first real lifecycle from the derivation heuristics, then grow the state machine toward the coverage target.

## Acceptance criteria to embed

The architect writes these into the generated artifacts so implementing agents can self-grade:

- [ ] A model exists as a distinct artifact and is demonstrably *simpler* than the implementation (reviewable on its own; not a line-by-line copy of the code).
- [ ] The operation alphabet covers the subsystem's state-mutating public API; out-of-scope operations are listed explicitly.
- [ ] Forbidden/illegal transitions are modeled — each has a defined expected rejection behavior, and at least one generated sequence exercises an illegal call.
- [ ] The conformance relation is stated (state parity, output parity, or ioco) and checked after every step of every generated sequence.
- [ ] Sequence generation meets the stated coverage target (e.g. 100% transition coverage) and the achieved coverage is reported as evidence.
- [ ] Failing runs record the generated sequence, the diverging step, model-expected vs actual, the shrunk minimal sequence, and the seed; the sequence is added to the regression set.
- [ ] Divergences produce a structured report (trace + step + expected/actual), not an opaque assertion failure.
- [ ] `make verify` (including whichever subtarget owns conformance) passes locally and in CI, with the seed pinned in CI.
- [ ] **Kill test:** inject a state bug (allow an operation in a state where it must be rejected, or land a transition in the wrong state). At least one generated sequence must catch it. Record the mutation, the catching sequence, and the shrunk trace as evidence.

## Composition

**Upstream feeds:**
- **Property-based testing** is the loose predecessor and the on-ramp: its stateful/model-based rung (reference-implementation model + random command sequences) *is* lightweight MBT. Existing PBT generators, shrinking, and CI wiring are reused directly; explicit-model MBT is the upgrade when you need coverage metrics, coverage-directed generation, or online execution.
- **Type-driven assurance** constrains the command alphabet and operation arguments, shrinking the model's input space so generated sequences stay valid by construction.

**Downstream consumers:**
- **Differential testing** shares MBT's machinery: when a reference implementation exists, the model *is* that reference and conformance *is* the differential check across generated sequences — the canonical bundle for stateful algorithmic cores.
- **End-to-end testing** can be driven by an MBT model of UI navigation: the model of screens-and-transitions generates E2E journey sequences, systematically exploring the navigation space instead of a handful of hand-written paths.

**Stricter sibling — model checking the model:**
- **Formal spec + model checking** is the upgrade-stricter path, and it is complementary, not redundant: model checking *exhaustively proves properties over the model* (and only the model); MBT *tests the running system against the model* on sampled paths. Model checking finds bugs in your abstraction; MBT finds bugs where the implementation diverges from a (now-trusted) abstraction. The strongest configuration writes the model once in a spec language, model-checks it for safety/liveness, then uses that *same* model to drive conformance testing of the code — collapsing the standard rung, the stricter sibling, and trace validation into one artifact. Some spec tools span this whole bridge from a single model: Quint (TLA+ family), for example, offers simulation (the random-exploration rung), model checking, model-based test generation, and production-trace validation — and because the model lives outside the system under test, it applies to any implementation stack. This makes a single-model spec tool a strong default when a subsystem justifies both proof and conformance.

## Failure modes & retirement triggers

| Sign | Diagnosis | Correction |
|---|---|---|
| Model finds almost nothing despite real bugs existing | The model is as complex as (or copied from) the implementation, so it shares the same mistakes | Re-abstract to equivalence-class states simpler than the code; if no simpler correct model exists, MBT is the wrong tool here |
| Conformance fails on correct system behavior (false alarms) | The model has drifted from intended behavior, or a legitimate nondeterministic outcome isn't modeled | Treat the model as a spec: review it on every behavior change; model nondeterministic transitions as allowed-outcome sets |
| Runs are flaky / non-reproducible | Online generation against a nondeterministic system without modeled nondeterminism, or no seed capture | Model the nondeterminism explicitly, switch the check to set membership, pin and record seeds |
| Failing sequences are long and unreadable | Shrinking disabled or generated paths unbounded | Enable trace shrinking; bound maximum sequence length; ensure abstract operations have readable names |
| Bug yield drops to near zero after the first runs | Coverage criterion saturated — every transition already exercised | Raise the criterion (transition-pair / bounded paths), or accept saturation and move the suite to regression mode |
| Generation is slow / explodes | Aiming at exhaustive path coverage on a large state space | Switch to coverage-directed sampling at transition coverage with bounded length; do not chase exhaustiveness — that is model checking's job |
| Model maintenance dominates effort | The modeled surface is too volatile for an explicit model | Pull back to reference-model stateful PBT, or model only the stable behavioral contract until the surface settles |

**Retirement triggers:** MBT for a subsystem is a candidate for retirement when (1) formal spec + model checking subsumes the same behavior with exhaustive guarantees over a verified model; (2) transition coverage has long saturated with no new bugs and the surface is stable — keep the suite as cheap regression, stop investing in new model breadth; (3) the subsystem loses its statefulness (refactored to pure functions — revert to unit tests / PBT invariants); or (4) the subsystem is deprecated. Never retire without confirming the replacement covers the same ordering/lifecycle bug classes and the kill test still fails against an injected state bug.

## Tool pointers

Two categories matter, and they map onto the stack question differently. **Stack-agnostic tools** keep the model *outside* the system under test and drive it through its interface or recorded traces — naming them implies nothing about the implementation language, so prefer them when you want the model decoupled from the code. **Stack-bound tools** are libraries that live *inside* the SUT's language; pick the one matching the project's stack.

*Stack-agnostic (model outside the SUT):*
- **Single-model spec tools (author + check + generate + validate from one model):** Quint and TLA+/TLC, Alloy — write the model once, model-check it, then drive conformance and/or trace validation from the same artifact. See the formal-spec-model-checking card.
- **Dedicated MBT engines (explicit models, coverage, online/offline):** GraphWalker (model as graphs, online + offline, coverage criteria), AltWalker (GraphWalker-based runner), fMBT (online testing).
- **Conformance / ioco standards-grade:** TorXakis and JTorX (input-output conformance for labeled transition systems); TESTAR (scriptless, model-inferred UI testing).

*Stack-bound (library inside the SUT — the light stateful-PBT rung):*
- Hypothesis `RuleBasedStateMachine` (Python), fast-check model-based `commands` (TS/JS), proptest state-machine testing (Rust), PropEr `statem`/`fsm` and Erlang/Elixir QuickCheck, ScalaCheck stateful (Scala/JVM), ModelJUnit (Java, FSM/EFSM) — reuse the PBT card's infra and shrinking.
