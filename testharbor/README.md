# testharbor — does `auto graph` actually help?

A small **A/B (differential) Harbor eval** that measures whether giving an agent
`auto graph` helps it perform a real code-comprehension task, versus the stock
tools Claude Code ships with (read / grep / glob / bash) — across three axes:

- **a. Quality** — F1 of the answer vs a neutral ground truth
- **b. Time** — the agent's own wall-clock (`agent_execution`)
- **c. Tokens** — input / output / cost

All three come straight out of each trial's `result.json`; no custom
instrumentation.

## The task

A pinned checkout of `github.com/sirupsen/logrus` (`v1.9.3`) sits at
`/app/logrus`. The agent must list **every file that transitively imports the
root `logrus` package** and write it to `/app/answer.json`. This is a transitive
impact / blast-radius question: `auto graph` answers it directly, while the
baseline agent must reconstruct the import closure by hand (slow and easy to get
wrong), so a genuine tool should show up as deltas on all three axes.

### Why the ground truth is fair

The expected set (`shared/tests/expected_importers.json`, 17 files) was computed
**independently** with `go list` transitive import resolution — and it matches
`auto graph`'s reverse-reachable set exactly. So the score is not "did you match
the tool's own output"; it's "did you match Go's real import graph" (which also
happens to validate that `auto graph` is correct).

## The two arms

`render.sh` generates two Harbor task dirs from the single source in `shared/`:

| Arm | `auto` binary | Instruction |
| --- | --- | --- |
| `with-graph/` (treatment) | on `PATH` | mentions `auto graph` |
| `baseline/` (control) | **absent** | same task, no tool named |

Everything else — seed repo, question, ground truth, graded verifier — is
identical. The binary is physically absent in the baseline image, so it is a
leak-proof control. (Harbor's *task* image build doesn't take build-args, so the
arms are two rendered dirs rather than one parametrized Dockerfile.)

## Layout

```
testharbor/
├── render.sh                 # make dist -> stage binary -> generate both arms
├── compare.py                # read two Harbor jobs -> quality/time/token delta table
├── shared/                   # single source of truth for both arms
│   ├── instruction.md        # __TOOL_HINT__ is substituted per arm
│   ├── task.toml.tmpl        # __NAME__ is substituted per arm
│   ├── env-with/Dockerfile   # ubuntu + git/jq/python3 + COPY auto + clone logrus
│   ├── env-baseline/Dockerfile   # same, minus the binary
│   ├── solution/solve.sh     # Oracle (treatment only): auto graph -> answer.json
│   └── tests/
│       ├── test.sh           # runs score.py
│       ├── score.py          # reward = F1 vs the fixture (pure stdlib)
│       └── expected_importers.json   # 17-file neutral ground truth
├── with-graph/  (generated, gitignored)
└── baseline/    (generated, gitignored)
```

## Running

The agent is Harbor's first-class **`claude-code`** adapter — the real Claude
Code CLI, which the adapter auto-installs into the container at agent-setup
(needs the `public` network both arms already have) and runs non-interactively
(`--print --permission-mode=bypassPermissions`, `IS_SANDBOX=1`). The stock
Claude Code toolset (Read/Grep/Glob/Bash/Edit) *is* the baseline; the treatment
arm only adds `auto` on `PATH`. Token/time/cost flow into each trial's
`result.json`, so `compare.py` works as-is.

### Auth (pick one, set before running)

```bash
# A) Anthropic API billing
export ANTHROPIC_API_KEY=sk-ant-...

# B) Claude subscription (Max/Pro) via a setup token
export CLAUDE_CODE_OAUTH_TOKEN="$(claude setup-token)"   # run once, paste value
export CLAUDE_FORCE_OAUTH=1
```

(Or pass either per-run with `--ae ANTHROPIC_API_KEY=...` instead of exporting.)

### Commands

```bash
./render.sh                                        # (re)generate both arms

# 1. sanity-check the treatment arm + verifier (reward/F1 should be 1.0)
harbor run -p testharbor/with-graph -a oracle

# 2. the real comparison — SAME agent+knobs, both arms, several ATTEMPTS each.
#    -k N = N repeated attempts (the repetition knob); -n = concurrency only.
#    Omit -m to use the subscription's default model (identical across arms).
harbor run -p testharbor/with-graph -a claude-code --ak max_turns=40 -k 5 -n 5
harbor run -p testharbor/baseline   -a claude-code --ak max_turns=40 -k 5 -n 5

# 3. print the delta table (means across attempts)
python3 testharbor/compare.py jobs/<with-graph-job> jobs/<baseline-job>
```

Hold everything constant across the two arms (same `-m`/`--ak`/`-k`) so the only
variable is `auto` on `PATH`. Use **`-k` > 1** (attempts) — time and tokens are
noisy, so compare the means; `-n` only sets how many run concurrently. The Oracle
in step 1 has no LLM, so its token/time columns stay empty; the real signal comes
from steps 2–3.
