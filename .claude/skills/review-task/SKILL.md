---
name: review-task
description: "Reviews task planning docs or any files (code, markdown, config) and leaves structured comment threads. Use when 'review task docs', 'review the plan', 'review task 042', 'review <file> and leave comments'. Not for PR/diff review (use code-review)."
---

# Review

Review a target and leave structured comment threads flagging problems, questions, and improvements. The full codebase is your ground truth: verify every claim the target makes.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Input

`/review-task <target...> [--focus "<what to look for>"]`

| Target | Mode |
| --- | --- |
| One task ID (`042`), task folder name (`042-add-team-settings`), or `docs/tasks/<folder>` | **task** — the task's planning docs, reviewed as a set |
| Anything else: file paths and/or directories | **files** — each file on its own terms; directories expand to their non-ignored files |

`--focus` narrows file mode to what the caller cares about. If a single argument is neither a task nor an existing path, check `docs/tasks/` for a near match, then ask.

## Modes

Read the reference for your mode before reviewing — it holds the checklist:

- **task** → [references/review-plan.md](references/review-plan.md): load the docs, verify against the codebase, check cross-document consistency, place threads.
- **files** → [references/review-files.md](references/review-files.md): expand targets, pick a lens per file kind, check cross-file consistency, assign priorities. A pd-* planning doc among the targets also uses the review-plan.md checklist.

In both modes, [references/review-targets.md](references/review-targets.md) decides **where** each thread goes (inline, or a `<file>.review.md` sidecar) — read it before writing the first thread.

## Rules

You are a reviewer. **Your only edit action is inserting comment threads.** Never change the target's content — not docs, not code. Describe the issue; the author resolves it.

- Use tools (grep, glob, read, bash) to gather evidence before commenting. "I checked the file and the signature is actually X" is worth far more than "this might be wrong."
- Only comment on actual problems, genuine ambiguities, or missing information — not formatting, correct content, or style preferences.
- One thread per issue. Threads are **append-only**: never edit or delete an existing comment.
- Match the syntax to the sink — see **Comment Format** below.

**If no issues are found:** leave the single clean-review thread described in review-targets.md, so the caller can tell a clean review from a failed one that produced no output.

## Summary

After leaving all comments, provide:
- Total comment count by priority (P1/P2/P3), and which files (and sidecars) received threads
- The most critical issues to resolve
- Overall assessment: ready (to execute, for a task; to ship, for files), or needs another revision?

## Comment Format

- **Which file gets the thread**: [references/review-targets.md](references/review-targets.md)
- **Markdown syntax** (`.md`, plain HTML, sidecars): [references/review-format.md](references/review-format.md)
- **`<pd-thread>` syntax** (pd-* HTML docs): [references/review-format-html.md](references/review-format-html.md)
