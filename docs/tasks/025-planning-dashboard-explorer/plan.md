# Plan: Task 025 — Planning Dashboard Explorer

## Summary

Build the planning-docs explorer as the default `auto ui` view in six frontend-only phases — an
observability foundation in `rpc.js`, the leaf render components (`tree.js`, `content.js`), the
composed explorer shell + project switcher (`explorer.js`), the default-view cutover + connection
indicator, the `/debug` diagnostics page (`debug.js`), then docs + the agent-browser conformance
run. **No Go code changes**; all backend endpoints come from task 024.

## Preconditions (read first)

- **Stack on task 024.** 025's ACs consume `project.list`, the widened `doc.list` (`{path,type}`),
  `/api/doc/raw`, and the validation harness — **none exist on `main` yet**. Branch this work off
  **024's branch** (`task/024-planning-dashboard-backend`), not `main`. Do not target `main` until
  024 merges. (Per CLAUDE.md worktree discipline, fetch + base on the up-to-date 024 tip.)
- **Validation needs a running server with 024's endpoints.** Per-phase behavioral checks use
  `agent-browser` against an isolated instance launched with 024's harness
  (`auto ui serve --port 0 --ready-file <tmp> --projects <fixture>`, `AUTO_UI_DEBUG=1`). Syntax is
  gated per file with `node --check`.
- **Frontend, no Go tests.** `go build ./...` always succeeds (assets are `//go:embed`-ed), so it is
  *not* a real check here. Acceptance is the conformance artifact (`artifacts/conformance.md`),
  re-run on **both** the embed and dev builds (013 feedback: browser-layer defects are invisible to
  Go).

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| ~ | auto-ui/web/static/rpc.js | `window.__autoui` ring (gated `?debug=1`/localStorage); always-on error ring + `recordError(source,err)` + `window.onerror`/`unhandledrejection`; export reconnect count + status/counters getter + `whenOpen()` readiness promise |
| + | auto-ui/web/static/uistate.js | module-level cross-route snapshot `{project,path,type,revision,docCount,lastUpdated}` + `setUIState(patch)`; written by explorer/tree/content, read by `/debug` |
| + | auto-ui/web/static/tree.js | `await whenOpen()` → `doc.list` fetch + client-side prefix grouping → collapsible tree; `data-doc-path`/`-type`, `data-doc-count`; writes `docCount` to `uiState`; re-fetch on `onStatus("open")` |
| + | auto-ui/web/static/content.js | type-aware pane: markdown (`doc.get`+marked/dompurify) \| html (`<iframe src=/api/doc/raw…&v=nonce>` + open-in-new-tab); `data-revision`/`-last-updated`; refresh button; `await whenOpen()`; imports `recordError` (md try/catch + iframe `onError`); writes `revision`/`path`/`type` to `uiState` |
| + | auto-ui/web/static/explorer.js | two-pane shell + project switcher (`project.list`, normalized remote); `await whenOpen()` + re-fetch on reconnect; hash state; empty-state; connection indicator; writes `project` to `uiState` |
| + | auto-ui/web/static/debug.js | `#/debug` page: connection / event log / error log / current state (reads `uiState`); live-subscribed; backfills from `window.__autoui` |
| ~ | auto-ui/web/static/app.js | explorer is the default view; add `#/explore` + `#/debug` routes; delete Home/Dashboard; small connection indicator |
| - | auto-ui/web/static/doc.js | retired — logic generalized into `content.js`; broken `doc.changed` match NOT carried forward |
| ~ | auto-ui/web/static/index.html | reference new module(s) as needed; **import map unchanged** |
| ~ | auto-ui/CLAUDE.md | document explorer/tree/content/debug views, `?debug=1` gate, `window.__autoui` |
| ~ | docs/epics/002-planning-docs-dashboard.md | mark sub-tasks 2.1–2.5 status |
| + | docs/tasks/025-planning-dashboard-explorer/artifacts/conformance.md | agent-browser acceptance harness for AC-1..AC-5 |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)
- [Conformance harness](./artifacts/conformance.md)

## How to Test
- [ ] `node --check` passes on every edited/added `.js` file.
- [ ] `artifacts/conformance.md` AC-1 — switcher lists fixture projects; switch re-lists + updates hash; reload restores; empty registry → empty-state.
- [ ] `artifacts/conformance.md` AC-2 — tree groups (Tasks/Epics/Research/…); `data-doc-count` == `doc.list` length; selecting a node updates hash + loads pane.
- [ ] `artifacts/conformance.md` AC-3 — md renders inline; html renders in iframe (`src` has `/api/doc/raw…&v=`); open-in-new-tab present; refresh bumps `data-revision` + nonce; explorer is the landing view; no Home/Dashboard.
- [ ] `artifacts/conformance.md` AC-4 — `window.__autoui` is object with `?debug=1`, undefined without; `auto ui emit` → buffered `doc.changed` visible via `eval`.
- [ ] `artifacts/conformance.md` AC-5 — `#/debug` shows the four sections (rows have `data-testid`); a forced bad `doc.get` appears in the error log.
- [ ] Conformance re-run on **embed** build AND **dev** (`-tags dev`) build.

## Execution Sequence
```
Phase 1 (rpc.js obs) ─┐
                      ├─> Phase 3 (explorer.js + #/explore) ─> Phase 4 (default view + retire demo) ─> Phase 5 (debug.js + #/debug) ─> Phase 6 (docs + conformance run)
Phase 2 (tree+content)┘
```
> **Seriality note (project rule):** concurrent subagents sharing one worktree leak writes into the
> MAIN worktree. Phases 1 and 2 touch disjoint files (`rpc.js` vs new `tree.js`/`content.js`) and are
> dependency-independent, but **dispatch them serially** unless each runs in its own isolated
> worktree. Phases 3–6 each edit `app.js`/shared files and are strictly serial.

## Plan

### Phase 1: `rpc.js` observability foundation (AC-4 substrate + AC-5 substrate)
> Land first (epic guidance: 2.4 early) — validatable against the *current* demo app before the
> explorer exists, de-risking the observation layer.
- [x] Step 1.1: Add a module-level bounded ring (cap `N=200`) recording every received notification as `{t, method, params}` in the `onmessage` notification branch (`rpc.js` ≈line 63, the same path that fans out to `on()` handlers), plus a per-method counter map.
- [x] Step 1.2: Add a `debugEnabled()` helper: true when `new URLSearchParams(location.search).get("debug")==="1"` **or** `localStorage.getItem("autouiDebug")==="1"`. When true, assign `window.__autoui = { events, counters, max:N }` (live references); when false, never assign it.
- [x] Step 1.3: Add an always-on bounded error ring (cap 100) and a `recordError(source, err)` helper; capture failed RPC calls (wrap the reject path in `call`), and install `window.onerror` + `window.addEventListener("unhandledrejection", …)` once at module init. Export `recentErrors()`.
- [x] Step 1.4: Export `reconnectCount()` (increment the existing `reconnect` path, `rpc.js` ≈line 67) and a `connInfo()` getter returning `{status, reconnects}` for the `/debug` Connection section.
- [x] Step 1.4b: Export `whenOpen(): Promise<void>` — resolves immediately if `status==="open"`, else resolves on the next `onStatus` transition to `"open"`. This is the readiness gate every mount-time fetch awaits before `call(...)` (the explorer is the landing view, so its first `project.list`/`doc.list`/`doc.get` would otherwise reject "not connected" on a cold load). Also export `recordError(source, err)` (referenced by Step 1.3) so `content.js` can record markdown/iframe failures that never reach `rpc.js`.
- [x] Step 1.5: Verify: `node --check auto-ui/web/static/rpc.js`. Launch the **current** app (`auto ui serve --port 0 --ready-file /tmp/r.json`, read addr); `agent-browser open http://<addr>/?debug=1` → `eval "typeof window.__autoui"` is `"object"`; open `http://<addr>/` (no param) → `eval "typeof window.__autoui"` is `"undefined"`. Trigger a ping (already streaming) → `eval "window.__autoui.events.length"` > 0.
- [x] Step 1.6: Commit: `feat(025): phase 1 — rpc.js observability (window.__autoui, error ring, reconnect count)`

### Phase 2: Leaf render components `tree.js` + `content.js` (AC-2, AC-3 render logic)
> New files only; no `app.js` wiring yet (composed in Phase 3). Independent of Phase 1.
- [x] Step 2.0: Create `uistate.js` — a module-level `uiState = {project, path, type, revision, docCount, lastUpdated}` and `setUIState(patch)` that shallow-merges into it. No reactivity (nothing subscribes for rendering); `/debug` reads it on mount/refresh. This is the single cross-route shared mutable (consumed in Steps 2.1/2.2/3.1 writes, Step 5.1 read).
- [x] Step 2.1: Create `tree.js` exporting a `DocTree({project, worktree, selected, onSelect})` component: on mount/prop-change `await whenOpen()` then `call("doc.list", {project, worktree})` → `[{id,path,type}]`; group client-side by path prefix per the context.md table (Tasks→`NNN-slug`→files; Epics; Research; Reference; Experiments; Spikes; root docs; unknown prefix → generic group). Render a collapsible tree; each leaf carries `data-testid="doc-node"`, `data-doc-path`, `data-doc-type`; the tree root carries `data-doc-count="<n>"`. Clicking a leaf calls `onSelect(path, type)`. Write `setUIState({docCount: n})` after listing; subscribe to `onStatus` and re-fetch on a fresh `"open"` (reconnect self-heal).
- [x] Step 2.2: Create `content.js` exporting a `DocContent({project, path, type, worktree})` component generalizing `doc.js`'s render: for `type==="markdown"` (or `.md` suffix) `await whenOpen()` then `call("doc.get", {project,path,worktree})` → render `DOMPurify.sanitize(marked.parse(markdown))` **wrapped in try/catch → `recordError("markdown", e)`**; for `type==="html"` (or `.html`) render `<iframe src="/api/doc/raw?project=…&path=…&worktree=…&v=<nonce>" data-testid="doc-iframe" onError=${() => recordError("iframe", …)}>` + an `<a target="_blank" rel="noopener">open in new tab</a>` to the same URL. A `useRef` revision counter increments on each fetch/nonce-bump; expose it via `data-revision` + `data-last-updated` on the pane root and a `data-testid="doc-refresh"` button that re-fetches / bumps the nonce. Write `setUIState({path, type, revision, lastUpdated})` on each (re-)fetch. **No `doc.changed` subscription.** Import `recordError`/`setUIState`; use `URLSearchParams` to build the raw URL (encode path/worktree).
- [x] Step 2.3: Verify: `node --check` on both files. (Behavioral validation deferred to Phase 3 when composed — note this in the commit body.)
- [x] Step 2.4: Commit: `feat(025): phase 2 — tree.js + content.js render components`

### Phase 3: `explorer.js` shell + project switcher + `#/explore` route (AC-1; AC-2/AC-3 validatable)
- [ ] Step 3.1: Create `explorer.js` exporting an `Explorer({params})` component: read `project`/`path`/`worktree` from `params` (URLSearchParams). On mount `await whenOpen()` then `call("project.list")` → `[{id,name,path,remote}]`; render a switcher (`data-testid="project-switcher"`, each option `data-project="<id>"`) defaulting to the hash project (or first project). Selecting a project `setHash("explore", {project})` (clears `path`) and `setUIState({project})`. Two-pane layout: `<${DocTree} …>` left, `<${DocContent} …>` right; tree `onSelect` does `setHash("explore", {project, path, worktree})`. Empty `project.list` → an empty-state element (`data-testid="no-projects"`), not an error. Subscribe to `onStatus` and re-fetch `project.list` on a fresh `"open"` (reconnect self-heal). Include a `<${ConnIndicator}/>` (Step 3.2) in the header.
- [ ] Step 3.2: Add a small `ConnIndicator` component (in `explorer.js` or a tiny shared spot) using `onStatus` (distilled from the Dashboard ping/WS code in `app.js` ≈line 70-75): a dot + `connected`/`reconnecting`/`closed` label, `data-testid="conn-indicator"`, `data-conn-status`.
- [ ] Step 3.3: Wire `#/explore` into `app.js`'s view dispatch (render `<${Explorer} params=${params} />` when `view==="explore"`); leave the demo views reachable for now (default flips in Phase 4).
- [ ] Step 3.4: Verify (agent-browser, isolated instance with a **fixture registry of ≥2 projects**, each with a `docs/` tree containing tasks/epics/research + ≥1 `.md` and ≥1 self-contained `.html`):
  - AC-1: `open http://<addr>/#/explore` → `get attr data-project` lists fixture projects; click a switcher option → hash gains `project=`, tree re-lists, content clears; reload on `#/explore?project=X` restores; a **second** isolated instance with an empty fixture registry shows `data-testid="no-projects"`.
  - AC-2: tree groups correctly; `eval` tree `data-doc-count` == `doc.list` length; click a node → hash gains `path=`, pane loads (assert `data-doc-path`/`data-doc-type`).
  - AC-3: select a `.md` → markdown renders inline; select a `.html` → `get attr [data-testid=doc-iframe] src` contains `/api/doc/raw` + `v=`; open-in-new-tab link present; click `data-testid="doc-refresh"` → `data-revision` increments and the iframe `v=` nonce changes.
- [ ] Step 3.5: Commit: `feat(025): phase 3 — explorer shell, project switcher, #/explore route`

### Phase 4: Make the explorer the default view; retire demo; connection indicator (AC-3 default-view clause)
- [ ] Step 4.1: In `app.js`, change the default/empty hash to land on `#/explore` (e.g. `parseHash` default view or a redirect when `view` is empty/`home`). Remove the `Home` and `Dashboard` components and their `Nav` buttons; remove the `DocView` import.
- [ ] Step 4.2: Delete `auto-ui/web/static/doc.js` (its render logic now lives in `content.js`; its broken `doc.changed` match is intentionally gone — 026 writes the correct one). Update `index.html` if it referenced `doc.js` (it does not import directly, but confirm no dangling reference).
- [ ] Step 4.3: Ensure the `ConnIndicator` is present in the explorer chrome (already added Phase 3); keep `Nav`/chrome minimal (project switcher + indicator only).
- [ ] Step 4.4: Verify (agent-browser): `open http://<addr>/` (bare, no hash) lands on the explorer (`get url` shows `#/explore`); `eval` confirms no `Home`/`Dashboard` text/buttons; `data-testid="conn-indicator"` present and reads `connected`; `node --check app.js`; confirm `doc.js` is gone (`! test -f auto-ui/web/static/doc.js`).
- [ ] Step 4.5: Commit: `feat(025): phase 4 — explorer is the default view; retire demo pages`

### Phase 5: `#/debug` diagnostics page (AC-5)
- [ ] Step 5.1: Create `debug.js` exporting a `Debug()` component rendering four `data-testid`-tagged sections — **connection** (`onStatus` + `connInfo()`/`reconnectCount()` from Phase 1; `/api/hello` `mode` via `fetch`; bound port from `location.host`), **event log** (subscribe from mount via `on("doc.changed",…)`, `on("ping",…)` into local state — reverse-chronological: type/time/project/path + expandable raw payload; backfill from `window.__autoui.events` when present), **error log** (`recentErrors()` from Phase 1, refreshed on a short interval or on new error), **current state** (active project, open doc path/type, content `data-revision`, tree `data-doc-count`, last-updated — **read from `uistate.js`'s `uiState` snapshot**, which the explorer/tree/content write via `setUIState`; the explorer components are unmounted on `#/debug`, so DOM-read is impossible cross-route).
- [ ] Step 5.2: Wire `#/debug` into `app.js`'s view dispatch (`<${Debug}/>` when `view==="debug"`). The route is always reachable (read-only); only the pre-mount history (backfill) depends on `?debug=1`.
- [ ] Step 5.3: Verify (agent-browser): `open http://<addr>/?debug=1#/debug`; `snapshot` shows all four sections with `data-testid` rows; `connInfo` shows `open`/`connected` and `mode`; run `auto ui emit --project <id> --path docs/…/x.md` → the event log gains a `doc.changed` row; force an error (`agent-browser` navigates the explorer to a bad `doc.get` path, or `eval` calls `call("doc.get",{project:"x",path:"docs/nope.md"})`) → it appears in the error-log section.
- [ ] Step 5.4: Commit: `feat(025): phase 5 — /debug diagnostics page`

### Phase 6: Docs + conformance artifact + full acceptance run — depends on all
- [ ] Step 6.1: Fill in `artifacts/conformance.md` (skeleton from planning) with the exact agent-browser commands, the fixture-registry + `docs/` tree setup, and the AC-1..AC-5 assertions; include the **fixture builder** (temp dir + `projects.json` + sample `.md`/`.html` docs) and cleanup.
- [ ] Step 6.2: Run the full conformance suite against the **embed** build (`go build` default) — all AC-1..AC-5 pass; capture evidence (screenshots / `eval` outputs) under `artifacts/evidence/`.
- [ ] Step 6.3: Run the full conformance suite against the **dev** build (`-tags dev`, served from `auto-ui/`) — confirms `Cache-Control: no-store` + disk serving still work end-to-end (013 feedback).
- [ ] Step 6.4: Update `auto-ui/CLAUDE.md` (explorer/tree/content/debug views, `?debug=1` gate, `window.__autoui`, the `/debug` route). Mark epic `docs/epics/002-planning-docs-dashboard.md` sub-tasks 2.1–2.5 status and tick this plan's boxes.
- [ ] Step 6.5: `node --check` on all `.js`; `gofmt`/`make check` (no Go changed, but run the repo gate for stale-refs/format on the docs). Commit: `docs(025): conformance harness, CLAUDE.md, mark epic Phase 2 sub-tasks`

## Success Criteria
- [ ] AC-1: `project.list`-backed switcher lists every project; switching re-lists + updates the hash; direct `#/explore?project=` URL restores; empty registry → empty-state, not error (conformance AC-1).
- [ ] AC-2: tree groups the whole `docs/` tree client-side by prefix; leaves carry `data-doc-path`/`-type`; root `data-doc-count` == `doc.list` length; selection routes + loads (conformance AC-2).
- [ ] AC-3: markdown renders inline, HTML renders in `<iframe src=/api/doc/raw…&v=nonce>` + open-in-new-tab; content pane carries `data-revision`/`-last-updated`; explorer is the default landing view; demo pages gone; connection indicator present (conformance AC-3).
- [ ] AC-4: `window.__autoui` exposed only under `?debug=1`/localStorage; records every notification with payload; `auto ui emit` event is captured even though no view re-renders (conformance AC-4).
- [ ] AC-5: `#/debug` renders connection/event-log/error-log/current-state with `data-testid` rows; live-subscribed from mount; captures a forced error (conformance AC-5).
- [ ] Conformance passes on **both** embed and dev builds; `node --check` clean; no Go code changed (only `//go:embed`-ed assets + docs); import map unchanged; no `doc.changed` subscription added to any view.

## Open Questions
- (none — requirements Open Questions resolved: static explorer only, Phase 3 liveness → task 026; small connection indicator kept.)
