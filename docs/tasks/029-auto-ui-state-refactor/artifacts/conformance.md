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
| `embed` (`/api/hello` → `mode:embed`) | 20 | **19 PASS / 1 FAIL** |
| `-tags dev` (`/api/hello` → `mode:disk`, served from `auto-ui/web/static`) | 20 | **19 PASS / 1 FAIL** |

The single failure is **identical on both builds**: **AC-4 path-survival** — a real
source regression the refactor introduced, which the harness correctly catches (it is
*not* a harness flake). See **"AC-4 regression (open issue)"** below. All other AC-3..AC-8
assertions pass on both builds. Full run log:
[`evidence/state-harness-run.txt`](./evidence/state-harness-run.txt).

> **The harness asserts AC-4 as written (path MUST survive) and therefore exits
> non-zero until the source is fixed.** Per the Phase-3 contract ("if a harness fails
> because of a real source bug, STOP and report it — don't quietly patch source in the
> test phase"), the AC-4 assertion was left faithful and the bug is reported rather than
> masked. The remaining 19 assertions are green on both builds.

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
| **AC-4** | `uistate.js` deleted; `/debug` reflects live store state across nav | **runtime** (browser) + HTTP | select project+doc on `#/explore` (record `data-revision` R, `data-doc-count` N); `location.hash='#/debug'` **without reload**; read `debug-current-state` rows → `project`/`path`/`revision`/`docCount` must equal R/N; `curl /uistate.js` → **404** | **project ✓ / revision ✓ / docCount ✓ / uistate.js 404 ✓ / `path` ✗** | same |
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

## AC-4 regression (OPEN ISSUE — reported, not patched)

**Status: FAIL on both builds.** `debug-current-state` `path` shows `—` after navigating
`#/explore?...&path=…plan.md` → `#/debug` without reload, instead of the open doc's path.
`project`, `revision (R)`, and `docCount (N)` *do* survive, so the regression is
**path-only** (and `type`, which derives from path).

**Root cause** (captured: [`evidence/ac4-path-regression.txt`](./evidence/ac4-path-regression.txt)):
`store.js` `syncFromHash()` runs on **every** `hashchange`, including the `#/debug` route.
`#/debug` carries no `project`/`path` query, so it dispatches `selection/set` with empty
values and `fetchOpenDoc()` resets `openDoc.path` to `""`. Because
`selectDebugSnapshot().path = state.openDoc.path`, `/debug` reads the just-cleared path.

```
ON  #/explore…plan.md : snapshot.path = "docs/tasks/001-alpha/plan.md"
AFTER hash → #/debug  : selection = {project:"",path:"",worktree:""}, openDoc.path = ""
                        snapshot.path = ""   ← regression
```

Pre-refactor (`uistate.js`, see `fb37605^:auto-ui/web/static/uistate.js`) this was a
**write-only mirror untouched by navigation** — the explorer wrote `path` into it and
`#/debug` only *read* it, so path survived. `revision` survives now only because it is
monotonic (never reset); `docCount` survives because `selectActiveProject` falls back to
the first registered project — both mask the regression for those fields.

This contradicts **AC-4** ("`debug-current-state` shows the same `project`/**`path`**/
`revision (R)`/`docCount (N)` — sourced from the store, module-scope state survives the
route change **unconditionally**") and **Verification → Known Gaps** ("store-survives-
navigation … survives any unmount/remount"). The gap is that the *route handler itself*
actively clears selection on a param-less route — a path the design note overlooked.

**Suggested fix (for the implementation phase, NOT applied here):** make `#/debug`
non-destructive to `selection`/`openDoc` — e.g. `syncFromHash()` should only mirror
selection for the `explore` view (skip clearing on `#/debug`), **or** `selectDebugSnapshot`
should read a `lastSelection`/`lastOpenDoc` the store retains across a param-less route,
restoring the `uistate.js` "last-known explorer state" semantics. Once fixed, AC-4's
`path` assertion will pass and the harness will exit 0 on both builds.

## Cleanup

`state-harness.sh` kills its serve process, runs `agent-browser close --all`, and
`find "$TMP" -delete` per build (non-destructive temp-fixture removal).
