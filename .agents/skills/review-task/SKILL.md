---
name: review-task
description: "Reviews task planning documents (requirements.md, solution.md, context.md, plan.md) and leaves structured inline comments flagging problems and improvements. Use when 'review task docs', 'review the plan', 'review task 042', 'check the planning docs', or a task ID/folder for doc review. Not applicable for code review of implementation changes."
---

# Review Task Docs

Review all planning documents for a task and leave structured inline comments flagging problems, questions, and improvements. This is a planning review -- you use the full codebase as ground truth to verify claims in the docs.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Input

The user provides a task ID (e.g. `042`) or a task folder name (e.g. `042-add-team-settings`). If ambiguous, check `docs/tasks/` for a matching folder.

## Process

### Step 1: Load Context

1. Find the task folder under `docs/tasks/` matching the provided ID
2. Read ALL docs: `requirements.md`, `solution.md`, `context.md`, `plan.md`
3. Read any project docs referenced by the task docs (linked concept docs, how-to guides)

### Step 2: Codebase Verification

For each doc, verify claims against the actual codebase:

**requirements.md**
- Are acceptance criteria testable and unambiguous?
- Do referenced features/pages actually exist?
- Are there implicit dependencies not mentioned?
- Does "Out of Scope" make sense, or will the task be incomplete without those items?

**solution.md**
- Do listed file paths exist (for `~` modified files) or make sense as new files (`+`)?
- Do referenced types, functions, and services have the described signatures?
- Does the approach match established project patterns?
- Are there security concerns -- auth, tenant isolation, input validation?
- Does the test coverage table cover all acceptance criteria?

**context.md**
- Are code snippets accurate to the current state of the files?
- Are line number references correct?
- Are important related files or patterns missing?

**plan.md**
- Will the execution sequence work? Are phase dependencies correct?
- Are commands correct (test paths, npm scripts, workspace flags)?
- Do success criteria verify all acceptance criteria?
- Are there missing steps the implementer will need to figure out?

### Step 3: Cross-Document Consistency

- Every AC in requirements.md maps to test coverage in solution.md and plan steps in plan.md
- File paths are consistent across solution.md, context.md, and plan.md
- Types/interfaces in context.md match what solution.md proposes to use
- The approach in solution.md matches the plan steps in plan.md

### Step 4: Leave Comments

Insert comments directly into the task docs using the Edit tool. Place each comment directly below the content it addresses, with a blank line above and below.

**You are a reviewer. Your only edit action is inserting comment blocks.** Do NOT change the author's content -- describe issues in comments, and the author resolves them.

Use tools (grep, glob, read, bash) to gather evidence before commenting. Comments backed by "I checked the file and the signature is actually X" are far more valuable than "this might be wrong."

Only comment on actual problems, genuine ambiguities, or missing information. Do not comment on formatting, correct content, or style preferences.

**If no issues are found:** insert a single clean-review comment at the top of `plan.md` (below the title):

```markdown
<!-- RESOLVED(P3): Review complete — no issues found
REVIEW: All planning documents reviewed against the codebase. No problems, inconsistencies, or missing information detected.
-->
```

This ensures the calling agent can distinguish a successful clean review from a failed review that produced no output.

### Step 5: Summary

After leaving all comments, provide:
- Total comment count by priority (P1/P2/P3)
- The most critical issues to resolve before implementation
- Overall assessment: ready to execute, or needs another revision?

## Comment Format

See [references/review-format.md](references/review-format.md) for comment syntax, priority levels, and rules.
