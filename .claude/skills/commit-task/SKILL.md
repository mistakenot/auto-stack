---
name: commit-task
description: "Verifies completeness of all planning docs and commits them to main. Use when 'commit the task', 'finalize the task', 'commit task docs', or after all planning docs have been reviewed and approved. Not applicable when implementation has already started or for committing code changes."
---

# Commit Task

Verify completeness of all planning docs and commit them to `main`. This is the final planning stage -- it does NOT create a feature branch or start implementation.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Process

### Step 1: Identify Task

Find the active task from user input or recent context. Locate the task folder under `docs/tasks/$ID-$NAME/`, then identify its layout — a folder uses one or the other, never both:

- **HTML** (current) — `plan.html` + `context.md`. One self-contained file with a `<pd-tab>` per stage: Requirements, Verification, Solution, Plan.
- **Markdown** (legacy) — `requirements.md`, `solution.md`, `context.md`, `plan.md`.

### Step 2: Verification Checklist

All checks must pass before committing. Each concern lives in a different place depending on layout:

| Check | HTML (`plan.html`) | Markdown (legacy) |
| --- | --- | --- |
| **Artifacts exist** | `plan.html` + `context.md` | all 4 `.md` files |
| **No unanswered Open Questions** | every `<pd-question>` is `status="answered"` | Open Questions sections in requirements.md and plan.md resolved or empty |
| **All ACs addressed** | every `<pd-ac>` carries non-empty `phases` and `tests` | every AC in requirements.md has coverage in solution.md and steps in plan.md |
| **Plan consistent with solution** | every `<pd-phase files>` path appears in the Solution tab's `<pd-files>` tree, and vice versa | the approach in solution.md matches the phases in plan.md |
| **Context covers plan references** | files and patterns referenced by phases are documented in `context.md` | same, from plan.md |
| **No unresolved P1 comments** | no `<pd-thread priority="p1">` left `status="unresolved"` | no `UNRESOLVED(P1)` threads remain |
| **Epic linkage consistent** | if `pd-meta` carries `epic:`, it names an epic that exists under `docs/epics/` | if any doc has `epic:` frontmatter, all four share the same value |

For an HTML task, run the bundled linter — it decides four of these rows mechanically:

```bash
node "$CLAUDE_SKILL_DIR/scripts/pd-lint.mjs" docs/tasks/$ID-$NAME/plan.html
```

It exits non-zero with JSON on any issue. `open-question` is the Open Questions gate; `unplanned-file` / `untracked-file` are the plan-vs-solution check; `missing-dep` / `dependency-cycle` mean the phase DAG is broken. A clean exit clears those rows. The AC, context, comment-thread and epic rows are yours to check by reading the doc — the linter does not cover them.

If any check fails, report the failures and stop. Do not commit incomplete docs.

### Step 3: Advance status to pending

The planning docs are complete and about to be committed — advance the task to
the `pending` stage (awaiting execution). See
[references/task-status.md](references/task-status.md): set `status="pending"` on
`<pd-doc>` in `plan.html`. Stage this change with the docs. (Legacy markdown
folders have no `<pd-doc>` — skip this step.)

### Step 4: Commit and Push

```bash
git add docs/tasks/$ID-$NAME/*
git commit -m "docs(tasks): add task $ID-$NAME planning docs"
```

After committing, push to origin:

```bash
git push origin main
```

If the push fails because the local branch is behind origin:

1. Try `git pull --rebase origin main` then `git push origin main`
2. If the rebase fails (conflicts), abort with `git rebase --abort` and try `git pull --no-rebase origin main` then `git push origin main`
3. If that also fails, stop and ask the user for help — do not force-push or discard changes

### Step 5: Next Steps

After successful push, tell the user:

"Task $ID planning docs committed and pushed to main. To begin implementation, run `/execute-task $ID`."

Do NOT create a feature branch. Do NOT start implementation.

## Commit Conventions

See [references/commit-conventions.md](references/commit-conventions.md) for commit message format and rules.
