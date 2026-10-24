---
hash: "47054da1"
id: "c114fe42"
read_when: "deciding whether auto-stack should adopt or borrow from Atomic, designing deterministic execution/verification gates for Plans, or comparing our skill-driven task pipeline with a workflow runtime"
summary: "Research note on Atomic (bastani-inc/atomic), an open-source model-agnostic coding-agent runtime built as a recursive state machine; compares its workflows, run contract, verification gates, Intercom and packages with auto-stack's skill-driven task pipeline, and ranks what to borrow."
title: "What auto-stack Can Learn from Atomic"
---

# What auto-stack Can Learn from Atomic

## Purpose

This note records findings from reading the Atomic documentation and the
companion essay on recursive state machines, and compares them with how
auto-stack plans and executes work today.

The question is not "should we switch to Atomic". Atomic is a whole agent
runtime — it replaces Claude Code as the host — while auto-stack is tooling
layered on top of Claude Code (Skills plus the Go `auto` binary). The useful
question is: which Atomic ideas close real gaps in our pipeline, and can we
adopt them without changing hosts?

## Sources

- Atomic docs index: <https://docs.bastani.ai/llms.txt>
- Pages read in full or in part: Overview, Workflows, Builtins, Reliable
  design (execution shape, run contract, scope guard, prompt anatomy,
  stacked slices), Verification, Intercom, Compaction, Packages.
- Not read: Operations, API reference, SDK, RPC, extension pages.
- Essay: Alex Lavaee, "Recursive State Machines for Language Models"
  <https://alexlavaee.me/blog/recursive-state-machines-execution-layer/>
- Underlying theory: Alur et al., "Analysis of Recursive State Machines",
  TOPLAS 27(4), 2005.

## What Atomic is

- A terminal coding-agent runtime built on Pi extensions, TypeScript,
  installed via npm or a release archive.
- Model-agnostic: subscription and API-key providers, local llama.cpp,
  per-stage model routing (`model: "auto"`) and fallback chains.
- Extensible through extensions, Skills, prompt templates, themes, MCP, and
  Atomic packages (npm/git/local bundles of all of the above).
- Its centrepiece is **workflows**: TypeScript `workflow({...})` definitions
  with tracked stages, parallel branches, artifacts, structured outputs,
  human-input gates, checkpoints and resumable background execution.

## The core idea: recursion in the execution layer

The essay positions Atomic against recursive language models (RLMs), where
a model spawns child model calls over slices of its context.

- **The RLM gap.** "A child returns text, and the parent believes it." The
  recursion is invisible: no state to inspect, no transition to guard,
  nothing to resume if the process dies.
- **The RSM answer.** A recursive state machine is a finite set of component
  machines whose nodes may be *boxes* — calls into another component with
  call-and-return semantics. The definition is finite; each run unfolds
  into a tree of activations as deep as the task needs.
- **The mapping.** A stage is a state (and a full harness instance); a
  workflow is a graph of stages; a child workflow is the call.
- **Stage lifecycle.** pending → running → awaiting input / paused →
  completed / failed. Every transition is validated and appended to the
  run history.
- **Durability.** Every transition is checkpointed. Resume replays the
  workflow and completed calls return stored results — memoised, including
  whole child workflows.
- **Deterministic control around a nondeterministic model.** Verification,
  retries (bounded), and human approval are all states. "The model gets a
  vote. The code decides."

What moving the recursion into the runtime buys, per the essay:

| Property | Why it follows |
|---|---|
| Scope is bounded | A box has one entry and one exit; depth is bounded by a readable definition |
| Verification is possible | A return is a transition, and a transition can be guarded |
| Cost is countable | Every activation is a tracked node; fan-out can be capped per level |
| Memory is free | A call that is a transition is a call you can checkpoint |
| Control survives restarts | Pause and approval are states, not process state |

## How Atomic plans

Atomic has no planning *document*. The plan is the machine definition plus
one run's contract.

1. **Execution-shape choice (agent-owned).** Inline → inline + subagents →
   named workflow → custom workflow → composed workflow, cheapest first.
   Non-trivial work with a verifiable objective runs as a workflow;
   brainstorming, unclear goals and tiny edits stay inline.
2. **Pre-launch architecture pass (internal, not reviewed).**
   - Coverage matrix: `requirement/risk | required evidence | producing
     stage | gap`; an open gap must change the graph before launch.
   - Topology check: no self-edges or back-edges; loops are unrolled into
     fresh tracked nodes.
   - Repository-intent mining: git log (with signatures), merged PRs,
     issues, review comments — the requesting user's own activity weighted
     highest.
   - Runtime-aware schedule from real CI/test timings; estimate the
     critical path, run the full suite once on the final candidate.
   - Per-stage model and thinking level by role and failure cost.
3. **The run contract.** Objective plus acceptance criteria.
   - Only the user may change it; agent-discovered work goes on a deferred
     list, never into the diff.
   - Mid-run steering must be restated downstream under a
     `Contract amendments received` heading, or reviewers will score the
     amendment as unrequested scope.
   - Scope discipline: state the goal, list the criteria, freeze them;
     every change traces to a criterion; smallest diff wins.
   - Contract text is wrapped in `keepContext` so compaction cannot drop it.
4. **Research as a stage (`ralph`).** Raw task → research question →
   codebase research artifact → delegated implementation → independent
   review, looping up to `max_loops`.
5. **Decomposition.** *Stacked slices* (one objective, a dependent chain of
   ~100–500-line slices, each with its own criteria, branch and child
   `goal`/`ralph` run, stopping at the first failed gate) versus *queues*
   (unrelated items as concurrent top-level runs, serialised only on a
   proven dependency).

## How Atomic verifies

- **Propose, measure, review, decide.** The model proposes; real checks run
  through a durable `ctx.tool` primitive; fresh-context reviewers see the
  diff and evidence but not the implementer's conversation; a reducer in
  code picks pass / repair / fail / stop-on-budget.
- **`goal` builtin.** Durable ledger holding the literal objective and
  immutable acceptance criteria; bounded sub-agent orchestration; parallel
  review; deterministic reducer. Outcome is `complete`, `blocked` or
  `needs_human`.
- **Authoritative convergence signal.** `stop_review_loop` stays false while
  any P0–P2 or `required_by_objective` finding is open. A `true` vote does
  not count when the same review reports the patch incorrect.
- **Low-confidence re-verification.** A blocking finding with one reviewer
  and confidence below 0.7 is rescored three times in fresh contexts; it is
  demoted only if the mean falls below a threshold.
- **`adversarial-verification` builtin.** Per-criterion verifier fan-out,
  mean-plus-veto gate, bounded repair; the consolidator cannot approve.
- **Schema checks beside executable checks.** TypeScript holds stage
  contracts at build time, TypeBox validates structured outputs at run
  time — explicitly not a substitute for tests.
- **Evidence for PRs.** Browser, terminal and desktop capture guidance and
  native GitHub media attachment.
- **Stated limits.** Evidence is not proof; complex, security-sensitive or
  irreversible changes still need a person. Durable replay can repeat a
  side effect, so external actions must be idempotent.

## Comparison with auto-stack

| Area | auto-stack today | Atomic |
|---|---|---|
| Layer | Skills + `auto` CLI on top of Claude Code | The agent runtime itself |
| Planning | `new-task` → `new-solution` → `new-plan` → `commit-task`; human hard-stop per stage; `plan.html` + `context.md` or a `graph.json` Plan (Goals, Acceptance Criteria, Decisions, Alternatives, Questions, Stages, Stage Briefs) | Internal architecture pass + run contract; no reviewed document |
| Execution control | `execute-task`: a model coordinator follows skill prose and dispatches Subagents per phase | A state machine walks a workflow graph; transitions validated and recorded |
| Done | Coordinator judgement + local tests; `code-review`, `address-feedback`, `complete-task` | Deterministic reducer over fresh-reviewer verdicts and tool-run checks |
| Resume | Manual — reread docs and git state | Checkpoint replay; completed calls memoised |
| Contract drift | ACs exist in the Plan but nothing re-injects them on follow-up runs | Immutable criteria passed to every stage and follow-up run |
| Agent messaging | `auto mail`: durable, addressed Mail with ack; `#parent` Handle; ntm panes | Intercom: live 1:1 send/ask/reply, groups, `contact_supervisor`; not durable |
| Distribution | `auto skill` (git sources, skills.yaml, lock, multi-target) | Atomic packages (npm/git/local; extensions, Skills, workflows, MCP) |
| Evidence | `auto artifact` uploads to S3 for PR links | Verification guidance + native GitHub media |
| Learning | Offline: `auto etl` → `search` → `reflect` → Playbook Rules | Runtime: reviewers mine repo conventions per run |
| Session data | Cross-tool, cross-Host parquet with search and analytics | Own session format only |

### Where we are ahead

- **Planning depth.** Decisions with rejected Alternatives, gating
  Questions, Rails on Child Plans, Stage Briefs. Atomic's coverage matrix
  is ephemeral and never reviewed.
- **Durable messaging.** `auto mail` survives restarts and acks; Intercom's
  blocking `ask` times out after 10 minutes and nothing is stored.
- **Cross-tool, cross-Host Session history** and the reflect → Playbook
  loop. Atomic has nothing comparable.

### Where Atomic is ahead

- **Execution control lives in state, not prose.** Our `execute-task`
  coordinator is the RLM pattern the essay criticises: Subagents return
  text and the coordinator believes it.
- **Verification is a gate, not a judgement.** Checks run as real tool
  calls and a reducer in code decides.
- **Contract integrity.** Immutable criteria, propagated amendments, and
  compaction-proof constraints.
- **Durability.** Resume without redoing work or rediscovering state.

## What to borrow

Ranked by leverage over effort. None requires changing hosts.

1. **Pin Acceptance Criteria verbatim on every follow-up.** When
   `address-feedback`, a re-run of `execute-task`, or a repair Subagent
   works from findings, inject the Plan's original Acceptance Criteria
   text, not a paraphrase. `graph.json` already has them; Stage Briefs
   are the carrier.
2. **A code-owned Stage walker.** Treat a Plan as the finite definition:
   each Stage is a state, each Acceptance Criterion's command is a
   deterministic gate, Child Plans are calls. A small `auto plan run`-style
   driver (or a Workflow script) runs the commands itself, records each
   Stage transition, and refuses to advance on a failed gate. This is the
   single biggest structural gap.
3. **Fresh-context verifiers plus a reducer.** Between `execute-task` and
   `complete-task`, run reviewers that never saw the implementer's
   Session, have them return structured verdicts, and let code decide
   pass / repair / fail with a repair cap.
4. **Low-confidence finding re-verification** in `code-review` and the
   `request-*-review` Skills, to cut single-reviewer false positives.
5. **A deferred-work list.** Executors log out-of-contract observations
   instead of fixing them; the PR body surfaces the list. Pairs naturally
   with `auto reflect` Observations.
6. **`contact_supervisor` over `auto mail`.** Expose the `#parent` Handle to
   Subagents as a tool with fixed reasons (decision, structured interview,
   progress update).
7. **Verbatim compaction** as a design input for `content_truncated` and
   Outline derivation: delete lines, never rewrite the survivors.

## What not to borrow

- **The host swap.** Adopting Atomic means leaving Claude Code and
  re-homing every Skill, hook and the auto-ui event bus. The ideas above
  transfer without it.
- **Agent-owned execution-shape selection.** Our users choose the pipeline
  deliberately; the explicit `new-task` / `new-task-quick` split is a
  feature.
- **Unreviewed planning.** The private coverage matrix is fine for a single
  run but loses the Decision and Alternative record that our Plans keep.

## Open questions

- Should the Stage walker live in `auto plan` (Go, deterministic) or as a
  Claude Code Workflow script driven from `execute-task`?
- How should a Stage transition log relate to `auto etl` Sessions — a new
  dataset, or Events on the bus?
- Which Acceptance Criteria are safe to gate without a human (cheap,
  objective, hard to fake), and how is that marked in `graph.json`?
- Does durable replay require idempotency keys on side effects we already
  perform (PR creation, `auto mail send`, bead updates)?
