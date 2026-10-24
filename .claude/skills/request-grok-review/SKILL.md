---
name: request-grok-review
description: "Runs a Grok CLI review of task planning docs or any files (code, markdown, config), then resolves its comments. Use when 'request grok review', 'grok review task 042', 'have grok review <file>'."
---

# Request Grok Review

Have Grok CLI review a target in a fresh headless session, then resolve the comment threads it leaves.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Input

- **Target** — one of:
  - a **task ID** (`042`) or task folder (`042-add-team-settings`, `docs/tasks/042-…`) → **task mode**, the usual case: the task's planning docs, reviewed as a set.
  - one or more **file or directory paths** (code, markdown, HTML, config) → **file mode**. Directories expand to their non-ignored files.
- **Focus** (optional, file mode) — what to look for, e.g. `"error handling"`.
- The repository root path (absolute) -- use the current working directory if already at root.

Threads land inline in markdown and HTML, and in a `<file>.review.md` sidecar next to any other file — see [references/review-targets.md](references/review-targets.md).

## Process

### Step 1: Resolve the Targets

```bash
cd "<ABSOLUTE_REPO_ROOT>" || exit 1
CWD=$(pwd)
ARGS=(<TARGET> ...)   # a task ID / task folder, or file and directory paths
FOCUS=""              # optional file-mode focus, e.g. "error handling"
shopt -s nullglob

# Task mode: a single argument naming a task under docs/tasks/.
TASK_DIR=""
if [ "${#ARGS[@]}" -eq 1 ]; then
    A="${ARGS[0]%/}"
    if [[ "$A" =~ ^[0-9]+$ ]]; then
        TASK_DIR=$(find "$CWD/docs/tasks" -maxdepth 1 -type d -name "${A}-*" | head -1)
        [ -n "$TASK_DIR" ] || { echo "no task folder found for ID $A"; exit 1; }
    elif [ -d "$CWD/docs/tasks/$A" ]; then
        TASK_DIR="$CWD/docs/tasks/$A"
    elif [ -d "$A" ] && [ "$(realpath "$A/..")" = "$(realpath "$CWD/docs/tasks" 2>/dev/null)" ]; then
        TASK_DIR=$(realpath "$A")
    fi
fi

TARGETS=()
if [ -n "$TASK_DIR" ]; then
    MODE=task
    SLUG=$(basename "$TASK_DIR")
    REVIEW_ARGS="$SLUG"
    TARGETS=("$TASK_DIR"/*.md "$TASK_DIR"/*.html)
else
    MODE=files
    for A in "${ARGS[@]}"; do
        if [ -d "$A" ]; then
            while IFS= read -r F; do TARGETS+=("$F"); done < <(rg --files "$A" | grep -v '\.review\.md$' | sort)
        elif [ -f "$A" ]; then
            TARGETS+=("$A")
        else
            echo "target not found: $A"; exit 1
        fi
    done
    [ "${#TARGETS[@]}" -gt 0 ] || { echo "no files to review"; exit 1; }
    SLUG="files-$(basename "${TARGETS[0]}" | tr -c 'A-Za-z0-9._\n' '-')-$(printf '%s\n' "${TARGETS[@]}" | sha1sum | cut -c1-8)"
    REVIEW_ARGS="${ARGS[*]}${FOCUS:+ --focus \"$FOCUS\"}"
fi

# Comment sinks: markdown and HTML take threads inline; any other file gets a
# <file>.review.md sidecar (it may not exist until the reviewer writes it).
SINKS=()
for F in "${TARGETS[@]}"; do
    case "$F" in
        *.md|*.markdown|*.html|*.htm) SINKS+=("$F") ;;
        *) SINKS+=("$F.review.md") ;;
    esac
done
SCOPE_FILE="/tmp/review-scope-$SLUG.txt"
printf '%s\n' "${SINKS[@]}" > "$SCOPE_FILE"
echo "mode=$MODE slug=$SLUG targets=${#TARGETS[@]} scope=$SCOPE_FILE"
echo "review args: $REVIEW_ARGS"
```

Later blocks use `CWD`, `MODE`, `SLUG`, `REVIEW_ARGS` and `SCOPE_FILE` from this
block. If your shell does not keep state between calls, set them from its output.

### Step 2: Run Grok Review

Invoke Grok in headless mode on the `review-task` skill:

```bash
LAST_MSG_FILE="/tmp/grok-$SLUG-review.txt"
LOG_FILE="/tmp/grok-$SLUG-review.log"

grok \
    --cwd "$CWD" \
    --permission-mode bypassPermissions \
    --always-approve \
    --single "/review-task $REVIEW_ARGS" \
    2>&1 | tee "$LOG_FILE" > "$LAST_MSG_FILE"
REVIEW_EXIT=${PIPESTATUS[0]}
echo "grok exit code: $REVIEW_EXIT"
```

**No `< /dev/null` redirect is needed.** Unlike `codex exec` and `claude -p`, Grok
headless mode does not read piped stdin into the prompt — it uses the `--single`
value directly and does not block on an inherited, never-closing stdin when
launched in the background.

`--permission-mode bypassPermissions` and `--always-approve` are both required for
unattended runs so Grok can read and edit files without prompting.

`--single` takes the prompt as its immediate value; it is not a boolean headless
flag. Do not place other flags between `--single` (or `-p`) and the prompt.

### Step 3: Check for Comments

Count the threads Grok left across every sink (inline docs and sidecars):

```bash
mapfile -t SINKS < "$SCOPE_FILE"
MD_COUNT=$(rg -o "<!-- (UNRESOLVED|RESOLVED|REJECTED)\(P[123]\):" "${SINKS[@]}" 2>/dev/null | wc -l)
HTML_COUNT=$(rg -o "<pd-thread " "${SINKS[@]}" 2>/dev/null | wc -l)
COMMENT_COUNT=$((MD_COUNT + HTML_COUNT))
echo "review comment count: $COMMENT_COUNT (md=$MD_COUNT html=$HTML_COUNT)"
```

### Step 4: Handle Failure

If Grok exited non-zero or left no comments, inspect the log to diagnose:

```bash
if [ "$REVIEW_EXIT" -ne 0 ] || [ "$COMMENT_COUNT" -eq 0 ]; then
    echo "grok failed or no comments found; inspect log output"
    tail -n 200 "$LOG_FILE"
fi
```

If the log contains `unexpected argument` (or `unknown option`), the installed `grok` has renamed or
dropped a flag this skill passes (it has happened before: codex's `--full-auto` became
`--sandbox workspace-write`). Check `--help` for the current spelling, and if this is a
vendored copy of the skill, run `auto skill update` first — the upstream copy may already
carry the fix.

Stop and report the failure to the user. Do not proceed to resolution.

### Step 5: Resolve Comments

**Mandatory**, in both modes. After a successful review with comments, invoke `resolve-comments` in the current agent context on the same target:

```
/resolve-comments $REVIEW_ARGS
```

In file mode this edits the target files themselves — that is the point: sidecar threads are fixed in the code they point at.

Do NOT resolve feedback by manually editing or deleting comment threads. Comment handling is append-only per the review format conventions.

### Step 6: Verify Resolution

Confirm that `resolve-comments` ran by checking for author replies across every sink:

```bash
mapfile -t SINKS < "$SCOPE_FILE"
MD_REPLIES=$(rg -o "AUTHOR:" "${SINKS[@]}" 2>/dev/null | wc -l)
HTML_REPLIES=$(rg -o 'by="author"' "${SINKS[@]}" 2>/dev/null | wc -l)
AUTHOR_REPLY_COUNT=$((MD_REPLIES + HTML_REPLIES))
echo "author reply count: $AUTHOR_REPLY_COUNT"
if [ "$AUTHOR_REPLY_COUNT" -eq 0 ]; then
    echo "resolve-comments was not applied; run /resolve-comments $REVIEW_ARGS"
fi
```

### Step 7: Strip Closed Threads from Regular Docs

Plan files keep every thread as their decision log. A regular doc (a README, guide, or plain HTML page) should not carry review history, so once resolution is verified, remove its RESOLVED and REJECTED threads:

```bash
bash <skill-dir>/scripts/strip-closed-threads.sh "$SCOPE_FILE"
```

The script skips plan files (anything under `docs/tasks/`, and pd-* HTML docs) and `.review.md` sidecars, and always keeps UNRESOLVED threads — they still need an answer. Run it only after Step 6, which counts the replies it removes.

### Step 8: Report

Summarize:
- Whether the Grok review succeeded, and the mode (task / files)
- Comment count by priority (P1/P2/P3)
- How many threads were resolved, rejected, or left unresolved
- Which files were changed, which regular docs had closed threads stripped, and any `.review.md` sidecars created (the user decides whether to keep or delete them)
- Any threads needing user input

## Comment Format

- **Which file gets the thread**: [references/review-targets.md](references/review-targets.md)
- **Markdown syntax** (`.md`, plain HTML, sidecars): [references/review-format.md](references/review-format.md)
- **`<pd-thread>` syntax** (pd-* HTML docs): [references/review-format-html.md](references/review-format-html.md)
