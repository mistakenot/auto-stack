#!/usr/bin/env bash
# reveal-harness.sh — conformance for AC-3b: a newly-created doc is REVEALED.
#
# 026 AC-3 only asserted data-doc-count grows when a doc.changed for an unknown
# path arrives. The explorer redesign made groups collapsed-by-default, which
# exposed the gap: the new node is added to the model but never rendered, so the
# user "sees nothing change". AC-3b tightens the contract: the new node must be
# VISIBLE in the DOM (its ancestor group + subgroup auto-expand).
#
# Browser-driven (agent-browser) against the EMBED build, asserting via data-*
# attributes + window.__autoui (013 feedback: browser-layer defects are invisible
# to Go tests). Isolated temp fixture registry; never touches ~/.auto or real docs.
#
# Usage:  bash reveal-harness.sh
# Exit 0 = all assertions pass; non-zero = a failed assertion (the bug).
set -uo pipefail

REPO="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
BB="${AGENT_BROWSER:-$HOME/.local/bin/agent-browser}"
BIN=/tmp/auto-reveal-conf
TMP="$(mktemp -d)"
SRV_PID=""

cleanup() {
  [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null
  rm -f "$TMP"/* 2>/dev/null
  rmdir "$TMP/probe-proj/docs/tasks/050-brand-new" 2>/dev/null
  true
}
trap cleanup EXIT

pass=0; fail=0
check() { # check <label> <actual> <expected>
  if [ "$2" = "$3" ]; then echo "  PASS  $1 ($2)"; pass=$((pass+1));
  else echo "  FAIL  $1 — got [$2] want [$3]"; fail=$((fail+1)); fi
}

echo "== build embed binary (current web/static baked in) =="
( cd "$REPO" && go build -o "$BIN" ./auto-cli/cmd/auto ) || { echo "build failed"; exit 2; }
# NB: `strings | grep -q` trips `pipefail` (grep -q closes the pipe → strings gets
# SIGPIPE/141), so count matches instead of short-circuiting the reader.
[ "$(strings "$BIN" | grep -c parseDocChanged)" -ge 1 ] || { echo "binary missing 026 JS"; exit 2; }

echo "== build isolated fixture (lowercase id; several tasks so groups can fold) =="
mkdir -p "$TMP/probe-proj/docs/tasks/001-alpha" \
         "$TMP/probe-proj/docs/tasks/002-bravo" \
         "$TMP/probe-proj/docs/research"
printf '# Alpha\n'    > "$TMP/probe-proj/docs/tasks/001-alpha/plan.md"
printf '# Bravo\n'    > "$TMP/probe-proj/docs/tasks/002-bravo/plan.md"
printf '# Research\n' > "$TMP/probe-proj/docs/research/notes.md"
cat > "$TMP/projects.json" <<JSON
{"projects":[{"id":"probe-proj","name":"Probe","path":"$TMP/probe-proj","remote":"https://github.com/x/probe.git"}]}
JSON

echo "== start server (--projects fixture, --port 0, debug) =="
AUTO_UI_DEBUG=1 "$BIN" ui serve --projects "$TMP/projects.json" --port 0 --ready-file "$TMP/ready.json" \
  >"$TMP/serve.log" 2>&1 &
SRV_PID=$!
for _ in $(seq 1 50); do [ -s "$TMP/ready.json" ] && break; sleep 0.1; done
ADDR=$(python3 -c "import json;print(json.load(open('$TMP/ready.json'))['addr'])") || { echo "no ready file"; exit 2; }
echo "  server at $ADDR"

ev() { "$BB" eval "$1" 2>/dev/null; }

echo "== open explorer, select project =="
"$BB" open "http://$ADDR/?debug=1#/explore?project=probe-proj" >/dev/null 2>&1
sleep 2

COUNT_BEFORE=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
DC_BEFORE=$(ev "window.__autoui?(window.__autoui.counters.get('doc.changed')||0):-1")
echo "  doc-count before: $COUNT_BEFORE ; doc.changed seen: $DC_BEFORE"

echo "== create a BRAND-NEW task doc on disk, then fire doc.changed =="
mkdir -p "$TMP/probe-proj/docs/tasks/050-brand-new"
printf '# Brand new\n' > "$TMP/probe-proj/docs/tasks/050-brand-new/requirements.md"
"$BIN" ui emit --port "${ADDR##*:}" --project probe-proj --worktree "$TMP/probe-proj" \
  --path "docs/tasks/050-brand-new/requirements.md" >/dev/null 2>&1
sleep 1.5

COUNT_AFTER=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
DC_AFTER=$(ev "window.__autoui?(window.__autoui.counters.get('doc.changed')||0):-1")
NODE_VISIBLE=$(ev "[...document.querySelectorAll('[data-doc-path]')].some(n=>n.getAttribute('data-doc-path').includes('050-brand-new'))")

echo "== assertions =="
check "browser received doc.changed (count grew by 1)" "$DC_AFTER" "$((DC_BEFORE+1))"
check "tree re-listed (data-doc-count grew by 1)"      "$COUNT_AFTER" "$((COUNT_BEFORE+1))"
check "AC-3b: new node is VISIBLE in the DOM"          "$NODE_VISIBLE" "true"

echo
echo "RESULT: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
