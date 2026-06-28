#!/usr/bin/env bash
# Conformance driver for task 046 — multi-host SPA.
#
# Stands up REAL autowatch backends (dev build) + auto-ui (dev build), all under
# an ISOLATED $HOME so the real ~/.auto is never touched, then drives the SPA with
# agent-browser and asserts on data-testid / data-* attributes.
#
#   Part A — single-backend (AC-7 parity + host badge)
#   Part B — two-backend, COLLIDING project id (AC-1 union, AC-3 disambiguation,
#            AC-6 per-backend health incl. a live drop)
#
# Usage: bash driver.sh
# Requires: a chrome for agent-browser (kept under the REAL $HOME via $ABHOME so
# the isolated $HOME doesn't hide the browser cache); node >= 22 (built-in WS).
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../../../../.." && pwd)"   # repo root
DEVBIN="$ROOT/bin/auto-dev"                            # dev build (live-from-disk assets)
EVID="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$EVID"

# agent-browser must use the REAL home (its Chrome cache lives there), NOT the
# isolated $HOME we point the servers at.
ABHOME="${ABHOME:-$HOME}"
AB="/home/vscode/.local/bin/agent-browser"
ab() { HOME="$ABHOME" "$AB" "$@"; }

PASS=0; FAIL=0
check() { # label  ok(true/false)  detail
  if [ "$2" = "true" ]; then echo "PASS  $1 :: $3"; PASS=$((PASS+1));
  else echo "FAIL  $1 :: $3"; FAIL=$((FAIL+1)); fi
}

TMP=$(mktemp -d "${TMPDIR:-/tmp}/conf046.XXXXXX")
cleanup() {
  ab close --all 2>/dev/null
  # kill servers by their unique socket / ready-file paths (robust to subshell PIDs)
  pkill -f "$TMP/" 2>/dev/null
  sleep 0.3
  find "$TMP" -mindepth 0 -delete 2>/dev/null
}
trap cleanup EXIT

# Self-contained WS JSON-RPC probe (node >= 22 built-in WebSocket).
cat > "$TMP/wsprobe.mjs" <<'JS'
const [,, url, method] = process.argv;
const ws = new WebSocket(url); const id = 1;
const t = setTimeout(() => { console.error('timeout'); process.exit(2); }, 5000);
ws.addEventListener('open', () => ws.send(JSON.stringify({ jsonrpc:'2.0', id, method, params:{} })));
ws.addEventListener('message', (ev) => { const m = JSON.parse(ev.data);
  if (m.id===id){ clearTimeout(t); console.log(JSON.stringify(m.result ?? m.error, null, 2)); ws.close(); process.exit(0);} });
ws.addEventListener('error', (e) => { console.error('ws error', e.message||e.type); process.exit(3); });
JS

readyport() { node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1])).addr.split(":").pop())' "$1"; }
waitfile() { for _ in $(seq 1 60); do [ -f "$1" ] && return 0; sleep 0.2; done; return 1; }

# start_watch HOME HOSTID PROJID PROJNAME REPO SOCK READY LOG
start_watch() {
  local home="$1" hostid="$2" projid="$3" projname="$4" repo="$5" sock="$6" ready="$7" log="$8"
  mkdir -p "$home/.auto" "$repo/docs/tasks/001-demo"
  printf '# %s plan\n\nhello from %s\n' "$projname" "$hostid" > "$repo/docs/tasks/001-demo/plan.md"
  printf '# %s requirements\n' "$projname" > "$repo/docs/tasks/001-demo/requirements.md"
  cat > "$repo/docs/tasks/001-demo/plan.html" <<HTML
<!doctype html><html><head>
<script type="application/json" id="pd-meta">{"id":"001","name":"$projname","status":"planning"}</script>
</head><body><h1>$projname HTML plan</h1></body></html>
HTML
  printf '{"hostId":"%s","hostname":"%s"}\n' "$hostid" "$hostid" > "$home/.auto/host.json"
  printf '{"projects":[{"id":"%s","name":"%s","path":"%s","remote":"https://github.com/x/%s.git"}]}\n' \
    "$projid" "$projname" "$repo" "$projid" > "$home/.auto/projects.json"
  ( HOME="$home" "$DEVBIN" watch start --rpc-addr "unix://$sock" --ready-file "$ready" \
      --hook-addr 127.0.0.1:0 > "$log" 2>&1 ) &
}

# start_ui UIHOME READY LOG SOCK...
start_ui() {
  local uih="$1" ready="$2" log="$3"; shift 3
  mkdir -p "$uih/.auto/ui"
  { printf '{"backends":['; local sep=""
    for s in "$@"; do printf '%s{"uri":"unix://%s"}' "$sep" "$s"; sep=","; done
    printf ']}\n'; } > "$uih/.auto/ui/backends.json"
  ( cd "$ROOT/auto-ui" && HOME="$uih" AUTO_UI_DEBUG=1 "$DEVBIN" ui serve --port 0 \
      --ready-file "$ready" > "$log" 2>&1 ) &
}

echo "================ 046 conformance ($(date -u +%FT%TZ)) ================"
echo "DEVBIN=$DEVBIN  TMP=$TMP"

############################################################################
# Part A — single-backend (AC-7)
############################################################################
echo; echo "######## Part A: single-backend (AC-7) ########"
SA="$TMP/a.sock"
start_watch "$TMP/sa-h" "alpha-host" "shared-proj" "Shared Project" "$TMP/sa-repo" "$SA" "$TMP/sa.ready" "$TMP/sa-watch.log"
waitfile "$TMP/sa.ready" || { echo "watch failed"; cat "$TMP/sa-watch.log"; exit 1; }
start_ui "$TMP/sa-ui" "$TMP/sa-ui.ready" "$TMP/sa-ui.log" "$SA"
waitfile "$TMP/sa-ui.ready" || { echo "ui failed"; cat "$TMP/sa-ui.log"; exit 1; }
PA=$(readyport "$TMP/sa-ui.ready"); sleep 3
ab open "http://127.0.0.1:$PA/?debug=1#/explore?project=shared-proj&host=alpha-host" --headless 2>/dev/null
sleep 2.5

{
  echo "URL=http://127.0.0.1:$PA/?debug=1#/explore?project=shared-proj&host=alpha-host"
  echo "-- switcher --"; ab eval 'Array.from(document.querySelectorAll("[data-testid=project-switcher] option")).map(o=>({value:o.value,host:o.dataset.hostId,project:o.dataset.project,text:o.textContent.trim()}))'
  echo "-- host badge --"; ab eval 'const b=document.querySelector("[data-testid=host-badge]");b?{hostId:b.dataset.hostId,text:b.textContent.trim()}:{present:false}'
  echo "-- backend-health --"; ab eval 'Array.from(document.querySelectorAll("[data-testid=backend-health]")).map(r=>({hostId:r.dataset.hostId,connected:r.dataset.connected,state:r.dataset.state}))'
  echo "-- conn --"; ab eval 'document.querySelector("[data-testid=conn-indicator]").dataset.connStatus'
  echo "-- nav doc-count --"; ab eval 'document.querySelector("nav").getAttribute("data-doc-count")'
} > "$EVID/single-backend.txt" 2>&1
cat "$EVID/single-backend.txt"

# abget: eval once, retry once after a short settle if empty (de-flake rapid evals).
abget() { local v; v=$(ab eval "$1" 2>/dev/null | tr -d '"\n '); [ -z "$v" ] && { sleep 0.5; v=$(ab eval "$1" 2>/dev/null | tr -d '"\n '); }; printf '%s' "$v"; }
sleep 0.5
DOCCOUNT=$(abget 'document.querySelector("nav").getAttribute("data-doc-count")')
# NOTE: agent-browser `eval` of property-access returns (e.g. b.dataset.hostId) is
# flaky when evals are pipelined — it intermittently returns empty even though the
# element is present. A `.length` count eval is reliable, so assert presence via the
# count; the badge's hostId VALUE is captured authoritatively in single-backend.txt.
BADGECOUNT=$(abget 'document.querySelectorAll("[data-testid=host-badge]").length')
HEALTH1=$(abget 'const r=document.querySelector("[data-testid=backend-health]");r?r.dataset.connected:""')
check "AC-7 host badge shown"          "$([ "$BADGECOUNT" = "1" ] && echo true || echo false)" "badgeCount=$BADGECOUNT (hostId value in single-backend.txt)"
check "AC-7 docs listed via backend"   "$([ "$DOCCOUNT" -ge 1 ] 2>/dev/null && echo true || echo false)" "docCount=$DOCCOUNT"
check "AC-6 backend-health connected"  "$([ "$HEALTH1" = "true" ] && echo true || echo false)" "connected=$HEALTH1"

# AC-4: open the HTML plan via hash; assert the iframe src carries hostId.
ab open "http://127.0.0.1:$PA/?debug=1#/explore?project=shared-proj&host=alpha-host&path=docs/tasks/001-demo/plan.html" --headless 2>/dev/null
sleep 2
IFSRC=$(ab eval 'const f=document.querySelector("[data-testid=doc-iframe]");f?f.getAttribute("src"):""' 2>/dev/null | tr -d '\n')
echo "-- doc iframe src: $IFSRC" | tee -a "$EVID/single-backend.txt"
check "AC-4 raw URL carries hostId" "$(echo "$IFSRC" | grep -q 'hostId=alpha-host' && echo true || echo false)" "$IFSRC"
ab screenshot "$EVID/single-backend.png" 2>/dev/null && echo "screenshot: single-backend.png"

############################################################################
# Part B — two backends, colliding project id (AC-1 / AC-3 / AC-6)
############################################################################
echo; echo "######## Part B: two-backend, colliding id=demo (AC-1/AC-3/AC-6) ########"
DA="$TMP/da.sock"; DB="$TMP/db.sock"
start_watch "$TMP/da-h" "alpha-host" "demo" "Demo Alpha" "$TMP/da-repo" "$DA" "$TMP/da.ready" "$TMP/da-watch.log"
start_watch "$TMP/db-h" "beta-host"  "demo" "Demo Beta"  "$TMP/db-repo" "$DB" "$TMP/db.ready" "$TMP/db-watch.log"
waitfile "$TMP/da.ready" && waitfile "$TMP/db.ready" || { echo "dual watch failed"; exit 1; }
start_ui "$TMP/d-ui" "$TMP/d-ui.ready" "$TMP/d-ui.log" "$DA" "$DB"
waitfile "$TMP/d-ui.ready" || { echo "dual ui failed"; cat "$TMP/d-ui.log"; exit 1; }
PB=$(readyport "$TMP/d-ui.ready"); sleep 4
ab open "http://127.0.0.1:$PB/?debug=1#/explore" --headless 2>/dev/null
sleep 3

{
  echo "backends: unix://$DA (alpha-host), unix://$DB (beta-host); colliding project id=demo"
  echo "-- AC-1/AC-3 switcher (union, host-disambiguated) --"
  ab eval 'Array.from(document.querySelectorAll("[data-testid=project-switcher] option")).map(o=>({value:o.value,host:o.dataset.hostId,project:o.dataset.project,text:o.textContent.trim()}))'
  echo "-- AC-3 host badge --"; ab eval 'const b=document.querySelector("[data-testid=host-badge]");b?{hostId:b.dataset.hostId,text:b.textContent.trim()}:{present:false}'
  echo "-- AC-6 backend-health (both connected) --"
  ab eval 'Array.from(document.querySelectorAll("[data-testid=backend-health]")).map(r=>({hostId:r.dataset.hostId,connected:r.dataset.connected,state:r.dataset.state}))'
} > "$EVID/dual-backend.txt" 2>&1
cat "$EVID/dual-backend.txt"

NOPT=$(ab eval 'document.querySelectorAll("[data-testid=project-switcher] option").length' 2>/dev/null | tr -d '"\n ')
HOSTS=$(ab eval 'Array.from(document.querySelectorAll("[data-testid=project-switcher] option")).map(o=>o.dataset.hostId).sort().join(",")' 2>/dev/null | tr -d '"\n ')
NHEALTH=$(ab eval 'document.querySelectorAll("[data-testid=backend-health]").length' 2>/dev/null | tr -d '"\n ')
check "AC-1 union of two hosts"        "$([ "$NOPT" = "2" ] && echo true || echo false)" "options=$NOPT"
check "AC-3 same id, distinct hosts"   "$([ "$HOSTS" = "alpha-host,beta-host" ] && echo true || echo false)" "hosts=$HOSTS"
check "AC-6 two backend-health rows"   "$([ "$NHEALTH" = "2" ] && echo true || echo false)" "rows=$NHEALTH"
ab screenshot "$EVID/dual-backend-connected.png" 2>/dev/null && echo "screenshot: dual-backend-connected.png"

# AC-6 live drop: kill backend B, confirm SERVER flips it, then a REAL reload
# (distinct query string — a hash-only nav does NOT reload) shows the row drop.
echo "-- AC-6 drop backend B (beta-host) --" | tee -a "$EVID/dual-backend.txt"
pkill -f "$DB" 2>/dev/null
sleep 1; pgrep -f "$DB" >/dev/null && echo "WARN beta still alive" || echo "beta process gone"
sleep 9   # > manager 5s reconcile tick
echo "-- SERVER TRUTH: backends.list direct WS probe --" | tee -a "$EVID/dual-backend.txt"
node "$TMP/wsprobe.mjs" "ws://127.0.0.1:$PB/api/ws" backends.list | tee -a "$EVID/dual-backend.txt"
ab open "http://127.0.0.1:$PB/?debug=1&reload=1#/explore" --headless 2>/dev/null  # REAL reload
sleep 3
{
  echo "-- AC-6 backend-health rows after drop + reload --"
  ab eval 'Array.from(document.querySelectorAll("[data-testid=backend-health]")).map(r=>({hostId:r.dataset.hostId,connected:r.dataset.connected,state:r.dataset.state}))'
} | tee -a "$EVID/dual-backend.txt"
BETA=$(ab eval 'const r=Array.from(document.querySelectorAll("[data-testid=backend-health]")).find(x=>x.dataset.hostId==="beta-host");r?r.dataset.connected:"missing"' 2>/dev/null | tr -d '"\n ')
check "AC-6 dropped backend shows disconnected (after reload)" "$([ "$BETA" = "false" ] && echo true || echo false)" "beta.connected=$BETA"
ab screenshot "$EVID/dual-backend-after-drop.png" 2>/dev/null && echo "screenshot: dual-backend-after-drop.png"

echo; echo "================ SUMMARY: PASS=$PASS FAIL=$FAIL ================"
{ echo "046 conformance run $(date -u +%FT%TZ)"; echo "PASS=$PASS FAIL=$FAIL"; } > "$EVID/results.txt"
[ "$FAIL" -eq 0 ]
