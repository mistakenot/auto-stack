# Context: Task 031

Codebase grounding for adding a JSON-RPC listener + HTTP hook-ingest endpoint + `ctl.*` events to the autowatch daemon, on top of the merged 030 libraries. See [plan.html](plan.html) for requirements, verification, and solution.

## The merged 030 surface this task consumes (commit 233f7cd, on main)

- `auto-shared/transport/transport.go:44` — `func Listen(uri string) (Listener, error)` (scheme dispatch `unix://`/`tcp://`); `:60` `func Dial(ctx, uri) (net.Conn, error)`. `Listener` iface (`Accept`/`Addr`/`Close`) at `:22-30`. `Conn = net.Conn` alias.
- `auto-shared/transport/tcp/tcp.go:18` `Listen(addr)` supports `127.0.0.1:0`; `Addr()` reports the resolved port. `unix/unix.go:27` `Listen(path)` with safe stale-socket handling + socket removal on `Close`.
- `auto-shared/rpc/peer.go` — `NewPeer(conn io.ReadWriteCloser, opts...) *Peer` (`:78`); `WithHandler`/`WithOnNotify`/`WithBufferSize` (`:30-42`); `Register(method, Handler)` (`:99`); `Serve(ctx) error` (`:106`); `Call(ctx, method, params) (json.RawMessage, error)` (`:318`); `Notify(method, params) error` (`:384`); `Close() error` (`:491`); `var ErrClosed` (`:13`).
- **`readLoop` dispatches requests via `go p.handleRequest(...)` (`peer.go:181`)** — **handlers run concurrently** (per-request goroutine). This is a hard constraint: every autowatch RPC handler must be goroutine-safe. (One of 030's review fixes.)
- `auto-shared/rpc/message.go:59` — `type Handler func(ctx context.Context, params json.RawMessage) (any, error)`; return `*rpc.Error` (`:47`) for a specific code, any other error → `InternalError`. Codes `ParseError=-32700 … InternalError=-32603` (`:15-20`).
- `auto-shared/rpc/conformance/conformance.go` — `Fixture{Client() RPCClient; Obs() Observations; Close() error}` (`:40`); `FixtureFactory func(testing.TB) Fixture` (`:51`); `Scenario{Name(); Run(t, f)}` (`:55`); `RunAcrossFixtures(t, s, factories...)` (`:65`).
- `auto-shared/rpc/conformance/fakes.go` — **`PeerClient` is a ready-made `RPCClient`** (`NewPeerClient(conn)`, Call/Notify/Notifications, drop-on-full notif chan cap 64) — **reuse it for the client side of the real-autowatch fixture**; we don't write a client adapter. `FakeServer` (`Obs`/`DispatchCount`), `CountingConn`/`CountingListener`, and the `unixFactory`/`tcpFactory` patterns (`:263`/`:327`) are the template for the real fixtures.

## The autowatch daemon being modified

- `auto-watch/internal/cli/ops.go:25-103` — `newStartCmd(app)`. Lock at `:34-45` (`daemon.AcquireLock`); `service := daemon.New(db, application.Backend, cmd.OutOrStdout(), application.Now)` at `:68`; `writePIDMetadata()` at `:64-66`/`:382-399`; **shutdown + tick loop at `:81-97`** (`ctx, stop := signal.NotifyContext(..., SIGTERM)`, then `for { select { <-ctx.Done() | <-ticker.C: service.Tick } }`). The RPC + HTTP listeners hang off this same `ctx`; closing the listener on `ctx.Done()` unblocks `Accept()`.
- `auto-watch/internal/daemon/daemon.go:28-35` — `Service{Store *store.Store; Backend runner.Backend; Output io.Writer; Now func() time.Time}`; `New(...)` at `:41`. Handlers close over this `*Service` for Store/Backend access (Tasks 4/5).
- `auto-shared/config/host.go:34` — `HostIDQuietly() string` (never errors; for `daemon.status`). `auto-shared/version/version.go:3` — `var Version = "dev"` (ldflags-overridable).

## HTTP ingest reference (mirror, don't import)

- `auto-ui/internal/server/rpc_ingest.go:27` — `handleRPC(...)`: body `io.LimitReader(r.Body, 1<<20)` (1 MiB, `:44`); frame `struct{ JSONRPC string; Method string; Params bus.Event }` (`:50-59`); `ev.Validate()` → 400 on failure (`:69`); success `204 No Content` (`:90`). `server.go:47-108` shows the mux + `loopbackOnly(mux)` wrapper. Task 3's ingest mirrors the parse/validate/ack; **it does NOT derive or relay** (that is Task 6) — additive only.

## Bus events / the `ctl.*` namespace this task adds

- `auto-shared/bus/event.go:24` — `dottedType = ^[a-z0-9]+(\.[a-z0-9]+)+$`. **So `ctl.log.info`/`ctl.log.warn`/`ctl.log.error`/`ctl.connect`/`ctl.disconnect`/`ctl.health` are valid; `ctl.log_event` (underscore) is NOT.** Use the dotted forms from the epic table.
- `NewEvent(typ, source, data) (Event, error)` (`:51`) stamps id/time/Host. `Validate()` (`:72`) requires specversion/type/source/id/time/host.
- `auto-shared/bus/payloads.go` — `ToolPost`/`DocChanged`/`PathRef` are plain structs with JSON tags, no factories. Add `ctl.LogEvent` (e.g. `{Level, Message, Op, Fields map[string]string}`) the same way.
- `auto-shared/bus/hub.go` — `Hub` + `Sink{Deliver(Event)}` + `Subscribe(Sink) (cancel func())` + `Broadcast(ev)` already exist (used by auto-ui). **Reuse `bus.Hub` as the daemon's in-process "event stream"**: Task 3 `Broadcast`es gated `ctl.log.*` to it; `bus.subscribe` (hub→connected-peer bridge) and data-plane relay are Task 6.

## Patterns / constraints

- **Two listeners, one daemon**: a JSON-RPC listener over `transport` (unix default, tcp configurable — `--rpc-addr`/`--ready-file`) for the auto-ui proxy, and a loopback **HTTP** listener for hook ingest (hooks speak `http://127.0.0.1:.../api/rpc`). Both share the start-command `ctx` and shut down with the tick loop.
- **GR-N3 layering**: the accept loop imports `transport`; RPC **handler funcs must not import `transport`** (only `rpc` for `*Error`). Keep them in separate files; extend the 030 recursive layering test to cover the autowatch handler package.
- **Handler concurrency** (from `peer.go:181`): handlers are invoked in their own goroutines — `daemon.status` is trivially safe; the `*store.Store`/`Backend` access that Tasks 4/5 add must be concurrency-safe.
- **JSON-first / flags** (root CLAUDE.md): `--rpc-addr`, `--ready-file`, `--ctl-events`, `--hook-addr` on `auto watch start`; ready-file is one JSON line `{"addr":"..."}`. `auto watch doctor` should report listener config health (existing doctor at `auto-watch/internal/doctor`).
- **auto-watch/CLAUDE.md** currently stubs "Build & Test: TODO" — this task should fill it (`cd auto-watch && go build ./... && go test ./...`).

## Related tasks

- **030** (merged): `transport`/`rpc`/`conformance`. 031 plugs the **real autowatch listener** into 030's harness and lands the **binary-tier** fixture (subprocess + `--ready-file`) 030 deferred.
- **Task 1 leftover**: `ctl.*` namespace was never built (028 shipped only `Host`); this task closes it.
- **Task 5**: data-plane `watch.task.*` events + RPC task dispatch (`task.dispatch` starts the worker directly via `ReserveRun`+`startWorker`, does NOT signal the tick loop — per the epic's resolved Task-5 thread). Not this task.
- **Task 6**: `bus.subscribe` (hub→peer bridge) + hook derive/relay. **Task 10**: retarget `auto hooks fire` to autowatch-only. Until then hooks keep posting to auto-ui in parallel.

## Git-history notes (enrichment)

- **Ready-file + graceful-shutdown precedent**: `auto-ui` commit `ccaaebd` (wire SIGINT/SIGTERM to graceful shutdown in serve) and `serve.go:126-141` (ready-file). Mirror this for the autowatch RPC listener; autowatch's `ops.go:81-97` already has the `signal.NotifyContext` + ticker scaffold to hang the listener off.
- **`loopbackOnly` is auto-ui-internal** (`auto-ui/internal/server/loopback.go:27`) — can't be imported across modules. The HTTP hook-ingest guard in autowatch must reimplement a tiny loopback check (inspect `r.RemoteAddr` / bind to `127.0.0.1`), not import auto-ui.
- **No binary-build E2E precedent in `auto-watch`** (grep found none). The binary-tier conformance fixture introduces a `TestMain` that `go build`s `./cmd/autowatch` to a temp path — new pattern for this module; document it in `auto-watch/CLAUDE.md`.
- **030 landed in 3 commits** (`30b0431` phase-1, `db0dcba` review fixes, `233f7cd` merge) — commit granularity to mirror: one commit per phase, code + tests together, `go build` + `go test -race` green before each.
