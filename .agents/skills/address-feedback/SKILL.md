---
name: address-feedback
description: "Works through all open PR review threads by fixing code, replying to reviewers, and resolving threads. Use when 'address feedback', 'fix PR comments', 'resolve review threads', or after a code review has left feedback on the PR."
---

# Address Feedback

Work through all open PR review threads: fix code, reply, resolve.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Input

Optional PR number or URL. If omitted, detect from current branch.

## Process

### Step 1: Find PR

```bash
gh pr view --json number,title,headRefName,baseRefName
gh repo view --json nameWithOwner
```

Extract `$OWNER`, `$REPO`, `$NUMBER` from the output. If no open PR on this branch, tell the user and stop.

### Step 2: Fetch unresolved threads

```bash
gh api graphql -f query='
{
  repository(owner: "$OWNER", name: "$REPO") {
    pullRequest(number: $NUMBER) {
      reviewThreads(first: 50) {
        nodes {
          id
          isResolved
          comments(first: 10) {
            nodes {
              databaseId
              body
              path
              line
              author { login }
              createdAt
            }
          }
        }
      }
    }
  }
}'
```

Filter to `isResolved: false` only.

### Step 3: Assess each thread

For each unresolved thread, classify:

- **Valid, fix needed**: real bug, missing edge case, convention violation, or clear improvement. Fix the code.
- **Valid, no code change**: question or concern addressed by explanation (e.g. "why did you do X?").
- **Invalid / already addressed**: concern doesn't apply, suggested change would make things worse, or already fixed. Explain why.

### Step 4: Fix code

Read files first, apply minimal targeted edits. Only change what the feedback requires -- don't "improve" adjacent code, comments, or formatting. Every changed line should trace directly to a review thread. After all fixes, run typecheck and relevant tests. Fix any failures before proceeding.

### Step 5: Reply to each thread

Reply to the **first comment** in each thread using its `databaseId`:

```bash
gh api repos/$OWNER/$REPO/pulls/$PR/comments/$COMMENT_ID/replies -f body="$REPLY"
```

- For fixes: briefly explain what changed and why
- For explanation-only: answer directly
- For invalid: be respectful, explain concisely

### Step 6: Resolve each thread

Use the GraphQL node `id` (e.g. `PRRT_kwDO...`) from step 2, **not** the `databaseId`:

```bash
gh api graphql -f query='
mutation {
  resolveReviewThread(input: {threadId: "$THREAD_ID"}) {
    thread { isResolved }
  }
}'
```

Resolve all threads -- including ones where you declined to make a change.

### Step 7: Push

```bash
git add -A && git commit -m "address PR feedback" && git push
```

Skip commit if no code changes were needed.

### Step 8: Summary

Report:
- How many threads were processed
- Which had code fixes vs explanation-only
- Any threads where you disagreed with the reviewer and why
- Whether typecheck/tests passed

## Rules

- Every thread gets a response (never silently skip)
- Keep replies short (1-3 sentences)
- Skip commit if no code changes were needed
