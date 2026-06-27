---
name: review-task
description: "Reviews task planning documents (markdown or HTML) and leaves structured inline comments flagging problems and improvements. Use when 'review task docs', 'review the plan', 'review task 042', 'check the planning docs', or a task ID/folder for doc review. Not applicable for code review of implementation changes."
---

# Review Task Docs

Review all planning documents for a task and leave structured inline comments flagging problems, questions, and improvements. This is a planning review -- you use the full codebase as ground truth to verify claims in the docs.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Input

The user provides a task ID (e.g. `042`) or a task folder name (e.g. `042-add-team-settings`). If ambiguous, check `docs/tasks/` for a matching folder.

## Process

### Step 1: Load Context

1. Find the task folder under `docs/tasks/` matching the provided ID.
2. Identify the doc layout and read **every** planning doc present. A folder uses one layout or the other, not both:
   - **HTML task** — `plan.html` + `context.md`. `plan.html` is a single self-contained file with one `<pd-tab>` per stage: **Requirements**, **Verification**, **Solution**, **Plan**. Read the full HTML source; sections carry stable kebab-case `id`s that you anchor comments to.
   - **HTML epic** — `epic.html`. Review it the same way, treating its direction / guard-rail / task-breakdown tabs as the units to verify.
3. Read any project docs referenced by the task docs (linked concept docs, how-to guides).

### Step 2: Codebase Verification

Verify each concern against the actual codebase. The same concerns live in different places depending on layout:

| Concern | Markdown file | HTML location (`plan.html`) |
| --- | --- | --- |
| Problem, goals, scope | requirements.md | Requirements tab |
| Acceptance criteria & test coverage | requirements.md (ACs) + solution.md (coverage table) | Verification tab (`<pd-ac>` cards) |
| Approach & file changes | solution.md | Solution tab (`<pd-files>`, `<pd-api>`) |
| Codebase snippets / references | context.md | context.md |
| Execution sequence | plan.md | Plan tab (`<pd-stepper>` / `<pd-phase>`) |

**Requirements / scope** (requirements.md · Requirements tab)
- Are acceptance criteria testable and unambiguous?
- Do referenced features/pages actually exist?
- Are there implicit dependencies not mentioned?
- Does "Out of Scope" make sense, or will the task be incomplete without those items?

**Solution & approach** (solution.md · Solution tab)
- Do listed file paths exist (for `~`/`edit` modified files) or make sense as new files (`+`/`add`)?
- Do referenced types, functions, and services have the described signatures?
- Does the approach match established project patterns?
- Are there security concerns -- auth, tenant isolation, input validation?

**Verification & test coverage** (solution.md coverage table · Verification tab)
- Does the test coverage cover all acceptance criteria?
- For HTML: does each `<pd-ac>` carry accurate `phases`/`tests` traceability chips, not placeholders?

**Context** (context.md)
- Are code snippets accurate to the current state of the files?
- Are line number references correct?
- Are important related files or patterns missing?

**Plan** (plan.md · Plan tab)
- Will the execution sequence work? Are phase dependencies correct?
- Are commands correct (test paths, npm scripts, workspace flags)?
- Do success criteria verify all acceptance criteria?
- Are there missing steps the implementer will need to figure out?

### Step 3: Cross-Document Consistency

- Every acceptance criterion traces to test coverage **and** to plan steps. (Markdown: solution.md coverage table + plan.md steps. HTML: each `<pd-ac>`'s `tests`/`phases` chips → matching `<pd-phase>` in the Plan tab.)
- File paths are consistent across approach, context, and plan. (Markdown: solution.md / context.md / plan.md. HTML: Solution-tab `<pd-files>` vs Plan-tab `<pd-phase files>`.)
- Types/interfaces in context.md match what the solution proposes to use.
- The approach (solution) matches the execution sequence (plan).
- Epic linkage is consistent. (Markdown: if any doc has `epic:` frontmatter, all four must share the same value; if requirements.md has none, none should. HTML: if the task claims an epic, the `pd-meta` block / epic reference must agree across the doc.)

### Step 4: Leave Comments

You are a reviewer. **Your only edit action is inserting comment threads.** Do NOT change the author's content -- describe issues in comments, and the author resolves them.

Use tools (grep, glob, read, bash) to gather evidence before commenting. Comments backed by "I checked the file and the signature is actually X" are far more valuable than "this might be wrong."

Only comment on actual problems, genuine ambiguities, or missing information. Do not comment on formatting, correct content, or style preferences.

Match the comment syntax to the file you are editing -- see the **Comment Format** section below. In both layouts threads are **append-only**: never edit or delete an existing comment.

- **Markdown docs**: insert the comment block directly below the content it addresses, with a blank line above and below.
- **HTML docs**: insert a `<pd-thread>` directly after the anchored element, with its `anchor` set to that element's `id` (or a `<pd-file>`'s `path`), inside the relevant tab. This keeps the doc compatible with the planning-doc workflow, which renders threads in place and surfaces resolved ones in `<pd-decisions>`.

**If no issues are found:** insert a single clean-review comment so the calling agent can distinguish a successful clean review from a failed review that produced no output.

For markdown task folders, insert at the top of `plan.md` (below the title):

```markdown
<!-- RESOLVED(P3): Review complete — no issues found
REVIEW: All planning documents reviewed against the codebase. No problems, inconsistencies, or missing information detected.
-->
```

For HTML planning docs, insert after the first `<pd-section>`:

```html
<pd-thread anchor="first-section-id" status="resolved" priority="p3" title="Review complete — no issues found">
  <pd-comment by="review">All planning documents reviewed against the codebase. No problems, inconsistencies, or missing information detected.</pd-comment>
</pd-thread>
```

### Step 5: Summary

After leaving all comments, provide:
- Total comment count by priority (P1/P2/P3)
- The most critical issues to resolve before implementation
- Overall assessment: ready to execute, or needs another revision?

## Comment Format

Determine the format from the file extension:
- **Markdown files** (`.md`): See [references/review-format.md](references/review-format.md)
- **HTML files** (`.html`): See [references/review-format-html.md](references/review-format-html.md)
