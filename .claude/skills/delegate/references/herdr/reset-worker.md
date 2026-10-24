# Getting a clean-context worker — spawn fresh, never `/clear`

"Reset a worker" means getting it back to empty context so it can take a new
task without inheriting the previous one's history. **There is no in-place
reset. Discard the worker and spawn a new one.**

Two independent reasons, and neither is a herdr wart:

## 1. `/clear` is cooperative, and it detonates

**Never** drive an agent's own reset by sending `/clear` (or any equivalent
slash reset) into its pane. `/clear` is queued by the agent's TUI behind
whatever it is currently doing. Sent to a pane you have merely *misread* as
idle, it does not reject and does not no-op — it silently waits and then wipes
the session mid-task. This destroyed a real task's work (task 047). Nothing in
the return value tells you whether it ran.

This hazard lives in the **agent CLI**, not the multiplexer, so it transfers
unchanged to any driver. Treat it as prohibited regardless of agent or runner.

herdr's `agent prompt` narrows the window — it refuses to send into a
recognised dialog (`agent_blocked`) — but it cannot make a queued `/clear` safe,
because a busy agent is not a blocked agent.

## 2. Permission mode is fixed at launch

Even setting `/clear` aside, a worker that came up in the wrong permission mode
cannot be repaired in place: `shift+tab` cannot cycle Claude Code from manual
into bypass (verified live). Reuse cannot fix a mis-launched worker; only a
relaunch can. See `references/herdr/spawn-worker.md`.

herdr has no `agent reset`/`restart` primitive either — searched across the
`agent`, `pane`, `workspace`, `tab`, `session`, and `config` command surfaces.
The only near-hits are `config reset-keys` (keybindings) and the
`resume_agents_on_restore` config flag, which restores agents across a *server*
restart — the opposite of clearing context.

## The supported path: reap, then spawn

1. `references/herdr/reap-worker.md` — remove the worktree, then close the
   workspace (or close the pane, for a non-worktree worker).
2. `references/herdr/spawn-worker.md` — create a fresh worker, with the
   permission flags, ideally dispatching via Pattern B in the same call.

A newly spawned worker is clean by construction, so there is no reset step for
it: just send the task. Once a dispatched worker finishes, its context is dirty
and it is reaped rather than reused.
