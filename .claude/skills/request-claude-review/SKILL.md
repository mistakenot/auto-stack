---
name: request-claude-review
description: "Runs a code review of task planning docs via Claude Code, then resolves any comments left. Use when 'request claude review', 'get claude review', 'claude review task', 'have claude review', or when a second pair of eyes is needed on task docs before execution."
---

# Request Claude Review

Send task planning docs to Claude Code for review, then resolve any comments it leaves.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Input

- **Task ID** (numeric, e.g. `042`)
- The repository root path (absolute) -- use the current working directory if already at root

## Process

### Step 1: Run Claude Review

Invoke Claude Code in print mode to review the task docs:

```bash
CWD="<ABSOLUTE_REPO_ROOT>"
TASK_ID="<TASK_ID>"
TASK_DIR=$(find "$CWD/docs/tasks" -maxdepth 1 -type d -name "${TASK_ID}-*" | head -1)
if [ -z "$TASK_DIR" ]; then
    echo "no task folder found for ID $TASK_ID"
    exit 1
fi
TASK_NAME=$(basename "$TASK_DIR")
LAST_MSG_FILE="/tmp/claude-$TASK_ID-review.txt"
LOG_FILE="/tmp/claude-$TASK_ID-review.log"

claude \
    --add-dir "$CWD" \
    -p \
    --dangerously-skip-permissions \
    "/review-task $TASK_NAME" \
    < /dev/null \
    2>&1 | tee "$LOG_FILE" > "$LAST_MSG_FILE"
CLAUDE_EXIT=${PIPESTATUS[0]}
echo "claude exit code: $CLAUDE_EXIT"
```

**`< /dev/null` is required.** In print mode (`-p`) with an inherited, open stdin
(as in a background launch) Claude waits several seconds for stdin data before
proceeding (`no stdin data received in 3s, proceeding without it`). Redirecting
from `/dev/null` gives an immediate EOF so the review starts without the stall.

### Step 2: Check for Comments

Count review comments Claude left in the task docs (check both markdown and HTML formats):

```bash
MD_COUNT=$(rg -cn "<!-- (UNRESOLVED|RESOLVED|REJECTED)\(P[123]\):" "$TASK_DIR"/*.md 2>/dev/null | awk -F: '{s+=$2}END{print s+0}')
HTML_COUNT=$(rg -cn "<pd-thread " "$TASK_DIR"/*.html 2>/dev/null | awk -F: '{s+=$2}END{print s+0}')
COMMENT_COUNT=$((MD_COUNT + HTML_COUNT))
echo "review comment count: $COMMENT_COUNT (md=$MD_COUNT html=$HTML_COUNT)"
```

### Step 3: Handle Failure

If Claude exited non-zero or left no comments, inspect the log to diagnose:

```bash
if [ "$CLAUDE_EXIT" -ne 0 ] || [ "$COMMENT_COUNT" -eq 0 ]; then
    echo "claude failed or no comments found; inspect log output"
    tail -n 200 "$LOG_FILE"
fi
```

Stop and report the failure to the user. Do not proceed to resolution.

### Step 4: Resolve Comments

**Mandatory.** After a successful review with comments, invoke `resolve-comments` in the current agent context:

```
/resolve-comments $TASK_ID
```

Do NOT resolve feedback by manually editing or deleting comment threads. Comment handling is append-only per the review format conventions.

### Step 5: Verify Resolution

Confirm that `resolve-comments` ran by checking for author replies (both formats):

```bash
MD_REPLIES=$(rg -cn "AUTHOR:" "$TASK_DIR"/*.md 2>/dev/null | awk -F: '{s+=$2}END{print s+0}')
HTML_REPLIES=$(rg -cn 'by="author"' "$TASK_DIR"/*.html 2>/dev/null | awk -F: '{s+=$2}END{print s+0}')
AUTHOR_REPLY_COUNT=$((MD_REPLIES + HTML_REPLIES))
echo "author reply count: $AUTHOR_REPLY_COUNT"
if [ "$AUTHOR_REPLY_COUNT" -eq 0 ]; then
    echo "resolve-comments was not applied; run /resolve-comments $TASK_ID"
fi
```

### Step 6: Report

Summarize:
- Whether Claude review succeeded
- Comment count by priority (P1/P2/P3)
- How many threads were resolved, rejected, or left unresolved
- Any threads needing user input

## Comment Format

Determine the format from the file extension:
- **Markdown files** (`.md`): See [references/review-format.md](references/review-format.md)
- **HTML files** (`.html`): See [references/review-format-html.md](references/review-format-html.md)
