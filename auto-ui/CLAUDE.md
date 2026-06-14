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
    single write pump, a 1s server-push `ping` notification, and a client-callable `ping` RPC
- `web/` — build-tag split asset delivery (`embed_prod.go` embeds, `embed_dev.go` reads from disk)
- `web/static/` — no-build Preact+htm SPA (index.html, app.js, router.js)
  - `rpc.js` — singleton JSON-RPC 2.0 client over WebSocket (`call`/`on`/`onStatus`); derives
    `wss://` vs `ws://` from the page origin so it works behind `tailscale serve` (HTTPS).
    Also exports `whenOpen()` (await before any mount-time `call()` so a cold load doesn't reject
    "not connected"), `connInfo()`/`reconnectCount()`, and `recordError(source, err)` /
    `recentErrors()` (an always-on bounded error ring fed by `call()` rejects, `window.onerror`,
    `unhandledrejection`, and explicit view-level records)
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
- `tree.js` — the nav tree. `call("doc.list", {project, worktree})` → groups the flat
  `[{path, type}]` **client-side** by path prefix (Tasks → `NNN-slug`; Epics; Research; Reference;
  Experiments; Spikes; root docs). Each leaf carries `data-testid="doc-node"`, `data-doc-path`,
  `data-doc-type`; the `nav` root carries `data-doc-count`.
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

## Live updates (task 026)

The explorer refreshes itself when an agent edits a planning doc — no polling, no file watcher. The
only signal is the existing bus `doc.changed` notification: agent edit → hook → `agent.tool.post` →
`/api/rpc` ingest → `bus.DeriveDocChanged` → a `doc.changed` on the WS. Two views subscribe:

- `content.js` — a `doc.changed` matching the **open** doc's `{project, path}` (and `worktree` when
  present) auto-applies the refresh action: markdown re-runs `doc.get` + re-renders
  (`data-revision++`), HTML bumps the iframe `v=<nonce>` (cache-busted reload, no `doc.get`). A
  non-matching event is a no-op.
- `tree.js` — a `doc.changed` for the **active project** carrying a path the tree does **not** yet
  know (a newly created doc) triggers exactly one `doc.list` re-list + regroup, so the new node
  appears (`data-doc-count` grows). Known-path edits need no re-list (content.js handles those).
  Expansion state survives the reconcile because `Collapsible` is keyed by stable group name.

- `docevents.js` — the **single source of truth** for reading the notification:
  `parseDocChanged(ev)` + `matchesDoc(ev, target)`, imported by both views.

**THE GOTCHA:** the changed path is at **`ev.data.path`** (== `params.data.path` on the wire), NOT
top-level `ev.path`. `Event.AsNotification` (`auto-shared/bus/event.go`) puts the whole event
envelope under JSON-RPC `params`; the envelope carries top-level `project`/`worktree` but `path`/
`abs_path`/`branch` live under `data`. The retired `doc.js` read `ev.path` (always `undefined`), so
its live refresh never fired — reading `ev.data.path` (via `docevents.js`) is the fix.
`rpc_ingest_test.go`'s `TestRPCIngestBroadcastAndDerive` pins `params.data.path` so the shape can't
silently regress.

## Debug surfaces

- **`window.__autoui`** — a bounded ring of every received WS notification (`{t, method, params}`)
  plus per-method counters, exposed by `rpc.js` **only when debug is enabled**: `?debug=1` in the URL
  query **or** `localStorage.autouiDebug === "1"`. Production (no flag) never gets the global. The
  ring records even for notifications no view subscribed to, so a harness can
  `eval "window.__autoui.events.filter(e=>e.method==='doc.changed')"`.
- **`#/debug`** (`debug.js`) — a screenshot-able read-only diagnostics page with four
  `data-testid`-tagged sections: `debug-connection` (status/reconnects/`/api/hello` mode/host),
  `debug-event-log` (live `on("doc.changed"/"ping")` from mount + backfill from `window.__autoui`),
  `debug-error-log` (`recentErrors()`), and `debug-current-state` (reads the `uistate.js` snapshot).
  The route is always reachable; only the pre-mount event backfill depends on `?debug=1`.

Acceptance for the explorer is browser-driven (frontend-only) — see
`docs/tasks/025-planning-dashboard-explorer/artifacts/conformance.md` (static explorer) and
`docs/tasks/026-planning-dashboard-live-updates/artifacts/conformance.md` (liveness), each with its
own `evidence/`.
