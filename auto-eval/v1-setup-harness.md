# auto-eval v1 — The Setup Harness

**Spike spec — infrastructure only. No automated scoring.**

## 1. What this is (and isn't)

This is the **first** auto-eval deliverable: the plumbing that turns a config file into
running agents in controlled environments whose results land as **GitHub PRs for a human to
review**. It is deliberately *not* the scoring engine.

The existing docs describe the full vision:

- `requirements.md` — the context-recall PRD (compile → run → **score**), with mechanical
  SQL evaluators and prompt judges.
- `docs/evaluating-playbook-rule-utility.md` — the causal-utility research (ablation, oracles,
  statistical-N, harm worst-cases).

Both are dominated by genuinely *unsolved research*. This spike amputates all of it and keeps
only the part that is pure **engineering**: get an agent running in a reproducible environment
and capture the result as something a human can review. That substrate is the common
denominator of *every* future scoring approach — mechanical, prompt-judge, per-rule ablation,
two-arm replay all consume "compile a branch → run an agent → capture output" unchanged. We
build the part with zero wasted motion while the scoring research stays unsettled.

**The cheat that makes v1 shippable:** the "score" step is a **human reviewing a PR**. At
single-user volume, automated scoring can't reach statistical significance anyway (per the
rule-utility doc). A human eyeballing diffs side by side is qualitative but high-bandwidth and
trustworthy — and it lands in the tool we already use all day. As a bonus, the PR comments
become the labeled-data corpus that a future automated judge needs to calibrate against.

## 2. Scope (decided)

| Decision | v1 choice |
|----------|-----------|
| Agent runner | **Pluggable; Claude Code + Codex from day one** (the two we run). Common launch interface so a third slots in later. |
| Sweep breadth | **Single case → single PR spine first.** Prove one fixture × one case × one agent end-to-end before any fan-out. With both agents enabled, the natural first comparison is two PRs (claude vs codex) on the *identical* compiled branch. |
| Scoring | **None automated.** GitHub PR + human review/comments *is* the score. |
| Trials | Default 1. N-trial averaging is deferred (it only matters for automated scoring). |

## 3. The happy path (user flow)

```
1. Operator writes eval.yaml: a fixture (start SHA + prompt) and one case (setup/assert)
   plus the agents to run (claude, codex).

2. auto eval build eval.yaml
   → for the case, compiles a clean immutable git ref at the fixture's start state with the
     case's setup applied. Inspectable: `git checkout` it to see exactly what the agent runs against.

3. auto eval run eval.yaml
   → for each (case × agent): disposable worktree from the compiled ref, launch the agent
     headless with the fixture prompt + caps, wait for completion, commit the agent's work to
     an eval/ branch, push it, open a PR with provenance metadata in the body.

4. Operator reviews the PR(s) in GitHub. Comments are the feedback. Done.
```

## 4. Command surface (v1)

| Command | Does |
|---------|------|
| `auto eval build <config>` | Compile the config's case into an immutable `refs/eval-cache/...` ref. Idempotent: unchanged case rebuilds nothing. `--dry-run` reports what would happen without writing refs. |
| `auto eval run <config>` | For each (case × agent): worktree → launch agent → commit → push `eval/...` branch → open PR. Respects caps. |
| `auto eval list` | Show compiled refs and the eval branches/PRs they produced. |
| `auto eval status` | State of in-flight / recent runs (running, completed, capped-out, failed). |

`build` and `run` are separate verbs (not fused) so an operator can inspect compiled state
before spending agent time, and re-run agents against an already-compiled branch.

## 5. Config schema (v1, minimal)

```yaml
fixture:
  id: "450-game-scoped-scoring"
  startHash: "abc123def456"        # MUST be a SHA, not a branch/HEAD
  prompt: "..."                    # the task prompt, captured verbatim

case:
  id: "baseline"
  setup:                           # bash, fail-fast, transforms start state → eval state
    - rm -rf ./docs
    - git checkout abc123 -- skills/testing-architect/
  assert:                          # bash, run-all-and-report (verify the transform)
    - test -f ./skills/testing-architect/SKILL.md
    - test ! -d ./docs

agents:
  - type: claude                   # claude | codex
  - type: codex

limits:                            # applied to every agent run
  wall_clock: 20m
  turns: 60
  tokens: 300000
```

A `fixtures: [...]` / `cases: [...]` plural form and cross-product sweep are the v2 shape; v1
parses exactly one fixture and one case so the spine stays small. (The parser should reject
the plural forms with a clear "deferred to v2" error rather than silently ignoring them.)

## 6. `build` — the compile step

For the single case:

1. `cache_key = hash(fixture.startHash, canonicalized_case, config_version)`.
2. If `refs/eval-cache/{fixture}-{case}-{key}` exists → skip (cached).
3. `git worktree add` at `fixture.startHash`.
4. Run `setup:` steps in order, `set -euo pipefail`, each in its own subshell. First non-zero
   exit aborts the case with exit code + stderr + step index.
5. `git add -A && git commit --amend --no-edit` — squash the transform onto the start SHA so
   the agent sees **clean history**, no synthetic "eval setup" commit when it runs `git log`.
6. Run `assert:` steps **run-all-and-report** (collect every failure, not fail-fast).
7. Tag the commit `refs/eval-cache/{fixture}-{case}-{key}`. Delete the worktree.

**Hermetic:** no network, no `$HOME` reads beyond git's repo config, operates only on config +
repo. (Carry-over of PRD FR-11; cheap to honor now, contaminating if skipped.)

**Pin to immutable refs:** `main`/`HEAD` in `startHash` or in a `git checkout` setup step → a
loud warning (and a hard error under `--strict`/CI).

## 7. `run` — the control loop + PR push

For each (case × agent):

1. Worktree at `/tmp/eval-{run_id}/` from the compiled ref. The path encodes `run_id`.
2. Launch the agent **headless** with the fixture prompt and caps, in an **isolated config
   environment** (clean `CLAUDE_CONFIG_DIR` for Claude; equivalent isolation for Codex) so the
   operator's personal agent config can't contaminate the run.
3. Wait for completion or cap (wall-clock / turns / tokens). Record the completion reason.
4. Commit the agent's working-tree changes onto a branch `eval/{fixture}/{case}/{agent}/{run_id}`.
5. Push the branch and open a PR whose **body embeds the provenance block** (§8).
6. Delete the worktree. Leave the branch/PR (that's the artifact).

### The agent runner abstraction (claude + codex)

A small interface so both agents — and a future third — launch the same way:

```
Runner.Launch(workdir, prompt, limits, isolationEnv) -> (completionReason, sessionRef, error)
```

- **Claude** runner: `claude -p` headless, clean `CLAUDE_CONFIG_DIR`.
- **Codex** runner: `codex exec` (non-interactive), equivalent isolated config/home.
- Exact flags, cap enforcement (native vs. wall-clock kill), and session-capture hooks are
  pinned during implementation; the interface is the contract, the launchers are swappable.

## 8. Run → PR identity convention (load-bearing — get it right now)

This is the integration contract everything downstream reads, and the expensive thing to
migrate later. Define it once:

- **Branch:** `eval/{fixture_id}/{case_id}/{agent}/{run_id}` — the `eval/` prefix is the single
  namespace for both PR isolation *and* ETL exclusion (§9).
- **PR body provenance block** (machine-parseable, e.g. fenced YAML/JSON), carrying:
  `run_id, fixture_id, fixture_start_sha, case_id, agent, trial, compiled_ref_digest,
  prompt, limits, completion_reason, started_at, completed_at`.
- Result: provenance is free and human-inspectable; `git checkout` of the branch reconstructs
  the exact agent state months later; GitHub *is* the v1 results store (no eval_runs /
  eval_scores tables yet). It also pre-defines the exact interface a future `auto eval score`
  will consume — laying the rail without building the train.

## 9. ETL exclusion (the open problem from CLAUDE.md)

Eval runs must **not** pollute the real session corpus that auto-reflect mines. The `eval/`
branch prefix is the hook: auto-etl (and any consumer) excludes sessions whose workspace
branch matches `eval/*` by default, with an opt-in flag to include them. One prefix solves PR
namespacing and ETL exclusion together.

## 10. Functional requirements

| ID | Requirement |
|----|-------------|
| FR-1 | `build` produces an inspectable, immutable git ref per case; `git checkout` shows the exact agent start state. |
| FR-2 | Setup failures report exit code + stderr + step index; assertion failures list **every** failed assertion. |
| FR-3 | Setup steps run `set -euo pipefail`, each in its own subshell; safety is the operator's responsibility (disposable worktree is the only sandbox). |
| FR-4 | `build` is idempotent: unchanged case rebuilds nothing; changed setup triggers recompile of that case. |
| FR-5 | Compile is hermetic (no network, no `$HOME` beyond git repo config). |
| FR-6 | Mutable refs (`main`/`HEAD`) in `startHash` or `git checkout` setup steps warn (error under `--strict`). |
| FR-7 | Every run maps unambiguously to one agent session (worktree path encodes `run_id`) and to one branch+PR. |
| FR-8 | Agent runs are isolated: clean per-agent config env; no operator-config leakage. |
| FR-9 | Pluggable runner; **Claude and Codex** both work in v1 via one interface. |
| FR-10 | Every run terminates: wall-clock / turn / token caps enforced; completion reason recorded. |
| FR-11 | Each run produces an `eval/...` branch + PR whose body carries the §8 provenance block. |
| FR-12 | `eval/*` branches/sessions are excluded from auto-etl ingestion by default. |

## 11. Acceptance criteria

v1 is done when:

1. An operator writes a one-fixture, one-case `eval.yaml` and runs `auto eval build` → a
   compiled `refs/eval-cache/...` ref appears; `git checkout` shows the expected transformed state.
2. Re-running `auto eval build` on an unchanged config rebuilds **zero** cases; editing the
   case's setup rebuilds exactly that case.
3. `auto eval run eval.yaml` with `agents: [claude, codex]` produces **two** PRs on `eval/...`
   branches, one per agent, both built from the identical compiled ref.
4. Each PR body contains the parseable provenance block (§8); checking out the branch
   reproduces the agent's resulting state.
5. A run that exceeds its caps terminates and records the completion reason rather than hanging.
6. Agent runs use an isolated config dir (verifiable: operator's personal config is untouched
   and absent from the run).
7. `eval/*` sessions do not appear in a default `auto search` / auto-etl listing.

## 12. Explicitly deferred (NOT in v1)

- **All automated scoring** — mechanical SQL evaluators, prompt judges, recall/precision
  metrics, `eval score`/`eval summary`. (PRD `requirements.md` owns this.)
- **A/B/N sweep & cross-product** — multiple fixtures/cases/trials in one invocation.
- **Statistical comparison, ablation, oracle design, variance reduction.** (rule-utility doc.)
- **`eval_runs` / `eval_scores` storage tables** — GitHub PRs are the v1 store.
- **Concept-based scoring, cost estimation/refusal, distributed execution, web dashboard.**

These plug into the §8 interface later; v1's job is to make that interface real and trustworthy.

## 13. Open questions

1. **Codex headless parity** — does `codex exec` expose comparable prompt/cap/isolation knobs
   to `claude -p`? Confirm during implementation; it may constrain the runner interface.
2. **Cap enforcement** — native (agent-reported) vs. external wall-clock kill, per agent.
   Which caps are honored natively vs. enforced by the harness?
3. **PR target branch** — do eval PRs target `main` (visible, noisy) or a parked `eval-base`
   branch (isolated, less reviewer-friendly)? Affects the diff a human reviews.
4. **Session capture timing** — does `run` trigger `auto etl run` itself, or rely on the
   daemon, given the run is on an excluded `eval/*` branch?
5. **No-change runs** — if the agent makes zero file changes (capped early, refused), do we
   still open an (empty) PR for the record, or just log the run?
