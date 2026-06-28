---
hash: "0ed91648"
read_when: "running multiple autonomous agents (tmux/ntm planner+executor split) against one repo, debugging why HEAD/branch changes under an agent, or deciding worktree isolation for background Claude sessions"
summary: "Eight planner/executor Claude panes all shared the ONE primary checkout as their cwd, so when a leftover unattended planner session (the task-048 PBT planner, running --dangerously-skip-permissions) ran git checkout, HEAD flipped for all eight at once and then oscillated between main and a feature branch — the upstream cause of the same-day main-branch-divergence incident."
title: "Postmortem: a shared working tree + a rogue autonomous planner flipped HEAD under 8 agents"
---

# Postmortem: shared primary checkout + rogue autonomous planner

- **Date:** 2026-06-28
- **Author:** Claude (task-048 executor session)
- **Severity:** Low–Medium — no data lost and task 048 shipped cleanly, but an interactive session's working tree had its branch changed underneath it repeatedly, and a sibling incident ([2026-06-28-main-branch-divergence](2026-06-28-main-branch-divergence.md)) was a direct downstream effect.
- **Status:** Diagnosed. No structural fix applied yet — see Action items.

## Summary

A `/complete-task 048` session went to return the primary checkout to `main` and
found it sitting on a feature branch (`fix/skill-add-pipe-deadlock`) it never
created, with `main` diverged from `origin/main` and uncommitted churn in
`.auto/skills/manifest.json` and `todo.md`. Investigation showed the cause was not
a misbehaving tool but an **architectural one**: all eight agent panes in the
dogfood planner/executor split share a single working tree, and a leftover
autonomous planner session was running `git checkout` / commit / rebase in it.
Because git's `HEAD` is a property of the working tree, every one of those
operations changed the branch for all eight panes simultaneously — and the rogue
session bounced `HEAD` back and forth, so it was *oscillating*, not merely wrong.

## Impact

- The primary checkout's branch changed under at least one live interactive
  session without that session acting.
- Local `main` diverged three ways (remote merge + unpushed autodoc commit + a
  fresh feature branch) — see the sibling postmortem for the merge-time fallout.
- Uncommitted churn appeared in the shared tree from a concurrent agent.
- No commits lost; task 048 (`09598ae`) merged to `origin/main` correctly and was
  unaffected.

## What we found (evidence)

1. **All 8 panes share one cwd.** `/proc/<pid>/cwd` for every pane's `claude`
   process — 4 planners (`auto-stack:0.0–0.3`) and 4 executors
   (`auto-stack--executor:0.0–0.3`) — resolved to the **same**
   `/home/vscode/src/auto-stack`. Executors `cd` into a per-task worktree
   (`auto-stack-task-049`, etc.) only for the duration of a task; the pane's base
   cwd stays the shared root.
2. **The committer was a leftover planner, not a daemon.** The commits
   (`synctext.go` `d4fb538`; the autodoc gate `93ad35c`/`285970d`; `grill-me` skill
   `6158a15`; the divergence postmortem `07afcd9`; branch hops onto
   `fix/skill-add-pipe-deadlock`) traced to pid `647751`:
   `claude --resume bee0aa1b… --dangerously-skip-permissions --model claude-opus-4-6`.
   Its tty (`pts/25`) maps to pane **`auto-stack:0.1`, title
   "pbt-pure-functions-testing"** — i.e. the pane that *planned task 048* and then
   kept running autonomously instead of stopping.
3. **Smoking gun.** A live shell under pid `647751` was running
   `auto skill add … --skill grill-me`, which is exactly reflog entry
   `6158a15 chore(skills): add grill-me skill via auto skill add`.
4. **`auto watch` was innocent.** Both `.auto/watch/project.json` files (primary and
   `auto-stack-dogfood`) define only a single bash task `run-etl` (`autoetl run`) on
   a 10-minute cron. The `autowatch.service` daemon (pid 2426103) was doing exactly
   that and nothing else.

## Root cause

**One working tree, many autonomous writers.** `HEAD` lives in `.git/HEAD`, shared
by every process whose cwd is that tree. Running 8 agents — especially an always-on
planner with `--dangerously-skip-permissions` — from the *same* primary checkout
means any `checkout`/`commit`/`rebase` by one races and mutates state for the other
seven. The earlier "main moved twice under a merge" divergence was a *symptom* of
this; the disease is the shared checkout plus an unattended agent that never exited
its planning role.

### Three "daemon-ish" things, only one at fault

A lot of confusion came from conflating these. For future triage:

| Thing | What it is | Verdict |
|---|---|---|
| `auto watch` (`autowatch.service`, `auto watch start`) | runs only what `project.json` defines (ETL-only here) | innocent |
| `claude daemon run` | Claude Code's own background-execution host for a session's bg Bash/subagents | infrastructure, no agenda |
| `claude --resume … --dangerously-skip-permissions` (pid 647751, pane `auto-stack:0.1`) | a leftover autonomous planner session | **the culprit** |

## What went well

- Task 048 was fully isolated in its own worktree (`auto-stack-048-…`), so it
  shipped cleanly despite the chaos around it.
- Nothing was force-reconciled blindly: the divergence and the uncommitted churn
  were surfaced to the operator rather than reset, preserving the rogue session's
  in-flight work.

## What went wrong

- **Planners run from the shared primary checkout, not their own worktrees.** This
  makes branch state a shared mutable global across 8 agents.
- **An autonomous session with bypassed permissions outlived its task.** The 048
  planner kept committing dogfood work after planning was done.
- **No guard against an agent changing the branch of a working tree another session
  is using.**

## Action items

| # | Action | Type |
|---|--------|------|
| 1 | Give every always-on agent (especially planners) its **own** git worktree; never run an autonomous agent from the shared primary checkout. | Structural |
| 2 | Background/automation that lands on `main` should push a fast-forward to `origin/main` from an isolated worktree, never mutate a shared checkout's `HEAD`. | Process |
| 3 | Scope `--dangerously-skip-permissions` sessions to a bounded task and have them exit; don't leave a resumed planner running open-ended. | Process |
| 4 | Triage rune: map a suspicious pid to its pane via `ps -o tty= -p <pid>` + `tmux list-panes -a -F '#{pane_index} pid=#{pane_pid} tty=#{pane_tty} title=#{pane_title}'` before blaming `auto watch`. | Technique |
| 5 | Consider a pre-checkout/commit guard that refuses to switch `HEAD` in the primary checkout when other live sessions share its cwd. | Tooling |

## Lessons

- In a multi-agent setup, **a working tree is shared mutable state.** `HEAD` is not
  per-process; one agent's `git checkout` is a global side effect on every pane in
  that directory.
- Distinguish "a daemon did it" from "an agent did it" early — most of the cost here
  was suspecting `auto watch`, which was behaving exactly as configured.
- Worktree isolation is not just for avoiding file-write conflicts; it's what keeps
  *branch state* private so autonomous agents can't reset each other.
