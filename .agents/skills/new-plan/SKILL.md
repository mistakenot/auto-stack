---
name: new-plan
description: "Writes context.md and plan.md for an existing task by gathering codebase context and breaking the solution into executable phases. Use when 'write a plan', 'new plan', 'create the plan', 'plan the task', or after solution.md has been approved. Not applicable when solution.md doesn't exist yet."
---

# New Plan

Read requirements + solution, gather codebase context, write `context.md` and `plan.md`.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Process

### Phase 1: Enrich Context

`context.md` already exists from the solution stage (code + docs findings). Enrich it with historical context by spawning a subagent:

**CB3 (History):**
- Search git commits for related changes
- Check `docs/tasks/` for related completed tasks
- Note relevant decisions or patterns from past work

Merge findings into the existing `context.md` -- append a **Related Tasks** section if one doesn't exist, or update it. Also verify that file paths from solution.md and existing context.md still hold (flag any that have drifted). Ensure context.md has `epic:` frontmatter if requirements.md does.

Run an impact analysis on the proposed file changes (each file + one-sentence change summary); fold flagged files into the changes list and any data, permission, or integration concerns into plan.md as acceptance criteria.

### Phase 2: Write plan.md

Using requirements.md, solution.md, and the freshly written context.md:

1. Break the solution into atomic phases (each phase = one subagent during execution)
2. Define the execution DAG -- which phases depend on which
3. List concrete steps within each phase, with checkboxes
4. Every step must have an explicit verify check (not just "run typecheck" but "verify: typecheck passes, new route returns 200")
5. End each phase with a commit step and verification (typecheck, tests, lint)
6. Define success criteria that map back to acceptance criteria
7. If requirements.md has `epic:` frontmatter, copy it to plan.md
8. Create artifact files if needed (architecture diagrams, sequence diagrams for complex flows)

Strong success criteria let subagents loop independently. Weak criteria ("make it work") cause confusion and wasted cycles. Each step should be verifiable without human judgement.

### Phase 4: Hard-stop

Present both context.md and plan.md to the user. Do NOT proceed to execution or commit. Tell them: "Review context.md and plan.md. When ready, run `/review-task` for a review or `/commit-task` to finalize."

## Context Template and Rules

See [references/template-context.md](references/template-context.md) for the full template and rules.

## Plan Template and Rules

See [references/template-plan.md](references/template-plan.md) for the full template and rules.

## Artifact Guidelines

See [references/artifact-guidelines.md](references/artifact-guidelines.md) for artifact creation rules and examples.
