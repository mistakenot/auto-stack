# Spawning a worker (herdr)

> **The delegate skills ship this procedure as a script.** `scripts/herdr-worker.sh
> worker|planner` (bundled with `delegate`, `delegate-task` and `status-report`)
> performs everything below — pane, flags, verification, kickoff — and prints one
> JSON object. Prefer it; this reference is the manual procedure behind it, and
> the background for the failure paths the script reports but does not act on.

Create a new coding-agent worker. herdr never picks the agent's flags for you:
**you always supply the launch arguments**, and getting them wrong is the single
most common delegation failure — see the permission section below before
anything else.

Requires **herdr 0.8.2 or newer** (`herdr --version`). Earlier versions lack
`agent start --kind`, `agent prompt`, and blocked-startup detection, and the
recipes here will not work on them.

## Permission flags are mandatory — never launch an agent bare

A background worker has nobody at the keyboard. Launched without an explicit
permission flag it comes up in its **interactive, ask-a-human-first** mode and
stalls on the first tool call, looking "idle" while doing nothing.

| Agent | Launch argv | Resulting mode |
| ----- | ----------- | -------------- |
| **Claude Code** | `claude --dangerously-skip-permissions` | `⏵⏵ bypass permissions on` |
| **Codex** | `codex --dangerously-bypass-approvals-and-sandbox` | `permissions: YOLO mode` |

Verified live: bare `claude` comes up in **`⏸ manual mode on`**, and
`shift+tab` **cannot** cycle out of manual into bypass — bypass is only
reachable via the launch flag. A pane that was launched bare is unrecoverable
for unattended work: **reap it and spawn a new one** rather than trying to
repair it.

`claude --permission-mode bypassPermissions` reaches the same mode and needs no
extra opt-in flag, so either form works; prefer
`--dangerously-skip-permissions` for consistency with Codex's single-flag form.

## If the launch itself is denied by the auto-mode classifier

Since Claude Code 2.1.2xx the delegating session usually starts in **auto
mode**, and auto mode's classifier blocks the launch above with
`Denied by auto mode classifier ∙ [Create Unsafe Agents]` — the
`--dangerously-skip-permissions` flag is exactly what that rule looks for.
Verified live on 2.1.270 with `/delegate`.

**Do not respond by dropping the flag or swapping in `--permission-mode auto`.**
An auto-mode worker looks healthy but cannot `gh pr merge` (its own classifier
blocks "Merge Without Review"), so the task stalls at the very end. Stop and
tell the user instead. The fix is one entry in **`~/.claude/settings.json`**
(user scope — the classifier ignores `autoMode` in project settings):

```json
{
  "autoMode": {
    "allow": [
      "$defaults",
      "Delegated worker launches: on this machine the user runs background coding-agent workers through herdr. Running `herdr agent start <name> --kind claude ... -- --dangerously-skip-permissions` or `herdr agent start <name> --kind codex ... -- --dangerously-bypass-approvals-and-sandbox` (usually into a git worktree created by `herdr worktree create`), directly or via the delegate skills' `scripts/herdr-worker.sh worker|planner` wrapper, is the approved, user-sanctioned way to dispatch work to a worker. Treat these launches as allowed; they are not Create Unsafe Agents or Safety Bypass Flag."
    ]
  }
}
```

Confirm with `claude auto-mode config`, then retry the spawn. A
`permissions.allow` rule such as `Bash(herdr agent start:*)` is **not** enough:
it only matches when the launch is the whole command, and the spawn is normally
part of a compound command. A parent started with
`--dangerously-skip-permissions` has no classifier and never hits this.

Always confirm the flags actually took effect after spawning — see
`references/herdr/verify-worker.md`. Re-confirm after any herdr **server
restart** too: `resume_agents_on_restore` (default true) relaunches agents as
`claude --resume <uuid>` and **does not carry the permission flag over**, so a
restart silently downgrades the whole fleet.

## Pattern A — worktree worker (recommended for task execution)

One call makes the git worktree **and** a dedicated workspace + tab + pane;
`agent start` then launches the agent into that pane.

```bash
J=$(herdr worktree create --cwd <repo> --branch task/NNN --base main \
      --label task-NNN --no-focus)
PANE=$(echo "$J" | jq -r .result.root_pane.pane_id)

herdr agent start task-NNN --kind claude --pane "$PANE" --timeout 90000 \
  -- --dangerously-skip-permissions
```

The agent lives in the worktree's **own** workspace with `cwd` = worktree — one
worktree ↔ one workspace ↔ one agent.

### Sidebar placement (verified live, herdr 0.9.0)

| Call | Sidebar result |
| ---- | -------------- |
| `worktree create --cwd <primary checkout or any subdir>` | Space **nested** under the repo's Space; opens that parent Space first if it is not open |
| `worktree create --workspace <repo's primary Space>` | nested, same as above |
| `worktree create --cwd <linked worktree>` / `--workspace <child Space>` | **fails** `linked_worktree_source` — creation must start from the primary |
| `worktree open --path <existing linked worktree>` | nested |
| `workspace create --cwd <repo>` | an unrelated **top-level** Space (no worktree provenance) |
| `tab create --workspace <repo's Space>` | a tab inside the repo's Space (the planner shape) |

`herdr worktree list --cwd <any path in the repo>` returns
`.result.source.source_checkout_path` (the primary) and, when it is open,
`.source_workspace_id` — even when called from a linked worktree. The script
resolves this before every launch, which is what keeps placement consistent.

`worktree create` accepts `[--workspace ID | --cwd PATH] [--branch NAME]
[--base REF] [--path PATH] [--label TEXT] [--focus|--no-focus]` and returns
`root_pane`, `tab`, `workspace`, and `worktree` in its JSON. The pane it creates
is a **plain shell** — there is no `--agent`/`--run` flag on `worktree create`,
so launching the agent is always a deliberate second step.

## Pattern B — launch with the kickoff prompt as argv (short tasks only)

Both CLIs take an optional initial prompt as a positional argument, so a worker
can be created *and* dispatched in a single `agent start`:

```bash
herdr agent start note-fix --kind claude --pane "$PANE" --timeout 90000 \
  -- --dangerously-skip-permissions "Fix the typo in README.md"
```

**`agent start` blocks until the dispatched work finishes.** It returns when the
agent is ready for *interactive* input, and an agent chewing through an argv
prompt is not — verified live: a worker given a 50-second task returned from
`agent start` after 61s, not immediately.

That makes Pattern B **wrong for any long-running dispatch**. It blocks the
caller for the whole task (defeating the point of delegating), and if the task
outlives `--timeout` you get:

```json
{"error":{"code":"timeout","message":"timed out waiting for agent startup"}}
```

**This error does not mean the worker failed.** Verified live: the agent stays
alive, keeps its permission flags, keeps working, and completes normally — but
**its name is never bound** (`agent get <name>` returns `agent_not_found`,
`name` is `null`), so the handle you intended is gone. Do **not** treat the
timeout as a spawn failure and respawn: that leaves two workers on one task.
Recover instead:

```bash
herdr pane process-info --pane "$PANE"    # confirm the agent is alive and flagged
herdr agent get "$PANE"                   # address by pane id; status will be `working`
herdr agent rename "$PANE" <name>         # rebind the handle
```

Use Pattern B only when the prompt is short enough to finish inside the timeout
and you are content to block. For anything longer — `/execute-task`, a real
feature, anything you are delegating precisely so you can walk away — use
**Pattern A**, then dispatch with `agent prompt` **without** `--wait` and
confirm the transition to `working` (`references/herdr/send-prompt.md`,
`references/herdr/wait-for-ready.md`). That returns immediately and keeps the
name bound.

## `agent start` semantics

```
herdr agent start <name> --kind KIND --pane ID [--timeout MS] [-- <agent-args...>]
```

- **`--pane` is required and must already exist.** `agent start` never creates,
  splits, or moves layout — get a pane from `worktree create`,
  `workspace create`, `tab create`, or `pane split` first.
- The target pane must be an **available shell** — at its interactive prompt,
  no foreground command, editor, or agent running.
- `--kind` picks the integration used for status reporting. Valid kinds include
  `claude`, `codex`, `opencode`, `gemini`, `copilot`, `droid`, `amp`, `grok`;
  run `herdr agent` for the installed list.
- Everything after `--` is passed through to the agent CLI verbatim. This is
  where the permission flags go.
- `<name>` must match `[a-z][a-z0-9_-]{0,31}` and be unique among live agents.
  It becomes the agent's target handle for every later command.
- **It blocks until the agent is genuinely ready** and returns
  `agent_status` plus `interactive_ready: true`. There is no registration race
  to poll around — a successful return means you can prompt immediately.
  Startup defaults to a 30s timeout; raise it with `--timeout` on slow machines.
  Note that "ready" means ready for *interactive* input, so if you passed an
  argv prompt this call blocks until that prompt's work is done — see Pattern B.

### Handling `agent_not_ready`

If the agent hits a startup interstitial, `agent start` returns immediately with

```json
{"error":{"code":"agent_not_ready","message":"agent <name> is blocked during startup and is not ready for prompts"}}
```

**This is not a failure to retry blindly.** The name stays bound to the pane, so:

1. Read the pane (`references/herdr/read-output.md`) to see *which* interstitial.
2. Answer it with `herdr agent send-keys <name> <key>` — one press at a time,
   re-reading the pane after each to confirm it landed — and pick the option
   deliberately, never a bare `enter` (see
   [references/agent-conventions.md](references/agent-conventions.md); Codex's
   update prompt defaults to "Update now", which drops the pane to a shell).
   If a re-read shows nothing moved, `references/herdr/unblock-worker.md` has
   the fallback path.
3. Wait for it to settle — `references/herdr/wait-for-ready.md`.

Interstitials seen live: Claude Code's *"Is this a project you trust?"* folder
prompt, Codex's *"Do you trust the contents of this directory?"* prompt, Codex's
*"Update available"* prompt, and Codex's *"Hooks need review"* prompt (triggered
by herdr's own agent-state hook on first run). Each is answered once and
remembered, so they mostly bite on a machine's first worker.

Claude Code does **not** re-prompt for trust in a git worktree of a repo it has
already trusted, so Pattern A rarely trips its folder dialog.

## Lower-level building blocks

```bash
herdr workspace create [--cwd PATH] [--label TEXT] [--env KEY=VALUE] [--no-focus]
herdr tab create [--workspace ID] [--cwd PATH] [--label TEXT] [--no-focus]
herdr pane split [<pane_id>|--current] --direction right|down [--cwd PATH] [--no-focus]
```

Read the new pane from `.result.root_pane.pane_id` (`workspace create`,
`tab create`) or `.result.pane.pane_id` (`pane split`). Use these only when
Pattern A does not fit — e.g. a worker that must not have its own worktree.

Split a wide pane `right` and a narrow or tall pane `down`, and avoid repeated
same-direction splits: panes narrower than ~80 columns wrap agent output badly
and make `read-output` hard to parse. Prefer a fresh tab or workspace per worker
over stacking many panes into one tab.

## Never run the herdr server from inside an agent session

A herdr server started from a shell **inside** a coding-agent session leaks that
session's environment into every pane it later creates. Observed live: panes
inherited `CLAUDE_CODE_CHILD_SESSION`, which silently disabled transcript saving
and changed the launched agent's default permission mode. Start the server from
a plain terminal.
