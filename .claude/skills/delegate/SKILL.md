---
name: delegate
description: "Hands a freeform prompt to a fresh background coding agent (herdr) — a worker in its own worktree, or a planner on main. Use when 'delegate this', 'send to an executor', 'run this in background', 'spin up a planner'. Not for planned tasks with docs (use delegate-task)."
---

# Delegate

Hand a freeform prompt to a background coding agent running under **herdr**,
without requiring task planning documents.

> The launch is a bundled script, not a procedure: `scripts/herdr-worker.sh`
> (next to this file). It creates the pane, starts the agent with the right
> permission flags, verifies the process table, sends the prompt and confirms
> kickoff, and prints one JSON object. **Do not hand-roll `herdr agent start`**
> — every past inconsistency (wrong flags, wrong checkout, unverified kickoff)
> came from re-deriving those steps by hand.

> For delegating **planned tasks** with requirements/solution/plan docs, use
> [/delegate-task](../delegate-task/SKILL.md) instead.
>
> Roles follow the pool model in
> [references/worker-pools.md](references/worker-pools.md); per-agent CLI
> behaviour is in [references/agent-conventions.md](references/agent-conventions.md).

## Prerequisites

herdr **0.8.2+** with the integration installed for the agent you target
(`herdr integration status`). The script checks both and refuses with
`herdr_too_old` / `integration_missing` rather than falling back.

## Input

- The **prompt** — sent as-is, no slash-command wrapper, no preamble.
- A **role**: `worker` or `planner` (choose below).
- A **slug** for the agent's name: lowercase, digits, `-`/`_`, ≤ 27 chars.
  Name it after the work (`fix-readme-links`), not the agent (`worker3`).
- Optionally `--agent claude` (default) or `--agent codex`, and `--repo` if the
  work is not on the current repo.

If no prompt is given, ask the user what they'd like to delegate.

## Choosing the role

| Role | Runs in | Named | Use for |
| ---- | ------- | ----- | ------- |
| **worker** | a fresh git worktree on its own branch (`task/<slug>` unless `--branch`); its own Space **nested under the repo's Space** in the sidebar | `work-<slug>` | anything that edits files: it stays isolated and reviewable, and opens a PR |
| **planner** | the primary checkout, which must be on `main`/`master`; a **new tab in the repo's Space** | `plan-<slug>` | analysis, research, writing planning docs that commit straight to main |

When in doubt, `worker`. A planner that starts editing code is working in the
primary checkout, which the pool model forbids.

Placement is fixed by the script, never chosen per launch: `--repo` may be any
path in the repo (even a linked worktree) and is resolved to the primary
checkout first. Never create the pane yourself with `workspace create`,
`tab create` or `pane split` — each gives a different sidebar layout.

## Dispatch workflow

### Step 1: Survey

```bash
bash <skill-dir>/scripts/herdr-worker.sh list --repo <repo>
```

Report anything already running on this repo before launching — if an agent is
mid-task on the same area, say so and let the user decide. For the fields and
the herdr hierarchy behind them see `references/herdr/list-workers.md`.

### Step 2: Shape the prompt

Send the user's prompt verbatim. Two additions are worth making:

- **Unattended questions.** A worker that asks a question mid-run goes
  `blocked`, and only keystrokes or a human clear it. If the user is happy for
  the agent to decide, append a line telling it to proceed on its recommended
  answer and record the question and choice in its output. Leave it out when
  the user wants to be consulted — then `references/herdr/unblock-worker.md` is
  how you answer.
- **Paths.** Leave repo-relative paths relative: they resolve inside the
  worker's worktree, which is what you want. Only absolutise paths outside
  the repo.

### Step 3: Launch

Always a fresh agent — never reuse one (`references/herdr/reset-worker.md`
explains why there is no in-place reset).

```bash
bash <skill-dir>/scripts/herdr-worker.sh worker  --slug <slug> --repo <repo> --prompt "<prompt>" [--agent codex]
bash <skill-dir>/scripts/herdr-worker.sh planner --slug <slug> --repo <repo> --prompt "<prompt>" [--agent codex]
```

The call returns in a few seconds, **after** kickoff is confirmed; it never
blocks for the work itself. Success looks like:

```json
{"ok":true,"role":"worker","name":"work-<slug>","pane_id":"w12:p1","workspace_id":"w12",
 "cwd":"/…/.herdr/worktrees/<repo>/task-<slug>","branch":"task/<slug>",
 "cmdline":"claude --dangerously-skip-permissions",
 "verified":{"flags":true,"cwd":true,"role":true},
 "kickoff":{"ok":true,"agent_status":"working"},"follow":{"read":"…","status":"…","reap":"…"}}
```

### Step 4: Act on a failure

`ok:false` carries an `error` code and, where useful, a pane `snapshot`.

| `error` | Meaning | Do |
| ------- | ------- | -- |
| `worktree_exists`, `name_in_use` | something is already on that branch/name | do **not** launch a second one; report what is there |
| `not_on_main` | planner invariant violated | tell the user; do not launch a planner elsewhere |
| `agent_not_ready` | startup interstitial (trust prompt, update prompt) | read the snapshot, answer it deliberately per `references/herdr/unblock-worker.md` and `references/agent-conventions.md`, then `verify` and `prompt` |
| `kickoff_failed` with `agent_blocked` | the agent is at a dialog | same as above, then `prompt` again (`references/herdr/send-prompt.md`) |
| `kickoff_failed` with `kickoff_not_observed` | the prompt may not have landed | read the pane before retrying `prompt` — never send it twice blind |
| `verify_failed`, `start_timeout` | wrong flags / cwd / no agent | `reap` and launch again; permission mode cannot be repaired in place (`references/herdr/verify-worker.md`) |
| `herdr_*`, `integration_missing`, `missing_dependency` | environment | stop and tell the user what to install |

If the launch command itself is **denied by the auto-mode classifier** in this
session, do not switch the agent to a weaker permission mode: the fix is a
settings entry, described in `references/herdr/spawn-worker.md`.

### Step 5: Report

State the agent's **name**, role, pane/workspace, branch or cwd, agent kind,
that flags and checkout verified, and what it is doing now (the `kickoff`
status). Give the user the `follow.read` command for progress
(`references/herdr/read-output.md`; `references/herdr/scan-output.md` for the
fleet), and note [/status-report](../status-report/SKILL.md).

## Afterwards

```bash
bash <skill-dir>/scripts/herdr-worker.sh prompt --name work-<slug> --prompt "<follow-up>"
bash <skill-dir>/scripts/herdr-worker.sh verify --name work-<slug>
bash <skill-dir>/scripts/herdr-worker.sh reap   --name work-<slug> [--force]
```

`reap` removes a worker's worktree **before** closing its workspace and refuses
if there is dirty or unpushed work unless `--force`; the branch is left for you
to delete once merged (`references/herdr/reap-worker.md`). A planner reap
closes just its tab, leaving the repo's Space open. Status semantics (`done` is the normal resting
state; `idle` never appears for a CLI-driven agent) are in
`references/herdr/wait-for-ready.md`; display labels in
`references/herdr/label-worker.md`.
