---
name: request-codex-review
description: "Runs a code review of task planning docs via Codex, then resolves any comments left. Use when 'request codex review', 'get codex review', 'codex review task', 'have codex review', or when a second pair of eyes is needed on task docs before execution."
---

# Request Codex Review

Send task planning docs to Codex for review, then resolve any comments it leaves.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Input

- **Task ID** (numeric, e.g. `042`)
- The repository root path (absolute) -- use the current working directory if already at root

## Process

### Step 1: Run Codex Review

Invoke Codex in full-auto mode to review the task docs:

```bash
CWD="<ABSOLUTE_REPO_ROOT>"
TASK_ID="<TASK_ID>"
TASK_DIR=$(find "$CWD/docs/tasks" -maxdepth 1 -type d -name "${TASK_ID}-*" | head -1)
if [ -z "$TASK_DIR" ]; then
    echo "no task folder found for ID $TASK_ID"
    exit 1
fi
TASK_NAME=$(basename "$TASK_DIR")
LAST_MSG_FILE="/tmp/codex-$TASK_ID-review.txt"
LOG_FILE="/tmp/codex-$TASK_ID-review.log"

codex exec \
    --cd "$CWD" \
    --full-auto \
    -o "$LAST_MSG_FILE" \
    "\$review-task $TASK_NAME" \
    2>&1 | tee "$LOG_FILE" >/dev/null
CODEX_EXIT=${PIPESTATUS[0]}
echo "codex exit code: $CODEX_EXIT"
```

### Step 2: Check for Comments

Count review comments Codex left in the task docs:

```bash
COMMENT_COUNT=$(rg -n "<!-- (UNRESOLVED|RESOLVED|REJECTED)\(P[123]\):" "$TASK_DIR"/*.md | wc -l)
echo "review comment count: $COMMENT_COUNT"
```

### Step 3: Handle Failure

If Codex exited non-zero or left no comments, inspect the log to diagnose:

```bash
if [ "$CODEX_EXIT" -ne 0 ] || [ "$COMMENT_COUNT" -eq 0 ]; then
    echo "codex failed or no comments found; inspect log output"
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

Confirm that `resolve-comments` ran by checking for author replies:

```bash
AUTHOR_REPLY_COUNT=$(rg -n "AUTHOR:" "$TASK_DIR"/*.md | wc -l)
echo "author reply count: $AUTHOR_REPLY_COUNT"
if [ "$AUTHOR_REPLY_COUNT" -eq 0 ]; then
    echo "resolve-comments was not applied; run /resolve-comments $TASK_ID"
fi
```

### Step 6: Report

Summarize:
- Whether Codex review succeeded
- Comment count by priority (P1/P2/P3)
- How many threads were resolved, rejected, or left unresolved
- Any threads needing user input

## Comment Format

See [references/review-format.md](references/review-format.md) for comment syntax, priority levels, and rules.
