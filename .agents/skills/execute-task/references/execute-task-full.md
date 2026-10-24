# Execute Task — Full Workflow

## Input

Task ID (numeric, e.g. `042`).

## Startup

1. Find task folder matching ID under `docs/tasks/` (glob `docs/tasks/$ID-*`)
2. Read ALL of it: `plan.html` — the whole file, all four tabs (Requirements,
   Verification, Solution, Plan) — and `context.md`
3. Verify prerequisites:
   - Both artifacts exist: `plan.html` and `context.md`
   - No unanswered Open Questions — every `<pd-question>` is `status="answered"`.
     pd-lint reports an open one as `open-question` with a non-zero exit; a clean
     lint is the gate
   - Every AC is covered by plan phases — each `<pd-ac>` in the Verification tab
     carries a non-empty `phases` attribute pointing at phases that exist
4. Create isolated worktree with branch `task/$ID-$NAME`
5. Parse the Plan tab for the execution phases: the `<pd-phase>` elements inside
   `<pd-stepper>`, each carrying `n`, `title`, `files`, `status`, and
   `depends-on`. `depends-on` is the dependency DAG — it is the structured data,
   not the mermaid diagram beside it
6. Find the first phase not yet `status="done"` (supports session resumption)

## Coordinator-Subagent Pattern

Dispatch one subagent per phase. No nesting beyond two levels.

**Dispatch each subagent unnamed.** If your subagent tool takes a `name` (or other
identity) parameter, omit it: a named agent is an addressable teammate whose report is
only delivered once your turn ends, and this coordinator runs as one long unattended
turn -- you would mark the phase complete having never read its result. Wait for each
completion notification; do not poll an agent-listing tool, and never ask a subagent to
resend its report.

### What each subagent receives

- Absolute worktree path (critical -- subagents do not inherit coordinator cwd)
- Task folder path (absolute)
- Phase number and name to execute
- Instructions:
  - Read `plan.html` (Plan tab for the phase steps, Solution and Verification tabs
    for the approach and the ACs it must satisfy) and `context.md` before starting
  - Identify and use relevant skills before coding
  - Only touch the files listed in that phase's `files` attribute -- don't "improve"
    adjacent code or refactor things that aren't broken
  - Match existing code style, even if you'd do it differently
  - State assumptions before coding. If the phase step is ambiguous, surface the ambiguity back to the coordinator rather than guessing
  - Never ask the user directly from a subagent; only the coordinator decides whether a question is worth stopping for
  - Fix routine failures (test bugs, type errors, lint) autonomously
  - Stop on fundamental issues (wrong architecture, missing prerequisites)
  - Leave `plan.html` alone -- the coordinator owns phase status
  - Commit at end: `feat($ID): phase N - description`

### What each subagent returns

- **Status**: pass or fail
- **Files changed**: list of paths
- **Verification results**: typecheck output, test summaries
- **Issues encountered**: even on pass

### Coordinator after each subagent

1. Read results
2. Update the phase in the Plan tab of `plan.html`: set `status="done"` on that
   `<pd-phase>` (it was `status="active"` while in flight). This is a plain
   attribute edit on the `<pd-phase n="N">` element
3. Commit: `docs($ID): mark phase N complete`
4. Decide next action:
   - Clean pass -> dispatch next phase (follow the `depends-on` DAG for parallelism)
   - Routine failure subagent couldn't fix -> attempt resolution
   - Fundamental failure -> stop, record what happened, skip to PR
   - Ambiguity a subagent surfaced -> resolve it from the task docs where they settle it, and record the choice. Ask the user only when the decision is load-bearing (schema, scope, anything destructive): this run is usually unattended in a background worker, where a question halts everything until a human answers it
5. Maintain running list of problems encountered

Set `status="active"` on a phase when you dispatch it, so an interrupted run leaves
a trace of where it got to.

### Parallel vs serial

- Follow the `depends-on` DAG from the Plan tab's `<pd-phase>` elements
- Independent phases can run in parallel when the DAG allows
- Core implementation phases -> serial to reduce conflicts
- When in doubt, go serial

## Session Resumption

If session ends mid-execution, a new session reads `plan.html` -- phases with
`status="done"` are finished, resume from the first phase that isn't. A phase left
`status="active"` was interrupted mid-flight: re-dispatch it. No additional state
tracking needed.

## Open PR

After all phases complete (or after stopping on failure), open a PR.

Before opening the PR, advance the task to `complete` — this is the worker
marking the work done. See [task-status.md](task-status.md): set `status="complete"`
on `<pd-doc>` in `plan.html`, and include it in the final docs commit.

See [template-pr-body.md](template-pr-body.md) for the PR body template.

## Address CI Feedback

After opening the PR, wait 5 minutes for CI and automated reviewers to post feedback, then run `/address-feedback` to resolve any threads they created.
