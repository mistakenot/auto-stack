# Naming and labelling a worker (herdr)

How to attach and query identifying metadata. This is mechanism only — what a
label *denotes* (planner vs worker) is policy, and lives in
[references/worker-pools.md](references/worker-pools.md).

## The agent name is the handle that matters

```bash
herdr agent start <name> --kind claude --pane <pane_id> -- <agent-args...>
herdr agent rename <target> <name>|--clear
```

`<name>` must match `[a-z][a-z0-9_-]{0,31}` and be unique among **live** agents.
It becomes the target for every later `agent` command, so choose something that
identifies the work — `task-042`, not `worker3`. The name follows the pane's
current occupant and is cleared when that agent exits or is replaced, so names
are reusable across a worker's lifecycle.

Prefer the name over a pane id everywhere: it is stable across layout changes
(including `pane move`, which reassigns pane ids) and it reads better in status
reports.

## Display labels at each level

```bash
herdr workspace rename <workspace_id> <label>
herdr tab       rename <tab_id> <label>
herdr pane      rename <pane_id> <label>|--clear
```

Or set them at creation: `--label TEXT` on `workspace create`, `tab create`,
`pane split`, and `worktree create`. Give a task worker's workspace the task
label (`--label task-042` on `worktree create`) so the user's sidebar is
readable.

For a pane hosting an agent, `pane list` reports the string as `label` and
`agent list` reports it as `name` — same value, two field names.

## No session-level metadata

`session list` exposes only `name`, `default`, `running`, `session_dir`, and
`socket_path`. There is no `session rename` and no arbitrary metadata: a
session's only identity is the immutable name given at creation
(`herdr --session <name>`).

## `--env` — the only metadata that *does* something

`--env KEY=VALUE` (on `workspace create`, `tab create`, `pane split`) lands in
the process environment, so a git hook or the agent itself can read it — e.g.
`--env DELEGATED_TASK=042`. Every other label is display-only.

Do not use `--env` to carry a *role*: role is derivable from git without any
metadata at all (below), and an env var can be missing while a worktree cannot.

## Deriving role without any label

herdr has no label-driven enforcement layer. It does not need one — for the
planner/worker split, role is a property of the checkout:

```bash
[ "$(git rev-parse --git-dir)" != "$(git rev-parse --git-common-dir)" ] \
  && role=worker || role=planner
```

Linked worktree ⇒ **worker**; primary checkout ⇒ **planner**. `worktree list`
surfaces the same distinction as `is_linked_worktree`, and
`references/herdr/verify-worker.md` shows how to check it for a given pane. This
works under any multiplexer, and under none.
