---
name: new-solution
description: "Writes a solution design (solution.md) for an existing task by exploring approaches and tradeoffs. Use when 'write a solution', 'design the solution', 'new solution', 'explore approaches', or after requirements have been approved. Not applicable when requirements.md doesn't exist yet."
---

# New Solution

Read approved requirements, explore approaches, write `solution.md`.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Guiding Principles

- If multiple approaches exist, surface them all with tradeoffs -- don't silently pick one.
- Bias toward the simplest approach. If a simpler option exists, say so even if it's less elegant.
- No speculative abstractions or "flexibility" that wasn't requested. The solution should be the minimum design that satisfies the acceptance criteria.
- If something in the requirements is ambiguous, stop and ask before designing around an assumption.

## Process

1. **Find task folder** -- identify the active task from recent context, user input, or by scanning `docs/tasks/` for the latest folder. Read `requirements.md`. Verify all Open Questions are resolved -- if not, resolve them first.
2. **Scan skills** -- check available skills for topic matches relevant to this task's domain. Load matched skills.
3. **Assess complexity**:
   - **Straightforward** (one obvious approach): go directly to step 5.
   - **Ambiguous** (multiple viable approaches): go to step 4.
4. **Explore options** -- spawn parallel subagents, one per candidate approach. Each subagent investigates feasibility by reading relevant code, checking patterns, and identifying risks. Collect results. Present a comparison table to the user with pros/cons for each option. Wait for the user to pick an approach.
5. **Write solution.md** -- use the template and rules below. Fill in Approach, Files, Test Coverage, Out of Scope, and Rejected Alternatives.
6. **Create artifacts** -- if the solution involves user-facing flows or complex architecture, create artifact files (wireframes, diagrams) in the task folder. Follow the artifact guidelines below.
7. **Hard-stop** -- present the completed solution.md to the user. Do NOT proceed to the plan stage. Tell them: "Review solution.md. When ready, run `/new-plan` to continue."

## Solution Template and Rules

See [references/template-solution.md](references/template-solution.md) for the full template and rules.

## Artifact Guidelines

See [references/artifact-guidelines.md](references/artifact-guidelines.md) for artifact creation rules and examples.
