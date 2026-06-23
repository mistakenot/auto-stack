---
name: new-plan
description: "Writes the Plan tab and backfills AC traceability for a task. Use when 'write a plan', 'new plan', 'create the plan', 'plan the task', or after solution is approved. Not applicable when solution doesn't exist yet."
---

# New Plan

Read plan.html and context.md, enrich context with git history, write the Plan tab, and backfill AC traceability.

> Part of the planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Process

### Phase 1: Enrich Context

1. **Find task folder** -- identify the active task from recent context, user input, or by scanning `docs/tasks/` for the latest folder. Read `plan.html` (all tabs) and `context.md`.
2. **Enrich context** -- spawn a subagent to add historical context:

   **CB3 (History):**
   - Search git commits for related changes
   - Check `docs/tasks/` for related completed tasks
   - Note relevant decisions or patterns from past work

   Merge findings into `context.md` -- append a **Related Tasks** section if one doesn't exist, or update it. Verify that file paths from the Solution tab and existing context.md still hold (flag any that have drifted).

3. **Impact analysis** -- run an impact analysis on the proposed file changes (each file + one-sentence change summary); fold flagged files into the changes list and any data, permission, or integration concerns into the Verification tab as acceptance criteria.

### Phase 2: Write Plan Tab

4. **Design execution phases** -- using the Requirements, Verification, and Solution tabs plus context.md:
   - Break the solution into atomic phases (each phase = one subagent during execution)
   - Define the execution DAG -- which phases depend on which
   - List concrete steps within each phase
   - Every step must have an explicit verify check
   - End each phase with a commit step and verification (typecheck, tests, lint)

5. **Write Plan tab** -- insert `<pd-tab name="Plan">` into plan.html after the Solution tab.

   See [references/tab-plan.md](references/tab-plan.md) for the tab structure and rules.

### Phase 3: Backfill Traceability

6. **Backfill pd-ac cards** -- go back to the Verification tab and update each `<pd-ac>` card:
   - Set `phases` to the comma-separated phase numbers that cover this acceptance criterion (e.g. `phases="1,2"`)
   - Set `tests` to the comma-separated test file paths from the solution (e.g. `tests="src/feature.test.ts"`)
   - This links acceptance criteria to the phases that implement them and the tests that verify them

### Phase 4: Hard-stop

7. **Hard-stop** -- present the Plan tab to the user. Tell them: "Review the Plan tab. The planning phase is now complete. When ready, run `/commit-task` to commit the task docs, then `/execute-task` to start implementation."
