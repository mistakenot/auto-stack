# Execute Task — Mini Workflow

Mini-tasks have a single `plan.md` (with `workflow: mini` frontmatter) instead of separate requirements, solution, context, and plan files. The executor gathers context, designs the solution, implements, and self-reviews — all in one pass.

## Input

Task ID (numeric, e.g. `042`).

## Startup

1. Find task folder matching ID under `docs/tasks/` (glob `docs/tasks/$ID-*`)
2. Read `plan.md` — this is the only task doc
3. Verify: `plan.md` exists and has `workflow: mini` in frontmatter
4. Extract acceptance criteria and any existing context from the file
5. Create isolated worktree with branch `task/$ID-$NAME`

## Execution Steps

Follow the Executor Instructions checkboxes in plan.md. Update each checkbox as you complete it (`- [ ]` -> `- [x]`).

### Step 1: Context Gathering

Spawn a **team of subagents** to explore the codebase. You MUST use subagents for this — do not explore inline, as it would displace the plan.md content from your context.

Suggested subagent split:
- **Code subagent**: search for files, functions, types, and patterns relevant to the acceptance criteria
- **Docs subagent**: search project docs, READMEs, CLAUDE.md files for relevant conventions and constraints

Collect findings from all subagents. Write them into the **Context** section of plan.md, replacing the placeholder comment. Include only verified facts: file paths, code snippets, pattern descriptions grounded in actual code.

### Step 2: Solution Design

Based on the AC and gathered context, write the **Solution** section of plan.md:
- What approach you'll take
- Which files you'll create or modify
- How you'll verify each AC is met

Keep it concise — this is a mini-task, not a full design doc. One paragraph of approach + a file list is enough.

### Step 3: Checkpoint

Commit plan.md with the new Context and Solution sections:
```
docs($ID): context and solution
```

This checkpoint enables session resumption — if the session dies after this commit, a new session can read plan.md and skip straight to implementation.

### Step 4: Implementation

Implement the solution. You have flexibility here:
- For multi-file changes: use the coordinator-subagent pattern (one subagent per logical unit of work)
- For small changes: implement directly

Match existing code style. Fix routine failures (type errors, test bugs, lint) autonomously. Stop on fundamental issues.

Commit as you go: `feat($ID): description`

### Step 5: Self-Review

Spawn a sub-agent team to review your changes with fresh context. Each reviewer reads the diff cold — no prior context from this session. They look for correctness bugs only.

```bash
git diff main...HEAD
```

Fix any real issues found. Push fixes.

### Step 6: Open PR

Before opening the PR, advance the task to `complete` — this is the worker
marking the work done. See [task-status.md](task-status.md): set `status: complete`
in `plan.md` frontmatter, and include it in the final docs commit.

Push and open a PR.

See [template-pr-body.md](template-pr-body.md) for the PR body template.

### Step 7: Address CI Feedback

After opening the PR, wait 5 minutes for CI and automated reviewers to post feedback, then run `/address-feedback` to resolve any threads they created.

## Session Resumption

If session ends mid-execution, a new session reads `plan.md`:
- Checked executor instruction boxes indicate completed steps
- If Context and Solution sections are populated and committed, skip to Step 4
- If implementation commits exist on the branch, assess progress and continue
