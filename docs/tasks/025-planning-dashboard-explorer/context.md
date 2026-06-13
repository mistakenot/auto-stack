# Context: Task 025 — Planning Dashboard Explorer

> Verified against source on 2026-06-13. Repo `/home/vscode/src/auto-stack`. This task is
> **frontend-only** and **stacks on task 024** — the backend endpoints it consumes do **not** exist
> on `main` yet (they are 024's deliverable, in progress on another thread).

## Frontend SPA (the surface this task rewrites)

All under `auto-ui/web/static/`. No-build Preact + htm, ES modules loaded via an esm.sh import map.

### `app.js` (current demo shell — to be replaced)
- Imports `render` (preact), `useState`/`useEffect` (preact/hooks), `html` (htm/preact),
  `parseHash`/`setHash`/`onRouteChange` (`router.js`), `call`/`on`/`onStatus` (`rpc.js`),
  `DocView` (`doc.js`).
- `Nav` (≈line 11-28): three buttons (Home/Dashboard/Docs), each `setHash(viewId, params)`.
- Views: `Home` (≈30-41, `?n=` counter), `Dashboard` (≈47-122, ping/WS demo), `DocView` (from doc.js).
- View dispatch (≈125-140): `parseHash()` → `{view, params}` (default `"home"`); App renders the
  matching component.
- Mount: `render(html\`<${App} />\`, document.getElementById("app"))`; re-renders the whole App on
  `onRouteChange` (hashchange).
- **025:** delete `Home`/`Dashboard`; make the explorer the default view; add a `#/debug` route;
  keep a small connection indicator distilled from the `Dashboard` ping/WS code.

### `router.js` (hash routing — reused as-is)
- `parseHash()` → `{ view: path.replace(/^\//,"") || "home", params: URLSearchParams(qs) }`.
  Hash format: `#/<view>?<query>`.
- `setHash(view, params)` → `location.hash = "/<view>?<qs>"` (omits `?` when empty).
- `onRouteChange(cb)` → `addEventListener("hashchange", cb)`.
- **025 uses** `#/explore?project=…&path=…&worktree=…` and `#/debug`.

### `rpc.js` (singleton JSON-RPC over WS — extended by 025)
- Exports: `call(method, params) → Promise` (resolves `msg.result`, rejects `msg.error`),
  `on(method, handler) → unsub`, `onStatus(handler) → unsub` (`"connecting"|"open"|"closed"`, fires
  immediately).
- `wsURL()` derives `ws://`/`wss://` from `location.protocol` (tailscale-serve compatible); endpoint
  `/api/ws`. Reconnect: exponential backoff 500ms→5000ms.
- **CRITICAL — notification callback shape:** in `onmessage`, a notification (`msg.method`, no `id`)
  invokes each handler as **`handler(msg.params)`** — the raw `params`, **not** the full message and
  **not** `params.data`. For `doc.changed`, `params` is the **full bus.Event envelope**:
  top-level `project`/`worktree` exist, but the changed **`path` is nested at `params.data.path`**
  (see auto-bus-spec). This is why `doc.js`'s `ev.path` match is always `undefined` (the known bug).
- **025 adds (in this file):** a bounded notification ring exposed as `window.__autoui` **only when**
  `?debug=1`/`localStorage.autouiDebug`; an always-on bounded error ring + `window.onerror` /
  `unhandledrejection` capture; an exported reconnect counter for the `/debug` Connection section.

### `doc.js` (current viewer — generalized into `content.js`, then retired)
- Reads `project`/`path`/`worktree` from URL params.
- `doc.list` (≈26-33) → flat picker; `doc.get` (≈36-51) → `{path, markdown}`.
- Renders markdown via `DOMPurify.sanitize(marked.parse(markdown))` + `dangerouslySetInnerHTML`
  (≈line 114).
- **Broken `doc.changed` subscription (≈56-68):** `if (ev.path !== open.path) return;` — `ev.path`
  is undefined (lives at `ev.data.path`). **025 does NOT fix or carry this** — the explorer content
  pane has no `doc.changed` wiring; the correct subscription is task 026's job.

### `index.html` (bootstrap — import map UNCHANGED)
- Import map (≈10-23): `preact@10.24.3`, `preact/hooks` (`*`-externalized), `htm@3.1.1` +
  `htm/preact`, `react`/`react-dom` → preact/compat, `marked@13.0.0`, `dompurify@3.2.4`.
- **Lesson (task 013 feedback):** every transitive bare specifier a dep imports must also be mapped,
  or modules fail silently → blank page. **025 adds no new dep**, so the map is untouched.
- CSS: vendored `./vendor/pico.min.css` (the only vendored asset). Mount: `<div id="app">` +
  `<script type="module" src="./app.js">`.

### Static asset serving / build split
- `auto-ui/web/embed_prod.go` (`//go:build !dev`): `//go:embed all:static`; `Mode="embed"`;
  `FS()=fs.Sub(content,"static")`. **New `.js` files are auto-embedded** — no list to update.
- `auto-ui/web/embed_dev.go` (`//go:build dev`): `Mode="disk"`; `FS()=os.DirFS("web/static")` (run
  from `auto-ui/`). Dev mode must send `Cache-Control: no-store` (013 feedback) so edited modules
  aren't stale.
- Served by `http.FileServer(http.FS(fsys))` at `/` (server.go ≈line 85), index.html fallback.

## Backend contracts 025 consumes (ALL from task 024 — verify they exist before building)

> On `main` today only `ping`, `doc.list` (.md-only), `doc.get` (.md-only), `/api/hello`, `/api/ws`,
> `/api/rpc` exist. The four below are task-024 deliverables — **025 must build on a branch stacked
> on 024 (or after 024 merges).**

- **`project.list` RPC** → `[{id, name, path, remote}]`; empty registry → `[]`. `remote` is passed
  through `git.NormalizeRemoteURL` server-side (credential-stripping at the UI boundary) — 025 can
  trust it is credential-free.
- **`doc.list` (widened)** → `[]docEntry` where `docEntry{ID, Path, Type}` and
  `Type ∈ {"markdown","html"}` (currently `{ID, Path}`, `.md`-only). Grouping is left to the client.
- **`/api/doc/raw?project=…&path=…&worktree=…`** (HTTP GET) → verbatim file bytes,
  `Content-Type: text/html`; only `.html` (validated by `cleanDocPath(path,".html")`); reuses
  `resolveRoot` (resolves optional `worktree`). This is the iframe `src` target. `doc.get` stays
  markdown-only.
- **`/api/debug/recent`** (HTTP GET, gated by server `AUTO_UI_DEBUG=1`, else 404) → last N raw +
  derived events. Optional cross-check for the `/debug` page's server half.

### `/api/hello` (exists — used unchanged)
- `GET /api/hello` → `{message, mode}` where `mode ∈ {"embed","disk"}`. **025 reads `mode`** for the
  `/debug` Connection section; **bound port comes from `location.host`** (no schema change needed).

### `doc.changed` notification (consumed only by the `/debug` event log; NOT for liveness)
- Derived server-side from `agent.tool.post` in `rpc_ingest.go` via `bus.DeriveDocChanged`, broadcast
  through the Hub, delivered as `ev.AsNotification()` → `{jsonrpc, method:"doc.changed", params:<Event>}`.
- Task 024 widens derivation coverage to `docs/**/*.html` as well as `*.md` (`isDocPath`).
- 025's `window.__autoui` and `/debug` event log just **record** these; no view re-renders on them.

## Agent-harness surface 025 validates against (task 024 Phase 1.5–1.7)

- `auto ui serve --port 0 --ready-file <path> --projects <fixture>`; ready-file is JSON
  `{"addr":"127.0.0.1:NNNN"}`; `AUTO_UI_PORT`/`AUTO_PROJECTS_PATH` envs honored.
- `auto ui emit --project <id> --path docs/…/x.md [--worktree …]` → POSTs a valid `agent.tool.post`
  to `/api/rpc` with **no `Origin` header** → derives one `doc.changed`. **Origin rule:** `/api/rpc`
  rejects any `Origin`-bearing request (403) — agents **trigger via CLI, observe via browser**.
- `AUTO_UI_DEBUG=1` gates the server `/api/debug/recent`.

## Conventions & constraints

- **No `data-testid` / agent-browser harness exists yet** — 025 introduces both. The acceptance
  attributes are mandated by the epic's "Validation & instrumentation" table: `data-testid`,
  `data-project`, `data-doc-path`, `data-doc-type`, `data-doc-count`, `data-revision`,
  `data-last-updated`, iframe cache-bust nonce.
- **Conformance must be re-run after any frontend/asset change** (013 feedback) — Go tests alone
  don't cover browser-layer defects (blank page, stale cache, iframe load). Validate both the
  **embed** artifact and the **dev** build.
- **Task 021 owns the hook→ui→socket bus loop** — 025 is a pure consumer; no bus/envelope/derivation
  change here. **Normalize remotes before any UI boundary** — already done server-side in
  `project.list`; 025 must not undo it (don't reconstruct credentialed URLs client-side).
- `auto-ui/CLAUDE.md` documents the web layer (`rpc.js` singleton, `doc.js`, `router.js`,
  `/api/hello` readiness, `/api/ws`) — update it for the new explorer views.

## Path-prefix grouping (client-side derivation for the tree)

From each `doc.list` entry's `path` (relative, forward-slashed, always under `docs/`):

| Path shape | Group | Sub-group |
|---|---|---|
| `docs/tasks/NNN-slug/<file>` | Tasks | `NNN-slug` |
| `docs/epics/<file>` and `docs/epics/phase*/…` | Epics | (optional phase) |
| `docs/research/<file>` | Research | — |
| `docs/reference/<file>` | Reference | — |
| `docs/experiments/<…>/<file>` | Experiments | experiment dir |
| `docs/spikes/<file>` | Spikes | — |
| `docs/<file>` (direct child) | Root docs | — |

Pure string parsing on the first segment after `docs/`; unknown prefixes fall into a generic group
named after the segment.

## Open risks / notes

- **Hard dependency on 024.** None of 025's ACs are demonstrable until `project.list`, the widened
  `doc.list`, and `/api/doc/raw` exist. Build 025 on a branch stacked on 024's branch; do not target
  `main` until 024 merges.
- **Iframe + trusted network.** The raw-doc iframe is intentionally **not** script-sandboxed
  (pd-components need scripts + CDN). This is a deliberate epic decision (single trusted host); the
  path-traversal guard in `/api/doc/raw` (024) is the only safeguard kept.
- **Whole-App re-render on hashchange** is the existing model; the explorer's per-pane state uses
  `useEffect` keyed on `{project, path, worktree}` (the `doc.js` pattern) — sufficient, no store.
