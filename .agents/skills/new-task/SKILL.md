---
name: new-task
description: "Creates a new task folder with requirements.md from user input. Use when 'create a task', 'new task', 'start a task', 'write requirements', or when a user describes a feature/fix to plan. Not applicable when executing an existing task or reviewing existing docs."
---

# New Task

Create `docs/tasks/$ID-$NAME/requirements.md` from user input.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Guiding Principles

Before writing anything, apply these checks:
- State your assumptions explicitly. If uncertain about what the user wants, ask -- don't guess.
- If the request has multiple interpretations, present them and let the user pick.
- If the scope seems too large for a single task, say so and suggest splitting.
- Push back if something is unclear. Name what's confusing and ask.

## Process

1. **Scan skills** -- check available skills for topic matches relevant to the user's request. Load any matched skills before proceeding.
2. **Read project docs** -- find and read project documentation relevant to the domain (READMEs, how-to guides, concept docs, CLAUDE.md).
3. **Determine task ID** -- scan `docs/tasks/` for existing folders. Assign the next 3-digit sequential ID (e.g. if `042-*` exists, next is `043`). If `docs/tasks/` doesn't exist, start at `001`.
4. **Derive task name** -- create a short kebab-case name from the user's description (e.g. `add-team-settings`).
5. **Create task folder** -- `mkdir -p docs/tasks/$ID-$NAME`
6. **Check for epic** -- if the user mentions an epic or the task clearly belongs to one, ask which epic file it belongs to (path relative from repo root). If the task is part of an epic, include `epic:` in YAML frontmatter on requirements.md. If not, omit frontmatter entirely.
7. **Write requirements.md** -- use the template and rules below. Fill in Problem, Goals, and Acceptance Criteria from user input. Add Out of Scope based on reasonable boundaries. List any unresolved questions in Open Questions.
8. **Resolve Open Questions** -- assume there are latent requirements in the user's head that they haven't communicated in their prompt. Proactively surface unstated assumptions, clarify ambiguity, and resolve Open Questions by asking the user interactively (use `AskUserQuestion` tool if available). Update the doc with answers as they come in. Repeat until all questions are resolved or the user defers them.
9. **Hard-stop** -- present the completed requirements.md to the user. Do NOT proceed to solution stage. Tell them: "Review requirements.md. When ready, run `/new-solution` to continue."

## Requirements Template and Rules

See [references/template-requirements.md](references/template-requirements.md) for the full template and rules.
