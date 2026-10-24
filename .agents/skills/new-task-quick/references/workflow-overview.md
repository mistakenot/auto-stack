## Workflow Overview

This skill is part of the planning workflow. Output is `plan.html` (with tabs) + `context.md`.

### Pipeline
```
Plan (on main)                          Execute                    Review & Complete
────────────────────                    ───────                    ─────────────────
/new-task                   /execute-task $ID          /address-feedback
  → plan.html (Requirements tab)         → worktree + branch      /complete-task
/new-solution                 → subagent per phase      → merge
  → context.md                            → PR
  → plan.html (Verification + Solution)
/new-plan
  → plan.html (Plan tab)
/commit-task
```

`/new-task-quick` runs all three planning stages in one unattended pass
instead, pauses only if it has open questions for the human, then runs
`/request-codex-review` on the answered doc. Same artifacts, one human review.

### Conventions
- Task folder: `docs/tasks/$ID-$NAME/` (3-digit ID, kebab-case name)
- Branch: `task/$ID-$NAME`
- Planning happens on `main`. Execution happens in isolated worktrees.
- Each stage hard-stops for user review before proceeding to the next — except under
  `new-task-quick`, which runs them unattended, recording decisions it could
  not make as gating `<pd-question>`s, stops for those answers only if there are any, then
  has Codex review the answered doc.
- Artifacts: `plan.html` + `context.md` (two files total)

### pd-meta status lifecycle
- `"planning"` — set on creation by `new-task`
- `"executing"` — set by `execute-task` when it creates the worktree/branch
- `"merged"` — set by `complete-task` on merge

### pd-doc status vs pd-meta status
- `pd-doc status` (draft/in-review/approved) = document review state
- `pd-meta status` (planning/executing/merged) = task lifecycle state
- These are complementary — a doc can be "approved" while the task is still "planning"
