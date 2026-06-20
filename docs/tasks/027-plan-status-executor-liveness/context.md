# Context: Task 027

Verified codebase context for implementing plan lifecycle status + executor liveness in the
auto-ui docs tree. Companion to [plan.html](plan.html).

## Key Files

### Server (status path)
- `auto-ui/internal/server/docs.go:17-20` — `docEntry struct { ID, Path, Type string }` (json `id`/`path`/`type`). Extend with an optional `Meta *PlanMeta json:"meta,omitempty"`. (Verified current 2026-06-20: server unchanged by task 029, which was client-only.)
- `auto-ui/internal/server/docs.go:120-160` — `walkDocs(root string) ([]docEntry, error)`; `filepath.WalkDir` loop (124-152) classifies `.md`→`markdown`, `.html`→`html`. The per-entry append (~146) is the seam: for `.html`, read a bounded prefix and parse `pd-meta` + `<pd-doc status>`.
- `auto-ui/internal/server/docs.go:26-47` — `docListHandler(regProvider func() config.ProjectsConfig) Handler`; calls `walkDocs(root)` after `resolveRoot()`, serializes entries. No bodies returned (cheap-rung contract).
- `auto-ui/internal/server/docs.go:164-195` — `cleanDocPath(p string, allowed ...string) string` (traversal guard, `docs/` prefix). Reuse for the bounded read path.
- `auto-ui/internal/server/raw.go:50-56` — precedent: `os.ReadFile(absPath)` on a `cleanDocPath`-validated path. For bounded reads use `os.Open` + `io.LimitReader` (~8KB) rather than slicing a full read.

### Server tests
- `auto-ui/internal/server/docs_test.go:19-50` — `setupDocsFixture` builds a `t.TempDir()` `docs/` tree with `.md`/`.html`/non-doc files.
- `:53-65` — `docsTestServer` wraps an `httptest.Server` with `WithRegistryProvider`.
- `:68-92` — `rpcCall` helper (WS JSON-RPC round-trip, matches by id, returns `map[string]any`).
- `:96-198` — `TestDocListHappy`, `TestDocListWorktree` assert `result[i]["path"]`/`["type"]`. Mirror these for `meta`.

### Client — POST-029 central store (this is the big change since planning)
Task 029 (`feat(029): auto-ui-state-refactor`, merged) introduced a Redux-style central store. **Architecture rule (029 AC-5 grep gate):** every bus subscription and every `call(...)` lives ONLY in `store.js`; views are presentational and read via `useStore`/selectors. `docs/tasks/029-auto-ui-state-refactor/artifacts/state-harness.sh` asserts `grep -rl 'on("doc.changed"' auto-ui/web/static` → only `store.js`. **Liveness must therefore live in the store, not a separate module.**

- `auto-ui/web/static/store.js:30-45` — state shape: `docsByProject: { [docsKey(project,worktree)]: [{id,path,type}] }` (doc.list cache — `meta` rides here once the server adds it), `selection`, `openDoc`, `lastDocChanged:{path,seq}`, `events`.
- `store.js:73-78` — `dispatch`: reducer is pure; returns SAME ref for a no-op so an irrelevant action fans no re-render (so `liveness/tick` must return same ref when nothing is tracked).
- `store.js:89-166` — `reducer` cases: `docs/set`, `docs/invalidate`, `docChanged/signal`, `events/append`. Add `liveness/note` + `liveness/tick` here.
- `store.js:174-192` — `useStore(selector)` (shallow-equal slice subscription); `:246` `selectDocs(state, project, worktree)` returns the cached array verbatim → `meta` passes through untouched.
- `store.js:483-559` — `initStore()`: the SINGLE `on("doc.changed", …)` (520) and `on("ping", …)` (554). **AC-4 gap:** at `:533-543` it re-lists (`fetchDocs(active,{force:true})`) ONLY when `!known` (a new path). A `planning→executing` edit to an EXISTING `.html` plan is a known path → no re-list → stale `meta`. Must widen: on a `doc.changed` whose path is a known `.html` under the active project, force a re-list. Add the `onAny`-based liveness subscription + 1s ticker here too.
- `auto-ui/web/static/rpc.js:119-140` — `onmessage`: `recordEvent(msg.method, msg.params)` (138, records ALL notifications) then fans `msg.params` to `notifyHandlers.get(msg.method)` (139-140). `:183-187` `on(method, handler)` is **per-method, no wildcard**. Add `onAny(handler)` fanned at the `onmessage` dispatch (transport only; the subscription that uses it lives in store.js).
- `auto-ui/web/static/tree.js:14-16` — imports `{ useStore, selectDocs }`; `:298` `const docs = useStore((s)=>selectDocs(s,project,worktree))`; `:303` reads `s.lastDocChanged`. `:94` `groupDocs(docs)` builds leaves `{id,path,type}` — carry `meta` onto leaves. `:174-191` `Leaf({leaf,…})` sets `data-testid="doc-node"` (181), `data-doc-path` (182), `data-doc-type` (183); icon span (190) then label span (191) — spinner/pill/liveness attach between them. Leaf may call `useStore(selectLiveness(project, leaf.meta?.branch))` for its own row.
- `auto-ui/web/static/docevents.js:7-15` — `parseDocChanged(ev)` reads `ev.data.path` (NOT `ev.path`); `project`/`worktree`/`branch` fall back to envelope top-level. Raw hook events carry top-level `params.branch` (the liveness key); only derived `doc.changed` nests `path` under `data`.
- `auto-ui/CLAUDE.md` — still documents the PRE-029 architecture (`uistate.js` "not a reactive store"; tree calling `doc.list` directly). Partially stale; update the parts this task touches.

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
- **029** (auto-ui-state-refactor, merged after 027 planning began): central `store.js`; all subscriptions/`call()` confined to the store (grep gate); `tree.js`/`content.js` presentational. **Reshapes 027's client side** — liveness goes in the store, not a new module; status repaint reuses the store's `docs/invalidate` but needs the known-path widening above. Validation extends `029/artifacts/state-harness.sh`.

## Execution Notes (git history — 024/025/026/029)
- **Phase shape**: recent auto-ui tasks ran 3–5 phases, each landing as `feat(NNN): phase N` + `docs(NNN): mark phase N complete`. 025/026 split observability/helpers → per-component render → test pin → conformance harness. 027 fits **5 phases** (server parser → server wire+tests → client store → client render → backfill+conformance).
- **Adding a doc.list field**: precedent is additive with `omitempty`; `.md` entries stay byte-identical; mirror existing `rpcCall` assertions in `docs_test.go`. (Task 024 added `coder/websocket` to auto-ui module-scoped — same pattern for `x/net`.)
- **Adding `golang.org/x/net` dep**: edit `auto-ui/go.mod` `require`, run `go mod tidy` in `auto-ui/` to populate `auto-ui/go.sum`; **no `go.work` edit needed** (go.work pins nothing). On merge to main a repo-wide `go mod tidy` folds it into `go.work.sum`.
- **`auto ui emit` limitation**: it only synthesizes an `agent.tool.post` (tool=Edit) with `--project`/`--path`/`--worktree`; it does **not** set `branch` or event `type`. Liveness conformance therefore drives the stream by `curl`-POSTing a hand-crafted `agent.tool.post` envelope (top-level `branch` set) to `POST /api/rpc` from loopback — `handleRPC` validates+broadcasts it raw to WS clients. (Loopback-only guard added in `3f234f0`; localhost curl passes, no Origin needed.)
- **No JS unit tests in auto-ui** — verification is 100% browser-driven (029 feedback: "the browser is the only oracle; `go build`/`go test` stayed green through three real client bugs"). So: server parser → Go unit tests; client store/render/liveness → `conformance.md` harness (dual build embed+dev), assert rendered `data-*` values (not counters), poll-to-settle ≤3s, **deltas not absolutes**, `find -delete` teardown, lowercase-kebab fixture ids, grep gates for structural invariants.
- **021** (auto-bus): CloudEvents envelope + provenance (`branch`/`worktree`/`project`/`remote`) the liveness join reads.
- **022** (hook event log): confirms no server recency source; durable JSONL + parquet only.
