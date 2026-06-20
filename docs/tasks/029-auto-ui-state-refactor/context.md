# Context: Task 029 — auto-ui-state-refactor

Codebase context for refactoring the auto-ui SPA's client-side state into a single
normalised store hook with presentational components. See [plan.html](plan.html) for
requirements and the chosen approach.

## Key Files (the state surface to refactor)

### Module loading — no new dependency needed
- `auto-ui/web/static/index.html:10-23` — the importmap. Pins `preact@10.24.3`,
  `preact/hooks@10.24.3`, `htm@3.1.1`, `marked@13.0.0`, `dompurify@3.2.4` (all via
  esm.sh; `*`-prefix on transitive specifiers to avoid duplicate instances — a 013
  lesson). **The chosen module-singleton store + `useStore` hook needs only `useState`/
  `useEffect` (already imported from `preact/hooks`) — NO new importmap entry.** (Satisfies
  the repo's "minimal runtime deps" rule.)

<!-- RESOLVED(P3): createContext rationale is moot under the accepted design (D-5)
REVIEW: This note justifies a Context-based store ("createContext is exported by preact main…").
The accepted Solution (D-5) is a module-singleton store with NO Context — the useStore hook is
built on useState+useEffect only. The export facts are true but no longer load-bearing; consider
trimming to just "no new importmap entry needed (useState/useEffect already available)" to avoid
implying Context is still the plan. Verified: index.html's importmap has no createContext/
useContext entry, and the chosen design needs none.
AUTHOR: Trimmed the note to state only that the module-singleton store + useStore hook needs
useState/useEffect (already imported) → no new importmap entry. Dropped the createContext/
useContext justification. Also corrected the render-lifecycle note below (was "useReducer store +
provider", now module-singleton survives nav unconditionally).
-->


### Current state mechanisms (four, overlapping)
- `auto-ui/web/static/router.js` — hash = navigational state (`parseHash`/`setHash`/
  `onRouteChange`). **Stays as-is.**
- `auto-ui/web/static/rpc.js` — singleton WS + pub/sub. `call` (rpc.js:164), `on`
  (rpc.js:183), `onStatus` (rpc.js:190), `whenOpen` (rpc.js:209), `connInfo`/
  `reconnectCount`, `recordError`/`recentErrors`, and the always-on bounded event ring
  exposed as `window.__autoui` only when `debugEnabled()` (rpc.js:92-94). **This is the
  one good shared layer; the store builds ON it, does not replace it.**
- `auto-ui/web/static/uistate.js` — the non-reactive write-only mirror
  `{project,path,type,revision,docCount,lastUpdated}` + `setUIState`. **To be DELETED;
  its fields move into the store and `/debug` reads the store.**
- Component-local `useState`/`useEffect` in explorer/tree/content — each fetches + owns
  its own slice. **To become presentational (read store, no own fetched data).**

### Call-site inventory (every site the refactor touches)
- `call(...)`: `explorer.js:65` (`project.list`), `tree.js:343` (`doc.list`),
  `content.js:90` (`doc.get`).
- `setUIState(...)`: `explorer.js:99,104`, `tree.js:345`, `content.js:77`.
- `uiState` reads: `debug.js:97,101` (1s poll snapshot — the only consumer).
- `on("doc.changed", ...)`: `content.js:130` (open-doc refresh, deps
  `[project,path,worktree,effType]`), `tree.js:385` (tree growth on unseen path, deps
  `[project,worktree]`). `debug.js:84-85` also subscribes `doc.changed`+`ping` for its log.
- `onStatus(...)`: `explorer.js:29` (ConnIndicator), `explorer.js:85` + `tree.js:362`
  (reconnect self-heal), `debug.js:48`.
- `whenOpen()`: `explorer.js:64`, `tree.js:339`, `content.js:87` (cold-load gates).

### The render lifecycle (load-bearing)
- `auto-ui/web/static/app.js:36-43` — `mount()` calls `render(html``<${App}/>``, #app)` on
  every `hashchange`. App (app.js:14-22) is a pure view selector: `view==="debug"` →
  `<Debug>`, else `<Explorer>`. **Preact `render()` on the same container DIFFS against the
  prior tree (update, not remount); only the swapped child (Explorer↔Debug) unmounts.**
  Under the chosen **module-singleton** store (D-5), state lives in module scope (like
  `rpc.js`/`uistate.js`) and survives navigation **unconditionally** — independent of
  reconcile — which is exactly why `uistate.js` can be
  deleted. (`uistate.js` exists today only because the *explorer subtree* unmounts on
  `#/debug`, so the sibling Debug page couldn't read the explorer's component state.)

### debug.js — the uistate.js consumer to repoint
- `auto-ui/web/static/debug.js:12-13` imports `{on,onStatus,connInfo,reconnectCount,
  recentErrors}` from rpc.js and `{uiState}` from uistate.js.
- `debug.js:97,101` snapshot `{...uiState}` initially + on a 1s `setInterval`.
- Renders four `data-testid` sections: `debug-connection` (debug.js:117),
  `debug-event-log` (145), `debug-error-log` (182), `debug-current-state` (210, rows read
  `snapshot.{project,path,type,revision,docCount,lastUpdated}`). **`debug-current-state`
  must read the store instead of `uiState`.**

### Live-update wire shape (must not regress)
- `auto-ui/web/static/docevents.js` — `parseDocChanged(ev)` + `matchesDoc(ev,target)`,
  the single source of truth. **THE GOTCHA: changed path is `ev.data.path`, NOT
  `ev.path`.** Pinned server-side by `auto-ui/internal/server/rpc_ingest_test.go`
  (`TestRPCIngestBroadcastAndDerive` asserts `params.data.path`). **Keep using
  `docevents.js`; the centralized subscription parses through it.**

### Backend (untouched by this task)
- RPCs the client consumes: `project.list` → `[{id,name,path,remote}]`; `doc.list
  {project,worktree}` → `[{id,path,type}]` (type `markdown|html`); `doc.get` →
  `{path,markdown}` (markdown only). `GET /api/doc/raw?…&v=<nonce>` serves `.html`.
  `/api/hello` → `{message,mode}` (`embed|disk`). No server/RPC/wire changes in 029.

## Patterns

- **Module-level singletons are the established idiom** for non-hash shared state:
  `rpc.js` (connection + ring) and `uistate.js` (snapshot) are both module singletons.
  The repo CLAUDE notes "all state lives in URL or module-level singletons." The store
  fits this idiom.
- **Subscribe-via-effect bridge**: `ConnIndicator` (explorer.js:26-31) already turns an
  rpc.js subscription into Preact state with `useState`+`useEffect(()=>onStatus(set),[])`.
  The store's `useStore` hook reuses exactly this shim — no `useSyncExternalStore`, no new
  importmap entry.
- **Cold-load + reconnect self-heal is non-negotiable**: every fetch gates on
  `whenOpen()` and re-fetches on a *fresh* `"open"` transition (tracked via a `prevStatus`
  ref so the initial open doesn't double-fetch — explorer.js:83-91, tree.js:360-368). The
  store must centralize this, preserving the "don't double-fetch on first open" guard.
- **Path-keyed expansion / stable keys**: `tree.js` keeps reconcile-safe open/closed state
  by keying `Collapsible` on stable group names and tracking a force-open token set
  (`expanded`) — load-bearing for the 026 "reveal a new doc" behaviour. Presentational
  tree must keep these keys.
- **Assert via `data-*` + `window.__autoui`, never rendered-text diffs** — a re-fetch can
  leave the DOM identical (026 conformance philosophy). All `data-*` hooks
  (`data-doc-count`, `data-revision`, `data-last-updated`, `data-conn-status`,
  `data-testid`, `data-doc-path`/`-type`, `data-project`) must remain on the same elements
  with the same names.
- **Browser is the real oracle**: Go tests pass while browser-layer defects hide (013
  feedback). Validate on BOTH embed and `-tags dev` builds.

## Related Tasks
- **024** (planning-dashboard-backend) — defined the RPCs/raw route the client consumes; pins
  `params.data.path` in `rpc_ingest_test.go`. Untouched here.
- **025** (planning-dashboard-explorer) — built the explorer + the `data-*`/`window.__autoui`/
  `/debug` conformance contract. Its `artifacts/conformance.md` is the static-behaviour
  regression oracle this refactor must pass unchanged.
- **026** (planning-dashboard-live-updates) — added liveness (`docevents.js`, the two
  `doc.changed` behaviours, reveal). Its `artifacts/conformance.md` + `reveal-harness.sh`
  are the liveness regression oracle. The centralized subscription must reproduce both
  behaviours.
- **028** (bus-event-host-field) — adds `Host` to `bus.Event`. Anticipated, NOT implemented
  here: the store's `selection` keeps `{project,path,worktree}` (D-3); a `host` dimension is
  a later additive change to one reducer shape + the selector layer.
- **Epic 003 / `docs/research/multi-host-architecture.md`** — the exploratory multi-host
  direction this cleanup opens the door for (per-host fan-in via the selector layer). Out of
  scope for 029.

## Related History & Lessons (git)

The whole SPA was built across three tasks in June 2026; the state mechanisms this refactor
touches were introduced very recently, so there is little legacy drift:

- `uistate.js` introduced in **9424680** (025, 2026-06-14) — documented as "deliberately
  minimal, no reactivity" purely for cross-route `/debug` reads (025 feedback). This refactor
  retires it.
- `docevents.js` introduced in **39cc6c8** / merged **5c8d963** (026) — the `params.data.path`
  parser. Keep it; the centralized subscription parses through it.
- Live-tree behaviour landed across **7947d42** (flash touched node + ancestors), **865a7b5**
  (reveal newly-created docs), **df2d76c** (Drafting Table redesign), **a288797** (tasks
  newest-first + top-10 cap) — all 026. The presentational `tree.js` must preserve every one.

**Lessons that change how this is executed/verified (quote + source):**
- **013 feedback (`013-auto-ui-tech-base/feedback.md`):** the esm.sh `*` external-all importmap
  prefix once made the SPA render blank while Go tests passed — "every defect in this task was
  invisible to the Go toolchain… trust the conformance run over the test suite for the
  frontend." → AC-6 must diff the importmap; verification runs in the browser on **both** builds.
- **025 feedback:** registry **ids must be lowercase-kebab** (`^[a-z0-9]+(?:-[a-z0-9]+)*$`) — the
  029 harness fixture must use lowercase ids. Cold-load race: `call()` rejects synchronously when
  the socket isn't `OPEN`; the `whenOpen()` gate + reconnect self-heal are mandatory (now owned by
  the store, AC-7).
- **026 feedback:** `data-revision` is **not deterministic across page opens — assert the DELTA,
  never an absolute** (record before, compare after). Events propagate async over WS; **poll up to
  ~2.5–3s** after `auto ui emit` before asserting (don't read immediately). `Collapsible` keyed by
  stable **name** makes expansion survive reconcile "for free" — the presentational tree must keep
  that keying.

**Path verification (CB3, 2026-06-19):** all files referenced by the Solution tab exist at their
current paths; `auto-ui/web/static/store.js` is correctly absent (it is the 029 deliverable). The
026 artifacts `artifacts/conformance.md`, `artifacts/reveal-harness.sh`, `artifacts/
reveal-conformance.md`, and `artifacts/evidence/` all still exist unmoved.
