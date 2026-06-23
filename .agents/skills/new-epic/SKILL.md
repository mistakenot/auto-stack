---
name: new-epic
description: "Plans an epic.html: user-facing direction, guard rails, and a breakdown into deployable, sequenced tasks. Use when 'new epic', 'create epic', 'plan an epic'. Not for a single task (use new-task)."
---

# New Epic

Create `docs/epics/epic-$ID-$NAME.html` — a high-level plan that sets the direction and guard rails for an initiative spanning multiple tasks, then breaks it into deployable, sequenced tasks.

> Part of the planning workflow. An epic sits **above** tasks: it frames the user-facing outcome and the constraints every task inherits, then decomposes the work. Each breakdown item is later planned with `/new-task`. See [references/workflow-overview.md](references/workflow-overview.md) for the downstream pipeline and [references/epic-overview.md](references/epic-overview.md) for what an epic is and how it differs from a task.

## Guiding Principles

An epic works at a different altitude than a task. Hold the line on it:
- **Direction and constraints, not construction.** State the user-facing outcome, the seams, and the guard rails. Leave *how* to the task planners — never list files, code, or intra-task steps.
- State your assumptions explicitly. An epic has more latent direction than any single task — surface it and ask rather than guess.
- If the request has multiple interpretations of scope or sequencing, present them and let the user pick.
- Push back if the initiative isn't really epic-sized (one deployable change → it's a task, use `/new-task`), or if it's so large it should be several epics.

## Process

1. **Scan skills** -- check available skills for topic matches relevant to the initiative. Load any matched skills before proceeding.
2. **Read project docs** -- find and read project documentation relevant to the domain (READMEs, how-to guides, concept docs, CLAUDE.md) so the direction is grounded in how the system actually works.
3. **Determine epic ID** -- scan `docs/epics/` for existing `epic-*.html` files. Assign the next 3-digit sequential ID (the highest existing number + 1; e.g. if `epic-004-*.html` exists, next is `005`). If `docs/epics/` doesn't exist or has no epic files, start at `001`.
4. **Derive epic name** -- create a short kebab-case name from the initiative (e.g. `partner-api-program`).
5. **Create epics folder** -- `mkdir -p docs/epics` (the epics directory itself; epics are single files, not per-epic folders).
6. **Create the epic file** -- load the `/planning-doc` skill and follow its process to create `docs/epics/epic-$ID-$NAME.html`. It will fetch `llms.txt` for the current component reference and version pin. Set the document title to `$ID: $NAME`, status to `draft`, and include `pd-meta` with id, name, `kind: "epic"`, status `planning`, and created date.
7. **Populate the tabs** -- author the epic's tabs. Lead with the user-facing final shape; keep every tab at epic altitude.

   See [references/epic-tabs.md](references/epic-tabs.md) for what each tab focuses on and which component to reach for per element.

8. **Resolve Open Questions** -- an epic sets direction, so unstated direction is the main risk. Proactively surface assumptions about scope, sequencing, and guard rails, and resolve them by asking the user interactively (use `AskUserQuestion` if available). Update the doc as answers come in. Repeat until resolved or deferred.
9. **Hard-stop** -- present the completed `epic-$ID-$NAME.html` to the user. Do NOT start planning tasks. Tell them: "Review the epic file. When ready, plan the first task with `/new-task` and reference this epic."
