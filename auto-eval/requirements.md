# Coding Agent Context Recall Evaluation Framework

**Product Requirements Document — v1**

## 1. Overview

A test harness that measures whether coding agents (initially Claude Code) retrieve the expected context files before beginning a task. The framework lets us A/B compare different ways of organizing project context — documentation, skills, agent memory files — and produces quantitative evidence about which approaches the agent actually uses.

The framework is conceptually three stages: **compile** (turn a config file into a set of immutable git refs, each representing an evaluation environment), **run** (launch agent sessions against those environments), **score** (query session data to produce structured metrics).

It integrates with the existing `auto-stack` (`autoetl`, `autosearch`) and lives in the `auto-eval` sub-project.

---

## 2. Motivation

We currently invest significant effort in authoring `CLAUDE.md`, skills, doc trees, and `AGENTS.md` indexes to give coding agents the context they need. We have no systematic way to verify these strategies work. Specifically, we cannot answer:

- When we ask Claude Code to do task X, does it read the docs we expected?
- If we move content from `docs/` into `skills/`, does context-gathering improve or degrade?
- Is putting the index in `AGENTS.md` more effective than relying on the agent to discover files via `Glob`?

These are empirical questions and we should be answering them with data, not intuition. The framework exists to produce that data on demand for any context-organization hypothesis we want to test.

---

## 3. Goals & Non-Goals

### Goals

- **Quantitatively measure context recall**: which expected files the agent reads before starting the task, which it misses, which unexpected files it reads (noise).
- **A/B/N comparison** of different context-organization strategies on the same underlying task.
- **Real historical tasks as fixtures**, replayed under modified context conditions, so the eval matches our actual workflow rather than synthetic benchmarks.
- **Reproducibility**: same config + same fixtures → same compiled artifacts and (modulo agent stochasticity, handled by N-trial averaging) same scoring outputs.
- **Auto-stack integration**: scoring runs as SQL against `autoetl` Parquet; results are queryable via `autosearch` patterns.
- **Cost-efficient**: a full sweep is feasible on a weekly cadence within a reasonable API budget.

### Non-Goals (v1)

- Measuring task **completion** quality. We're testing context retrieval only, not whether the agent solved the problem correctly.
- **Comparing agents** to each other (Claude Code vs Codex vs others). The schema allows for it; v1 only exercises Claude Code.
- **Concept-based scoring** (where files are tagged by concept and the eval scores concept coverage instead of file paths). Useful but deferred until we have enough scenarios to justify the abstraction.
- **Live monitoring** of production agent runs.

### Constraints

- Must not modify the operator's actual codebase. All runs occur in disposable git worktrees.
- The compile step must be hermetic: no network, no `$HOME` reads, no environment leakage.
- Must run on macOS and Linux. Windows not supported in v1.

---

## 4. User Stories

**As an agent operator,** I want to author a new evaluation scenario in under 30 minutes so I can quickly test a hypothesis without significant setup cost.

**As an agent operator,** I want to compare context strategies side-by-side and see per-fixture deltas so I can identify which strategy works best for which task type.

**As an agent operator,** I want to inspect the exact filesystem state the agent ran against for any historical evaluation run so I can debug surprising results months after the fact.

**As an agent operator,** I want the framework to refuse to run if my scenario is misconfigured (expected files don't exist, base SHA unreachable, etc.) so I don't waste API spend on sweeps that can't produce meaningful results.

**As an agent operator,** I want to use real historical tasks as fixtures so the evaluation conditions match my actual workflow.

---

## 5. System Model

The framework is organized around five concepts:

### Fixture
A frozen starting state: a specific repository commit (SHA) plus the original user prompt that initiated the task at that point in history. Fixtures are immutable references to historical work.

### Scenario
A named experiment that compares multiple variants of a single underlying setup. A scenario binds to one or more fixtures, declares the evaluators it uses, declares the agent configuration(s) under test, and contains the cases (variants) being compared.

### Case
A specific variant within a scenario. Each case defines:
- `setup:` — bash steps that transform the fixture's starting state into this case's context layout
- `assert:` — bash steps that verify the transform produced the intended state
- `eval_inputs:` — inputs for the scenario's evaluators (e.g., the list of files the agent was expected to read)

### Evaluator
A reusable scoring function with a structured output schema. Two types:
- **Mechanical** — body is a SQL query (or referenced code) that operates on `autoetl` Parquet datasets. Deterministic, free, used for measurable signals like "did this file get read."
- **Prompt** — body is an LLM prompt that scores a session trajectory or output. Non-deterministic, costs API calls, used for genuinely judgmental signals.

### Run
An execution of (scenario, case, agent, trial). The orchestrator creates a worktree from the compiled branch, launches the agent, ingests the resulting session via `autoetl`, and scores it.

---

## 6. The Compile-Run-Score Pipeline

### 6.1 Compile

**Input**: config file + assets directory + base git repo (with fixture SHAs reachable).
**Output**: a set of immutable git refs in the `refs/eval-cache/` namespace, one per (fixture, case) pair.
**Side effects**: writes git refs; no filesystem, network, or environment side effects outside that.

For each (fixture, case):

1. Compute `cache_key = hash(fixture_sha, canonicalized_case_definition, assets_hash)`.
2. If `refs/eval-cache/{fixture}-{case}-{cache_key}` already exists, skip (cached).
3. Otherwise: create a fresh `git worktree` checked out at `fixture.startHash`.
4. Execute the case's `setup:` steps in order with `set -euo pipefail`. Each step runs in its own subshell. First non-zero exit aborts the case.
5. `git add -A && git commit --amend --no-edit` to squash the transformation onto the fixture's start SHA. This produces a clean `git log` so the agent sees no synthetic "scenario: ..." commit when it inspects history.
6. Execute the case's `assert:` steps. Unlike setup, assertions run all-the-way-through and collect all failures rather than fail-fast.
7. Tag the resulting commit as `refs/eval-cache/{fixture}-{case}-{cache_key}`.
8. Delete the worktree.

Compile reports aggregate results: cases compiled, cases cached, cases failed, with the failure reason per case. A failure in one case does not abort compilation of others.

`compile --dry-run` performs all steps except writing refs and reports what would be done.

### 6.2 Run

**Input**: set of compiled refs, agent configuration(s), trial count.
**Output**: session records in `autoetl`'s output, plus an `eval_runs` table linking each session to its (fixture, case, agent, trial, cache_key) tuple.

For each (compiled_ref, agent, trial):

1. Create a worktree at `/tmp/eval-{run_id}/` from the compiled ref. The path encodes `run_id` so `autoetl` (which keys sessions by workspace path) can map back to the eval run.
2. Launch the agent with the case's prompt and limits. Use a clean `CLAUDE_CONFIG_DIR` to isolate from the operator's personal Claude Code config.
3. Wait for the session to complete, with wall-clock, turn-count, and token-count caps.
4. After the session ends, trigger `autoetl run` (or rely on the daemon) to ingest the session into Parquet.
5. Write a row to `eval_runs` capturing `(run_id, scenario_id, case_id, fixture_id, agent_id, trial, compiled_ref_digest, session_id, started_at, completed_at, completion_reason)`.
6. Delete the worktree.

Runs can be parallelized; the orchestrator respects a configured concurrency limit.

### 6.3 Score

**Input**: `eval_runs` table, `autoetl` Parquet datasets, evaluator definitions, per-case `eval_inputs`.
**Output**: an `eval_scores` table with one row per `(run_id, evaluator_id)` containing the evaluator's structured output.

For each (run_id, evaluator_id):

1. Resolve the evaluator's body and the case's `eval_inputs`.
2. **Mechanical evaluators**: execute the SQL against `autoetl`'s Parquet, scoped to this run's `session_id`. Materialize the output schema.
3. **Prompt evaluators**: assemble the prompt by substituting `eval_inputs` *and* the scenario specification — judges must see scenario context, not just trajectory, or they will score inconsistently across cases. Call the model. Parse the output against the output schema.
4. Write to `eval_scores`.

The default mechanical evaluator must define "before the agent started the task" as the index of the first mutating tool call (`Edit`, `Write`, `MultiEdit`, `NotebookEdit`). Reads after that index are excluded from scoring. Sessions with no mutations score against the full transcript.

Partial reads count toward recall. Bash commands that read files (`cat`, `head`, `grep`, `less`) count if the command string includes the expected path; this is a "best-effort with false positives" mode, acceptable for v1.

---

## 7. Configuration Schema

```yaml
fixtures:
  - id: "450-tw3-game-scoped-scoring"
    startHash: "abc123def456"      # MUST be a SHA, not a branch name
    original_prompt: "..."         # captured verbatim from autoetl

evaluators:
  - id: read-expected-files-before-implementation
    type: mechanical               # mechanical | prompt
    body_ref: sql/recall.sql
    output_schema:
      expected:              { type: array, items: string }
      read_expected:         { type: array, items: string }
      read_unexpected:       { type: array, items: string }   # noise / precision signal
      first_target_position: { type: number, nullable: true } # how quickly target was found

scenarios:
  - id: test-architecture-context-retrieval
    description: "Validate 3x approaches for providing testing architecture context"
    fixture_id: "450-tw3-game-scoped-scoring"
    trials: 5
    agents:
      - type: claude
        prompt: "..."              # literal prompt; slash commands expanded at compile time
        limits:
          wall_clock: 10m
          turns: 50
          tokens: 200000
    evals:
      - id: read-expected-files-before-implementation
    cases:
      - id: a-default
        setup: []
        assert: []
        eval_inputs:
          read-expected-files-before-implementation:
            files: [./docs/testing/architecture.md, ./skills/testing-architect/SKILL.md]

      - id: b-skills-only
        setup:
          - rm -rf ./docs
          - git checkout abc123 -- skills/testing-architect/
        assert:
          - test -f ./skills/testing-architect/SKILL.md
          - test ! -d ./docs
        eval_inputs:
          read-expected-files-before-implementation:
            files: [./skills/testing-architect/SKILL.md]
```

A formal JSON Schema should accompany the implementation.

---

## 8. Functional Requirements

| ID | Requirement |
|---|---|
| FR-1 | Compile produces inspectable, immutable artifacts. Every compiled case is a real git commit reachable through a stable ref. An operator can `git checkout` any compiled ref and see exactly the filesystem state the agent will run against. |
| FR-2 | Compile failures are explicit and informative. Setup failures include exit code, stderr, and step index. Assertion failures list every failed assertion, not just the first. |
| FR-3 | Setup steps are bash. The runner executes them with `set -euo pipefail`, each in its own subshell. Operators are responsible for command safety; no sandboxing beyond the disposable worktree. |
| FR-4 | Setup and assertion are syntactically distinct: separate fields with different execution semantics (setup is fail-fast, assertion is run-all-and-report). |
| FR-5 | Cherry-pick is documented as fragile. Documentation and examples use `git checkout <sha> -- <path>` or `cp -r assets/...` for cross-commit content transfer. |
| FR-6 | Pin to immutable refs. Configurations referencing `main`, `HEAD`, or other mutable refs in `startHash` or in `git checkout` setup steps produce a compile-time warning (and an error in CI). |
| FR-7 | Run-to-session mapping is unambiguous. Every run record resolves to exactly one session in `autoetl`'s output via a stable convention (worktree path encoding the run_id). |
| FR-8 | Mechanical scoring is primary. Built-in evaluators for recall, precision, and read-position metrics are mechanical. Prompt evaluators are supported but reserved for judgmental questions. |
| FR-9 | Multiple trials per case. Every (scenario, case, agent) cell is executed N times where N is configurable per scenario (default 1, recommended 5). Trial number is stamped on every run record. |
| FR-10 | Cache management. A `cache prune` command removes `refs/eval-cache/` entries by age, scenario id, or fixture id. Run records reference compiled refs by commit SHA, not ref name, so pruning does not orphan historical results. |
| FR-11 | Hermetic compile. The compile step makes no network requests, reads no `$HOME` configuration except git's repo-level config, and operates only on the input config, assets, and git repo. CI verifies this. |
| FR-12 | Isolated agent execution. Run-time agent invocations use a clean `CLAUDE_CONFIG_DIR` to prevent the operator's personal Claude Code configuration from contaminating evaluation results. |

---

## 9. Non-Functional Requirements

| ID | Requirement |
|---|---|
| NFR-1 | **Reproducibility**. Given the same config, assets, and reachable fixture SHAs, the compile step produces git commits with identical trees (commit SHAs may differ due to timestamps; tree SHAs must be equal). CI asserts this by compiling the same config twice and diffing trees. |
| NFR-2 | **Performance**. Compile: < 5s per case (excluding setup steps that run user-provided bash). Run startup: < 10s from "request run" to "agent launched." Score: mechanical evaluators < 1s per (run, evaluator); prompt evaluators bounded only by API latency. |
| NFR-3 | **Observability**. Structured logs (JSON Lines) for every compile, run, and score operation. A `summary` command produces a tabular view of the most recent sweep. |
| NFR-4 | **Cost predictability**. The orchestrator computes a sweep's estimated API cost before launching and refuses to proceed if the cost exceeds a configured threshold (default $50) without an explicit confirmation flag. |

---

## 10. Integration Points

- **Claude Code**: invoked via headless mode (`claude -p`). Session events include tool calls with arguments; this is the primary observable behavior.
- **autoetl**: must surface `session_id`, `message_index`, `tool_name`, `tool_file_path`, `tool_input` (for Bash command strings), and timestamps. May require schema additions if not all currently present — to be confirmed during implementation.
- **autosearch**: not directly consumed by the framework but enables manual debugging by operators inspecting eval results.
- **git**: requires `git worktree add`, `git checkout <ref> -- <path>`, `git commit --amend`, `git for-each-ref`, and tag/ref creation. Tested against git 2.40+.

---

## 11. Open Decisions (Deliberate v1 Deferrals)

| ID | Decision | Trigger for revisit |
|---|---|---|
| OD-1 | **Cross-fixture cross-product**. Scenarios currently bind to one fixture. For statistical signal, scenarios should declare `fixture_ids: [...]` and the runner cross-products. Schema change required. | ≥5 fixtures |
| OD-2 | **Concept-based scoring**. File paths are hardcoded in `eval_inputs`. A concept-tagging frontmatter scheme + concept-required-by-task mapping would make scoring scenario-agnostic. | ≥10 scenarios |
| OD-3 | **Versioning stamps**. `config_version`, `evaluator_version`, `scoring_sql_version` on every run record. Required for cross-time comparison. | Add proactively |
| OD-4 | **Subagent reads**. How do reads inside `Task`-spawned subagents count toward parent recall? Default: union them in. Confirm in implementation. | Implementation |
| OD-5 | **Bash-as-read precision**. Current proposal matches expected path as substring of command. Produces false positives. | If distorts results |

---

## 12. Risks

| ID | Risk | Mitigation |
|---|---|---|
| R-1 | Operator's global Claude Code config bleeds into eval runs, invalidating cross-time comparisons | FR-12 (clean `CLAUDE_CONFIG_DIR`) + CI test asserting isolation |
| R-2 | Compile step picks up environment differences across operators' machines | NFR-1 reproducibility test in CI; hermetic compile (FR-11) |
| R-3 | API costs grow faster than expected as scenario set expands | NFR-4 cost predictability + `--max-cost` flag with hard refusal |
| R-4 | Agent stochasticity larger than effect size we're trying to measure | N-trial averaging (FR-9); plot per-task deltas to make variance visible |
| R-5 | `autoetl` schema doesn't currently capture all needed signals (e.g., Bash command arguments) | Audit `autoetl` schema during implementation; extend if needed before building scorers |

---

## 13. Acceptance Criteria

The implementation is considered complete for v1 when:

1. An operator can author a new scenario (3 cases) targeting an existing fixture in under 30 minutes, including assets if needed.
2. `eval compile config.yaml` produces inspectable `refs/eval-cache/...` for every case; output reports per-case status.
3. `eval run config.yaml` executes all (case × agent × trial) cells in disposable worktrees, integrating with `autoetl` for session capture.
4. `eval score config.yaml` produces per-run, per-evaluator structured scores in a queryable form (CSV or Parquet).
5. `eval summary config.yaml` prints a per-scenario A/B/N comparison showing per-case recall, precision, and noise across trials.
6. Re-running `eval compile` on an unchanged config rebuilds zero cases.
7. Changing one case's setup script triggers recompilation of only that case.
8. CI tests verify hermeticity, reproducibility (tree-SHA equality across compiles), and that the default mechanical evaluator correctly counts reads before/after the first mutation.

---

## 14. Out of Scope for This Document

- Detailed UX for `auto-eval` CLI commands (help text, flag names, output formatting).
- Storage schema details for `eval_runs` and `eval_scores` tables.
- Specific SQL implementation of the default recall evaluator.
- Multi-machine / distributed execution.
- Web UI / dashboard for visualizing results.

These should be addressed in the implementation plan that follows this document.
