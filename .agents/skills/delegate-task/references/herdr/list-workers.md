# Discovering workers (herdr)

Enumerate the panes and agents herdr knows about, and read their metadata.

## Mental model

A hierarchy — the commands mirror it:

```
session        one persistent herdr server (daemon + Unix socket + attachable "desktop")
  └ workspace  a project / cwd context
      └ tab     a subcontext within a workspace
          └ pane a terminal split; may run a shell or an agent
              └ agent  a coding-agent process (claude/codex/…) inside that pane
```

Two consequences that shape every query:

- **A session is project-agnostic.** One session holds workspaces for many
  projects at once. The project boundary is a workspace's **cwd**, not the
  session — so anything enumerating "the work for this repo" must filter by repo
  (`worktree list --cwd <repo>`, or each pane's `cwd`), never assume one session
  means one project.
- **An agent is not a pane.** It is the process *running in* a pane, tracked as
  a first-class named entity. A pane may host no agent (a plain shell), in which
  case it is invisible to `agent list`.

## Commands

```bash
herdr workspace list                       # workspaces, labels, rolled-up agent_status
herdr tab list [--workspace <id>]
herdr pane list [--workspace <id>]         # every pane, agent or not
herdr agent list                           # only panes hosting a recognised agent
herdr agent get <target>                   # one agent's full record
herdr worktree list [--cwd <repo> | --workspace <id>]
herdr pane process-info --pane <pane_id>   # live process + argv (see verify-worker.md)
```

Most commands print a JSON envelope with results under `.result.*`. Read
identifiers out of those responses rather than predicting them; do not add
`--json` reflexively, since several of these commands reject the flag outright.

## Useful fields

| Field | Meaning |
| --- | --- |
| `agent` | agent kind (`claude`, `codex`, …); absent for a plain shell |
| `agent_status` | `idle \| working \| blocked \| done \| unknown` — see `references/herdr/wait-for-ready.md` |
| `interactive_ready` | boolean; the agent can accept a prompt now |
| `state_change_seq` | increments on each observed transition |
| `name` | agent handle from `agent start <name>` / `agent rename` |
| `cwd` | the agent process's working directory |
| `foreground_cwd` | cwd of whatever is in the pane's foreground |
| `agent_session` | native conversation-session id/path, when the integration reports one |
| `terminal_title_stripped` | the pane title the agent set — often a one-line summary of its last turn |
| `workspace_id` / `tab_id` / `pane_id` | `w1`, `w1:t1`, `w1:p1` |
| `is_linked_worktree` | on a `worktree list` entry: linked worktree ⇒ worker, primary checkout ⇒ planner |

## Targets

`agent *` subcommands accept a **unique live agent name** or a **pane id that
currently hosts that agent**. They do **not** accept terminal ids or bare agent
kinds — `herdr agent get claude` is not a valid lookup when several claude
agents are running.

An agent name follows the pane's current occupant and is cleared when that agent
exits, is released, or is replaced.

## ID durability

Public IDs (`w1`, `w1:t1`, `w1:p1`) are opaque stable handles, and closed tab
and pane IDs are **not reused**. One exception: a pane moved into another
workspace gets a new workspace-qualified pane id — after `pane move`, continue
with `.result.move_result.pane.pane_id` or the agent name, not the value in
`.result.move_result.previous_pane_id`.

Prefer the **agent name** as your handle for a worker's whole lifetime. It
survives everything a pane id does and reads far better in reports.

## Status is not enough on its own

`agent_status` tells you whether an agent is mid-turn. It says nothing about
whether the agent can work **unattended** — a worker launched without permission
flags reports a perfectly healthy `done` while being unable to run a single
tool. Pair discovery with `references/herdr/verify-worker.md`.
