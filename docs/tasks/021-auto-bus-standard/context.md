---
hash: "47d8e929"
id: "0940b912"
read_when: "implementing the auto-bus standard or understanding its codebase dependencies"
summary: "Verified codebase facts grounding the auto-bus implementation: auto-ui JSON-RPC/WebSocket transport, auto-shared/git provenance helpers, auto-cli hook producer, and SPA consumer signatures."
title: "Context: Task 021 — Auto Bus Standard"
---

# Context: Task 021

Verified codebase facts (signatures, line refs, gaps) grounding [plan.md](plan.md) and [solution.md](solution.md). All paths confirmed to exist as of this writing unless marked **MISSING**.

## Key Files

### auto-ui JSON-RPC + WebSocket transport (the foundation we extend)
- `auto-ui/internal/server/rpc.go:30-51` — frame structs: `rpcRequest{ JSONRPC, ID json.RawMessage(omitempty), Method, Params json.RawMessage }`, `rpcResponse{ JSONRPC, ID, Result any, Error *rpcError }`, `rpcError{ Code, Message, Data }`.
- `auto-ui/internal/server/rpc.go:58` — `type Handler func(ctx, params json.RawMessage)(any,error)`.
- `auto-ui/internal/server/rpc.go:63-74` — `Dispatcher{methods map[string]Handler}`, `newDispatcher()`, `Register(method,h)`.
- `auto-ui/internal/server/rpc.go:80-109` — `dispatch(ctx,raw)(*rpcResponse,bool)`; **notification detected by `len(req.ID)==0` (line 87)**; handler still runs, response discarded → `nil,false`.
- `auto-ui/internal/server/ws.go:23` — `const outboundBuffer = 16`.
- `auto-ui/internal/server/ws.go:67-76` — `session{ c *websocket.Conn; out chan any; cancel context.CancelFunc; seq atomic.Int64 }`; `newSession(c,cancel)`.
- `auto-ui/internal/server/ws.go:82-97` — `enqueue(ctx,msg)` drops slow clients via `s.cancel()` on full buffer (non-blocking `default`); `notify(ctx,method,params)` wraps an id-less `rpcRequest` and enqueues.
- `auto-ui/internal/server/ws.go:31-60` — `handleWS`: **creates its own `newDispatcher()` + registers `ping` inline (lines 49-56)**, `s:=newSession(c,cancel)` (line 43), launches `writePump`/`pingLoop`/`readLoop`. **Connect point = line 43; teardown = `defer cancel()` line 41.** No hub/session registry today — each connection is isolated.
- `auto-ui/internal/server/server.go:18-43` — `New(fsys fs.FS, mode string) http.Handler`; routes `/api/hello`, `/api/ws`→`handleWS`, `/`→static. **No `/api/rpc`.**
- `auto-ui/internal/cli/serve.go:62` — `handler := server.New(web.FS(), web.Mode)`; binds `127.0.0.1:<port>` (loopback); `--port` default 8080 (serve.go:96).

### auto-shared/git (provenance helpers — partially present)
- `auto-shared/git/detect.go:13-23` — `RepoRoot(dir)(string,error)` (✅ exists) → worktree root.
- `auto-shared/git/detect.go:27-33` — `OriginRemote(dir)(string,error)` (✅ exists) → raw origin URL or "".
- `auto-shared/git/normalize.go` — `NormalizeRemoteURL(raw)string`, `ComputeRepoID`, `ComputeRepoIDFromPath` (✅).
- **MISSING:** `CurrentBranch(dir)(string,error)` and `CurrentCommit(dir)(string,error)` — net-new in detect.go.

### auto-shared/config registry (identity glue)
- `auto-shared/config/projects.go:20-29` — `ProjectRef{ ID, Path, Remote, Name, Tools[], RegisteredAt }`.
- `auto-shared/config/projects.go:190-228` — `FindProjectByPath` (longest-prefix), `FindProjectByExactPath`, `FindProjectByID`. **MISSING: `FindProjectByRemote(remote)*ProjectRef`** — needed because a worktree path won't prefix-match the registered main path, but its remote will.
- `auto-shared/config/projects.go:37-94` — `ProjectsConfigPath()`, `LoadProjects(path)`, `SaveProjects`, `EnsureProjects()`.

### auto-cli hook producer (migrate onto the bus)
- `auto-cli/cmd/auto/hookscmd.go:35-44` — `HookEvent{ Agent, Event, SessionID, Project, Cwd, Tool, Paths []string, Timestamp }` (current bespoke shape).
- `auto-cli/cmd/auto/hookscmd.go:62-91` — `newHooksFireCmd` RunE: fail-fast on bad `--agent`, then **never errors**; reads ≤1MiB stdin; `buildHookEvent`→`postHookEvent(uiPort(), ev)`.
- `auto-cli/cmd/auto/hookscmd.go:96-145` — `buildHookEvent` (resolves project via `FindProjectByPath(cwd)`), `extractPaths` (pulls `tool_input.{file_path,notebook_path,path}`, **as-given/relative, no abs resolution**).
- `auto-cli/cmd/auto/hookscmd.go:196-214` — `postHookEvent` POSTs to **`/api/hooks`** (to be replaced by `/api/rpc`), 150ms timeout, all errors swallowed; `uiPort()` reads `~/.auto/ui/settings.json` (default 8080).
- `auto-cli/cmd/auto/hookscmd_test.go` — existing tests incl. `TestFireExitsZeroWhenUIDown`, `TestPostHookEventDelivers` (httptest), sample `claudePostToolUse` payload. Pattern to extend for AC-4.

### Static SPA (the consumer)
- `auto-ui/web/static/rpc.js:86-110` — `call(method,params)→Promise`, `on(method,handler)→unsub` (keys by method), `onStatus(handler)`.
- `auto-ui/web/static/router.js:1-25` — `parseHash()→{view,params:URLSearchParams}` (`#/<view>?<qs>`), `setHash(view,params)`.
- `auto-ui/web/static/app.js:10-26,123-134` — `App()` picks view by `parseHash().view` string; `Nav` adds buttons via `link(id,label)`; views receive `params`. New view = component + nav link + render-case.
- `auto-ui/web/static/index.html:10-20` — importmap (esm.sh: preact, htm, compat). Add `"marked":"https://esm.sh/marked@<v>"` for client-side markdown.

### Envelope precedent — auto-reflect (mirror in spirit)
- `auto-reflect/internal/events/model.go:49-60` — `Event{ ID, Type, SchemaVersion, Seq, TS, Host, SessionID, Agent, Git, Payload json.RawMessage }`; `model.go:186-227` `Validate()` → `[]ValidationError`. Pattern: typed envelope + opaque `json.RawMessage` + `Validate`. (Their type names use underscores; **our bus uses dots**.)

### auto-watch (named second adopter — paper only)
- `auto-watch/internal/store/store.go:45-55` — `EventInput{ Timestamp, Level, EventType, ProjectID, TriggerID, TaskID, RunID *int64, Message, Metadata }`. Spec maps these onto `watch.task.*` envelopes (TaskID/RunID in `data`, provenance attrs at top level).

## Patterns
- **Build/test (root `Makefile`):** `make build` (→`bin/auto`), `make test` (loops modules `go test ./...`), `make vet`, `make lint` (golangci-lint), `make check`. Per-module: `cd <mod> && go build ./... && go test ./...`. **Go build discipline: build after each file** (CLAUDE.md).
- **Dev server with live assets:** `go build -tags dev -o bin/auto ./auto-cli/cmd/auto` then `cd auto-ui && ../bin/auto ui serve --port 8080` (Mode=`disk`, `web.FS()=os.DirFS("web/static")` — edit + browser-reload, no rebuild). Prod embeds via `//go:embed`.
- **Test style:** unit handlers via `httptest.NewRequest/NewRecorder` + `fstest.MapFS` (`server_test.go`); WS integration via `httptest.NewServer(server.New(...))` + `websocket.Dial` + a `readUntil(predicate)` helper that skips interleaved notifications (`ws_test.go`); dispatcher unit tests register handlers inline + feed raw JSON (`rpc_test.go`). No mocking frameworks.
- **JSON stdout / diagnostics stderr; JSON-default output** (CLAUDE.md).
- **Resource pattern** (`docs/auto-package-patterns.md`): `list` = IDs + metadata, no bodies (cheap); `get` = full content. → `doc.list`/`doc.get`.
- **Spec docs live in `docs/`**; root CLAUDE.md "Documentation Index" is auto-generated — `auto doc fix` (run by pre-commit) regenerates it, never hand-edit.
- **Module wiring:** `auto-ui/go.mod` already `require`s + `replace`s `auto-shared` (declared, currently unused) → importing `auto-shared/bus` is free. `auto-cli` already imports `auto-shared/config`.
- **Commit style:** `feat(0NN): phase N — …` per phase (one commit per phase), optionally contextual body (`intent()`/`decision()`/`learned()`), end each phase with build+test+gofmt.

## Related Tasks
- **Task 020 (auto-hooks-install):** telemetry-safe allowlist (Claude 9 / Codex 10 events as Go constants) + the `HookEvent` producer this task migrates; 3 serial phases, dogfood-in-repo verification.
- **Task 019 (playbook-retrieval-loop):** typed-envelope + opaque-payload + `Validate()` precedent; 4 serial phases, golden-fixture schema test.
- **Task 018 (auto-watch-easy-daemon):** DAG with parallel doc/script phases; note "dispatch phases serially in the shared worktree, verify files on disk before next."
- **PR #65 (7bca951):** added the rpc.go/ws.go/rpc.js JSON-RPC-over-WS layer. **No structural change since** (only lint fixes in 0cf561b) — building on current code.
- **auto-env** (active): per-worktree port allocation — worktrees live outside the registered main path (motivates `FindProjectByRemote`).
