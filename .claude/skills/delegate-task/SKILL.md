---
name: delegate-task
description: "Dispatches /execute-task for a planned task to a fresh background worker (herdr), freeing this session. Use when 'delegate task', 'send to executor', 'hand off this task'."
---

# Delegate Task

Dispatch `/execute-task` for a planned task to a fresh background worker.

> The launch is a bundled script, not a procedure: `scripts/herdr-worker.sh`
> (next to this file). It creates the worktree, starts the agent with the right
> permission flags, verifies the process table, sends `/execute-task` and
> confirms kickoff, and prints one JSON object. **Do not hand-roll
> `herdr agent start`**, and never launch a task worker in the primary
> checkout or with a weaker permission mode — both end in a worker that cannot
> merge its PR.

> Part of the task planning workflow. See
> [references/workflow-overview.md](references/workflow-overview.md) for the
> full pipeline.
>
> **Ad-hoc work without task docs?** Use
> [/delegate](../delegate/SKILL.md) instead.
>
> **Pool model.** Task workers always run in a **worktree** on `task/$ID`,
> distinct from planners which commit to `main`. See
> [references/worker-pools.md](references/worker-pools.md). For per-agent CLI
> behaviour, see
> [references/agent-conventions.md](references/agent-conventions.md).

## Prerequisites

herdr **0.8.2+** with the integration installed for the target agent
(`herdr integration status`). The script checks both and refuses with
`herdr_too_old` / `integration_missing` rather than falling back.

## Input

Task ID (numeric), and optionally the agent to target — **Claude Code**
(default) or **Codex**.

## Dispatch workflow

### Step 1: Advance status to executing

Mark the task executing **before** creating the worker, so the worker's fresh
worktree (branched from `origin/main`) sees the right stage. Per
[references/task-status.md](references/task-status.md), set `status="executing"`
on `<pd-doc>` in `plan.html`, then:

```bash
git commit -am "docs($ID): status executing" && git push origin main
```

### Step 2: Confirm the task docs are on `origin/main`

The worker branches from `origin/main` and reads the docs from there.

```bash
git log origin/main --oneline -5 -- tasks/$ID/
```

If the docs are not on `origin/main`, push them first or stop and tell the user.
Step 1's push normally satisfies this.

### Step 3: Launch the worker

```bash
bash <skill-dir>/scripts/herdr-worker.sh worker \
  --slug $ID --branch task/$ID --repo <repo> \
  --prompt "/execute-task $ID" [--agent codex]
```

This names the agent and its tab `work-$ID`, creates the worktree off
`origin/main` in a Space nested under the repo's Space (from the primary
checkout, whatever `--repo` path you pass), launches with the mandatory flags, verifies argv and cwd, sends
the prompt, and returns in a few seconds once `working` is observed. It
**refuses** (`worktree_exists`) if a worktree already exists on `task/$ID` —
two workers on one task means two PRs and a merge conflict — so there is no
separate "check for an existing worker" step; if you want the survey anyway,
`bash <skill-dir>/scripts/herdr-worker.sh list --repo <repo>`
(`references/herdr/list-workers.md`).

Success looks like:

```json
{"ok":true,"role":"worker","name":"work-042","pane_id":"w12:p1","workspace_id":"w12",
 "worktree_path":"/…/.herdr/worktrees/<repo>/task-042","branch":"task/042","base":"origin/main",
 "cmdline":"claude --dangerously-skip-permissions",
 "verified":{"flags":true,"cwd":true,"role":true},
 "kickoff":{"ok":true,"agent_status":"working"},"follow":{"read":"…","status":"…","reap":"…"}}
```

### Step 4: Act on a failure

`ok:false` carries an `error` code and, where useful, a pane `snapshot`.

| `error` | Meaning | Do |
| ------- | ------- | -- |
| `worktree_exists`, `name_in_use` | a worker is (or was) already on this task | do **not** dispatch a second one; report what is there and stop |
| `agent_not_ready` | startup interstitial | read the snapshot, answer it deliberately per `references/herdr/unblock-worker.md` (one key per call, re-read after each; never a bare `enter` — `references/agent-conventions.md`), then `verify` and `prompt --prompt "/execute-task $ID"` |
| `kickoff_failed` with `agent_blocked` | the agent is at a dialog | as above, then `prompt` again (`references/herdr/send-prompt.md`) |
| `kickoff_failed` with `kickoff_not_observed` | `/execute-task` may not have landed | read the pane before retrying — never send it twice blind |
| `verify_failed`, `start_timeout` | wrong flags / cwd / no agent | `reap --name work-$ID` and launch again; a worker cannot be repaired in place (`references/herdr/verify-worker.md`, `references/herdr/reset-worker.md`) |
| `herdr_*`, `integration_missing`, `missing_dependency` | environment | stop and tell the user what to install |

If the launch command itself is **denied by the auto-mode classifier** in this
session, do not switch the worker to a weaker permission mode: the fix is a
settings entry, described in `references/herdr/spawn-worker.md`.

### Step 5: Report

State the worker's name (`work-$ID`), workspace/pane, branch, worktree path,
agent kind, that flags and worktree verified, and that `/execute-task $ID` was
accepted (the `kickoff` status).

Tell the user how to follow progress — the `follow.read` command
(`references/herdr/read-output.md`; `references/herdr/scan-output.md` for the
fleet; status semantics in `references/herdr/wait-for-ready.md`) — and that
[/status-report](../status-report/SKILL.md) covers the whole fleet.
If the worker later goes `blocked` (it stopped to ask a question),
`references/herdr/unblock-worker.md` is how to answer it — a prompt will be
refused, and the answer has to go in as keypresses. Display labels, should you
need to rename anything, are in `references/herdr/label-worker.md`.
