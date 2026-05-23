---
name: commit-task
description: "Verifies completeness of all planning docs and commits them to main. Use when 'commit the task', 'finalize the task', 'commit task docs', or after all planning docs have been reviewed and approved. Not applicable when implementation has already started or for committing code changes."
---

# Commit Task

Verify completeness of all planning docs and commit them to `main`. This is the final planning stage -- it does NOT create a feature branch or start implementation.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Process

### Step 1: Identify Task

Find the active task from user input or recent context. Locate the task folder under `docs/tasks/$ID-$NAME/`.

### Step 2: Verification Checklist

All checks must pass before committing:

- [ ] **All 4 files exist**: `requirements.md`, `solution.md`, `context.md`, `plan.md`
- [ ] **No unanswered Open Questions**: check Open Questions sections in requirements.md and plan.md -- all must be resolved or empty
- [ ] **All ACs addressed**: every acceptance criterion in requirements.md has corresponding test coverage in solution.md and plan steps in plan.md
- [ ] **Plan consistent with solution**: the approach in solution.md matches the phases and steps in plan.md
- [ ] **Context covers plan references**: files and patterns referenced in plan.md are documented in context.md
- [ ] **No unresolved P1 comments**: if review comments exist, no `UNRESOLVED(P1)` threads remain

If any check fails, report the failures and stop. Do not commit incomplete docs.

### Step 3: Commit

```bash
git add docs/tasks/$ID-$NAME/*
git commit -m "docs(tasks): add task $ID-$NAME planning docs"
```

### Step 4: Next Steps

After successful commit, tell the user:

"Task $ID planning docs committed to main. To begin implementation, run `/execute-task $ID`."

Do NOT create a feature branch. Do NOT start implementation.

## Commit Conventions

See [references/commit-conventions.md](references/commit-conventions.md) for commit message format and rules.
