## Workflow Overview

This skill is part of a multi-stage task workflow. There are two paths:

### Full Workflow
```
Plan (on main)                Execute (on feature branch)         Review & Complete
─────────────────             ──────────────────────────          ─────────────────
/new-task                     /execute-task $ID                   /address-feedback
  → requirements.md             → worktree + branch              /code-review
/new-solution                    → subagent per phase             /complete-task
  → solution.md                  → PR                              → feedback.md
/new-plan                                                          → merge
  → context.md + plan.md     /delegate-task (optional)
/review-task (optional)       /status-report (optional)
/request-codex-review (optional)
/resolve-comments (optional)
/commit-task
```

### Mini Workflow (fast path)
```
Plan (on main)                Execute (on feature branch)         Review & Complete
─────────────────             ──────────────────────────          ─────────────────
/new-mini-task                /execute-task $ID                   /address-feedback
  → plan.md only                → worktree + branch              /complete-task
  (AC + context dump)            → self-directed execution          → merge
                                 → self-review + PR
                              /delegate-task (optional)
```

**Conventions:**
- Task folder: `docs/tasks/$ID-$NAME/` (3-digit ID, kebab-case name)
- Branch: `task/$ID-$NAME`
- Planning happens on `main`. Execution happens in isolated worktrees.
- Each stage hard-stops for user review before proceeding to the next.
- Mini-tasks use a single `plan.md` with `workflow: mini` frontmatter instead of separate requirements/solution/context/plan docs.
