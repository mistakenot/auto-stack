---
name: resolve-comments
description: "Resolves review comment threads — inline in planning docs/markdown/HTML or in .review.md sidecars — by fixing, rejecting, or escalating. Use when 'resolve comments', 'address comments', 'fix review comments', or after a review. Not for PR feedback (use address-feedback)."
---

# Resolve Comments

Process review comment threads -- inline in planning docs, markdown and HTML, or in `<file>.review.md` sidecars next to code and other files -- and resolve, reject, or continue each one.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Process

### Step 1: Find Files

Identify which files to process, in priority order:
1. Files or directories explicitly given (e.g. by a request-*-review skill in file mode) -- plus the `<file>.review.md` sidecar of each one, where the threads for non-markdown files live. A directory covers every file in it and every sidecar under it.
2. If given a task ID or task folder: all docs in `docs/tasks/$ID-*/`
3. Files already read/edited in the current session
4. If unclear, ask the user

A `--focus "..."` argument is the reviewer's lens; ignore it here.

### Step 2: Parse Threads

For each file, find all review comment threads: `<!-- STATUS(Pn): -->` blocks in markdown, plain HTML and sidecars, and `<pd-thread>` elements in pd-* HTML docs. Read the surrounding content to understand what each thread is about -- for a sidecar thread, that content is the target file at the thread's `LOCATION:` (the symbol or snippet, not just the line number, since lines drift).

### Step 3: Resolve Each Thread

For each comment thread, decide one of:

**Resolve** -- the concern is valid and you can fix it:
- Make the requested content change in the doc -- or, for a sidecar thread, in the target file itself. When the fix touches code, run the relevant tests or linters before marking it resolved
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
- Status changes and `AUTHOR:` replies go in the thread's own file: a sidecar thread is updated in the sidecar, never copied into the code

### Step 5: Summary

After processing all threads, report:
- How many resolved, rejected, continued unresolved
- Which files were changed (docs and code), and which sidecars hold the thread history
- Any threads that need user input to proceed

## Comment Format

- **Where threads live, sidecar format**: [references/review-targets.md](references/review-targets.md)
- **Markdown syntax** (`.md`, plain HTML, sidecars): [references/review-format.md](references/review-format.md)
- **`<pd-thread>` syntax** (pd-* HTML docs): [references/review-format-html.md](references/review-format-html.md)
