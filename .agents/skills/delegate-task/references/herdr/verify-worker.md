# Verifying a worker is fit to receive work (herdr)

Before dispatching to a pane — and when auditing a fleet — confirm three things
that no status field tells you: that the pane is running the **agent** you think
it is, that it was launched with **bypass-permissions** flags (not merely a mode that runs unattended), and that it
is **where** you think it is.

## The mechanism: `pane process-info`

```bash
herdr pane process-info --pane <pane_id>
```

Returns the live foreground process, including its **exact argv**:

```json
{"result":{"process_info":{"foreground_processes":[
  {"argv":["claude","--dangerously-skip-permissions"],
   "cmdline":"claude --dangerously-skip-permissions",
   "cwd":"/home/vscode/.herdr/worktrees/skills/task-042",
   "name":"claude","pid":922176}],
  "pane_id":"w4:p2","shell_pid":921787}}}
```

This is ground truth. Prefer it over reading the agent's status line, which is
cosmetic, truncates in narrow panes, and differs per agent version.

## What to check

```bash
herdr pane process-info --pane "$PANE" \
  | jq -r '.result.process_info.foreground_processes[].cmdline'
```

| Observed `cmdline` | Verdict |
| ------------------ | ------- |
| `/bin/bash`, `zsh`, … | **Not an agent.** A plain shell. Never dispatch here — spawn an agent into it first. |
| `claude` (no flags) | **Manual mode. Reject.** It will stall on the first tool call, and `shift+tab` cannot rescue it. Reap and respawn. |
| `claude --dangerously-skip-permissions` | Good — bypass permissions. |
| `claude --permission-mode bypassPermissions` | Good — equivalent. |
| `claude --permission-mode auto` | **Auto mode. Reject.** It runs, but its classifier blocks `gh pr merge` ("Merge Without Review"), so the task stalls at the end. This is what a delegating session falls back to when *its* classifier denied the bypass launch — see `references/herdr/spawn-worker.md`. Reap and respawn. |
| `codex` (no flags) | **Approval-gated. Reject.** Reap and respawn. |
| `codex --dangerously-bypass-approvals-and-sandbox` | Good — bypass/YOLO mode. |
| `claude --resume <uuid>` (no flag) | **Downgraded by a server restart. Reject** — see below. |

Codex is the case that makes argv non-negotiable: its `permissions: YOLO mode`
banner is printed only by newer builds (verified live — v0.143.0 shows it,
v0.115.0 omits it while running in the same mode), so a status-line check
reports a correctly-launched worker as unverifiable.

**Codex reports two processes** — the Node launcher and the native binary:

```
node /home/vscode/.local/bin/codex --dangerously-bypass-approvals-and-sandbox
.../codex-linux-x64/.../bin/codex --dangerously-bypass-approvals-and-sandbox
```

Both carry the flags, so matching against *any* foreground process is
sufficient. Do not key on the process **name** alone: Codex's launcher is
`node`, so a `name == "codex"` test can miss it. Trust `--kind`/`agent` from
`agent get` for identity, and `cmdline` for flags.

A worker launched with its kickoff prompt as argv (see
`references/herdr/spawn-worker.md` Pattern B) carries the whole prompt in
`cmdline` too — match on the flag substring, not on the full string.

## A server restart silently strips the flags

herdr's `[session] resume_agents_on_restore` defaults to **true**: after the
server restarts (an upgrade, a crash, a reboot) it relaunches each agent into
its native conversation session. Verified live on 0.8.2: the relaunch command is
`claude --resume <session-uuid>` — **the original launch argv, including the
permission flag, is not carried over.** Every worker in the fleet came back
without bypass mode; a `claude` that had been `--dangerously-skip-permissions`
was resumed in `auto mode`.

This is the main reason a fleet drifts into approval-gated modes without anyone
having launched a worker wrongly, so **re-run this check after any server
restart**, not only at spawn time. `herdr status` reporting
`restart_needed: no` does not mean a restart has not already happened.

The remedy differs from an ordinary failed check, because the conversation is
worth keeping. Relaunch into the *same* session with the flag restored, rather
than reaping:

```bash
herdr pane process-info --pane "$PANE" \
  | jq -r '.result.process_info.foreground_processes[0].cmdline'   # grab the --resume uuid
# then, in that pane, exit the agent and relaunch:
claude --dangerously-skip-permissions --resume <uuid>
```

Only fall back to reap-and-respawn if the session id cannot be recovered.

## Confirming placement

The same call returns `cwd`. For a task worker it must be the **worktree**, not
the primary checkout:

```bash
herdr pane process-info --pane "$PANE" \
  | jq -r '.result.process_info.foreground_processes[0].cwd'
git -C "<that cwd>" rev-parse --git-dir --git-common-dir
```

If the two git paths differ, it is a linked worktree ⇒ a legitimate **worker**.
If they are equal it is the primary checkout ⇒ a **planner** pane, and a task
must not be executed there. See
[references/worker-pools.md](references/worker-pools.md).

## When a worker fails verification

Do not attempt repair. Permission mode is fixed at launch and context cannot be
cleared in place — reap the worker (`references/herdr/reap-worker.md`) and spawn
a replacement (`references/herdr/spawn-worker.md`). Report which pane was
rejected and why.
