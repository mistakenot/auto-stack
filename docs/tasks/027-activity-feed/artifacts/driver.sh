#!/usr/bin/env bash
# Conformance driver for task 027 — Activity Feed.
# Usage: bash driver.sh [worktree-root]
#   worktree-root defaults to the git toplevel of the script's location.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="${1:-$(cd "$SCRIPT_DIR/../../../.." && pwd)}"
EVIDENCE="$SCRIPT_DIR/evidence"
mkdir -p "$EVIDENCE"

PASS=0; FAIL=0
check() {
  local label="$1" ok="$2" detail="$3"
  if [ "$ok" = "true" ]; then
    echo "PASS  $label :: $detail"
    PASS=$((PASS+1))
  else
    echo "FAIL  $label :: $detail"
    FAIL=$((FAIL+1))
  fi
}

# ── fixture ──
TMP=$(mktemp -d)
trap 'agent-browser close --all 2>/dev/null; kill %1 2>/dev/null; find "$TMP" -delete 2>/dev/null' EXIT

mkdir -p "$TMP/proj-a/docs/tasks/001-demo" \
         "$TMP/proj-a/docs/tasks/002-second" \
         "$TMP/proj-a/docs/research"
printf '# Demo plan\nhello\n'  > "$TMP/proj-a/docs/tasks/001-demo/plan.md"
printf '# Demo reqs\n'         > "$TMP/proj-a/docs/tasks/001-demo/requirements.md"
printf '# Second\n'            > "$TMP/proj-a/docs/tasks/002-second/plan.md"
printf '# Research\n'          > "$TMP/proj-a/docs/research/notes.md"
cat > "$TMP/projects.json" <<JSON
{"projects":[
  {"id":"proj-a","name":"Project A","path":"$TMP/proj-a","remote":"https://github.com/x/a.git"}
]}
JSON

# ── build ──
echo "building binary from $ROOT ..."
cd "$ROOT/auto-cli" && go build -o /tmp/auto-027 ./cmd/auto
BIN=/tmp/auto-027

# ── launch ──
( cd "$ROOT/auto-ui" && \
  AUTO_UI_DEBUG=1 "$BIN" ui serve --port 0 --ready-file "$TMP/ready.json" --projects "$TMP/projects.json" ) &
# wait for ready file
for i in $(seq 1 30); do [ -f "$TMP/ready.json" ] && break; sleep 0.2; done
ADDR=$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1])).addr)' "$TMP/ready.json")
PORT=${ADDR##*:}
echo "server at $ADDR (port $PORT)"

URL="http://$ADDR/?debug=1#/explore?project=proj-a"

# ── open browser ──
agent-browser open "$URL" --headless 2>/dev/null
sleep 1.5

echo "================ activity-feed conformance ($(date -u +%FT%TZ)) ================"
echo "ADDR=$ADDR PORT=$PORT BIN=$BIN"

# ── AC-1: feed appears after doc.changed ──
echo "---- AC-1 feed appears after doc.changed ----"

# Confirm feed does NOT exist before any event
FEED_BEFORE=$(agent-browser eval 'document.querySelector("[data-testid=activity-feed]") ? "yes" : "no"' 2>/dev/null | grep -o 'yes\|no' || echo "no")
echo "AC-1 activity-feed before emit = $FEED_BEFORE"
check "AC-1 no feed before events" "$([ "$FEED_BEFORE" = "no" ] && echo true || echo false)" "$FEED_BEFORE"

# Emit a doc.changed
EMIT1=$("$BIN" ui emit --port "$PORT" --project proj-a --path docs/tasks/001-demo/plan.md 2>&1)
echo "emit 1: $EMIT1"
sleep 1.5

# Poll for feed
FEED_AFTER=$(agent-browser eval 'document.querySelector("[data-testid=activity-feed]") ? "yes" : "no"' 2>/dev/null | grep -o 'yes\|no' || echo "no")
ITEM_COUNT=$(agent-browser eval 'document.querySelectorAll("[data-testid=activity-item]").length' 2>/dev/null | grep -o '[0-9]*' || echo "0")
ITEM_PATH=$(agent-browser eval 'const el = document.querySelector("[data-testid=activity-item]"); el ? el.getAttribute("data-activity-path") : ""' 2>/dev/null | tr -d '\n' || echo "")
echo "AC-1 activity-feed after emit = $FEED_AFTER, item count = $ITEM_COUNT"
check "AC-1 feed appears" "$([ "$FEED_AFTER" = "yes" ] && echo true || echo false)" "feed=$FEED_AFTER"
check "AC-1 one item" "$([ "$ITEM_COUNT" = "1" ] && echo true || echo false)" "count=$ITEM_COUNT"
echo "AC-1 item path = $ITEM_PATH"

# ── AC-2: dedup + ordering ──
echo "---- AC-2 deduplication and ordering ----"

# Emit same path again
EMIT2=$("$BIN" ui emit --port "$PORT" --project proj-a --path docs/tasks/001-demo/plan.md 2>&1)
echo "emit 2 (same path): $EMIT2"
sleep 1

ITEM_COUNT2=$(agent-browser eval 'document.querySelectorAll("[data-testid=activity-item]").length' 2>/dev/null | grep -o '[0-9]*' || echo "0")
echo "AC-2 item count after same-path emit = $ITEM_COUNT2"
check "AC-2 dedup keeps count at 1" "$([ "$ITEM_COUNT2" = "1" ] && echo true || echo false)" "count=$ITEM_COUNT2"

# Check for edit count badge
HAS_BADGE=$(agent-browser eval '(document.querySelector(".activity-count") || {}).textContent || "none"' 2>/dev/null | tr -d '\n"' || echo "none")
echo "AC-2 edit count badge = $HAS_BADGE"
check "AC-2 edit count badge shown" "$(echo "$HAS_BADGE" | grep -q '2x' && echo true || echo false)" "$HAS_BADGE"

# Emit different path
EMIT3=$("$BIN" ui emit --port "$PORT" --project proj-a --path docs/tasks/002-second/plan.md 2>&1)
echo "emit 3 (different path): $EMIT3"
sleep 1.5

ITEM_COUNT3=$(agent-browser eval 'document.querySelectorAll("[data-testid=activity-item]").length' 2>/dev/null | grep -o '[0-9]*' || echo "0")
FIRST_PATH=$(agent-browser eval '(document.querySelector("[data-testid=activity-item]") || {}).dataset.activityPath || ""' 2>/dev/null | tr -d '\n"' || echo "")
echo "AC-2 item count after different-path emit = $ITEM_COUNT3, first item = $FIRST_PATH"
check "AC-2 two items after different path" "$([ "$ITEM_COUNT3" = "2" ] && echo true || echo false)" "count=$ITEM_COUNT3"
check "AC-2 newest first" "$(echo "$FIRST_PATH" | grep -q '002-second' && echo true || echo false)" "first=$FIRST_PATH"

# ── AC-3: click navigates ──
echo "---- AC-3 click navigates to doc ----"
agent-browser click '[data-testid="activity-item"]' 2>/dev/null
sleep 1

HASH=$(agent-browser eval 'location.hash' 2>/dev/null | tr -d '\n"' || echo "")
echo "AC-3 hash after click = $HASH"
check "AC-3 click navigates to doc" "$(echo "$HASH" | grep -q 'path=docs%2Ftasks%2F002-second%2Fplan.md' && echo true || echo false)" "$HASH"

# ── screenshot ──
agent-browser screenshot "$EVIDENCE/activity-feed.png" 2>/dev/null || true

echo ""
echo "================ SUMMARY: PASS=$PASS FAIL=$FAIL ================"

# Write results
{
  echo "================ activity-feed conformance ($(date -u +%FT%TZ)) ================"
  echo "PASS=$PASS FAIL=$FAIL"
} > "$EVIDENCE/results.txt"

[ "$FAIL" -eq 0 ] || exit 1
