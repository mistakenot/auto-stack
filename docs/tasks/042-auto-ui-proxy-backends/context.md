# Context: Task 042

Codebase + docs grounding for [plan.html](./plan.html) — epic-003 Task 7: auto-ui becomes a proxy over autowatch backends (`~/.auto/ui/backends.json`, live reload, clean break from local FS).

## Key Files

### auto-ui dispatcher + handlers (the proxy seam)
- `auto-ui/internal/server/rpc.go:60` — `type Handler func(ctx context.Context, params json.RawMessage) (any, error)`; `Dispatcher{methods map[string]Handler}`, `Register`, `dispatch()` (returns `(*rpcResponse, bool)`; bool=false for notifications). **Proxy = a `Handler` that calls a backend `rpc.Peer.Call` and returns the parsed result.**
- `auto-ui/internal/server/server.go:47-76` — `func New(fsys fs.FS, mode string, opts ...Option) http.Handler`; creates dispatcher + `hub := bus.NewHub()` (:55); registers `ping`, `doc.list`→`docListHandler(o.regProvider)`, `doc.get`→`docGetHandler`, `project.list`→`projectListHandler` (:73-75). Mux: `/api/ws`, `/api/rpc`, `/api/doc/raw`, `/` assets (:77-108).
- `auto-ui/internal/server/server.go:16-27` — `options{regProvider func() config.ProjectsConfig; debug bool}`, `WithRegistryProvider`, `WithDebug`. **Add `WithBackendManager(...)` here.**

### Local FS code to REMOVE (clean break — GR-F6/D-2)
- `auto-ui/internal/server/docs.go:28-93` — `docListHandler` (`walkDocs`) + `docGetHandler` (`os.ReadFile` :82). Replace with proxy.
- `auto-ui/internal/server/docs.go:101-208` — `resolveRoot`, `walkDocs`, `cleanDocPath` (local-FS helpers). Remove — autowatch does resolution/walk now.
- `auto-ui/internal/server/raw.go:18-65` — `/api/doc/raw` via `os.ReadFile` (:51), writes bytes + `Content-Type` (:58). Replace with proxy to `doc.raw` + base64-decode.
- `auto-ui/internal/server/project.go:27-41` — `projectListHandler` reads local registry. Replace: fetch from backends' `project.list` (D-4).
- `auto-ui/internal/server/planmeta.go:14-21` — `PlanMeta`/`ExtractPlanMeta`. autowatch already returns meta in `doc.list`, so auto-ui no longer extracts it.

### Transport + RPC client (what the proxy dials)
- `auto-shared/transport/transport.go:59` — `func Dial(ctx, uri string) (net.Conn, error)` (tcp/unix; GR-N3/GR-N8).
- `auto-shared/rpc/peer.go:318-381` — `func (p *Peer) Call(ctx, method string, params any) (json.RawMessage, error)`; returns `rpc.ErrClosed` after shutdown. `NewPeer(conn, opts...)`, `Serve(ctx)`.
- **Note (Task 041 in flight):** `rpc.Peer` is gaining `WithKeepAlive` liveness — backend peers should enable it once 041 lands so dead backends are reaped (ties into live reconcile health).

### autowatch RPC surface (the proxy target — all MERGED, frozen)
- `auto-watch/internal/rpcmethods/methods.go:59-70` — methods: `daemon.status`, `doc.list`, `doc.get`, `doc.raw`, `project.list`, `task.*`.
- `auto-watch/internal/rpcmethods/methods.go:20-27` — `StatusResult{HostID, Version, UptimeSeconds, PID, StartedAt}` (`json:"hostId"` etc). Call `daemon.status` on connect to learn authoritative `hostId`.
- `auto-watch/internal/rpcmethods/docs.go:13-18` — `DocRawResult{Path, ContentType, ContentBase64}` (`json:"path"/"contentType"/"contentBase64"`). Proxy: decode `contentBase64` → bytes, set `Content-Type` from `contentType`.
- `auto-watch/internal/rpcmethods/docfs.go` — `docEntry{id,path,type,meta?}`, `projectEntry{id,name,path,remote,host}` (note **host** field per GR-F8), `PlanMeta{status,branch,epic,pr,created,reviewState}`. Same shapes auto-ui already returns, plus `host`.

### Config (mirror for backends.json)
- `auto-shared/config/jsonfile.go:25-98` — `DecodeJSONFileStrict(path, target)`, `WriteJSONFile`, `WriteJSONFileAtomic`.
- `auto-shared/config/paths.go:28-46` — `AutoDir()` → `~/.auto`, `EnsureAutoDir()`.
- `auto-ui/internal/config/settings.go:33-177` — `UIDir()` → `~/.auto/ui`, `LoadUISettings`/`SaveUISettings`/`ValidateUISettings`/`EnsureUISettings`, `ValidationError = sharedconfig.ValidationError`, `ValidationErrorsError` wrapper. **Mirror for `BackendsConfig` + `~/.auto/ui/backends.json`.**

### CLI
- `auto-ui/internal/cli/root.go:43-65` — `NewRootCmd` `AddCommand(newInitCmd, newDoctorCmd, newQuickstartCmd, newDocsCmd, newUpdateCmd, newServeCmd, newEmitCmd)`. **Add `newBackendsCmd` (add/remove/list).**
- `auto-ui/internal/cli/serve.go:66-144` — `ctx = signal.NotifyContext(...)`, `baseCtx, cancelBase` (:70); `server.New(...)` (:80); `http.Server{BaseContext: ...baseCtx}` (:98); shutdown goroutine cancels baseCtx then `srv.Shutdown` (:110-121). **Start the backend manager + reconcile loop on `baseCtx`; fail-fast before `srv.Serve` if no backend configured.**
- `auto-ui/internal/cli/` (doctor cmd) — add per-backend connectivity + `project.list`-fetch checks (`DoctorCheck{check,status,message}`).

### Hub / WS (UNCHANGED here — Task 8's surface)
- `auto-shared/bus/hub.go:22-68`, `auto-ui/internal/server/ws.go:33-115`, `auto-ui/internal/server/rpc_ingest.go:27-92` — leave as-is. Feeding remote `bus.subscribe` events into the Hub is Task 8.

### Existing tests (patterns to mirror / rewrite)
- `auto-ui/internal/server/docs_test.go:19-217` — `setupDocsFixture` (temp docs tree), `docsTestServer`, `rpcCall(...)` over WS. **Rewrite to drive a fake in-process autowatch backend (rpc.Peer over `net.Pipe`) instead of a local FS fixture (GR-N6 in-process tier).**
- `auto-ui/internal/server/raw_test.go:19-61` — `rawTestServer` (httptest) + `http.Get(/api/doc/raw...)`. Rewrite against the fake backend.
- `auto-ui/internal/server/ws_test.go:16-50`, `server_test.go:27-33` (MapFS) — helpers reusable.

## Patterns
- **Proxy handler = Handler that calls `peer.Call(ctx, method, params)` and returns `json.RawMessage`** (re-emit verbatim). Handlers stay transport-agnostic (GR-N3) — the BackendManager owns transport.
- **Backend identity:** user supplies URI in `backends.json`; on connect auto-ui calls `daemon.status` to learn authoritative `hostId`; routing keyed by `hostId`. Routing rule (until Task 9): one backend → all calls route to it; `hostId` param routes explicitly; multi-backend aggregation deferred to Task 9.
- **Live reconcile:** a loop bound to `baseCtx` periodically re-reads `backends.json`, diffs vs active connections, dials new / closes removed — no restart. Unreachable backend retried on later ticks, never kills the server.
- **Clean break (GR-F6/D-2):** remove all local FS reads; `serve` fails fast with a remediation hint (`run auto ui backends add <uri>`) when no backend is configured. No fallback mode.
- **Config convention:** `~/.auto/ui/backends.json`, strict decode + atomic write, shared `ValidationError{code,path,field,message}`; CLI JSON-default output, errors+remediation to stderr (CLAUDE.md CLI conventions).
- **Testing (GR-N7 API-first):** ~90% at the RPC/HTTP layer via an in-process fake autowatch backend (GR-N6 tier 1, `net.Pipe`); browser is a thin smoke layer only. Use `t.TempDir()`/`t.Setenv()` for config isolation.
- **Doctor:** `DoctorCheck{check,status,message}` per backend (config valid? reachable? `project.list` responds?).

## Related Tasks
- **Tasks 1–6** (028/030/031/038/039/040, MERGED): built the autowatch daemon, transport abstraction, and the exact RPC surface (`doc.*`/`project.list`/`task.*`/`daemon.status`/`bus.subscribe`) this task proxies to.
- **Task 041** (dataplane-backbone-assurance, executing): adds `rpc.Peer` keepalive/liveness + conformance depth. Backend peers here should enable `WithKeepAlive` once 041 lands (dead-backend reaping aligns with live reconcile health). Not a hard dependency for T7.
- **Task 8** (event aggregation, next): subscribes backends' `bus.subscribe` into the Hub — uses T7's connections. **Out of scope here.**
- **Task 9** (multi-host SPA): flat host-tagged project list, `(hostId, projectId)` everywhere (GR-F8), per-backend health UI. T7's single-backend routing rule is replaced then.

## Git History & Drift Check (CB3)
- **No drift:** all Solution/context.md file:line references confirmed current (rpc.go Handler@60; server.go New@47, options@16-19, registrations@66-75, mux@77-108; docs.go handlers@28-93 + walkDocs@122/resolveRoot@101; raw.go @18 + os.ReadFile@51; project.go @27; planmeta.go @14-37; serve.go signal@66/baseCtx@74/New@80/shutdown@111; transport.Dial@60; peer Call@318/NewPeer@78/Serve@106; jsonfile DecodeJSONFileStrict@27/WriteJSONFileAtomic@66; settings UIDir@41).
- **Last-touched-by:** server.go `3f234f0` (loopback 403); docs.go/planmeta.go `405bf42` feat(027); raw.go/project.go/serve.go/root.go `0976a47` feat(024); doctor.go `cd80ea9` feat(017); settings.go `d3a659d` feat(013). autowatch RPC surface (methods.go/docs.go) frozen since feat(038/040).
- **`planmeta.go` SAFE TO DELETE (verdict confirmed):** exhaustive grep — the only production caller of `ExtractPlanMeta`/`PlanMeta` is `walkDocs` (`docs.go:150-155`), which is itself deleted; plus `docEntry.Meta` field (`docs.go:22`, gone with docs.go) and `planmeta_test.go` (delete too). autowatch's `doc.list` already returns `meta` (its own `PlanMeta` in `docfs.go`), so the SPA loses nothing.
- **doctor.go:** `doctorCheck{Check,Status,Message,Hint}` (already has a `Hint` field for remediation); `runDoctorChecks() []doctorCheck` (`:42`) is the extension point — append per-backend checks before marshal (`:26`). Current checks: `ui_settings`, `port`.
- **quickstart.go:** embedded markdown (`:9-112`); currently assumes a local filesystem layout, **no backend-prerequisite docs** — add the `auto ui backends add` setup + the "needs a reachable autowatch" note.
- **Test infra:** no `e2e/`/`testutil/` dir. Server tests use `localReq()` (sets `RemoteAddr=127.0.0.1` to pass the loopback guard) + `newTestFS()` (fstest.MapFS) + mocked `WithRegistryProvider`. Affected test files: rewrite `docs_test.go`, `raw_test.go`, `project_test.go` against a fake backend; **delete `planmeta_test.go`**. The fake in-process autowatch backend (rpc.Peer over net.Pipe) is net-new test scaffolding.
- **Feedback/patterns:** the loopback-only guard (`loopback_test.go`, `server.go` `3f234f0`) must be preserved (GR-N2). golangci-lint debt is actively kept clean — unused local handlers after the cut-over would fail `vet`/`lint`, so the proxy-switch and the local-code deletion must land in the **same** phase to keep the build green.
