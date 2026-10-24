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
