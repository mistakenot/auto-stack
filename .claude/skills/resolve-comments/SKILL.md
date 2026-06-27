---
name: resolve-comments
description: "Resolves inline review comment threads in planning docs (markdown or HTML) by fixing content, rejecting invalid concerns, or escalating blockers. Use when 'resolve comments', 'address comments', 'fix review comments', 'go through comments', or after a review has left comments on task docs. Not applicable for PR code review feedback (use address-feedback instead)."
---

# Resolve Comments

Process inline comment threads in planning docs (markdown or HTML) -- resolve, reject, or continue each thread.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Process

### Step 1: Find Files

Identify which files to process, in priority order:
1. Files explicitly mentioned by the user
2. If user gave a task ID: all docs in `docs/tasks/$ID-*/`
3. Files already read/edited in the current session
4. If unclear, ask the user

### Step 2: Parse Threads

For each file, find all review comment threads (HTML comments with status labels). Read the surrounding content to understand what each thread is about.

### Step 3: Resolve Each Thread

For each comment thread, decide one of:

**Resolve** -- the concern is valid and you can fix it:
- Make the requested content change in the doc
- Update the comment status to `RESOLVED`
- Add an `AUTHOR:` reply explaining what was changed

**Reject** -- the concern doesn't apply or the current content is correct:
- Keep content unchanged
- Update the comment status to `REJECTED`
- Add an `AUTHOR:` reply with rationale (cite evidence when possible)

**Continue unresolved** -- you need more information or can't resolve alone:
- Keep content unchanged (or make a partial fix)
- Keep the comment as `UNRESOLVED`
- Add an `AUTHOR:` reply with your question or blocker

### Step 4: Preserve Thread History

- Comments are append-only -- never delete prior entries in a thread
- Keep one thread per issue
- Maintain the original priority level unless the thread clearly reclassifies it

### Step 5: Summary

After processing all threads, report:
- How many resolved, rejected, continued unresolved
- Which files were changed
- Any threads that need user input to proceed

## Comment Format

Determine the format from the file extension:
- **Markdown files** (`.md`): See [references/review-format.md](references/review-format.md)
- **HTML files** (`.html`): See [references/review-format-html.md](references/review-format-html.md)
