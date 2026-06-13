# Context: Task 026 — Planning Dashboard Live Updates

Verified codebase facts grounding the liveness solution (`solution.md`): the `doc.changed` wire
shape (and the exact bug to fix), the rpc.js subscription path, the 025 explorer surfaces 026 wires
into, the backend test to extend, and the 024 harness used for validation. All facts confirmed in
source as of 2026-06-13.

## Key Files

### The wire shape (this is the whole task)
- `auto-shared/bus/event.go:28-41` — `Event` struct. Top-level json fields: `specversion`, `type`,
  `source`, `id`, `time`, `project`, `session`, `remote`, `branch`, `worktree`, `commit`. `Data` is
  `json.RawMessage` — opaque at the envelope level, serialized under `"data"`.
- `auto-shared/bus/payloads.go:23-30` — the `doc.changed` data payload:
  ```go
  type DocChanged struct {
    Project  string `json:"project"`
    Path     string `json:"path"`
    AbsPath  string `json:"abs_path"`
    Worktree string `json:"worktree"`
    Branch   string `json:"branch"`
  }
  ```
  So `path` lives at `params.data.path`; `project`/`worktree` exist both top-level (envelope) and
  inside `data`.
- `auto-shared/bus/event.go:103-109` — `Event.AsNotification()` returns
  `Notification{JSONRPC:"2.0", Method: ev.Type, Params: ev}` — `Params` is the **full Event
  envelope**, not the unwrapped data. The client receives
  `{method:"doc.changed", params:{type:"doc.changed", project, worktree, data:{path, …}}}`.
- `auto-shared/bus/derive.go:69-72` — `isDocPath` **already** matches `.md` AND `.html`
  (024 / 1.4 is live): `strings.HasPrefix(rel,"docs/") && (HasSuffix ".md" || HasSuffix ".html")`.
  HTML liveness needs no further derive change.

### The bug to fix (reference; doc.js is retired by 025)
- `auto-ui/web/static/doc.js:55-68` — the broken subscription 026 replaces:
  ```js
  on("doc.changed", (ev) => {
    if (ev.project !== open.project) return;
    if (ev.path !== open.path) return;       // BUG: ev.path is undefined; path is at ev.data.path
    if (open.worktree && ev.worktree && ev.worktree !== open.worktree) return;
    fetchDoc();
  });
  ```
  `ev.project` and `ev.worktree` (envelope top-level) do read correctly; only `ev.path` is wrong.
  025 retires `doc.js` into `content.js` and does **not** carry this subscription over.

### rpc.js dispatch (how a notification reaches a handler)
- `auto-ui/web/static/rpc.js:86-110` — `call(method, params)` (Promise, id-correlated),
  `on(method, handler)` (returns unsubscribe), `onStatus(handler)`.
- `auto-ui/web/static/rpc.js:45-65` — `onmessage`: messages with an `id` resolve a pending `call`;
  messages with a `method` and no id are notifications → `notifyHandlers.get(msg.method)` handlers
  are each called with **`msg.params`** (the full Event envelope for `doc.changed`).
- `auto-ui/web/static/rpc.js:67-82` — reconnect: on close, fail in-flight `pending`; exponential
  backoff 500ms→5s; `notifyHandlers` map persists across reconnects, so `on()` subscriptions survive
  a reconnect (no re-registration needed). **Implication:** 026's subscription has no cold-load
  problem — registration is synchronous, and the re-fetch only fires on an arriving event (socket
  already OPEN).

### Backend ingest + the test to extend
- `auto-ui/internal/server/rpc_ingest.go:27-92` — POST `/api/rpc` parses the JSON-RPC frame into
  `bus.Event`, `hub.Broadcast(ev)` (raw), then `derived := bus.DeriveDocChanged(ev, reg)` and
  broadcasts each derived event.
- `auto-ui/internal/server/rpc_ingest_test.go:58-121` — `TestRPCIngestBroadcastAndDerive`. Lines
  111-120 read the `doc.changed` message and assert only `docParams["type"] == "doc.changed"`.
  **Never asserts `params.data.path`** — AC-1 extends this to assert
  `docParams["data"].(map[string]any)["path"]` equals the emitted path.

### 025 explorer surfaces 026 wires into (assumed landed)
- `auto-ui/web/static/content.js` — type-aware pane (generalized DocView). Markdown → `doc.get` +
  `marked`/`dompurify`; HTML → `<iframe src="/api/doc/raw?project=…&path=…&worktree=…&v=<nonce>">`
  with an open-in-new-tab fallback. Carries `data-revision` (increments on every fetch/refresh),
  `data-last-updated`, a `data-testid` refresh button, and `data-testid="doc-iframe"`. 026 adds the
  `doc.changed` subscription that calls the existing refresh action.
- `auto-ui/web/static/tree.js` — `doc.list` fetch + client-side prefix grouping; leaves carry
  `data-doc-path`/`data-doc-type`, root carries `data-doc-count`. 026 adds the re-list-on-unseen-path
  subscription. **Coupling risk:** expansion state must be keyed by stable path/prefix (not array
  index) for AC-3's "preserve expand/collapse across reconcile" — verify against 025's actual impl.
- `auto-ui/web/static/rpc.js` — 025 adds `window.__autoui` ring buffer (gated by `?debug=1` /
  `localStorage.autouiDebug`), fed from the same `onmessage` notification path — captures every
  `doc.changed` even when no view re-renders. 026's conformance asserts via this buffer.
- `auto-ui/web/static/app.js` + `router.js` — hash routing (`#/<view>?<query>`), whole-app
  re-render on `hashchange`; 025 makes `#/explore` the landing view and adds `#/debug`.

### 024 harness (validation)
- `auto-ui/internal/cli/emit.go:35-139` — `auto ui emit --project <id> --path <docs/…> [--worktree …]
  [--port …]`: builds a `bus.ToolPost` envelope, POSTs to `http://127.0.0.1:PORT/api/rpc` with **no
  Origin header** (ingest rejects cross-origin; agents trigger via CLI, observe via browser). Honors
  `AUTO_UI_PORT`.
- `auto-ui/internal/cli/serve.go:23-152` — `--port 0` (OS-assigned), `--ready-file <path>` writes
  `{"addr":"127.0.0.1:NNNN"}\n` after bind, `--projects <path>` (or `AUTO_PROJECTS_PATH`) points at
  an isolated fixture registry. Port precedence: flag > `AUTO_UI_PORT` > settings > 8080.
- `auto-ui/internal/server/server.go:29-34` + `serve.go:96` — `WithDebug(AUTO_UI_DEBUG=="1")` enables
  `GET /api/debug/recent` (last N raw+derived events); 404 otherwise.

## Patterns

- **Liveness = consume the bus signal, never poll** (epic decision 2). Both the open-doc refresh and
  the tree refresh subscribe to the single `doc.changed` stream; no file watcher.
- **Events are invalidations, not state transfer** (`docs/auto-bus-spec.md` §6.2): `doc.changed`
  carries no content — the client must `doc.get` (markdown) or reload the raw iframe (html) to get
  fresh state. Delivery is **at-most-once / lossy** (no acks, replay, or durability).
- **Observe via attributes + `window.__autoui`, never text diffs** (epic "Validation &
  instrumentation"): a re-fetch can leave the DOM identical, so liveness is asserted via
  `data-revision`, the iframe `v=` nonce, `data-doc-count`, and the event ring buffer.
- **Frontend-only conventions** (`auto-ui/CLAUDE.md`, task 013 lesson): no new import-map bare
  specifiers; `//go:embed all:static` auto-picks new `.js`; embed-vs-dev is a build tag (`-tags
  dev`), validate the embed build and iterate on the dev build; `Cache-Control: no-store` on dev
  assets avoids stale `.js`.
- **agent-browser conformance as acceptance** (`docs/tasks/013-auto-ui-tech-base/conformance.md`,
  `docs/tasks/025-.../artifacts/conformance.md`): launch isolated via the 024 harness, drive a real
  browser, assert via `eval`/`get attr`/`snapshot`, run both builds, save evidence to the task folder.

## Related Tasks

- **Task 024** (Phase 1, merged #78): `project.list`, widened `doc.list` (`.md`+`.html`),
  `/api/doc/raw`, `.html` `doc.changed` derivation (1.4 — `isDocPath` already widened), the agent
  harness (`auto ui emit`, `--port 0`/`--ready-file`/`--projects`, `/api/debug/recent`). 026 consumes
  all of it.
- **Task 025** (Phase 2, in progress): the static explorer (`explorer.js`/`tree.js`/`content.js`/
  `rpc.js` `window.__autoui`/`#/debug`). Explicitly adds **no** `doc.changed` subscription and does
  **not** carry over `doc.js`'s broken match — "fixing and wiring it is 026's job." 025 has open P2
  review items (cold-load WS race, `recordError` wiring, `/debug` cross-route state) that are **025's
  to close**; 026 does not depend on them.
- **Sequencing fact (git, 2026-06-13):** on `main`, `auto-ui/web/static/` contains only `app.js`,
  `doc.js`, `router.js`, `rpc.js`. The 025 files 026 modifies (`content.js`, `tree.js`) and the
  `window.__autoui` / `#/debug` additions **do not exist on main yet** — 025 is unmerged. **026
  cannot start until 025 lands.** Recent merges: `feat(024) #78`, `feat(021) #75`,
  `feat(013) #56` (the SPA tech base + `conformance.md` pattern). No commit yet mentions
  `doc.changed` liveness on the client — 026 is the first.
- **Task 021** (auto-bus-standard, merged #75): owns the CloudEvents bus + `doc.changed` derivation
  and the `Event.AsNotification` envelope shape 026 reads. 026 is a pure consumer; AC-1 pins the
  `params.data.path` contract with a test but changes nothing in the bus.
- **Memory** `project_event_loop_waits_for_021.md`: records the "doc.changed match bug
  (`ev.data.path`)" and "isDocPath .md-only" gotchas — the first is what 026 fixes; the second is
  already resolved (`.html` is live).
