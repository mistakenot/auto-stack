---
hash: "10aa6dda"
id: "e297343a"
read_when: "deciding what to test next with the evals suite, judging whether auto graph improves agent outcomes, or designing new eval tasks and avoiding the methodology traps already hit"
summary: "Findings from the first Harbor-based eval campaign (Sept 2026): auto graph showed no measurable effect on Haiku 4.5 or Opus 5.5 across impact-analysis and real bug-fix tasks, because the tasks sit at the model's ceiling; also the auto graph bugs found, and the methodology lessons (circular answer keys, implementation-coupled tests, errored trials graded as zero, nondeterministic stats)."
title: "Eval Findings: auto graph on Haiku 4.5 and Opus 5.5 (Sept 2026)"
---

# Eval Findings: auto graph on Haiku 4.5 and Opus 5.5 (Sept 2026)

## Bottom line

`auto graph` did not measurably change agent outcomes on any task set we built,
on either Claude Haiku 4.5 or Claude Opus 5.5. Every verdict was inconclusive.
The cause is the tasks, not the harness: the control agent already solves them.
It writes its own import-graph scripts for impact questions, and finds the code
with `grep` for bug fixes. Showing an effect needs harder tasks (see "Next").

The suite itself is sound. Every task passes an oracle gate, the fix-task grader
scores the upstream fix 1.0 and a do-nothing agent 0.0 on every task, and every
run so far completed with no infrastructure errors.

## What was built

The `evals/` suite (see `evals/README.md`) runs on Harbor 0.23, with a thin
`evals` CLI on top that adds lint, an oracle gate, one-command experiment runs,
and paired statistics.

| Task family | Count | What it asks | Ground truth |
|---|---|---|---|
| `impact-pkg-dependents-*` | 9 | Every package that transitively depends on a target package | `go list`, non-test imports |
| `impact-file-dependents-*` | 9 | Every Go file whose imports lead to a target package | `go list` plus per-file parsing |
| `fix-issue-*` | 9 | Fix a real upstream bug from its issue text | The fix commit's own hidden tests |

Fixtures: logrus, go-git, auto-stack (`auto-search`), the GitHub CLI, Hugo,
Caddy and Cobra. Every task is generated from a spec in `evals/generators/` and
checked for drift with `evals generate --check`.

## Results

### 1. `auto-graph-on-impact`: Haiku 4.5, 12 impact tasks, 5 attempts per arm

| Metric | Control | auto-graph | Delta | Verdict |
|---|---|---|---|---|
| F1 (primary) | 0.952 | 0.981 | +0.029 | inconclusive, p = 0.21 |
| Agent time | 113 s | 95 s | -18 s | not significant |
| Output tokens | 10,652 | 8,612 | -19% | not significant after correction |

Treatment uptake was 60 of 60. The only signal came from file-level tasks, with
+0.15 and +0.20 F1 on two of them. Package-level tasks sat at the ceiling. The
run cost about $18 at API prices, all on subscription quota, and took 3.5 hours.
The `impact` dataset has since grown to 18 tasks, so a rerun would cover more.

### 2. `auto-graph-opus-on-impact-file`: Opus 5.5, 9 file-level tasks, 3 attempts per arm

Control scored F1 1.0 on all 27 trials, so there was nothing to improve.
`auto graph` was used in 21 of 27 treatment trials. Time, tokens and cost
trended slightly lower, not significantly. Verdict: inconclusive.

### 3. `auto-graph-context-opus-on-fix`: Opus 5.5, 9 fix tasks, 3 attempts per arm

| Arm | Resolved | Used `auto graph code context` | Input tokens |
|---|---|---|---|
| control | 27/27 | n/a | 131k |
| context offered | 26/27 | 0/27 | 145k |
| context required | 26/27 | 27/27 | 156k (+19%) |

When context packs are only offered, Opus never uses them. When they are
required, resolution does not change, and cost rises slightly (not
significantly). Verdict: inconclusive for both treatments.

## Findings about auto graph

- **It is file-level only.** It builds a file-to-file import graph for Go and
  TypeScript. There is no symbol, call or type information.
- **`_test.go` files are treated as importable** (bug `auto-f7j`). An import of
  package P links to every file in P, including tests, then follows the tests'
  own imports. This invents dependency chains Go cannot compile. On go-git it
  reported 27 dependents of `utils/trace` where `go list` finds 14.
- **Context packs inherit the same bug.** They list test and fuzz files as
  "direct runtime dependencies".
- **Context packs can exceed their token budget.** One returned 30,112 tokens
  against a 20,000 limit, because seed files are always included whole.

## Methodology lessons

These are the traps this campaign hit. The suite now guards against each one.

1. **An answer key must never come from the tool under test.** The first go-git
   key was `auto graph`'s own output, bug included. The treatment "won" 5 of 5
   by agreeing with itself, while control was marked wrong for reasoning the way
   Go does. Keys now come from `go list`.
2. **Hidden tests can be coupled to one implementation.** One gh CLI task's
   test only accepted upstream's choice of API field. All 9 trials fixed the
   issue correctly and still scored 0. That task was removed. Rule: a task that
   every arm fails while the oracle passes must be reviewed before it counts.
3. **Harbor grades errored trials as zero.** A rate-limited agent still gets a
   verifier result of 0. `evals compare` excludes any trial with an exception.
4. **Many metrics at 95% produce flukes.** The first report called a cost
   difference significant. It did not survive a Holm correction. Verdicts now
   use the pre-registered primary metric only.
5. **Two tasks do not generalize.** Intervals now resample tasks, and a
   generalized verdict needs at least 8 paired tasks.
6. **One trial per cell fakes certainty.** It collapses the bootstrap to a
   point. Intervals need at least 2 valid trials per task per arm.
7. **Set iteration order made the stats nondeterministic.** String hashing is
   randomized per process, so the same jobs gave different p-values. This is
   fixed, and a test runs compare under three hash seeds.
8. **Agents script around the question.** With `python3` in the image, both arms
   answer impact questions by writing an import parser. Removing `python3` would
   not help, since the agent would switch to `awk` or `perl`. The questions
   themselves have to be ones a script cannot answer.
9. **PR descriptions leak the fix.** Fix-task statements are the linked issue
   verbatim, or a symptom-only report written from the PR.

## Next

- **Harder fix tasks.** Look for multi-file changes, issues that don't name the
  code, and larger unfamiliar repositories. Commits with 50 or more changed lines
  across 3 or more packages are a starting filter for the fix generator.
- **Long-horizon feature tasks**, where gathering context is most of the work.
- **Fix `auto-f7j`** before evaluating context packs again, since the packs
  currently include files that are not real dependencies.
- **Symbol-level support in `auto graph`** would open task types, such as
  callers and interface implementers, that scripting agents cannot answer as
  easily. Build the tasks alongside the feature.
- **GPT-6 via Codex** needs OpenAI auth for Harbor's Codex adapter.
- **A results store** (a Harbor plugin writing one row per trial) so results
  survive worktree cleanup and accumulate across runs.

## Reproduce

```bash
cd evals && uv sync
export CLAUDE_CODE_OAUTH_TOKEN=<token from `claude setup-token`>
uv run evals lint --text && uv run evals oracle --text
uv run evals run auto-graph-context-opus-on-fix
uv run evals compare auto-graph-context-opus-on-fix --text
```

Job results are gitignored. The numbers above come from these local runs:
`auto-graph-on-impact__*__20260923T232959Z`,
`auto-graph-opus-on-impact-file__*__20260925T185139Z` and
`auto-graph-context-opus-on-fix__*__20260925T200149Z`.
