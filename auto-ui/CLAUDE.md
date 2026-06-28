# Autoui

Local web dashboard and HTTP server for the auto stack. Serves a self-contained, no-build Preact+htm single-page app either embedded in the binary (default) or live-from-disk (dev mode) for fast iteration.

## Build

```bash
cd auto-ui
go build ./...
```

The merged `auto` binary is built from the repo root with `make build` (the UI tool ships as `auto ui`).

## Test

```bash
cd auto-ui
go test ./...
```

## Vet

```bash
cd auto-ui
go vet ./...
```

## Dev server

Live-from-disk assets — edit `web/static/*` and reload the browser with no Go rebuild. Build the `auto` binary with the `dev` tag, then run it from the `auto-ui/` module root (assets resolve relative to cwd):

```bash
go build -tags dev -o bin/auto ./auto-cli/cmd/auto   # from repo root
cd auto-ui && ../bin/auto ui serve
```

## Embedded single binary

The default build of the merged `auto` binary embeds `web/static/` via `//go:embed`, producing a self-contained binary:

```bash
make build && ./bin/auto ui serve
```

## Architecture

- `rootcmd/` — public seam mounted by the merged `auto` binary as `auto ui`
- `internal/app/` — runtime context (stdout, stderr, cwd)
- `internal/cli/` — Cobra commands (init, doctor, quickstart, docs, update, serve)
- `internal/config/` — settings loading and validation (~/.auto/ui/settings.json)
- `internal/server/` — HTTP handler: `/api/hello`, `/api/ws`, plus static file server
  - `rpc.go` — transport-agnostic JSON-RPC 2.0 dispatcher (request/response + notifications)
  - `ws.go` — `/api/ws` WebSocket handler (coder/websocket): per-connection session with a
    single write pump and hub-relayed server-push notifications
- `web/` — build-tag split asset delivery (`embed_prod.go` embeds, `embed_dev.go` reads from disk)
- `web/static/` — no-build Preact+htm SPA (index.html, app.js, router.js)
  - `rpc.js` — singleton JSON-RPC 2.0 client over WebSocket (`call`/`on`/`onAny`/`onStatus`); derives
    `wss://` vs `ws://` from the page origin so it works behind `tailscale serve` (HTTPS).
    Also exports `whenOpen()` (await before any mount-time `call()` so a cold load doesn't reject
    "not connected"), `connInfo()`/`reconnectCount()`, and `recordError(source, err)` /
    `recentErrors()` (an always-on bounded error ring fed by `call()` rejects, `window.onerror`,
    `unhandledrejection`, and explicit view-level records).
    `onAny(handler)` subscribes to ALL server-push notifications (any method) — used by the store
    for executor liveness tracking
  - `vendor/pico.min.css` — vendored Pico CSS v2 (embedded; offline-capable)

## The planning-docs explorer (default view)

`auto ui` lands on the **explorer** (`#/explore`) — a multi-project planning-docs browser, not a
demo. The old `Home`/`Dashboard`/`Doc` views are retired; `app.js` normalizes a bare/legacy hash to
`#/explore` and dispatches only two routes: the explorer and `#/debug`.

The explorer is composed from four `web/static/` modules:

- `explorer.js` — the two-pane shell + project switcher. On mount it `await whenOpen()` then
  `call("project.list")`, populating a `data-testid="project-switcher"` (each option carries
  `data-project="<id>"`); re-fetches on a fresh reconnect. An empty registry renders a
  `data-testid="no-projects"` empty-state (not an error). All view state lives in the hash
  (`#/explore?project=…&path=…&worktree=…`). Hosts a small `ConnIndicator`
  (`data-testid="conn-indicator"`, `data-conn-status`).
- `tree.js` — the nav tree. Reads docs via `useStore(selectDocs(...))` → groups the flat
  `[{path, type, meta?}]` **client-side** by path prefix (Tasks → `NNN-slug`; Epics; Research;
  Reference; Experiments; Spikes; root docs). Each leaf carries `data-testid="doc-node"`,
  `data-doc-path`, `data-doc-type`. HTML plans with `pd-meta` also carry `data-plan-status`,
  `data-review-state`, and `data-liveness` (active/idle) attributes. A spinner renders for
  `executing` plans; a review-state pill renders when `reviewState` is set. Liveness reads from
  `selectLiveness(state, project, branch)` in the store. The `nav` root carries `data-doc-count`.
- `content.js` — the type-aware pane (this **replaces the retired `doc.js`**, which is gone). It
  renders **by type**: markdown via `call("doc.get")` + `marked`/`dompurify` inline; HTML via an
  `<iframe src="/api/doc/raw?…&v=<nonce>" data-testid="doc-iframe">` (never through `doc.get`) plus
  an "open in new tab" link. The `article` root carries `data-revision` + `data-last-updated`; a
  `data-testid="doc-refresh"` button re-fetches markdown / bumps the iframe nonce. It is **live** (task
  026): a `doc.changed` subscription matching the open doc auto-applies the same refresh (see
  "Live updates" below). Markdown parse/sanitize and iframe-load failures are reported via
  `recordError`.
- `uistate.js` — a tiny module-level cross-route snapshot
  (`{project, path, type, revision, docCount, lastUpdated}` + `setUIState(patch)`). Explorer/tree/
  content write to it; `/debug` reads it (the explorer components are unmounted on `#/debug`, so the
  DOM can't be read cross-route). It is **not** a reactive store.

## Live updates (tasks 026, 027, 029)

The explorer refreshes itself when an agent edits a planning doc — no polling, no file watcher. The
only signal is the existing bus `doc.changed` notification. Since **task 047**, auto-ui no longer
ingests hook events locally: `auto hooks fire` posts to the **autowatch** daemon's hook-ingest (the
old auto-ui `POST /api/rpc` route is removed). autowatch stamps `hostId`, derives `doc.changed` via
`bus.DeriveDocChanged` (now the **sole** derive site), and **relays** the events to every subscribed
backend. auto-ui has subscribed to each backend since **task 045**, so its `BackendManager`
forwards relayed events straight into the Hub (`SetEventSink` → `hub.Broadcast`). The 045
transition-window `eventGate` id-dedup is gone (task 047): with a single ingest path an event can no
longer arrive by two routes, so there is nothing to dedup. The same sink also records into the
server debug ring so `/api/debug/recent` keeps reflecting received events now that local ingest is
gone. End to end: agent edit → hook → autowatch ingest + derive → relay → auto-ui Hub →
`doc.changed` on the WS.

Post-029, **all bus subscriptions live in `store.js`** (grep gate: `on("` only in `store.js`;
`onAny(` only in `rpc.js` + `store.js`). Views are presentational and read state via
`useStore`/selectors.

- `store.js` `on("doc.changed", ...)` — handles two cases:
  - **New doc path** under the active project: forces a `doc.list` re-list so the node appears.
  - **Known `.html` path**: also forces a re-list to pick up `pd-meta` changes (e.g.
    `planning` → `executing`). This is the AC-4 repaint path added by task 027.
  - Matching the open doc: triggers `refreshOpenDoc()` (markdown re-fetch or iframe nonce bump).
- `store.js` `onAny(...)` — **executor liveness** (task 027): subscribes to ALL notifications and
  records `(project, branch) → timestamp` for non-main/master branches in the `liveness` state
  slice. A 1s `setInterval` tick drives the `"Ns ago"` / `"Nm ago"` display.
- `docevents.js` — the **single source of truth** for reading the notification:
  `parseDocChanged(ev)` + `matchesDoc(ev, target)`, imported by the store.

## Plan metadata and liveness (task 027)

`doc.list` entries for HTML plans with a `<script type="application/json" id="pd-meta">` block
carry an optional `meta` field (`PlanMeta` struct server-side, parsed by `ExtractPlanMeta` in
`docs.go`). Fields: `id`, `name`, `status` (planning/executing/merged), `branch`, `epic`,
`created`, `pr`, `reviewState` (from `<pd-doc status="...">`). Markdown and non-pd HTML entries
omit `meta` (`omitempty`).

The store's `liveness` slice (`{ byKey: {(project\0branch): timestamp}, now: ms }`) tracks
executor activity. `selectLiveness(state, project, branch)` returns `{ ageMs, active }` or `null`
(null for missing/main/master branches). The active window defaults to 120s; overridable in debug
via `?liveWindowMs=N`. Tree leaves read liveness via `useStore((s) => selectLiveness(s, project,
meta?.branch))`.

**THE GOTCHA:** the changed path is at **`ev.data.path`** (== `params.data.path` on the wire), NOT
top-level `ev.path`. `Event.AsNotification` (`auto-shared/bus/event.go`) puts the whole event
envelope under JSON-RPC `params`; the envelope carries top-level `project`/`worktree` but `path`/
`abs_path`/`branch` live under `data`. The retired `doc.js` read `ev.path` (always `undefined`), so
its live refresh never fired — reading `ev.data.path` (via `docevents.js`) is the fix. Since task
047 moved the sole derive site to autowatch, the shape pin lives there:
`TestHookIngest_DeriveDocChanged_RegisteredProject`
(`auto-watch/internal/rpcserver/ingest_test.go`) asserts the derived `doc.changed` carries
`data.path`, so it can't silently regress. (It replaced auto-ui's deleted
`rpc_ingest_test.go`/`TestRPCIngestBroadcastAndDerive`.)

## Host dimension (task 046)

auto-ui is a **proxy** (post-042): it dials one or more autowatch backends
(`auto ui backends add <uri>`), each reporting a `hostId` via `daemon.status`. UI
identity is therefore `(hostId, projectId)` end to end (GR-F8) — two hosts can expose
the same project id, so host disambiguates everywhere.

- **Aggregating `project.list`** (`internal/server/project_aggregate.go`) fans
  `project.list` out to every connected backend CONCURRENTLY, tags each returned
  project with its authoritative `host` (the backend's learned hostId), and merges in
  hostID-sorted order. A backend whose call errors is **skipped** (partial results) —
  the list never fails because one backend is down. This replaces the single-backend
  `Resolve("")` that errored `ambiguous host` with >1 backend.
- **`backends.list`** (`project_aggregate.go` `backendsList`) returns
  `Manager.Health()` verbatim — every known backend's `{hostId, uri, connected,
  relayDegraded, lastErr}`. It is a single-lock local snapshot (no fan-out).
- **Per-backend status UI** (`explorer.js` `BackendHealth`): one row per backend,
  `[data-testid=backend-health]` carrying `data-host-id` + `data-connected`
  (+ `data-state` connected/degraded/disconnected). Fed by the store's `backends`
  slice. NB: `store.js` `fetchBackends` runs only on initial load and on a store
  (re)connect (`onStatus` open) — there is **no** event-driven re-fetch on a passive
  backend drop, so rows refresh on the next reconnect / page reload, not the instant a
  backend dies. (The server's `Health()` flips immediately; gated by
  `backends_list_test.go` + `manager_test.go`.)
- **Host badge** (`explorer.js`): `[data-testid=host-badge][data-host-id=<activeHost>]`
  — **always shown**, even with one host (D-4), for consistent identity.
- **Host-aware `matchesDoc`** (`docevents.js`): a host differs only when **both** the
  event and the target carry one (`if (target.host && c.host && c.host !== target.host)
  return false`) — a legacy host-less event still matches by project. The re-list path
  in `store.js`'s `on("doc.changed")` mirrors this guard. 045 stamps `Host` on relayed
  events, so two same-named projects on different hosts never cross-refresh.
- **`host=` hash param** (D-3): selection is `{host, project, path, worktree}`; the
  hash carries a separate `host=`. `selectActiveHost` falls back to the active
  project's host (then the first project's host) so a legacy host-less URL resolves to
  the sole backend (AC-7 back-compat).
- **`docsKey(host, project, worktree)`** (`store.js`): the `docsByProject` cache is
  now keyed by all three (fully positional, `\0`-joined) — host AND worktree matter
  because `doc.list` is routed to a specific backend and sent with the selected
  worktree. The raw-doc iframe `src` and `doc.list`/`doc.get` params all carry
  `hostId` (omitted when empty so a single-backend URL stays clean).

Host-dimension acceptance is browser-driven + Go-gated — see
`docs/tasks/046-multi-host-spa/artifacts/conformance.md` and `evidence/`.

## Debug surfaces

- **`window.__autoui`** — a bounded ring of every received WS notification (`{t, method, params}`)
  plus per-method counters, exposed by `rpc.js` **only when debug is enabled**: `?debug=1` in the URL
  query **or** `localStorage.autouiDebug === "1"`. Production (no flag) never gets the global. The
  ring records even for notifications no view subscribed to, so a harness can
  `eval "window.__autoui.events.filter(e=>e.method==='doc.changed')"`.
- **`#/debug`** (`debug.js`) — a screenshot-able read-only diagnostics page with four
  `data-testid`-tagged sections: `debug-connection` (status/reconnects/`/api/hello` mode/host),
  `debug-event-log` (live bus events from mount + backfill from `window.__autoui`),
  `debug-error-log` (`recentErrors()`), and `debug-current-state` (reads the `uistate.js` snapshot).
  The route is always reachable; only the pre-mount event backfill depends on `?debug=1`.

Acceptance for the explorer is browser-driven (frontend-only) — see
`docs/tasks/025-planning-dashboard-explorer/artifacts/conformance.md` (static explorer),
`docs/tasks/026-planning-dashboard-live-updates/artifacts/conformance.md` (doc liveness), and
`docs/tasks/027-plan-status-executor-liveness/artifacts/conformance.md` (plan status + executor
liveness), each with its own `evidence/` where applicable.
