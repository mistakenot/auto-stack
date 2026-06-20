# Context: Task 027

Verified codebase context for implementing plan lifecycle status + executor liveness in the
auto-ui docs tree. Companion to [plan.html](plan.html).

## Key Files

### Server (status path)
- `auto-ui/internal/server/docs.go:16-21` — `docEntry struct { ID, Path, Type string }` (json `id`/`path`/`type`). Extend with an optional `Meta *PlanMeta json:"meta,omitempty"`.
- `auto-ui/internal/server/docs.go:120-160` — `walkDocs(root string) ([]docEntry, error)`; `filepath.WalkDir` loop (124-152) classifies `.md`→`markdown`, `.html`→`html`. The per-entry append (~146) is the seam: for `.html`, read a bounded prefix and parse `pd-meta` + `<pd-doc status>`.
- `auto-ui/internal/server/docs.go:26-47` — `docListHandler(regProvider func() config.ProjectsConfig) Handler`; calls `walkDocs(root)` after `resolveRoot()`, serializes entries. No bodies returned (cheap-rung contract).
- `auto-ui/internal/server/docs.go:164-195` — `cleanDocPath(p string, allowed ...string) string` (traversal guard, `docs/` prefix). Reuse for the bounded read path.
- `auto-ui/internal/server/raw.go:50-56` — precedent: `os.ReadFile(absPath)` on a `cleanDocPath`-validated path. For bounded reads use `os.Open` + `io.LimitReader` (~8KB) rather than slicing a full read.

### Server tests
- `auto-ui/internal/server/docs_test.go:19-50` — `setupDocsFixture` builds a `t.TempDir()` `docs/` tree with `.md`/`.html`/non-doc files.
- `:53-65` — `docsTestServer` wraps an `httptest.Server` with `WithRegistryProvider`.
- `:68-92` — `rpcCall` helper (WS JSON-RPC round-trip, matches by id, returns `map[string]any`).
- `:96-198` — `TestDocListHappy`, `TestDocListWorktree` assert `result[i]["path"]`/`["type"]`. Mirror these for `meta`.

### Client (status + liveness path)
- `auto-ui/web/static/tree.js:176-197` — `Leaf({leaf, selected, onSelect, depth, flashId})`; sets `data-testid="doc-node"` (183), `data-doc-path` (184), `data-doc-type` (185); icon span (192) then label span (193). Status spinner / review pill / liveness indicator attach between icon and label. Leaf needs `leaf.meta` threaded through.
- `tree.js:96-172` — `groupDocs`; `tree.js:82-90` `GROUP_ORDER`. Grouping keyed by stable name → expansion state survives re-list (026 feedback). `meta` rides through unchanged; no new grouping rules.
- `tree.js:384-405` — existing `on("doc.changed", ...)` handler: `parseDocChanged(ev)` → `triggerFlash(c.path)`; re-lists when path unknown. Status repaint = re-fetch `doc.list` on `doc.changed` (already happens for new paths; widen to always refetch so `meta` updates).
- `auto-ui/web/static/rpc.js:182-187` — `on(method, handler)` subscribes **per method**, returns unsub; `:164-180` `call(method, params)`; `onmessage` (~119-141) fans `msg.params` to handlers for `msg.method` and records into `window.__autoui` ring (~93). **No wildcard** — add `onAny(handler)` here for liveness (all event types).
- `auto-ui/web/static/rpc.js` — `whenOpen()` / `onStatus("open")` (025 feedback): mount-time fetches await readiness; re-fetch on reconnect.
- `auto-ui/web/static/docevents.js:7-15` — `parseDocChanged(ev)` reads `ev.data.path` (NOT `ev.path`); `project`/`worktree`/`branch` fall back to envelope top-level. `:20-27` `matchesDoc`.

### Bus / producer
- `auto-shared/bus/event.go:28-45` — `Event{ SpecVersion, Type, Source, ID, Time(json:"time", RFC3339), Project, Session, Remote, Branch, Worktree, Commit, Env, Data }`. **Branch/Worktree/Project are top-level.**
- `auto-shared/bus/event.go:107-113` — `AsNotification()` → `{jsonrpc, method: e.Type, params: e}`. So on the wire `params.branch`/`params.project`/`params.time` are top-level.
- `auto-cli/cmd/auto/hookscmd.go:126-146` — `mapEventType` emits `agent.tool.post`, `agent.tool.pre`, `agent.session.start`, `agent.session.end`, `agent.posttooluse`, `agent.unknown`.
- `auto-cli/cmd/auto/hookscmd.go:151-233` — `buildBusEvent` sets `ev.Branch`/`ev.Worktree`/`ev.Commit` (194-207) from git provenance and `ev.Project` (217-229). `postBusEvent` (363-381) POSTs `ev.AsNotification()` to `http://127.0.0.1:{port}/api/rpc` (best-effort, 150ms).
- `auto-ui/internal/server/rpc_ingest.go:27-92` — `handleRPC`; `hub.Broadcast(ev)` (75) sends the **raw** event to all WS clients, then derives+broadcasts `doc.changed` (82-84). Raw hook events reach the client → liveness source.

### Backfill targets
- `docs/tasks/021-auto-bus-standard/artifacts/auto-bus-standard.html` — no `pd-meta`; `<pd-doc ... status="draft" generated="2026-06-11">` (line 11), pins `pd-v0.2.0`.
- `docs/tasks/022-hook-event-log/artifacts/hook-event-log.html` — no `pd-meta`; `<pd-doc ... status="approved" pr="pending" generated="2026-06-14">` (line 12), pins `pd-v0.3.0`. Both tasks are merged → backfill `pd-meta.status: merged`.

### Dependency
- `auto-ui/go.mod:1-16` — module `github.com/mistakenot/auto-ui`; deps: `coder/websocket`, `auto-shared`, `cobra`. `golang.org/x/net` is **not** in any workspace module — adding `golang.org/x/net/html` is a fresh (pure-Go) dependency, approved in requirements AC-1.

## Patterns
- **Optional doc.list fields are additive/back-compat**: markdown + non-pd HTML omit `meta`; existing assertions unchanged (024/025/026 schema discipline).
- **Liveness rides existing signals** (epic 002 decision): no new file watcher, no new bus event. Status repaint reuses `doc.changed`; liveness reads raw broadcast events.
- **Conformance is browser-driven** (024 harness): `AUTO_UI_DEBUG=1 auto ui serve --port 0 --ready-file --projects`, `agent-browser` at `?debug=1`, inject via `auto ui emit`, assert via `window.__autoui` + `data-*` (lowercase-kebab project ids; assert deltas not absolutes; poll ~2.5-3s; `find -delete` teardown).
- **Wire-shape regression guard**: `rpc_ingest_test.go` pins `params.data.path`; add a sibling assertion that raw events carry top-level `params.branch`.
- **No server-side hook recency exists** (022 is durable log + ETL only) → client-side `(project,branch)→lastTs` map is the only option; best-effort, resets on reload.

## Related Tasks
- **024** (backend): `doc.list` widened to `.md`+`.html` with `type`; `cleanDocPath` parameterized; `.html` `doc.changed` derivation; agent-validation harness (`--port 0`/`--ready-file`/`--projects`, `auto ui emit`, debug buffer).
- **025** (explorer): `tree.js` grouping, `Leaf` rendering, `whenOpen()` readiness, `window.__autoui` ring, `data-testid` conventions.
- **026** (doc liveness): `docevents.js` `ev.data.path` fix + `params.data.path` test pin; `doc.changed` open-doc + tree refresh. 027 adds a **second**, orthogonal liveness axis (executor activity) on top.
- **021** (auto-bus): CloudEvents envelope + provenance (`branch`/`worktree`/`project`/`remote`) the liveness join reads.
- **022** (hook event log): confirms no server recency source; durable JSONL + parquet only.
