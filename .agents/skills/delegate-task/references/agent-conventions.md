# Per-agent CLI conventions

How Claude Code and Codex behave **in themselves** — permission modes, process
identity, startup interstitials, and reset semantics. This holds regardless of
what is driving the pane. The herdr commands that drive them live in
`references/herdr/`.

For headless (no-pane) delegation — running a CLI in print mode from bash and
exiting, which is also how Grok is driven — see
[references/headless-delegation.md](references/headless-delegation.md).

The launch flags below are pinned in `src/planning-workflow/tests/agent-cli-contract.toml`
and checked against the installed CLIs by `make test`. If a worker stalls on a
permission prompt or a CLI rejects a flag, run that first — the CLIs change
weekly and the test names exactly which flag moved.

## Permission modes — the thing that most often goes wrong

Every one of these CLIs defaults to an **interactive, ask-a-human** mode. A
background worker has no human, so it stalls on its first tool call while
reporting a perfectly healthy status.

| Agent | Launch argv for unattended work | Status line shows |
| ----- | ------------------------------- | ----------------- |
| **Claude Code** | `claude --dangerously-skip-permissions` | `⏵⏵ bypass permissions on` |
| **Codex** | `codex --dangerously-bypass-approvals-and-sandbox` | `permissions: YOLO mode` — **only on newer builds**; see below |
| **OpenCode** | consult its own CLI; not verified here | — |

Verified live on Claude Code 2.1.251: a bare `claude` starts in
**`⏸ manual mode on`**, and `shift+tab` cycles only between manual, accept-edits
and plan — it **cannot** reach bypass, because bypass is gated on the launch
flag. A mis-launched worker is therefore unrecoverable and must be relaunched,
not repaired.

`claude --permission-mode bypassPermissions` reaches the same state without the
`--dangerously-` spelling and needs no extra opt-in flag; either works.

These flags disable the agent's own guard rails, which is the point for an
isolated worktree worker doing reviewable work behind a PR. Do not use them for
an agent operating directly on a primary checkout.

**The status line is not a reliable mode check.** Codex prints its
`permissions:` row only on newer builds — verified live, v0.143.0 shows it and
v0.115.0 omits the row entirely while running in exactly the same bypass mode.
Claude Code's mode text also truncates in narrow panes. Verify from the launched
process's argv instead, and do it after launch rather than trusting the command
line you *meant* to run — `references/herdr/verify-worker.md`.

Watch for **more than one build on PATH**: this machine has both an nvm-managed
`codex` and one in `~/.local/bin`, at different versions, and the pane's shell
PATH decides which `agent start --kind codex` actually launches.

## Process identity

| Agent | Foreground process |
| ----- | ------------------ |
| Claude Code | `claude` |
| Codex | `node` launcher **plus** a native `codex` binary — both appear, both carry the flags |
| OpenCode | `opencode` |

A pane whose live process is a plain shell (`bash`, `zsh`) is not a running
agent, whatever any status label claims — never dispatch there. Because Codex's
launcher is `node`, match on the **command line** rather than the process name.

## Startup interstitials

A freshly launched agent may sit at a blocking prompt instead of accepting
input. herdr 0.8.2 detects this and returns `agent_not_ready` from
`agent start` rather than letting you prompt into the dialog.

Seen live:

| Prompt | Agent | Default option | Correct answer |
| ------ | ----- | -------------- | -------------- |
| *"Is this a project you trust?"* | Claude Code | **No, exit** | `down`, `enter` |
| *"Do you trust the contents of this directory?"* | Codex | Yes, continue | `1`, `enter` |
| *"Update available… 1. Update now"* | Codex | **Update now** | `3`, `enter` (skip until next version) |
| *"Hooks need review"* | Codex | Review hooks | `2`, `enter` (trust all) |

**Never answer an interstitial with a bare `enter`.** The defaults are not
uniformly safe: Codex's update prompt defaults to *Update now*, which runs
`npm install` and drops the pane back to a shell, swallowing whatever you sent.
Read the pane, then send the specific key.

Send one key per call and **re-read the pane after a short pause** before
the next: `{"type":"ok"}` only means herdr accepted the key names, and a read
fired in the same instant can still show the previous frame. The Claude Code
row was verified live (herdr 0.8.2, Claude Code 2.1.268) with both
`agent send-keys <name>` + `enter` and `pane send-keys <pane_id>` + `return`.
If a re-read shows the selection unmoved, `references/herdr/unblock-worker.md`
has the fallback path.

The same mechanism clears a mid-task `blocked` state (a Claude Code
`AskUserQuestion` picker): `agent prompt` is refused with `agent_blocked`, and
only keystrokes or a human at `agent attach` can answer it. Do not send `esc` —
the picker treats it as *cancel* and discards the question.

Each of these is remembered once answered, so they mostly bite on a machine's
first worker. Claude Code does **not** re-prompt for trust inside a git worktree
of a repo it has already trusted, so the worktree dispatch pattern rarely trips
it. Keeping the CLIs updated avoids the update prompt entirely.

## Slash commands

Slash commands are not a protocol — every agent treats them as literal text
typed at its prompt. Send the command **with its argument in one string**
(`/execute-task 042`): a bare `/name` leaves the autocomplete menu open, where
Enter picks a menu entry instead of submitting. With an argument attached the
menu is already closed. Verified live on Claude Code.

## `/clear` and reset semantics

| Agent | What `/clear` does |
| ----- | ------------------ |
| Claude Code | Resets context in place; returns to the empty prompt. |
| Codex | Starts a new conversation; prints prior token usage and a `codex resume <id>` line. |
| OpenCode | Returns to the splash; context dropped. |

> That table is for **interactive human use only**. Automated dispatch never
> relies on `/clear`: it is cooperative, so sent to a pane you have misread as
> idle it does not reject or no-op — it queues behind the in-flight turn and
> wipes the session later. This destroyed a real task's work (task 047). The
> delegate skills always get a clean agent by spawning a new one — see
> `references/herdr/reset-worker.md` and
> `references/herdr/spawn-worker.md`.

## Passing the first prompt as an argument

Both Claude Code and Codex accept an optional initial prompt as a positional
argument:

```bash
claude --dangerously-skip-permissions "/execute-task 042"
codex --dangerously-bypass-approvals-and-sandbox "/execute-task 042"
```

The agent starts, runs that prompt, and stays interactive. Verified live for
both. This is the most reliable way to deliver a kickoff, because it avoids
terminal input delivery entirely — see `references/herdr/spawn-worker.md`
Pattern B.
