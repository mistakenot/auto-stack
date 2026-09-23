# evals

The auto-stack eval suite. [Harbor](https://docs.harborframework.com) is the
runner: it builds sandboxes, runs agents, collects trajectories and artifacts,
and grades. This directory holds the tasks, the experiments, and a thin `evals`
CLI that adds the few things Harbor does not: naming and hygiene lint, an oracle
gate, one-command experiment runs, and paired statistics across arms.

## Quickstart

```bash
cd evals
uv sync

# One-time: create a subscription token, then export it in each shell.
claude setup-token
export CLAUDE_CODE_OAUTH_TOKEN=<token>

uv run evals lint --text                          # names, task hygiene, one-factor arms
uv run evals oracle --text                        # every task must score 1.0
uv run evals run auto-graph-on-impact             # one Harbor job per arm
uv run evals compare auto-graph-on-impact --text  # paired effects with 95% intervals
uv run harbor view jobs                           # browse trajectories and rewards
```

For a cheap smoke run, add `--attempts 1 --task 'impact-file-*'` to `evals run`.

## Vocabulary

| Term | Meaning | Lives in |
|---|---|---|
| **Task** | One self-contained Harbor task: instruction, environment, verifier, oracle. Knows nothing about arms. | `tasks/<name>/` |
| **Dataset** | A named selection of tasks, expressed as a job-config layer that globs task names. | `datasets/<name>.yaml` |
| **Experiment** | One pre-registered question. It names a dataset, a primary metric, and its arms. | `experiments/<name>/experiment.toml` |
| **Arm** | One complete agent configuration within an experiment. Exactly one is `control`. | `experiments/<name>/<arm>.yaml` |
| **Job** | One Harbor run of one arm over its dataset. | `jobs/` (gitignored) |
| **Trial** | One attempt by one arm at one task. | inside a job |

Harbor's words are kept unchanged because its docs, CLI and result files use them.
In repo-wide prose, say **eval task** to avoid confusion with a watch TaskDef.

## Naming conventions

Names are for humans and globs. Metadata is for machines. `evals lint` checks
that the two agree, so a name can never drift from what it describes.
`conventions.toml` holds the machine-readable rules.

| Thing | Pattern | Example |
|---|---|---|
| Task | `<area>-<objective>-<fixture>` | `impact-pkg-dependents-gogit` |
| Harbor task name | `auto-stack/<task>` | `auto-stack/impact-pkg-dependents-gogit` |
| Dataset | `<purpose>` | `impact` |
| Experiment | `<subject>-on-<dataset>` | `auto-graph-on-impact` |
| Arm | `control`, or the one factor it changes | `auto-graph`, `model-sonnet-5` |
| Job | `<experiment>__<arm>__<utc>` | `auto-graph-on-impact__control__20260923T181057Z` |
| Oracle job | `oracle__<dataset or all>__<utc>` | `oracle__all__20260923T181008Z` |
| Reward keys | `reward` is primary; other dimensions are snake_case | `reward`, `precision`, `recall` |

All names are lowercase kebab-case. The rules below exist for the day there are
hundreds of tasks and dozens of experiments.

1. **A task is named for what it asks, never for the tool being tested.** The same
   task serves every experiment. A task called `auto-graph-...` could only ever
   measure one thing.
2. **The area comes first and is a closed list.** `impact-*` is then a safe glob,
   so a new task joins its datasets automatically. Adding an area means adding
   one line to `conventions.toml`.
3. **The fixture comes last.** Related tasks sort together, and one objective can
   run over many repositories.
4. **No versions or dates in task names.** A change that alters what a task
   measures bumps `[task].version`. Harbor records a content checksum per trial,
   and `evals compare` refuses to pair trials of a task that changed between arms.
5. **An experiment names its subject and its dataset.** The directory listing then
   answers "what have we tested, and on what".
6. **A treatment arm is named for its one factor.** It must declare the config
   paths it changes under `changes`. Lint resolves both arms through Harbor and
   fails if the real difference is anything else.
7. **Job names are machine-parseable.** `evals compare` finds the latest job per
   arm from the name alone. The double underscore matches Harbor's own separator
   for trial directories.

## Rules every task follows

These come from Harbor's documented practice. Lint enforces the checkable ones.

- **Separate verifier.** It runs in its own no-network image with grading
  dependencies baked in. Grading never trusts the agent's container, and
  `harbor job regrade` can rescore old trials when a verifier changes.
- **Declared artifacts.** Every file the verifier needs is listed in `artifacts`,
  including the ATIF trajectory, so later criteria can grade process as well.
- **A tool-agnostic oracle.** `solution/` writes a known-correct answer.
  `evals oracle` must score 1.0 before any agent runs.
- **The instruction is identical for every arm.** Arm-specific prompting goes in
  the arm's `extra_instructions`, never in `instruction.md`.
- **Pinned everything.** Base images use digests, fixtures use commit SHAs, and
  Harbor and RewardKit use exact versions.
- **Canary GUID** from `conventions.toml` in `instruction.md` and `task.toml`.
- **RewardKit verifiers.** Each reward dimension is a directory under `tests/`,
  and `reward.toml` defines the primary `reward`.

## Rules every experiment follows

- **Pre-register** the question, hypothesis, primary metric and each arm's
  `changes` in `experiment.toml` before running.
- **One factor per treatment**, enforced by lint.
- **Declare `uptake`** for treatments that offer a tool. `evals compare` reports
  how many trials actually used it, because an unused treatment measures nothing.
- **Errored trials are not failures.** Harbor still grades a rate-limited trial
  as 0. `evals compare` excludes any trial with an exception and reports errors
  by type.

## Adding things

**A task:**

```bash
uv run evals new-task --area impact --objective callers --fixture cobra --description "..."
# fill instruction.md, environment/, tests/, solution/, and the remaining [metadata]
uv run evals lint --text && uv run evals oracle --text
```

Copy `tests/` from an existing task with a similar grading shape. For example,
`set_match.py` scores any set-valued answer.

**An experiment:** create `experiments/<subject>-on-<dataset>/` holding an
`experiment.toml` and one `<arm>.yaml` per arm. Start from
`auto-graph-on-impact`. Each arm file is complete, because Harbor appends the
`agents` list across layered configs. Keep shared runtime settings in
`defaults.yaml`.

## Layout

```
evals/
  conventions.toml        naming rules and area vocabulary (read by lint)
  defaults.yaml           runtime-only job settings, layered first
  datasets/<name>.yaml    task selections by glob
  experiments/<name>/     experiment.toml plus one <arm>.yaml per arm
  tasks/<name>/           Harbor tasks
  templates/              metadata template for `harbor task init`
  src/auto_evals/         the evals CLI
  tests/                  offline tests for the CLI
  jobs/  .build/          run output and built binaries (gitignored)
```

## Known limits

- **Arms run one after another.** API latency and rate limits drift over time, so
  wall-clock deltas carry some time-of-day noise. Compare quality metrics first.
- **Two tasks give weak cross-task intervals.** The bootstrap resamples trials
  within tasks. Its interval says the result is stable, not that it generalises.
  More `impact-*` tasks fix this.
- **Ground truth has no derivation script yet.** Each task README records how its
  truth was computed.
- **Shared criteria are copied into each task** to keep tasks self-contained.
  Once several tasks share one, publish a versioned verifier base image instead.
- **Datasets are local globs.** Harbor Hub `dataset.toml` manifests pin tasks by
  digest but resolve only through a registry. Adopt them when publishing.
