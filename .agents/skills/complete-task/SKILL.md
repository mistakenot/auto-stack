---
name: complete-task
description: "Finalizes a feature branch and merges it to main. Addresses remaining PR feedback, runs tests, writes feedback.md, merges the PR, exits the worktree, and verifies the merge. Use when 'complete task', 'finalize and merge', 'ship this task', or when the feature branch is ready to ship."
---

# Complete Task

Finalize feature branch and merge to main.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Prerequisites

Verify all before starting:

- On a feature branch (not `main`)
- Clean working tree (`git status` shows no uncommitted changes)
- Open PR exists for this branch

If any prerequisite fails, report which one and stop.

## Process

### Step 1: Address remaining PR feedback

Check for unresolved review threads. If any exist, invoke the `address-feedback` skill to resolve them before continuing.

### Step 2: Run affected tests

- Typecheck: always run
- Unit tests: if test files were changed
- E2E tests: if frontend or server functions were changed

All must pass before proceeding.

### Step 3: Push fixes

If steps 1-2 produced new commits, push them.

### Step 4: Write task feedback

Create `feedback.md` in the task folder using the template below.

See [references/template-feedback.md](references/template-feedback.md) for the feedback template.

### Step 5: Tear down worktree environment

Shut down any worktree-local services (dev servers, watchers, etc.).

### Step 6: Merge PR

```bash
gh pr merge --squash --delete-branch
```

### Step 7: Exit worktree

Switch back to main working tree.

### Step 8: Pull latest

```bash
git pull
```

### Step 9: Verify merge

Check `git log` for the squashed commit and verify key files are present.

## Commit Conventions

See [references/commit-conventions.md](references/commit-conventions.md) for commit message format and rules.
