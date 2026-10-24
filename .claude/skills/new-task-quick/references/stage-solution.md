## Guiding Principles

- If multiple approaches exist, surface them all with tradeoffs -- don't silently pick one.
- Bias toward the simplest approach. If a simpler option exists, say so even if it's less elegant.
- No speculative abstractions or flexibility that wasn't requested. Design the minimum that satisfies the requirements.
- If something in the requirements is ambiguous, stop and ask before designing around an assumption.

## Process

### Stage 1: Context Gathering

1. **Find task folder** -- identify the active task from recent context, user input, or by scanning `docs/tasks/` for the latest folder. Read `plan.html` and check the Requirements tab. **When interactive**, verify all Open Questions are resolved -- if not, resolve them first. **When running autonomously** (told not to ask, or no user available), unanswered `<pd-question>`s that carry a `recommendedAnswer` are expected — proceed on that lean rather than resolving them; the doc stays gated for the human.
2. **Scan skills** -- check available skills for topic matches relevant to this task's domain. Load matched skills.
3. **Gather codebase context** -- spawn 2 subagents in a single message, in parallel, to ground the design in codebase reality.

   **Spawn them unnamed.** CB1/CB2 below are labels for the prompt and the one-line
   description, nothing more. If your subagent tool takes a `name` (or other identity)
   parameter, omit it: a named agent is registered as an addressable teammate, and a
   teammate's report is only delivered once your turn ends -- which, in an autonomous
   run, is long after these tabs are written.

   **CB1 (Code):**
   - Search files, functions, types, and patterns relevant to the task
   - Find similar implementations in the codebase for pattern reference
   - Note existing conventions and constraints that will shape the solution

   **CB2 (Docs):**
   - Search project documentation for relevant how-tos, concept docs, and architecture guides
   - Check for related rules or conventions that apply
   - Note any documented constraints or patterns the implementation must follow
   - Run `auto search quickstart` to discover available search tools, then use the best fit if useful

   **Cap each report at ~10k characters** -- `file:line` bullets with a one-line
   finding each, no long quotes or file dumps. Say so in the prompt. Oversized reports
   are truncated in transit and the tail is lost.

   **Wait for both completion notifications before proceeding, and do not chase them.**
   Do not poll an agent-listing tool for status, and never ask an agent to resend its
   report: an agent that shows as idle with no result has not failed, its result is
   still in flight, and a resend request only makes it do the work twice.

4. **Write context.md** -- combine the findings into `context.md` in the task folder.

   See [references/template-context.md](references/template-context.md) for the template and rules.

5. **Impact analysis** -- run an impact analysis on the proposed file changes (each file + one-sentence change summary); fold flagged files into the changes list and any data, permission, or integration concerns into the Verification tab as acceptance criteria.

### Stage 2: Verification Tab

6. **Scan for verification strategies** -- check available project skills for testing, verification, and assurance strategies. Load any matched skills.
7. **Write Verification tab** -- using the context and requirements, design the verification strategy. Insert `<pd-tab name="Verification">` into plan.html after the Requirements tab.

   See [references/tab-verification.md](references/tab-verification.md) for the tab structure and rules.

   Do not stop after the Verification tab -- continue straight through to the Solution tab.

### Stage 3: Solution Tab

8. **Assess complexity** (informed by context):
   - **Straightforward** (one obvious approach): go directly to step 10.
   - **Ambiguous** (multiple viable approaches): go to step 9.
9. **Explore options** -- spawn parallel subagents, one per candidate approach, under the same spawning rules as step 3 (unnamed, one message, ~10k-character reports, wait for the completion notifications rather than polling). Each subagent investigates feasibility using the gathered context. Collect results. **When interactive**, present a comparison table with pros/cons for each option and wait for the user to pick. **When running autonomously**, pick the option the gathered evidence favours, record the rest under Rejected Alternatives in the Solution tab, and record the choice itself as a `<pd-question>` with `recommendedAnswer` set to your pick — do not stall waiting for a pick that isn't coming.
10. **Write Solution tab** -- design the solution and insert `<pd-tab name="Solution">` into plan.html after the Verification tab.

    See [references/tab-solution.md](references/tab-solution.md) for the tab structure and rules.

11. **Validate assumptions** -- review every design decision and identify any that were NOT clearly dictated by (a) the requirements or (b) pre-established patterns in the repository. **When interactive**, surface uncertain decisions with the user (use `AskUserQuestion` tool) and update plan.html with their answers. **When running autonomously** (told not to ask, or no user available), record each load-bearing uncertain decision as a `<pd-question>` with a `recommendedAnswer` rather than silently committing to it — it gates the doc so the executor can't build on an unvalidated assumption, while the human can rubber-stamp your lean in one click. Reserve prose notes for assumptions you're comfortable proceeding on.
