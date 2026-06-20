#!/usr/bin/env bash
# state-harness.sh — 029 refactor-structure conformance (AC-3..AC-8).
#
# Task 029 moved all client state into a single module-singleton store
# (web/static/store.js) and made explorer/tree/content/debug presentational.
# 025 (static explorer) and 026 (liveness + reveal) are the behaviour-preserving
# regression oracle and are re-run UNEDITED. THIS harness proves the invariants
# the refactor ADDS, which the 025/026 harnesses cannot see:
#
#   AC-3  single normalised source; views hold no data-fetching useState/call(
#         (grep gate — the only call(...) sites live in store.js)
#   AC-4  uistate.js deleted; /debug reflects live store state across a route
#         change with NO reload (module-scope state survives the unmount), and
#         uistate.js 404s on the wire
#   AC-5  ONE app-root doc.changed subscription drives both live behaviours;
#         the single-subscription EVIDENCE is the grep clause
#         `grep -rl 'on("doc.changed"' auto-ui/web/static` → only store.js.
#         window.__autoui.counters.get('doc.changed') only GATES the ~3s poll
#         (it is a Map → use .get(); recordEvent fires once per received event
#         at the rpc.js dispatch layer, so it cannot prove subscription count).
#   AC-6  no new runtime dep / importmap entry: git diff --quiet on index.html
#   AC-7  cold-load gating + reconnect self-heal: no "not connected" reject on a
#         cold load; conn-indicator tracks connecting -> open
#   AC-8  warm docsByProject cache: switch A->B->A makes NO second doc.list,
#         observable via the store-level doc.list call counter
#         (window.__autoui.store.docListCalls — NOT /api/debug/recent, which
#         buffers server ingest events, not client WS calls); a doc.changed /
#         reconnect still re-lists (cache does not mask liveness).
#
# Browser-driven (agent-browser) against BOTH the embed and -tags dev builds
# (013 feedback: browser-layer defects are invisible to Go tests). Isolated temp
# fixture registry; never touches ~/.auto or real docs.
#
# Usage:
#   bash state-harness.sh            # runs embed, then dev
#   bash state-harness.sh embed      # one build only
#   bash state-harness.sh dev
#
# Evidence (eval outputs + screenshots) is written under artifacts/evidence/.
# Exit 0 = all assertions pass on every requested build; non-zero = a failure.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(git -C "$HERE" rev-parse --show-toplevel)"
BB="${AGENT_BROWSER:-$HOME/.local/bin/agent-browser}"
EVID="$HERE/evidence"
mkdir -p "$EVID"

BUILDS=("$@")
[ "${#BUILDS[@]}" -eq 0 ] && BUILDS=(embed dev)

total_fail=0

# --- per-build run ---------------------------------------------------------
run_build() {
  local BUILD="$1"
  local BIN TMP SRV_PID="" ADDR PORT
  BIN="/tmp/auto-029-$BUILD"
  TMP="$(mktemp -d)"

  echo
  echo "########################################################"
  echo "## BUILD: $BUILD"
  echo "########################################################"

  local pass=0 fail=0
  check() { # check <label> <actual> <expected>
    if [ "$2" = "$3" ]; then echo "  PASS  $1 ($2)"; pass=$((pass+1));
    else echo "  FAIL  $1 — got [$2] want [$3]"; fail=$((fail+1)); fi
  }

  cleanup_build() {
    [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null
    "$BB" close --all 2>/dev/null
    find "$TMP" -delete 2>/dev/null
    true
  }
  trap cleanup_build RETURN

  echo "== build $BUILD binary (current web/static) =="
  if [ "$BUILD" = "embed" ]; then
    ( cd "$REPO" && go build -o "$BIN" ./auto-cli/cmd/auto ) || { echo "build failed"; fail=$((fail+1)); return 1; }
  else
    ( cd "$REPO" && go build -tags dev -o "$BIN" ./auto-cli/cmd/auto ) || { echo "build failed"; fail=$((fail+1)); return 1; }
  fi
  # store.js must be the new single state module (sanity: docListCount baked in).
  [ "$(strings "$BIN" | grep -c docListCalls)" -ge 1 ] || echo "  WARN  binary may lack 029 store.js (docListCalls not found in strings)"

  echo "== build isolated fixture (lowercase-kebab ids; two projects for AC-8) =="
  # proj-a: rich tree (folded groups) so a NEW doc can reveal; proj-b: distinct
  # so an A->B->A switch exercises the warm cache.
  mkdir -p "$TMP/proj-a/docs/tasks/001-alpha/artifacts" \
           "$TMP/proj-a/docs/tasks/002-bravo" \
           "$TMP/proj-a/docs/research" \
           "$TMP/proj-b/docs/tasks/010-bee"
  printf '# Alpha\n\nhello **markdown**\n' > "$TMP/proj-a/docs/tasks/001-alpha/plan.md"
  printf '# Alpha reqs\n'                  > "$TMP/proj-a/docs/tasks/001-alpha/requirements.md"
  printf '# Bravo\n'                       > "$TMP/proj-a/docs/tasks/002-bravo/plan.md"
  printf '# Research\n'                    > "$TMP/proj-a/docs/research/notes.md"
  printf '# Bee\n'                         > "$TMP/proj-b/docs/tasks/010-bee/plan.md"
  cat > "$TMP/projects.json" <<JSON
{"projects":[
  {"id":"proj-a","name":"Project A","path":"$TMP/proj-a","remote":"https://github.com/x/a.git"},
  {"id":"proj-b","name":"Project B","path":"$TMP/proj-b","remote":""}
]}
JSON

  echo "== start server (--projects fixture, --port 0, debug) =="
  # The dev build resolves assets relative to cwd, so launch from auto-ui/.
  local CWD="$REPO"
  [ "$BUILD" = "dev" ] && CWD="$REPO/auto-ui"
  ( cd "$CWD" && AUTO_UI_DEBUG=1 "$BIN" ui serve --projects "$TMP/projects.json" --port 0 \
      --ready-file "$TMP/ready.json" >"$TMP/serve.log" 2>&1 ) &
  SRV_PID=$!
  for _ in $(seq 1 60); do [ -s "$TMP/ready.json" ] && break; sleep 0.1; done
  ADDR=$(python3 -c "import json;print(json.load(open('$TMP/ready.json'))['addr'])" 2>/dev/null) \
    || { echo "no ready file; serve.log:"; cat "$TMP/serve.log"; fail=$((fail+1)); return 1; }
  PORT="${ADDR##*:}"
  echo "  server at $ADDR (mode: $(curl -s "http://$ADDR/api/hello"))"

  ev() { "$BB" eval "$1" 2>/dev/null; }

  # =====================================================================
  # AC-6 — no new importmap entry / runtime dependency (git diff is the gate).
  # =====================================================================
  echo
  echo "== AC-6: importmap / index.html unchanged =="
  if git -C "$REPO" diff --quiet -- auto-ui/web/static/index.html; then
    check "AC-6 index.html importmap unchanged (git diff --quiet)" "clean" "clean"
  else
    check "AC-6 index.html importmap unchanged (git diff --quiet)" "dirty" "clean"
  fi
  git -C "$REPO" diff -- auto-ui/web/static/index.html > "$EVID/$BUILD-ac6-index-diff.txt"

  # =====================================================================
  # AC-3 — single source; views hold no data-fetching useState/call().
  # The grep is the evidence: the only call(...) sites live in store.js.
  # =====================================================================
  echo
  echo "== AC-3: no call() in presentational views; store owns the state shape =="
  local CALL_VIEWS STORE_HITS NONSTORE_HITS
  # grepcall = lines invoking call(...) as code, NOT comments (a `//`-prefixed
  # mention of call() is documentation, not a fetch). Strip leading-whitespace
  # comment lines before matching.
  grepcall() { grep -rn 'call(' "$@" 2>/dev/null | grep -vE ':[[:space:]]*//'; }
  CALL_VIEWS=$(grepcall "$REPO/auto-ui/web/static/explorer.js" \
                        "$REPO/auto-ui/web/static/tree.js" \
                        "$REPO/auto-ui/web/static/content.js" | grep -c . )
  check "AC-3 no call( in explorer/tree/content" "$CALL_VIEWS" "0"
  # Every state slice the store must own (conn/projects/docsByProject/selection/events).
  local SLICES=0
  for slice in conn projects docsByProject selection events; do
    grep -q "$slice" "$REPO/auto-ui/web/static/store.js" && SLICES=$((SLICES+1))
  done
  check "AC-3 store owns all 5 normalised slices" "$SLICES" "5"
  # call(...) invocations are store-only across the whole SPA (rpc.js DEFINES
  # call; comment-only mentions like debug.js's are not invocations).
  NONSTORE_HITS=$(grepcall "$REPO/auto-ui/web/static"/*.js \
                   | grep -vE '/(store|rpc)\.js:' | grep -c . )
  check "AC-3 call() sites are store-only (no other module)" "$NONSTORE_HITS" "0"
  {
    echo "explorer/tree/content files containing call(  : $CALL_VIEWS"
    echo "non-store/non-rpc files containing call(       : $NONSTORE_HITS"
    echo "store.js slices present                         : $SLICES/5"
  } > "$EVID/$BUILD-ac3-grep.txt"

  # =====================================================================
  # AC-5 (static evidence) — single doc.changed subscription = grep clause.
  # =====================================================================
  echo
  echo "== AC-5: single doc.changed subscription (grep evidence) =="
  local SUB_FILES SUB_ONLY_STORE
  SUB_FILES=$(grep -rl 'on("doc.changed"' "$REPO/auto-ui/web/static" 2>/dev/null)
  SUB_ONLY_STORE=$(echo "$SUB_FILES" | grep -vE '/store\.js$' | grep -c . )
  check "AC-5 doc.changed subscription is store-only" "$SUB_ONLY_STORE" "0"
  echo "$SUB_FILES" > "$EVID/$BUILD-ac5-grep.txt"

  # =====================================================================
  # AC-7 — cold load: socket not yet open, no "not connected" reject.
  # Open the explorer cold and assert the switcher AND tree populate without a
  # reload, and the conn dot reaches "open" (tracking connecting -> open).
  # =====================================================================
  echo
  echo "== AC-7: cold-load gating (whenOpen) + conn dot tracks =="
  "$BB" open "http://$ADDR/?debug=1#/explore?project=proj-a" >/dev/null 2>&1
  # Poll up to ~3s for the cold load to settle (whenOpen gates the fetches).
  local CONN="" DOCCOUNT="0"
  for _ in $(seq 1 30); do
    CONN=$(ev "document.querySelector('[data-testid=conn-indicator]')?.getAttribute('data-conn-status')" | tr -d '"')
    DOCCOUNT=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
    [ "$CONN" = "open" ] && [ -n "$DOCCOUNT" ] && [ "$DOCCOUNT" != "0" ] && break
    sleep 0.1
  done
  local NOT_CONNECTED
  # No error ring entry mentioning "not connected" (the whenOpen() gate prevents it).
  NOT_CONNECTED=$(ev "(function(){try{var es=(window.__autoui&&window.__autoui.recentErrors)?window.__autoui.recentErrors:[];return JSON.stringify(es).toLowerCase().includes('not connected')}catch(e){return false}})()")
  check "AC-7 conn-indicator reached open"          "$CONN" "open"
  check "AC-7 tree populated on cold load (no reload, count>0)" "$([ -n "$DOCCOUNT" ] && [ "$DOCCOUNT" -gt 0 ] && echo true || echo false)" "true"
  check "AC-7 no 'not connected' reject on cold load" "$NOT_CONNECTED" "false"
  "$BB" screenshot "$EVID/$BUILD-ac7-coldload.png" >/dev/null 2>&1
  { echo "conn-status: $CONN"; echo "doc-count: $DOCCOUNT"; echo "not-connected-error: $NOT_CONNECTED"; } > "$EVID/$BUILD-ac7.txt"

  # =====================================================================
  # AC-4 — cross-route survival: open a doc, record revision/doc-count, then
  # navigate to #/debug WITHOUT reload; debug-current-state must match
  # (module-scope store state survives the unmount). uistate.js must 404.
  # =====================================================================
  echo
  echo "== AC-4: cross-route survival + uistate.js 404 =="
  # Select a markdown doc so the content pane has a revision.
  "$BB" open "http://$ADDR/?debug=1#/explore?project=proj-a&path=docs%2Ftasks%2F001-alpha%2Fplan.md" >/dev/null 2>&1
  local REV_EXP DOCCOUNT_EXP
  REV_EXP="0"
  for _ in $(seq 1 30); do
    REV_EXP=$(ev "document.querySelector('article[data-revision]')?.getAttribute('data-revision')" | tr -d '"')
    DOCCOUNT_EXP=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
    [ -n "$REV_EXP" ] && [ "$REV_EXP" != "0" ] && [ -n "$DOCCOUNT_EXP" ] && break
    sleep 0.1
  done
  echo "  explorer: revision=$REV_EXP doc-count=$DOCCOUNT_EXP"
  # Navigate to #/debug WITHOUT reloading the page (hash-only nav via eval).
  "$BB" eval "location.hash = '#/debug'" >/dev/null 2>&1
  sleep 0.7
  # Read debug-current-state rows by their <th> label -> sibling <td>.
  read_state_row() { # read_state_row <label>
    ev "(function(){var rows=document.querySelectorAll('[data-testid=debug-state-row]');for(var i=0;i<rows.length;i++){var th=rows[i].querySelector('th');if(th&&th.textContent.trim()==='$1')return rows[i].querySelector('td').textContent.trim();}return null;})()" | sed 's/^"//;s/"$//'
  }
  local DBG_PROJECT DBG_PATH DBG_REV DBG_COUNT
  DBG_PROJECT=$(read_state_row "project")
  DBG_PATH=$(read_state_row "path")
  DBG_REV=$(read_state_row "revision")
  DBG_COUNT=$(read_state_row "doc count")
  echo "  /debug current-state: project=$DBG_PROJECT path=$DBG_PATH revision=$DBG_REV docCount=$DBG_COUNT"
  check "AC-4 /debug project survives nav"       "$DBG_PROJECT" "proj-a"
  check "AC-4 /debug path survives nav"          "$DBG_PATH"    "docs/tasks/001-alpha/plan.md"
  check "AC-4 /debug revision matches (R)"       "$DBG_REV"     "$REV_EXP"
  check "AC-4 /debug docCount matches (N)"       "$DBG_COUNT"   "$DOCCOUNT_EXP"
  # uistate.js must no longer be served (deleted from web/static).
  local UISTATE_CODE
  UISTATE_CODE=$(curl -s -o /dev/null -w '%{http_code}' "http://$ADDR/uistate.js")
  check "AC-4 uistate.js 404s (deleted)"         "$UISTATE_CODE" "404"
  "$BB" screenshot "$EVID/$BUILD-ac4-debug.png" >/dev/null 2>&1
  {
    echo "explorer revision=$REV_EXP doc-count=$DOCCOUNT_EXP"
    echo "debug project=$DBG_PROJECT path=$DBG_PATH revision=$DBG_REV docCount=$DBG_COUNT"
    echo "uistate.js HTTP=$UISTATE_CODE"
  } > "$EVID/$BUILD-ac4.txt"

  # =====================================================================
  # AC-8 — warm docsByProject cache. Switch A->B->A; the second view of A must
  # render from cache with NO new doc.list (store-level call counter unchanged).
  # =====================================================================
  echo
  echo "== AC-8: warm cache avoids redundant doc.list on A->B->A =="
  "$BB" open "http://$ADDR/?debug=1#/explore?project=proj-a" >/dev/null 2>&1
  for _ in $(seq 1 30); do
    DOCCOUNT=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
    [ -n "$DOCCOUNT" ] && [ "$DOCCOUNT" != "0" ] && break
    sleep 0.1
  done
  # Switch to B, then back to A, polling the doc-count to confirm each list settled.
  local CALLS_AFTER_A CALLS_AFTER_B CALLS_AFTER_A2 DOCCOUNT_B DOCCOUNT_A2
  CALLS_AFTER_A=$(ev "(window.__autoui&&window.__autoui.store&&window.__autoui.store.docListCalls)||0")
  "$BB" eval "location.hash = '#/explore?project=proj-b'" >/dev/null 2>&1
  for _ in $(seq 1 30); do
    DOCCOUNT=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
    [ "$DOCCOUNT" = "1" ] && break   # proj-b has exactly 1 doc
    sleep 0.1
  done
  DOCCOUNT_B="$DOCCOUNT"
  CALLS_AFTER_B=$(ev "(window.__autoui&&window.__autoui.store&&window.__autoui.store.docListCalls)||0")
  "$BB" eval "location.hash = '#/explore?project=proj-a'" >/dev/null 2>&1
  sleep 1.0   # generous: a (wrong) re-list would have bumped the counter by now
  for _ in $(seq 1 30); do
    DOCCOUNT=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
    [ "$DOCCOUNT" != "1" ] && [ -n "$DOCCOUNT" ] && [ "$DOCCOUNT" != "0" ] && break
    sleep 0.1
  done
  DOCCOUNT_A2="$DOCCOUNT"
  CALLS_AFTER_A2=$(ev "(window.__autoui&&window.__autoui.store&&window.__autoui.store.docListCalls)||0")
  echo "  doc.list calls: after-A=$CALLS_AFTER_A after-B=$CALLS_AFTER_B after-A(again)=$CALLS_AFTER_A2"
  echo "  tree doc-count: proj-b=$DOCCOUNT_B proj-a-again=$DOCCOUNT_A2"
  # B was a fresh project -> exactly one new list; A again must be served from cache.
  check "AC-8 switching A->B lists B exactly once" "$CALLS_AFTER_B" "$((CALLS_AFTER_A+1))"
  check "AC-8 returning to A serves from cache (no new doc.list)" "$CALLS_AFTER_A2" "$CALLS_AFTER_B"
  # The TREE must actually re-render to the switched project's docs, not just the
  # store cache. DocTree is reused (not remounted by hash nav) unless keyed, and
  # useStore captures its selector once — so a project switch would otherwise keep
  # showing the previous project's docs. Asserting the rendered doc-count guards
  # that regression (PR #82 review P1).
  check "AC-8 tree shows proj-b docs after A->B switch (P1)" "$DOCCOUNT_B" "1"
  check "AC-8 tree shows proj-a docs after B->A switch (P1)" "$DOCCOUNT_A2" "4"

  echo "== AC-8b: cache does NOT mask liveness (doc.changed for A re-lists) =="
  local DC_BEFORE COUNT_BEFORE CALLS_BEFORE_LIVE
  COUNT_BEFORE=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
  DC_BEFORE=$(ev "window.__autoui?(window.__autoui.counters.get('doc.changed')||0):-1")
  CALLS_BEFORE_LIVE=$CALLS_AFTER_A2
  # Create a brand-new doc on disk, THEN emit doc.changed (emit does not create files).
  mkdir -p "$TMP/proj-a/docs/tasks/099-new"
  printf '# New live\n' > "$TMP/proj-a/docs/tasks/099-new/plan.md"
  "$BIN" ui emit --port "$PORT" --project proj-a --worktree "$TMP/proj-a" \
    --path "docs/tasks/099-new/plan.md" >/dev/null 2>&1
  # Gate the assertion on the counter (received), then poll ~3s for the re-list.
  local COUNT_AFTER NODE_VISIBLE CALLS_AFTER_LIVE
  for _ in $(seq 1 30); do
    local DC_NOW; DC_NOW=$(ev "window.__autoui?(window.__autoui.counters.get('doc.changed')||0):-1")
    [ "$DC_NOW" -gt "$DC_BEFORE" ] && break
    sleep 0.1
  done
  for _ in $(seq 1 30); do
    COUNT_AFTER=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
    [ -n "$COUNT_AFTER" ] && [ "$COUNT_AFTER" -gt "${COUNT_BEFORE:-0}" ] && break
    sleep 0.1
  done
  NODE_VISIBLE=$(ev "[...document.querySelectorAll('[data-doc-path]')].some(n=>n.getAttribute('data-doc-path').includes('099-new'))")
  CALLS_AFTER_LIVE=$(ev "(window.__autoui&&window.__autoui.store&&window.__autoui.store.docListCalls)||0")
  echo "  live: count $COUNT_BEFORE -> $COUNT_AFTER ; doc.list calls $CALLS_BEFORE_LIVE -> $CALLS_AFTER_LIVE ; node visible=$NODE_VISIBLE"
  check "AC-8b doc.changed re-lists despite warm cache (count grew)" "$COUNT_AFTER" "$((COUNT_BEFORE+1))"
  check "AC-8b re-list issued a new doc.list (counter grew)"         "$([ "$CALLS_AFTER_LIVE" -gt "$CALLS_BEFORE_LIVE" ] && echo true || echo false)" "true"
  check "AC-8b new node is VISIBLE (reveal preserved)"               "$NODE_VISIBLE" "true"
  "$BB" screenshot "$EVID/$BUILD-ac8-livegrowth.png" >/dev/null 2>&1
  {
    echo "after-A=$CALLS_AFTER_A after-B=$CALLS_AFTER_B after-A-again=$CALLS_AFTER_A2"
    echo "live count $COUNT_BEFORE -> $COUNT_AFTER ; calls $CALLS_BEFORE_LIVE -> $CALLS_AFTER_LIVE ; node visible=$NODE_VISIBLE"
  } > "$EVID/$BUILD-ac8.txt"

  # =====================================================================
  # AC-5 (runtime) — the ONE subscription drives BOTH live behaviours:
  # open-doc refresh AND tree growth. (Static grep above is the subscription-
  # count evidence; this confirms the single sub actually fans out to both.)
  # =====================================================================
  echo
  echo "== AC-5: one subscription drives open-doc refresh AND tree growth =="
  "$BB" open "http://$ADDR/?debug=1#/explore?project=proj-a&path=docs%2Ftasks%2F001-alpha%2Fplan.md" >/dev/null 2>&1
  local REV0 DC0
  for _ in $(seq 1 30); do
    REV0=$(ev "document.querySelector('article[data-revision]')?.getAttribute('data-revision')" | tr -d '"')
    [ -n "$REV0" ] && [ "$REV0" != "0" ] && break
    sleep 0.1
  done
  DC0=$(ev "window.__autoui?(window.__autoui.counters.get('doc.changed')||0):-1")
  # (a) emit for the OPEN doc -> revision must bump.
  "$BIN" ui emit --port "$PORT" --project proj-a --worktree "$TMP/proj-a" \
    --path "docs/tasks/001-alpha/plan.md" >/dev/null 2>&1
  local REV1
  for _ in $(seq 1 30); do
    REV1=$(ev "document.querySelector('article[data-revision]')?.getAttribute('data-revision')" | tr -d '"')
    [ -n "$REV1" ] && [ "$REV1" -gt "${REV0:-0}" ] && break
    sleep 0.1
  done
  check "AC-5 open-doc refresh: data-revision bumped" "$([ "${REV1:-0}" -gt "${REV0:-0}" ] && echo true || echo false)" "true"
  # (b) emit for an UNSEEN path -> tree grows (same single subscription).
  local TC0 TC1
  TC0=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
  mkdir -p "$TMP/proj-a/docs/tasks/100-extra"
  printf '# Extra\n' > "$TMP/proj-a/docs/tasks/100-extra/plan.md"
  "$BIN" ui emit --port "$PORT" --project proj-a --worktree "$TMP/proj-a" \
    --path "docs/tasks/100-extra/plan.md" >/dev/null 2>&1
  for _ in $(seq 1 30); do
    TC1=$(ev "document.querySelector('[data-doc-count]')?.getAttribute('data-doc-count')" | tr -d '"')
    [ -n "$TC1" ] && [ "$TC1" -gt "${TC0:-0}" ] && break
    sleep 0.1
  done
  check "AC-5 tree growth from same subscription"     "$([ "${TC1:-0}" -gt "${TC0:-0}" ] && echo true || echo false)" "true"
  {
    echo "open-doc revision $REV0 -> $REV1 (emit on open doc)"
    echo "tree doc-count   $TC0 -> $TC1 (emit on unseen path)"
    echo "doc.changed counter at start: $DC0 (gate only; cannot prove sub count)"
    echo "subscription files (grep): $SUB_FILES"
  } > "$EVID/$BUILD-ac5.txt"
  "$BB" screenshot "$EVID/$BUILD-ac5-livesplit.png" >/dev/null 2>&1

  echo
  echo "---- $BUILD RESULT: $pass passed, $fail failed ----"
  echo "$BUILD: $pass passed, $fail failed" >> "$EVID/summary.txt"
  return "$fail"
}

# --- driver ----------------------------------------------------------------
: > "$EVID/summary.txt"
for b in "${BUILDS[@]}"; do
  run_build "$b"
  rc=$?
  total_fail=$((total_fail + rc))
done

echo
echo "========================================================"
echo "OVERALL across builds (${BUILDS[*]}): $total_fail failed"
cat "$EVID/summary.txt"
echo "========================================================"
[ "$total_fail" -eq 0 ]
