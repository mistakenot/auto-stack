---
name: review-resolve
description: Use when the user asks to "resolve comments", "address comments", "reply to comments", or "go through comments" in markdown planning docs. Resolve or respond to review threads using the project's authoritative comment format.
---

# Resolve Comments

Process inline comment threads in markdown files.

## Mandatory Format Source

Before writing or updating any comment thread, read:

- `docs/how-to/communicate-during-plan-reviews.md`

That doc is authoritative for status labels, roles, and thread structure. If anything in this skill conflicts with that doc, follow the doc.

## What to do

### Step 1: Find the files

Only process files directly relevant to the current request.

Use these signals in order:
1. Files explicitly mentioned by the user in this request
2. Files already read/edited in the current session
3. If unclear, ask the user which files to process

### Step 2: Read and parse each file

For each file, find review threads below the content they address.

### Step 3: Resolve or reply to each thread

For each comment thread, decide:

**Resolve:**
- Make the requested content change
- Reply in thread format from `docs/how-to/communicate-during-plan-reviews.md`
- Use `RESOLVED(Pn)` with an `AUTHOR:` response

**Reject:**
- Keep content unchanged
- Reply using `REJECTED(Pn)` with `AUTHOR:` rationale

**Continue unresolved:**
- Keep content unchanged (unless partial fix is appropriate)
- Reply using `UNRESOLVED(Pn)` with `AUTHOR:` follow-up/question/blocker

### Step 4: Preserve thread history

- Keep comments append-only
- Do not delete comment history
- Keep one thread per issue
- Keep priority level (`P1/P2/P3`) consistent with the original issue unless the thread clearly reclassifies it

### Step 5: Legacy comments

If you encounter older `DONE:` comment style, leave existing history intact unless the user asks to migrate it. New replies should follow `docs/how-to/communicate-during-plan-reviews.md`.

## Style notes

- When resolving, make minimal edits - add what's asked, don't restructure unrelated content
- When replying, be specific: ask exactly what you need to proceed
- After processing all threads, briefly summarize: how many resolved, how many need replies, which files were changed
