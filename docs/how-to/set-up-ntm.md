---
hash: "ab0870c7"
id: "b8be6dd8"
read_when: "setting up NTM tmux sessions, configuring planner/worker agent pools, or spawning multi-agent sessions with labels"
summary: "Step-by-step guide to spawning planner and worker agent pools using NTM labels, adding workers on demand, and sending prompts to specific pools."
title: "How to Set Up NTM for Multi-Agent Development"
---

# How to set up NTM for multi-agent development

NTM (Named Tmux Manager) manages tmux sessions that run coding agents. We run two agent pools — **planners** and **workers** — separated by NTM labels. Labels are important because hooks read them to enforce which git operations each pool is allowed to perform.

## Prerequisites

- `ntm` installed and on PATH (`ntm deps` to verify)
- A project directory under the configured `projects_base` (default `~/src`)
- `ntm init` run in the project directory (sets up `.ntm/` config and git hooks)

## Pool model

| Pool       | Label       | Default count | Works on    | Allowed operations             |
|------------|-------------|---------------|-------------|--------------------------------|
| Planners   | `planners`  | 4 (2 cc + 2 cod) | `main`  | Read code, analyse, write planning docs, commit to main |
| Workers    | `workers`   | 0 (added on demand) | Worktrees | Check out branches, create worktrees, open PRs |

**Planners** do deep analysis and write planning docs. They work directly on `main` — since they rarely touch the same files, conflicts are uncommon.

**Workers** execute tasks in isolated worktrees with their own branch and tooling. They open PRs when complete. Workers should never work in the primary checkout.

## Spawning the planner pool

```bash
ntm spawn auto-stack --label planners --cc=2 --cod=2
```

This creates a tmux session named `auto-stack--planners` with four agent panes (2 Claude, 2 Codex) plus a user pane.

## Adding workers on demand

Workers start as an empty pool. When a task is delegated, add a worker:

```bash
ntm add auto-stack --label workers --cc=1
```

Or spawn a dedicated worker pool session up front:

```bash
ntm spawn auto-stack --label workers --cc=2 --worktrees
```

The `--worktrees` flag gives each agent its own git worktree automatically.

## Sending prompts to a specific pool

Target by label using `--project` to scope to all sessions for a project, or address the session directly:

```bash
# Send to all planners
ntm send auto-stack--planners "review the auth module and write findings"

# Send to all workers
ntm send auto-stack--workers "implement the auth refactor from task 042"

# Send to a specific agent type within a pool
ntm send auto-stack--planners --cc "focus on the Go packages"
```

## Listing sessions

```bash
# All sessions
ntm list

# All sessions for a project (shows both planner and worker pools)
ntm list --project auto-stack
```

## Key points

- Always set the `--label` flag when spawning. Hooks read labels to enforce pool rules — without a label, the rules won't apply.
- Planners commit directly to `main`. Workers use branches and open PRs.
- Workers should never work in the primary checkout — always use worktrees.
- Add workers incrementally as tasks are delegated rather than pre-allocating a large pool.
