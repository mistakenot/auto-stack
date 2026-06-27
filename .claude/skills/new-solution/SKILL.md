---
name: new-solution
description: "Gathers context, writes Verification and Solution tabs for a task. Use when 'write a solution', 'design the solution', 'new solution', 'explore approaches', or after requirements have been approved. Not applicable when requirements don't exist yet."
---

# New Solution

Read approved requirements, gather codebase context, write `context.md`, then add Verification and Solution tabs to `plan.html`.

> Part of the planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Guiding Principles

- If multiple approaches exist, surface them all with tradeoffs -- don't silently pick one.
- Bias toward the simplest approach. If a simpler option exists, say so even if it's less elegant.
- No speculative abstractions or flexibility that wasn't requested. Design the minimum that satisfies the requirements.
- If something in the requirements is ambiguous, stop and ask before designing around an assumption.

## Process

### Stage 1: Context Gathering

1. **Find task folder** -- identify the active task from recent context, user input, or by scanning `docs/tasks/` for the latest folder. Read `plan.html` and check the Requirements tab. Verify all Open Questions are resolved -- if not, resolve them first.
2. **Scan skills** -- check available skills for topic matches relevant to this task's domain. Load matched skills.
3. **Gather codebase context** -- spawn 2 parallel subagents to ground the design in codebase reality:

   **CB1 (Code):**
   - Search files, functions, types, and patterns relevant to the task
   - Find similar implementations in the codebase for pattern reference
   - Note existing conventions and constraints that will shape the solution

   **CB2 (Docs):**
   - Search project documentation for relevant how-tos, concept docs, and architecture guides
   - Check for related rules or conventions that apply
   - Note any documented constraints or patterns the implementation must follow
   - Run `auto search quickstart` to discover available search tools, then use the best fit if useful

   Collect findings from both subagents before proceeding.

4. **Write context.md** -- combine the findings into `context.md` in the task folder.

   See [references/template-context.md](references/template-context.md) for the template and rules.

5. **Impact analysis** -- run an impact analysis on the proposed file changes (each file + one-sentence change summary); fold flagged files into the changes list and any data, permission, or integration concerns into the Verification tab as acceptance criteria.

### Stage 2: Verification Tab

6. **Scan for verification strategies** -- check available project skills for testing, verification, and assurance strategies. Load any matched skills.
7. **Write Verification tab** -- using the context and requirements, design the verification strategy. Insert `<pd-tab name="Verification">` into plan.html after the Requirements tab.

   See [references/tab-verification.md](references/tab-verification.md) for the tab structure and rules.

8. **Hard-stop (gate 1)** -- present the Verification tab to the user. Tell them: "Review the Verification tab. When ready, confirm to proceed to Solution design."

### Stage 3: Solution Tab

9. **Assess complexity** (informed by context):
   - **Straightforward** (one obvious approach): go directly to step 10.
   - **Ambiguous** (multiple viable approaches): go to step 9.
10. **Explore options** -- spawn parallel subagents, one per candidate approach. Each subagent investigates feasibility using the gathered context. Collect results. Present a comparison table with pros/cons for each option. Wait for the user to pick.
11. **Write Solution tab** -- design the solution and insert `<pd-tab name="Solution">` into plan.html after the Verification tab.

    See [references/tab-solution.md](references/tab-solution.md) for the tab structure and rules.

12. **Validate assumptions** -- review every design decision and identify any that were NOT clearly dictated by (a) the requirements or (b) pre-established patterns in the repository. **When interactive**, surface uncertain decisions with the user (use `AskUserQuestion` tool) and update plan.html with their answers. **When running autonomously** (told not to ask, or no user available), record each load-bearing uncertain decision as a `<pd-question>` with a `recommendedAnswer` rather than silently committing to it — it gates the doc so the executor can't build on an unvalidated assumption, while the human can rubber-stamp your lean in one click. Reserve prose notes for assumptions you're comfortable proceeding on.
13. **Hard-stop (gate 2)** -- present the completed Solution tab to the user. Do NOT proceed to the plan stage. Tell them: "Review the Solution tab. When ready, run `/new-plan` to continue."
