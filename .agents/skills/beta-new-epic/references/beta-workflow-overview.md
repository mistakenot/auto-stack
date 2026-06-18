## Beta Workflow Overview

This skill is part of the beta planning workflow — a single-HTML-doc variant of the task workflow. Output is `plan.html` (with tabs) + `context.md` instead of four separate markdown files.

### Pipeline
```
Plan (on main)                          Execute                    Review & Complete
────────────────────                    ───────                    ─────────────────
/beta-new-task              /execute-task $ID          /address-feedback
  → plan.html (Requirements tab)         → worktree + branch      /complete-task
/beta-new-solution            → subagent per phase      → merge
  → context.md                            → PR
  → plan.html (Verification + Solution)
/beta-new-plan
  → plan.html (Plan tab)
/commit-task (manual for now)
```

### Conventions
- Task folder: `docs/tasks/$ID-$NAME/` (3-digit ID, kebab-case name)
- Branch: `task/$ID-$NAME`
- Planning happens on `main`. Execution happens in isolated worktrees.
- Each stage hard-stops for user review before proceeding to the next.
- Artifacts: `plan.html` + `context.md` (two files total)

### pd-meta status lifecycle
- `"planning"` — set on creation by `beta-new-task`
- `"executing"` — set by `execute-task` when it creates the worktree/branch
- `"merged"` — set by `complete-task` on merge

### pd-doc status vs pd-meta status
- `pd-doc status` (draft/in-review/approved) = document review state
- `pd-meta status` (planning/executing/merged) = task lifecycle state
- These are complementary — a doc can be "approved" while the task is still "planning"
