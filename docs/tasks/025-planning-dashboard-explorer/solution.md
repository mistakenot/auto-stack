# Solution: Task 025 — Planning Dashboard Explorer

> Phase 2 of the Planning Docs Dashboard epic (`docs/epics/002-planning-docs-dashboard.md`).
> **Frontend-only**: all changes live under `auto-ui/web/static/`. No Go changes — every backend
> contract (`project.list`, widened `doc.list`, `/api/doc/raw`, `/api/debug/recent`,
> `AUTO_UI_DEBUG` server gating) is delivered by **task 024 (Phase 1)**, which this task stacks on.

## Approach

Build the explorer on the existing no-build Preact + htm SPA, reusing the singleton `rpc.js`
client (`call`/`on`/`onStatus`) and the hash router. The explorer **replaces** the demo
`Home`/`Dashboard` views and becomes the landing view; a small WS connection indicator is the only
survivor of the demo chrome. The whole task is browse-and-render (static): the content pane and tree
fetch on navigation/project-switch only — **no `doc.changed` wiring** (liveness is task 026).

Five additive pieces, each mapping to an epic sub-task:

1. **Explorer shell + project switcher (AC-1 / 2.1).** A new `explorer.js` renders a two-pane
   layout (tree left, content right) under a header that hosts the **project switcher** and the
   connection indicator. The switcher is populated by `await call("project.list")` → `[{id, name,
   path, remote}]`. The active project + open doc live in the hash: `#/explore?project=…&path=…
   &worktree=…` (`worktree` optional). Switching projects rewrites the hash (`setHash`), re-lists
   docs, and clears the content pane. Loading an `#/explore?project=…` URL directly restores the
   view (the existing whole-App re-render on `hashchange` already gives this for free). Empty
   registry → an empty-state, not an error. The switcher control carries `data-testid="project-switcher"`;
   each option carries `data-project="<id>"`.

   **RPC readiness (cold-load).** `rpc.js`'s `call()` rejects synchronously when the socket is not
   `OPEN` (it has no queue), and `connect()` only *starts* the handshake at module load — so a fetch
   fired during the initial render (when the WS is still `CONNECTING`) deterministically rejects. The
   explorer is the landing view, so its mount-time fetches (`project.list` here, `doc.list` in
   `tree.js`, `doc.get` in `content.js`) **must gate on readiness**. Phase 1 adds an exported
   `whenOpen(): Promise<void>` to `rpc.js` that resolves immediately if already `open` and otherwise
   on the next `onStatus("open")`; every mount-time fetch does `await whenOpen()` before `call(...)`,
   and each fetching component **also subscribes to `onStatus` and re-fetches on a fresh `"open"`**
   (reconnect) so a dropped socket self-heals. A cold-load conformance assertion (open the page with
   no prior connection, assert switcher + tree populate **without a reload**) guards against
   regression.

<!-- RESOLVED(P2): Explorer-as-landing races the WS open — `call()` rejects "not connected" before the socket is OPEN
REVIEW: `rpc.js` `call()` rejects synchronously when `ws.readyState !== WebSocket.OPEN` (rpc.js:88) — there is no
queue or retry; `connect()` only kicks off the handshake at module load (rpc.js:112). Today this is masked because the
demo landing view is `Home`, which makes no RPC on mount (Dashboard's calls are button-gated, and `ping` arrives via
`on()` which needs no `call`). This task makes the **explorer** the default landing view, and both `explorer.js`
(`await call("project.list")` on mount) and `tree.js` (`await call("doc.list")` on mount) fire RPCs during the initial
render, when the WS is still CONNECTING. The first `project.list`/`doc.list` will deterministically reject "not
connected" on a cold load, rendering an empty/error explorer until a manual reload. Neither solution.md nor plan
Phase 3 (Step 3.1) specifies gating the initial fetch on `onStatus === "open"` (or retrying on reconnect). Please
specify the readiness strategy — e.g. defer the mount fetch until the first `onStatus("open")`, and re-fetch on
reconnect — and add a conformance assertion for cold-load (open the page, assert the switcher/tree populate without a
reload).
AUTHOR: Adopted. Added a "RPC readiness (cold-load)" paragraph to piece #1: Phase 1 exports
`whenOpen(): Promise<void>` from `rpc.js` (resolves immediately if `open`, else on the next `onStatus("open")`); every
mount-time fetch (`project.list`/`doc.list`/`doc.get`) does `await whenOpen()` before `call(...)`, and each fetching
component subscribes to `onStatus` to re-fetch on a fresh `"open"` (reconnect self-heal). Added `whenOpen` to the
rpc.js Files row + the Phase-1 plan steps (1.4b), threaded `await whenOpen()` into plan Steps 2.1/2.2/3.1, and added a
cold-load conformance assertion (AC-1: open with no prior connection → switcher+tree populate without reload).
-->


2. **Doc tree (AC-2 / 2.2).** `tree.js` fetches `await call("doc.list", {project, worktree})` →
   `[{id, path, type}]` and groups **client-side** by path prefix into a collapsible tree:
   `docs/tasks/NNN-slug/*` → **Tasks** → `NNN-slug` → files; `docs/epics/*` → **Epics**;
   `docs/research`, `reference`, `experiments`, `spikes` → their groups; files directly under
   `docs/` → **root docs**. Grouping is pure string parsing on the `path` field (the backend stays
   generic per the epic). Selecting a leaf calls `setHash("explore", {project, path, worktree})`,
   which loads the content pane. Each leaf carries `data-testid`, `data-doc-path="<path>"`,
   `data-doc-type="markdown|html"`; the tree root carries `data-doc-count="<n>"`.

3. **Type-aware content pane (AC-3 / 2.3).** `content.js` generalizes the current `doc.js`
   render logic, dispatching on the entry's `type` (known from the tree / re-derived from the
   `.md`/`.html` suffix):
   - **markdown** → `await call("doc.get", {project, path, worktree})` → `{path, markdown}`, rendered
     in-page via the existing `DOMPurify.sanitize(marked.parse(md))` + `dangerouslySetInnerHTML`.
   - **html** → an `<iframe>` whose `src` is `/api/doc/raw?project=…&path=…&worktree=…&v=<nonce>`
     (the raw route serves verbatim `text/html`; **never** through `doc.get`), with a sibling
     **"open in new tab"** `<a target="_blank">` to the same URL as fallback.
   The pane carries `data-revision` (incremented on every fetch/refresh — a `useRef` counter),
   `data-last-updated`, and a `data-testid` refresh button that forces a re-fetch / iframe
   `src`-nonce bump. The iframe carries `data-testid="doc-iframe"` and the observable `v=<nonce>`
   in its `src`. **No `doc.changed` subscription** is added here (static; 026 wires liveness).
   `auto ui` lands on `#/explore`; `app.js`’s view dispatch makes the explorer the default and the
   `Home`/`Dashboard` components are deleted.

4. **`window.__autoui` ring buffer (AC-4 / 2.4).** `rpc.js` gains an internal bounded ring
   (cap N≈200) recording every received notification `{t, method, params}` plus per-method counters.
   It is exposed as `window.__autoui` **only when debug is enabled** — gated on `?debug=1` in
   `location.search` (or `localStorage.autouiDebug==="1"`). With debug off, `window.__autoui` is not
   assigned (production stays clean). The agent harness drives the browser URL, so it opens
   `http://<addr>/?debug=1#/explore?project=…` to turn the buffer on. The ring is fed from the same
   `onmessage` dispatch path that feeds `on()` handlers, so it captures notifications no view
   subscribes to.

5. **`/debug` diagnostics page (AC-5 / 2.5).** `debug.js` adds a `#/debug` route to `app.js`
   rendering one screenshot-able page with four `data-testid`-tagged sections:
   - **Connection** — `onStatus` state (connecting/open/closed), a reconnect counter exported from
     `rpc.js`, `/api/hello` `mode`, and the bound port (read from `location.host`).
   - **Event log** — reverse-chronological received notifications (type, time, project, path,
     expandable raw payload). The page **subscribes live from mount** via `on()` for all relevant
     methods (`doc.changed`, `ping`, …) so it works even with debug off; when `window.__autoui` is
     present it backfills pre-mount history from it.
   - **Error log** — a small always-on bounded error ring in `rpc.js` with an exported
     `recordError(source, err)`. `rpc.js` natively captures what flows through it: `call()` rejects
     (`project.list`/`doc.list`/`doc.get`) and the global `window.onerror` / `unhandledrejection`
     hooks (installed once at startup). Failures that **don't** reach `rpc.js` must call
     `recordError` explicitly: `content.js` wraps `marked.parse` / `DOMPurify.sanitize` in
     try/catch → `recordError("markdown", e)`, and gives the `<iframe>` an `onError` →
     `recordError("iframe", …)` (iframe resource-load failures fire the element's own `onerror`, not
     `window.onerror`). So `content.js` imports `recordError`; the error log is the union of both
     sources.

<!-- RESOLVED(P2): "markdown parse/sanitize failures, iframe load errors" can't be captured by rpc.js alone
REVIEW: An error ring living in `rpc.js` can natively capture only what flows through rpc.js: `call()` rejects (it
wraps them), `window.onerror`, and `unhandledrejection`. But (a) markdown parse/sanitize happens inside `content.js`,
and (b) **iframe resource-load failures do NOT fire `window.onerror`** — they only fire the iframe element's own
`onerror`. So neither is captured unless `content.js` imports the exported `recordError(source, err)` and calls it
explicitly (wrap `marked.parse`/`DOMPurify.sanitize` in try/catch; add `onError` to the `<iframe>`). Plan Step 2.2
(content.js) makes no mention of importing or calling `recordError`, and the Files table for `content.js` omits it —
so AC-5's "iframe-load failures" + "parse/sanitize failures" rows will be empty as written. Add the `recordError`
wiring to the content.js plan step (and Files note), or narrow AC-5's error-log claim.
AUTHOR: Adopted. Reworded the error-log bullet to split native (rpc.js: `call()` rejects + `window.onerror`/
`unhandledrejection`) from explicit (`content.js` imports `recordError` and wraps `marked.parse`/`DOMPurify.sanitize`
in try/catch + adds `<iframe onError>`). Updated the `content.js` Files row to note the `recordError` import, and
amended plan Step 2.2 to wire both the markdown try/catch and the iframe `onError` to `recordError`.
-->

   - **Current state** — active project, open doc path/type, content `data-revision`, tree
     `data-doc-count`, last-updated. On `#/debug` the whole App re-renders, so `Explorer`/`DocTree`/
     `DocContent` are **not** in the DOM — these values can't be read cross-route. A tiny shared
     module `uistate.js` exports a module-level snapshot
     `uiState = {project, path, type, revision, docCount, lastUpdated}` plus `setUIState(patch)`;
     the explorer/tree/content **write** to it as they fetch/render, and `/debug` **reads** it. This
     is a deliberately minimal cross-route mutable (one object + a setter), **not** a general state
     store — view-local rendering still uses hash + `useEffect` (see Rejected Alternatives).
   The route is always reachable (read-only diagnostics on a trusted host); only the buffered
   pre-mount history is gated.

<!-- RESOLVED(P2): /debug "current state" can't read content `data-revision` / tree `data-doc-count` — explorer isn't mounted on #/debug
REVIEW: The app re-renders the whole tree on `hashchange` (app.js:142-148), so on `#/debug` the `Debug` component
renders and the `Explorer`/`DocTree`/`DocContent` are NOT in the DOM. Content `data-revision` and tree
`data-doc-count` live on explorer elements that don't exist on this route, so they can't be read from the DOM, and
the hash-as-state model carries only `project`/`path`/`worktree` — not revision or doc-count. Plan Step 5.1 hand-waves
this as "a small shared snapshot, or recompute from the DOM/`window.__autoui`", but no such snapshot is defined, and
recomputing from the DOM is impossible cross-route. This also collides with the "Introduce a state store" rejected
alternative — current-state genuinely needs a small cross-route shared mutable (e.g. a module-level
`uiState = {project, path, type, revision, docCount, lastUpdated}` updated by the explorer, read by `/debug`). Please
define that mechanism explicitly, or drop revision/doc-count from AC-5's current-state section.
AUTHOR: Adopted exactly the suggested mechanism. Defined `uistate.js` — a module-level
`uiState = {project, path, type, revision, docCount, lastUpdated}` + `setUIState(patch)`; explorer/tree/content write
to it on fetch/render, `/debug` reads it. Reworded the current-state bullet to state this, added `uistate.js` to the
Files table (new file) and the plan (Step 2.x/3.x write, Step 5.1 read), and sharpened the "Introduce a state store"
rejected-alternative to carve out this one minimal cross-route mutable (one object + setter) as the explicit exception
— view-local rendering still uses hash + `useEffect`.
-->


### Why frontend-only (no Go changes)

- Debug gating uses a client `?debug=1` flag, **not** a new server field — the epic explicitly
  allows "query param or settings", and this avoids touching the 024-owned server surface. The
  *server* half (`/api/debug/recent`, `AUTO_UI_DEBUG`) is already gated by 024; the `/debug` page
  optionally cross-checks it but does not require it.
- The bound port for the Connection section comes from `location.host`, so `/api/hello` needs no
  new field.
- New `.js` files are picked up automatically by `//go:embed all:static`; no embed/list change.
- No new runtime dependency: markdown still renders via the already-mapped `marked` + `dompurify`;
  HTML rides a plain `<iframe>`. The import map is unchanged (per the 013 "every transitive bare
  specifier must be mapped" lesson, we add none).

## Files

```
~ auto-ui/web/static/app.js       # delete Home/Dashboard; Explorer is the default view; add #/debug route; mount unchanged; small connection indicator in chrome
+ auto-ui/web/static/explorer.js  # Explorer shell: two-pane layout, project switcher (project.list), hash state, connection indicator
+ auto-ui/web/static/tree.js      # doc.list fetch + client-side prefix grouping into a collapsible tree; data-doc-* attrs; data-doc-count
+ auto-ui/web/static/content.js   # type-aware pane: markdown (doc.get + marked/dompurify) | html (<iframe src=/api/doc/raw …&v=nonce> + open-in-new-tab); data-revision/-last-updated; refresh button; imports recordError (md try/catch + iframe onError) + setUIState
+ auto-ui/web/static/debug.js     # #/debug page: connection / event log / error log / current state (reads uiState); subscribes live; backfills from window.__autoui
+ auto-ui/web/static/uistate.js   # module-level cross-route snapshot {project,path,type,revision,docCount,lastUpdated} + setUIState(patch); written by explorer/tree/content, read by /debug
~ auto-ui/web/static/rpc.js       # window.__autoui ring (gated by ?debug=1/localStorage); always-on error ring + recordError(source,err) + window.onerror/unhandledrejection capture; export reconnect count + whenOpen() readiness promise
- auto-ui/web/static/doc.js       # retired — render logic generalized into content.js; its (broken, inert) doc.changed subscription is NOT carried into the explorer (026 writes the correct one)
~ auto-ui/web/static/index.html   # reference new module(s) if needed; import map UNCHANGED (no new deps)
~ auto-ui/CLAUDE.md               # document the explorer views (explorer/tree/content/debug), the ?debug=1 gate, and window.__autoui
~ docs/epics/002-planning-docs-dashboard.md  # mark sub-tasks 2.1–2.5 status
+ docs/tasks/025-planning-dashboard-explorer/artifacts/conformance.md  # agent-browser validation script for AC-1..AC-5 (the acceptance harness)
```

### Validation (agent-browser, per epic "Validation & instrumentation")

No Go unit tests are added (frontend-only, no Go code changes). Acceptance is proven by an
**agent-browser conformance script** (artifact `conformance.md`, modeled on
`docs/tasks/013-auto-ui-tech-base/conformance.md`) run against the **embed build** and the
**dev build**, using the task-024 harness for an isolated instance:

1. Launch isolated: `auto ui serve --port 0 --ready-file <tmp> --projects <fixture-registry>`
   (`AUTO_UI_DEBUG=1`); read the bound addr from the ready-file JSON.
2. Build a fixture project with a `docs/` tree containing tasks/epics/research, at least one
   `.md` and one self-contained `.html` planning doc.
3. `agent-browser open http://<addr>/?debug=1#/explore` →
   - **AC-1:** switcher lists fixture projects (`get attr data-project`); switching re-lists +
     updates the hash; reload restores the view; empty-registry instance shows empty-state.
   - **AC-2:** tree groups correctly; `data-doc-count` matches `doc.list` length; selecting a node
     updates the hash and loads the pane (assert via `data-doc-path`/`data-doc-type`).
   - **AC-3:** a markdown doc renders inline; an HTML doc renders in the iframe (`get attr src`
     shows `/api/doc/raw…&v=`); "open in new tab" link present; refresh bumps `data-revision` and
     the iframe `v=` nonce; landing view is the explorer; no `Home`/`Dashboard`.
   - **AC-4:** `eval "typeof window.__autoui"` is `object` with `?debug=1`, `undefined` without it;
     `auto ui emit --project … --path docs/…/x.md` then
     `eval "window.__autoui.events.slice(-1)"` shows the `doc.changed` notification (buffer captures
     it even though no view re-renders — static).
   - **AC-5:** `agent-browser snapshot #/debug` shows connection/event-log/error-log/current-state
     sections (rows have `data-testid`); a forced error (bad `doc.get` path) appears in the error log.

## Test Coverage

| AC   | Test Type           | Where |
|------|---------------------|-------|
| AC-1 | agent-browser e2e   | artifacts/conformance.md — switcher list/switch/restore/empty |
| AC-2 | agent-browser e2e   | artifacts/conformance.md — tree grouping + data-doc-count + select |
| AC-3 | agent-browser e2e   | artifacts/conformance.md — md inline / html iframe / default view / revision+nonce |
| AC-4 | agent-browser e2e   | artifacts/conformance.md — window.__autoui gated; emit→event captured |
| AC-5 | agent-browser e2e   | artifacts/conformance.md — /debug sections render + error captured |

## Out of Scope

- **Backend** (`project.list`, `doc.list`/`doc.get`, `/api/doc/raw`, `/api/debug/recent`, the
  harness flags, `AUTO_UI_DEBUG` server gating) — task 024.
- **Liveness** — open-doc live refresh, the `doc.js` `ev.data.path` match fix, live nav-tree
  refresh: **task 026 (Phase 3)**. 025 adds **no** `doc.changed` subscription to any view.
- **Editing docs**, auth/hardening, multi-host, search/breadcrumbs/mermaid/dark-mode/persisted
  last-open (Phase 4).
- **Any bus / hook-production / envelope / wire-shape change** — tasks 020/021/022.
- **Technical:** no new import-map dependency; no `/api/hello` schema change; no embed/list change;
  no Go code touched.

## Rejected Alternatives

- **Gate the client debug surface via a new `/api/hello` `debug` field (server-read AUTO_UI_DEBUG).**
  Rejected for v1 — couples 025 to a 024-owned server change for no real benefit; the epic permits a
  query-param gate, and the harness controls the browser URL, so `?debug=1` is sufficient and keeps
  025 strictly frontend. (A future task can promote it to a server-authoritative flag.)
- **Keep a single monolithic `app.js`.** Rejected — the codebase splits by concern (`doc.js`,
  `router.js`, `rpc.js`); the explorer is large enough that `explorer.js`/`tree.js`/`content.js`/
  `debug.js` keeps each file legible and reviewable. (The split is a guideline, not load-bearing.)
- **Wire `doc.changed` into the content pane now (fix the match) for a quick liveness win.**
  Rejected — explicitly out of scope (task 026); the requirements pin 025 as the *static* explorer so
  liveness gets its own focused task with the match-fix + missing e2e coverage.
- **Render HTML docs inline (sanitize + inject) instead of an iframe.** Rejected per epic decision 1
  — self-contained `pd-components` HTML pulls scripts/CDN and must execute; sanitize-and-inline would
  break it. The iframe + raw route is the chosen path; `doc.get` stays markdown-only.
- **Introduce a state store (signals/redux-like).** Rejected — the established hash-as-state +
  per-component `useEffect(keyed on project/path)` pattern (exactly what `doc.js` does today) covers
  view-local rendering without new machinery. The **one** explicit exception is `uistate.js`: a
  single module-level object + `setUIState(patch)` so `/debug` (a sibling route where the explorer
  components are unmounted — the App re-renders the whole tree on `hashchange`) can read the current
  project / open-doc / `data-revision` / `data-doc-count`. This is a minimal cross-route snapshot,
  not a reactive store: nothing subscribes to it for rendering; `/debug` reads it on mount/refresh.
