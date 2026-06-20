# Conformance Harness: Task 029 — auto-ui State Refactor (AC-3..AC-8)

> Task 029 moved all client state into a single module-singleton store
> (`auto-ui/web/static/store.js`) and reduced `explorer.js`/`tree.js`/`content.js`/
> `debug.js` to presentational components; `uistate.js` was deleted. The
> **025 (static explorer)** and **026 (liveness + reveal)** harnesses are the
> behaviour-preserving regression oracle (AC-1 / AC-2) and are re-run **UNEDITED**.
> THIS harness ([`state-harness.sh`](./state-harness.sh)) proves the invariants the
> refactor *adds* (AC-3..AC-8), which the 025/026 harnesses cannot see. Drive the
> served SPA with `agent-browser`, assert via `data-*` attributes + `window.__autoui`
> (never rendered-text diffs), on **both** the embed and `-tags dev` builds (013
> feedback: browser-layer defects are invisible to Go tests).

## Result summary

| Build | Assertions | Result |
|-------|-----------|--------|
| `embed` (`/api/hello` → `mode:embed`) | 20 | **20 PASS / 0 FAIL** |
| `-tags dev` (`/api/hello` → `mode:disk`, served from `auto-ui/web/static`) | 20 | **20 PASS / 0 FAIL** |

All AC-3..AC-8 assertions pass on **both** builds. Full run log:
[`evidence/state-harness-run.txt`](./evidence/state-harness-run.txt).

> Phase 3 first surfaced a real **AC-4 path-survival regression** (the refactor's
> `syncFromHash()` cleared `selection`/`openDoc` on the param-less `#/debug` route,
> reported per the Phase-3 contract rather than silently patched in the test pass). The
> coordinator then authorised the source fix: a one-line gate in `store.js`
> (`if (view !== "explore") return;` — `#/debug` is a read-only view and no longer drives
> selection). After the fix the harness is **20/20 on both builds**; see **"AC-4 fix"**
> below.

## Build & launch

```bash
# embed (default shipped artifact) — launched from repo root:
go build -o /tmp/auto-029-embed ./auto-cli/cmd/auto

# dev (-tags dev, disk-served) — launched with CWD=auto-ui/ so web/static resolves:
go build -tags dev -o /tmp/auto-029-dev ./auto-cli/cmd/auto

# Isolated temp fixture registry (lowercase-kebab ids ^[a-z0-9]+(?:-[a-z0-9]+)*$),
# AUTO_UI_DEBUG=1 + ?debug=1 to enable window.__autoui, --port 0 + --ready-file.
# Run both builds:  bash state-harness.sh            (embed, then dev)
#   one build only:  bash state-harness.sh embed | dev
```

The harness builds its own binaries, stands up an isolated `auto ui serve` against a
temp two-project fixture (`proj-a` rich tree, `proj-b` single doc — so an A→B→A switch
exercises the warm cache), drives headless `agent-browser`, polls `data-*` up to ~3s
after each `emit`, and never touches `~/.auto` or real docs.

## AC → assertion map (AC-3..AC-8)

| AC | Criterion | How proven | Harness assertion(s) | embed | dev |
|----|-----------|-----------|----------------------|:----:|:---:|
| **AC-3** | Single normalised source; views presentational | **grep/static** (the AC defines this as inspection: "the only `call(...)` sites live in the store module") | `no call( in explorer/tree/content` (= 0, comment-only mentions excluded); `store owns all 5 slices` conn/projects/docsByProject/selection/events; `call() sites are store-only` (every `call(` invocation across `web/static` is in `store.js`; `rpc.js` *defines* `call`) | PASS | PASS |
| **AC-4** | `uistate.js` deleted; `/debug` reflects live store state across nav | **runtime** (browser) + HTTP | select project+doc on `#/explore` (record `data-revision` R, `data-doc-count` N); `location.hash='#/debug'` **without reload**; read `debug-current-state` rows → `project`/`path`/`revision`/`docCount` must equal R/N; `curl /uistate.js` → **404** | PASS (project ✓ / path ✓ / revision ✓ / docCount ✓ / uistate.js 404 ✓) | PASS |
| **AC-5** | One app-root `doc.changed` subscription drives both live behaviours | **static grep** = subscription-count evidence; **runtime** = single-sub fan-out | `grep -rl 'on("doc.changed"' web/static` matches **only `store.js`** (gate asserts 0 non-store files); runtime: one `emit` on the open doc bumps `data-revision`, one `emit` on an unseen path grows `data-doc-count` — both from the same subscription. `counters.get('doc.changed')` is a **`Map`** used only to gate the poll (it cannot prove sub count — `recordEvent` fires once per received event at the rpc.js dispatch layer) | PASS | PASS |
| **AC-6** | No new runtime dependency / importmap entry | **static** (git) | `git diff --quiet -- auto-ui/web/static/index.html` (empty diff captured in `evidence/<build>-ac6-index-diff.txt`) | PASS | PASS |
| **AC-7** | Cold-load gating + reconnect self-heal preserved | **runtime** (browser) | cold `open #/explore?project=proj-a`: `conn-indicator[data-conn-status]` reaches `open` (tracking `connecting`→`open`); the switcher **and** tree populate with **no reload** (`data-doc-count` > 0); no `"not connected"` entry in the error ring (all fetches gate on `whenOpen()`) | PASS | PASS |
| **AC-8** | Warm `docsByProject` cache avoids redundant `doc.list`; liveness not masked | **runtime** via the **store-level `doc.list` call counter** | switch A→B→A reading `window.__autoui.store.docListCalls`: A→B lists B exactly once; returning to A is served from cache (counter **unchanged**). **AC-8b:** a `doc.changed` for a new doc on A still re-lists (counter grows, `data-doc-count` grows, new node visible) — cache does **not** mask the 026 re-list. (NOT `/api/debug/recent`, which buffers server ingest events, not client WS calls.) | PASS | PASS |

Per-AC eval-output evidence: `evidence/<build>-ac{3,4,5,7,8}*.txt`; screenshots:
`evidence/<build>-ac{4,5,7,8}*.png`; grep captures: `evidence/<build>-ac{3,5}-grep.txt`.

## Runtime-asserted vs grep/static (honest record)

- **Runtime (browser, both builds):** AC-4 (cross-route survival + 404), AC-5 *fan-out*
  (open-doc refresh + tree growth from one emit each), AC-7 (cold-load + conn dot),
  AC-8 + AC-8b (warm-cache counter + live re-list + reveal).
- **Static / grep (objective, but inspection-based — see Verification → Known Gaps):**
  AC-3 (no `call(` in views; store owns the slices; `call()` store-only), AC-5
  *subscription-count* (the `grep -rl 'on("doc.changed"'` → only `store.js` clause; the
  runtime counter deliberately does **not** prove sub count), AC-6 (importmap `git diff`).
- The AC-3 `call(` grep excludes comment-only mentions (`debug.js` has a `// … call() …`
  doc comment, not an invocation) so the gate measures real fetch sites.

## 025 + 026 re-run UNEDITED (AC-1 / AC-2, the behaviour-preserving spine)

- **AC-1 (025 static explorer)** and **AC-2 (026 liveness + reveal)** are the regression
  oracle and were re-run **with no edits to the harness** on **both** builds as part of
  **Phase 2** (commit `7aca7cc` "phase 2 — cut … over to store.js; delete uistate.js";
  Phase-2 completion `cc96548`). The cutover phase's exit gate was exactly this: 025 +
  026 + `reveal-harness.sh` pass unedited on embed and `-tags dev`.
- **Re-confirmed in Phase 3:** [`026/artifacts/reveal-harness.sh`](../../026-planning-dashboard-live-updates/artifacts/reveal-harness.sh)
  was re-run **UNEDITED** against the current `web/static` on the embed build — **3/3 PASS**
  (browser received `doc.changed`; tree re-listed `data-doc-count` +1; AC-3b new node
  VISIBLE). This proves the single store-owned subscription preserves 026's tree
  liveness/reveal behaviour byte-for-byte at the harness level.
- 025's acceptance is a manual `agent-browser` checklist
  ([`025/artifacts/conformance.md`](../../025-planning-dashboard-explorer/artifacts/conformance.md))
  with Phase-6 evidence (36/36 on both builds); 029's `state-harness.sh` independently
  re-exercises its core observables (switcher + tree cold-load population, `data-doc-*`
  attributes, `conn-indicator`, `window.__autoui` gating) under AC-7/AC-8/AC-4.

## AC-4 fix (`#/debug` no longer clears selection)

**Status: PASS on both builds** (fixed in `store.js`).

Phase 3 first surfaced this as a real regression: `debug-current-state` `path` showed `—`
after navigating `#/explore?...&path=…plan.md` → `#/debug` without reload.
`project`/`revision`/`docCount` survived (revision is monotonic; docCount falls back to the
first project), so the gap was **path-only** (and `type`, derived from path) — which masked
it for the other fields.

**Root cause:** `store.js` `syncFromHash()` ran on **every** `hashchange`, including
`#/debug`. `#/debug` carries no `project`/`path` query, so it dispatched `selection/set`
with empty values and `fetchOpenDoc()` reset `openDoc.path` to `""`; since
`selectDebugSnapshot().path = state.openDoc.path`, `/debug` read the just-cleared path.
Pre-refactor `uistate.js` (`fb37605^:auto-ui/web/static/uistate.js`) was a write-only
mirror **untouched by navigation**, so path survived.

**The fix (the only source change in this phase — `store.js`, +10/−1):** gate the
destructive selection/fetch update on the `explore` route. `#/debug` is a read-only view
of existing state, so the route handler must not drive selection:

```js
function syncFromHash() {
  const { view, params } = parseHash();
  if (view !== "explore") return;   // #/debug must not clear selection/openDoc (AC-4)
  // …mirror project/path/worktree into selection + orchestrate fetches as before…
}
```

This restores the pre-refactor "write-only mirror untouched by navigation" semantics: the
last explore selection survives the `#/debug` route change, so `selectDebugSnapshot` reads
the preserved `openDoc.path`. The hash stays the navigational source of truth for the
explore route (D-6); only `explore` drives selection + fetches. `fetchProjects()` is still
called separately in `initStore()`, so a cold load directly on `#/debug` still populates
the registry (and correctly shows an empty current-state — there is no explore selection
yet, matching the old `uistate.js` default).

**Verification of the fix** (captured: [`evidence/ac4-path-fix.txt`](./evidence/ac4-path-fix.txt)):

```
ON  #/explore…plan.md : snapshot.path = "docs/tasks/001-alpha/plan.md"
AFTER hash → #/debug  : selection = {project:"proj-a", path:"docs/tasks/001-alpha/plan.md", worktree:""}
                        openDoc.path = "docs/tasks/001-alpha/plan.md"
                        snapshot.path = "docs/tasks/001-alpha/plan.md"   ← survives ✓
```

No liveness regression from the change: 026 `reveal-harness.sh` re-run **UNEDITED** still
**3/3 PASS**; the AC-5 `on("doc.changed")` grep still matches only `store.js`; the AC-6
`index.html` importmap diff is still empty. `go build ./...` (embed) + `-tags dev` + `go
test ./...` all green in `auto-ui`.

## Cleanup

`state-harness.sh` kills its serve process, runs `agent-browser close --all`, and
`find "$TMP" -delete` per build (non-destructive temp-fixture removal).
