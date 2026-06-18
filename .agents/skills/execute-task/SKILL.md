---
name: execute-task
description: "Autonomously implements a planned task end-to-end using the coordinator-subagent pattern. Reads task docs, creates a worktree, dispatches subagents per phase, tracks progress, and opens a PR. Use when 'execute task', 'implement task', or after planning docs have been committed."
---

# Execute Task

Autonomous end-to-end implementation using the coordinator-subagent pattern.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Input

Task ID (numeric, e.g. `042`).

## Task Type Detection

Find the task folder matching ID under `docs/tasks/` (glob `docs/tasks/$ID-*`). Then determine the workflow type:

1. Read `plan.md` in the task folder
2. Check for `workflow: mini` in the YAML frontmatter

**If `workflow: mini`** — this is a mini-task with a single plan.md file. Read and follow [references/execute-task-mini.md](references/execute-task-mini.md).

**Otherwise** — this is a full task with requirements.md, solution.md, context.md, and plan.md. Read and follow [references/execute-task-full.md](references/execute-task-full.md).

## Worktree Conventions

See [references/worktree-conventions.md](references/worktree-conventions.md) for worktree setup and teardown rules.

## Commit Conventions

See [references/commit-conventions.md](references/commit-conventions.md) for commit message format and rules.

## PR Body Template

See [references/template-pr-body.md](references/template-pr-body.md) for the PR body template.

## Task Status

This is the worker stage of the lifecycle: advance the task to `complete` when
opening the PR. See [references/task-status.md](references/task-status.md) — the
per-workflow executor refs above carry out this step in their "Open PR" section.
