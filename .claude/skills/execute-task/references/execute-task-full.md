# Execute Task — Full Workflow

## Input

Task ID (numeric, e.g. `042`).

## Startup

1. Find task folder matching ID under `docs/tasks/` (glob `docs/tasks/$ID-*`)
2. Read ALL files: `requirements.md`, `solution.md`, `context.md`, `plan.md`
3. Verify prerequisites:
   - All 4 docs exist
   - No unanswered Open Questions (for HTML docs, no open `<pd-question>` — pd-lint reports `open-question` with a non-zero exit; a clean lint is the gate)
   - Every AC is covered by plan steps
4. Create isolated worktree with branch `task/$ID-$NAME`
5. Parse `plan.md` for phases, steps, and the Execution Sequence DAG
6. Find first unchecked phase (supports session resumption)

## Coordinator-Subagent Pattern

Dispatch one subagent per phase. No nesting beyond two levels.

### What each subagent receives

- Absolute worktree path (critical -- subagents do not inherit coordinator cwd)
- Task folder path (absolute)
- Phase number and name to execute
- Instructions:
  - Read `plan.md`, `context.md`, `solution.md` before starting
  - Identify and use relevant skills before coding
  - Only touch files listed in plan.md -- don't "improve" adjacent code or refactor things that aren't broken
  - Match existing code style, even if you'd do it differently
  - State assumptions before coding. If the plan step is ambiguous, surface the ambiguity back to the coordinator rather than guessing
  - Fix routine failures (test bugs, type errors, lint) autonomously
  - Stop on fundamental issues (wrong architecture, missing prerequisites)
  - Commit at end: `feat($ID): phase N - description`

### What each subagent returns

- **Status**: pass or fail
- **Files changed**: list of paths
- **Verification results**: typecheck output, test summaries
- **Issues encountered**: even on pass

### Coordinator after each subagent

1. Read results
2. Update `plan.md` checkboxes: `- [ ]` -> `- [x]`
3. Commit: `docs($ID): mark phase N complete`
4. Decide next action:
   - Clean pass -> dispatch next phase (follow DAG for parallelism)
   - Routine failure subagent couldn't fix -> attempt resolution
   - Fundamental failure -> stop, record what happened, skip to PR
5. Maintain running list of problems encountered

### Parallel vs serial

- Follow the Execution Sequence DAG from `plan.md`
- Independent phases can run in parallel when DAG allows
- Core implementation phases -> serial to reduce conflicts
- When in doubt, go serial

## Session Resumption

If session ends mid-execution, a new session reads `plan.md` -- checked phases are done, resume from first unchecked phase. No additional state tracking needed.

## Open PR

After all phases complete (or after stopping on failure), open a PR.

Before opening the PR, advance the task to `complete` — this is the worker
marking the work done. See [task-status.md](task-status.md): set `status="complete"`
on `<pd-doc>` in `plan.html`, and include it in the final docs commit.

See [template-pr-body.md](template-pr-body.md) for the PR body template.

## Address CI Feedback

After opening the PR, wait 5 minutes for CI and automated reviewers to post feedback, then run `/address-feedback` to resolve any threads they created.
