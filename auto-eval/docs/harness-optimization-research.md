---
hash: "b819a955"
id: "eval-harness-optimization"
read_when: "designing auto-eval as an optimizer for coding-agent harnesses, writing candidate-run storage, building proposer-facing eval tooling, or translating Meta-Harness ideas into auto-stack"
summary: "Research note translating Meta-Harness and LAB harness-optimization results into an auto-eval architecture: full-history experience store, proposer-facing CLI, dense composite metrics, split discipline, leakage guards, and PR-review calibration."
title: "Harness Optimization Research for auto-eval"
---

# Harness Optimization Research for auto-eval

This note translates two external sources into auto-eval design:

- [Meta-Harness: End-to-End Optimization of Model Harnesses](https://arxiv.org/abs/2603.28052)
  by Lee, Nair, Zhang, Lee, Khattab, and Finn.
- [Don't Train the Model, Evolve the Harness](https://huggingface.co/spaces/joelniklaus/harness-optimization),
  Joel Niklaus's LAB harness-optimization write-up.

It complements:

- `auto-eval/requirements.md`: the compile -> run -> score harness for context recall.
- `auto-eval/v1-setup-harness.md`: the first engineering slice, where GitHub PR review is
  the score.
- `auto-eval/docs/evaluating-playbook-rule-utility.md`: the causal-utility framing for
  Playbook rules.

The external papers use "harness" as a broad term for the code around a fixed model: prompt
construction, retrieval, memory, tool wrappers, state updates, output validation, and stop
logic. In auto-stack terms, a candidate harness is the configurable layer that shapes an
agent Session while the base model, Fixture, and evaluation split stay fixed.

## Executive Read

Auto-eval should grow from "run scenarios and score them" into a lab for improving coding-agent
harnesses. The v1 compile/run spine is still the right substrate, but the next architecture
should be designed around candidate harnesses, a full-history experience store, and a proposer
agent that can inspect prior code, raw Session traces, scores, and diffs.

The main design change is this: summaries are not enough. Meta-Harness works because the
proposer has filesystem access to every previous candidate's source, execution traces, and
scores. In its most demanding setting, the proposer read a median of 82 files per iteration
and used prior source and traces far more than score summaries. Auto-eval should therefore
store complete, queryable experience, not just aggregate score rows.

## Source Observations

### 1. Harness quality can dominate model choice

Meta-Harness frames the harness as the code that decides what to store, retrieve, and present
to the model. The paper reports gains across text classification, math retrieval, and
TerminalBench-2 while holding the base model fixed. The headline is not that one special
prompt won; it is that executable scaffolding around the model is a large optimization
surface.

The LAB write-up is the same Observation in legal-agent form. A fixed open model often
underperformed because useful work was saved to the wrong file, left in a scratch directory,
written under the wrong name, corrupted by tool-call issues, or caught in fragile run logic.
Deterministic harness code recovered a large part of that lost score without changing model
weights.

For auto-eval, this means context recall is only one early evaluator. The broader system
should measure and optimize:

- Context Pack selection and ordering.
- Rule and Playbook surfacing.
- tool-call repair and retry policy.
- output placement and final artifact validation.
- build/test/check cadence.
- loop control, cap handling, and no-change completion.
- PR provenance, reviewability, and cleanup.

### 2. Full-history filesystem feedback is the core mechanism

Meta-Harness deliberately avoids optimizing from compressed summaries. Each evaluated
candidate writes a directory with source code, scores, and execution traces. The proposer is a
coding agent that uses normal developer operations, such as file search and file reads, to
selectively inspect that history.

That maps directly to auto-stack's strengths. Auto-eval can expose:

- source diffs for each candidate harness.
- Scenario, Fixture, Case, Evaluator, and Run metadata.
- Session and Message IDs from auto-etl.
- exported tool-call traces and artifact diffs.
- raw mechanical scores and prompt-evaluator outputs.
- GitHub PR review comments from v1 runs.

Summaries remain useful as indexes. They should not replace raw traces.

### 3. The proposer is a coding agent, not a prompt-only optimizer

Meta-Harness uses a coding-agent proposer because the diagnostic footprint is larger than a
single prompt. The proposer can navigate, compare files, inspect failed runs, and edit code.
The outer loop stays simple: propose, validate, evaluate, log, repeat.

The implementation advice is especially relevant for auto-eval:

- write a good proposer Skill that defines the role, layout, CLI, output format, objective,
  and forbidden behavior.
- constrain artifacts and safety-relevant behavior, not diagnosis.
- run short 3-5 iteration loops to debug the Skill before a full sweep.
- automate evaluation outside the proposer.
- validate candidates cheaply before spending on a full evaluation.

Auto-eval should eventually ship an `auto-eval-proposer` Skill that tells agents how to
inspect experience, emit `pending_eval.json`, and avoid held-out data.

### 4. Dense metrics beat sparse success labels during search

The LAB write-up optimized pooled criterion pass rate because all-pass was too sparse and
noisy on a small dev set. Meta-Harness likewise optimizes against task-specific reward
functions and reports Pareto frontiers when multiple objectives matter.

For coding-agent work, final success is important but too sparse to be the only search signal.
Auto-eval should use a dense composite during search, with raw metrics preserved:

- tests passed, build passed, lint passed.
- count and type of hard errors.
- expected files read before first mutation.
- unexpected context read count.
- Rule/Playbook retrieval precision.
- wall-clock time.
- input/output tokens and context tokens.
- Message count and tool-call count.
- repeated failed commands.
- file churn and reverted edits.
- PR review defects by severity when available.
- final artifact presence, path, and schema.

The composite can be a versioned formula. Raw metrics must be stored separately so old runs
can be rescored when weights change.

### 5. Promotion must handle noise and stale derived scores

The LAB loop used three trials and a one-point `min_delta` because individual runs varied
enough to flip decisions. It also records a concrete failure mode: a candidate was promoted
after comparing its fresh score against an incumbent score computed under stale weights.

Auto-eval should treat this as a design constraint:

- store raw metric rows, not only blended scores.
- stamp `scoring_version`, `evaluator_version`, and objective weights.
- recompute challenger and incumbent under the same objective at comparison time.
- require a minimum observation count before promotion.
- interleave arms to reduce model/provider drift.
- emit confidence intervals or at least per-Fixture deltas.
- refuse to promote when the score delta is below the declared noise margin.

### 6. Code mechanisms transfer better than prompt playbooks

In the LAB run, deterministic code mechanisms drove the largest gains: deliverable landing,
matter checks, tool-call repair, and loop robustness. Prompt playbooks helped early but were
more model-specific and could backfire across model families.

Auto-eval should therefore classify candidates by mechanism type:

- `prompt`: system prompt, plan prompt, reviewer prompt.
- `context`: Context Pack selection, ordering, truncation, Rule/Playbook surfacing.
- `tooling`: tool wrapper, retry policy, repair, structured output enforcement.
- `validation`: output path, final artifact schema, tests, PR checks.
- `control`: loop termination, cap behavior, reflection cadence.
- `mixed`: multiple mechanism types.

This classification should be queryable in summaries and frontiers. It will let auto-eval ask
which mechanisms transfer across models, Projects, and Scenario families.

### 7. Leakage and infrastructure failures need first-class accounting

Both sources emphasize reliability. Meta-Harness keeps held-out results away from the
proposer. The LAB write-up treats provider failures, auth failures, retries, and timeouts as
measurement risks, not normal model failures.

Auto-eval should model these separately:

- `completion_reason`: success, cap_wall_clock, cap_turns, cap_tokens, agent_error,
  provider_error, harness_error, validation_failed, no_change.
- `split`: search, validation, held_out.
- `leakage_audit`: passed, failed, unknown.
- `infra_retry_count` and `provider_retry_count`.
- `excluded_from_score`: true/false with reason.

An eval that confuses infrastructure with model behavior will optimize the wrong thing.

## Proposed auto-eval Architecture

### Candidate harness interface

The compile/run spine stays unchanged. Add a candidate-harness layer around the agent runner:

```text
Fixture + Case + CandidateHarness + Agent + Trial -> Run -> Score rows
```

The first interface can be intentionally narrow:

```text
CandidateHarness.Prepare(worktree, run_context) -> prepared prompt/config/env
CandidateHarness.Validate(worktree, run_context) -> validation result
CandidateHarness.Finalize(worktree, run_context) -> final artifact checks/repairs
```

Allowed changes in the first optimizer pass:

- prompt text and planning instructions.
- Rule/Playbook retrieval policy.
- Context Pack assembly policy.
- final artifact checks.
- command retry policy and known tool-call repair.
- cap and stop-condition policy.

Fixed components:

- base model for the Scenario.
- Fixture start SHA.
- held-out split.
- Evaluator definitions for the active sweep.
- runner isolation and provenance format.

### Experience store

The canonical Session and Message data should remain in auto-etl. Auto-eval should add a
query-friendly experience store that links to that canonical data and exports high-signal
views for proposers.

Suggested layout:

```text
.auto/eval/experience/<scenario_id>/
  scenario.json
  objective.json
  splits.json
  frontier.json
  candidates/
    <candidate_id>/
      candidate.json
      parent_ids.json
      mechanism.json
      source/
      diff.patch
      validation.json
      scores.raw.jsonl
      scores.blended.json
      runs/
        <run_id>/
          run.json
          session_ref.json
          messages.preview.jsonl
          tool_calls.jsonl
          artifacts/
          git.diff
          evaluator_outputs/
      proposer/
        pending_eval.json
        proposer_session_ref.json
        notes.md
```

Important properties:

- every file is machine-readable unless it is explicitly a human note.
- IDs are stable and included in every row.
- raw score data is never overwritten.
- blended score files are derived and can be regenerated.
- proposer-visible data excludes held-out results.
- held-out results live under a separate access-controlled path or are hidden until final
  reporting.

### Proposer-facing CLI

Filesystem access should be enough, but a small CLI will reduce navigation cost and align
with coding-agent workflows.

Initial commands:

```bash
auto eval frontier <scenario>
auto eval candidate list <scenario>
auto eval candidate get <candidate_id>
auto eval candidate diff <a> <b>
auto eval failures <candidate_id> --split search
auto eval runs <candidate_id>
auto eval run get <run_id>
auto eval trace grep <scenario> <pattern>
auto eval score recompute <scenario> --objective objective.json
```

All commands should default to JSON where they expose data. Human text mode can be opt-in for
inspection.

### Scoring model

Use two score layers:

1. Raw metrics: immutable rows emitted by Evaluators.
2. Objective views: versioned formulas that compute blended scores or Pareto dominance.

Example objective:

```text
score =
  correctness_points
  + 0.5 * final_artifact_points
  + 0.2 * expected_context_recall
  - 0.1 * unexpected_context_rate
  - 0.005 * tokens_per_million
  - 0.01 * wall_clock_minutes
  - 0.5 * hard_error_count
```

The exact weights are less important than the discipline: raw metrics are durable; objective
views are explicit, versioned, and recomputed for all compared candidates.

When objectives conflict, report a Pareto frontier over:

- outcome quality.
- token/context cost.
- wall-clock.
- failure rate.
- review defect count.

### Split discipline

Preferred split:

- `search`: proposer-visible candidate evaluation.
- `validation`: optional cheap smoke subset used before full search-set evaluation.
- `held_out`: final reporting only; never visible to the proposer.

TerminalBench-2 in Meta-Harness used the same public benchmark for search and final
evaluation because it was small, expensive, and already a public discovery target. Auto-eval
should not copy that as the default. Historical auto-stack Sessions give enough material to
hold out Fixtures.

### Candidate validation

Before a full run, validate:

- config schema.
- candidate source imports/loads.
- `Prepare`, `Validate`, and `Finalize` execute on a tiny Fixture.
- runner isolation is present.
- provenance block parses.
- caps are enforceable.
- no held-out paths are read.
- no `$HOME` or secret-dependent state is required.
- output directories and artifact paths are deterministic.

Cheap validation failures should not enter the main score except as candidate metadata.

### PR review as calibration data

The v1 setup harness says GitHub PR review is the score. That should remain true for the first
slice, but every PR review should also be captured as future evaluator data:

- review comments linked to `run_id`.
- changed files and diff hunks.
- reviewer verdict.
- defect category and severity where available.
- whether the comment caused a fix.

This is the bridge from human review to prompt-evaluator calibration and eventually to
mechanical scoring.

## First auto-eval Optimizer Slice

The smallest useful slice is not a full Meta-Harness clone. It is:

1. Add experience-store writing for current v1 runs.
2. Add a dense process-metric Evaluator over Session and Message data.
3. Add `candidate_id`, `mechanism_type`, `split`, `trial`, and version stamps to run records.
4. Add proposer-facing read commands: `frontier`, `candidate list/get`, `candidate diff`,
   `failures`, and `run get`.
5. Write an `auto-eval-proposer` Skill.
6. Run a tiny search over one harness boundary, probably Context Pack/Rule surfacing or final
   artifact validation.

A good first Scenario should use hard historical Fixtures where the current baseline misses
expected context, fails a build, or produces a noisy PR. Use a small search set, 2-3 trials,
and a hidden held-out set. Do not optimize on the whole Project history.

## Design Decisions to Carry Forward

| Decision | Default |
|---|---|
| Candidate feedback | Full filesystem experience store, not summaries only |
| Proposer | Coding agent with a Skill and CLI access |
| Search unit | Candidate harness |
| Primary score | Dense composite over raw metrics |
| Comparison | Recompute raw metrics under one objective; report Pareto frontier when needed |
| Promotion | Minimum N plus `min_delta`; no promotion under the noise margin |
| Splits | Search and held-out by default |
| Leakage | First-class audit field |
| Infra failures | Separate completion reasons, not model failures |
| Human review | v1 score and future calibration data |

## Open Questions

- What is the first candidate-harness boundary: prompt/config only, Go code around runners, or
  a small scriptable layer?
- Should experience live under `.auto/eval/experience` in the Project, under `~/.auto/eval`,
  or both with different retention policies?
- How much Session and Message data should be exported into the experience store versus linked
  by ID into auto-etl?
- Which dense process metrics are already available from auto-etl, and which need schema work?
- What minimum N is acceptable for cheap local Scenarios before the tool refuses promotion?
- How should held-out leakage be enforced when a proposer has normal filesystem access?
- Should PR review comments become a formal Evaluator input before or after the first
  mechanical scoring slice?

## Bottom Line

Auto-eval's durable role should be the measurement and optimization loop for agent harnesses.
The current compile/run design gives it the right bones. The Meta-Harness and LAB work point
to the next load-bearing pieces: complete candidate experience, proposer-facing tooling,
dense metrics, strict split discipline, cheap validation, and promotion rules that respect
noise.
