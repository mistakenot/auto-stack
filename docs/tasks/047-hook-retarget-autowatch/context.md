# Context — 047 hook-retarget-autowatch

Grounding for the Solution. File:line refs verified 2026-06-27 against `main`.

## Current hook post (single, to auto-ui)
- `auto hooks fire` RunE → `postBusEvent(uiPort(), ev)` (`auto-cli/cmd/auto/hookscmd.go:115`). **Single** post; no autowatch post today (the epic's "dual-post" language is aspirational).
- `postBusEvent(port int, ev)` (`hookscmd.go:363-381`): marshals `ev.AsNotification()` (JSON-RPC notification), POSTs to `http://127.0.0.1:<port>/api/rpc`, sets `Content-Type: application/json`, 150ms timeout (`hookPostTimeout`), **swallows all errors** (fire-and-forget). Go's client sends **no `Origin` header**.
- `uiPort()` precedence (`hookscmd.go:337-357`): `AUTO_UI_PORT` env → `~/.auto/ui/settings.json` `.port` → `defaultUIPort`. **Mirror this for a new `watchHookAddr()`**.
- Durable append `hooks.Append(env)` (`hookscmd.go:109`) happens **before** the live post — canonical record, unchanged by T10.

## Target: autowatch HookIngest
- Mounted as the **root handler** of the hook HTTP server: `hookSrv := &http.Server{Handler: rpcserver.HookIngest(...)}` on `hookLn` (`auto-watch/internal/cli/ops.go:178-189`); `--hook-addr` default `127.0.0.1:7787` (`ops.go:220`). Serves **any path** → POST to `http://<hookAddr>/`.
- Ingest contract (`auto-watch/internal/rpcserver/ingest.go:26-79`): **POST** only (else 405); **loopback** host only (else 403); **`Origin` must be absent** (else 403); **`Content-Type: application/json`** (else 415); JSON-RPC `jsonrpc=="2.0"` + `method` + `params`(bus.Event), `ev.Validate()` must pass; **204** on success. The existing `postBusEvent` shape already satisfies all of these — only the URL changes.
- On ingest, autowatch stamps `hostId` (overwrite-always), broadcasts raw, derives `doc.changed`, broadcasts derived, relays to subscribers. **auto-ui already subscribes (045)** → events reach the browser via the relay.

## Discovery: autowatch hook address
- `writePIDMetadataWithAddrs(rpcAddr, hookAddr)` (`auto-watch/internal/cli/ops.go:521-540`) writes `~/.auto/watch/daemon.pid.json` with `{pid, startedAt, hostId, hostPath, rpcAddr, hookAddr}`. `hookAddr` is the **actually-bound** address (authoritative if `--hook-addr :0` was used).
- PID path: `config.PIDPath()` → `~/.auto/watch/daemon.pid.json` (`auto-watch/internal/config/paths.go:60-66`). No existing reader; read via `sharedconfig.DecodeJSONFile`.
- **`watchHookAddr()` precedence (recommended):** `AUTO_WATCH_HOOK_ADDR` env → `daemon.pid.json` `.hookAddr` → default `127.0.0.1:7787`.

## auto-ui removal surface
- `server.go:79` `gate := newEventGate(hub, dedupWindow)`; `:85` `o.mgr.SetEventSink(gate.Broadcast)`; `:132` `mux.HandleFunc("/api/rpc", handleRPC(gate.Broadcast, o.regProvider, buf))`. → delete gate + route; **rewire sink to `hub.Broadcast`** (`SetEventSink` stores into an atomic.Value, `manager.go:132-138`).
- `rpc_ingest.go` (`handleRPC` + local `bus.DeriveDocChanged` @ `:87` + `writeRPCError`) → **delete file**. `DeriveDocChanged` has **no other caller** in auto-ui (only comments in `cli/emit.go:70`, `cli/emit_test.go:22`) → after T10 it runs only in autowatch.
- `eventgate.go` + `eventgate_test.go` → **delete** (4 tests).
- `rpc_ingest_test.go` → **delete** (5 tests: broadcast/derive, non-doc, malformed, invalid-envelope, method-not-allowed). NB: `TestRPCIngestBroadcastAndDerive` was the `params.data.path` shape pin (per auto-ui/CLAUDE.md) — equivalent coverage lives in autowatch ingest tests; note the move.
- `event_flow_test.go:295` `TestRelayDedupAcrossPaths` (045 AC-4: dedup local `/api/rpc` vs relay) → **remove** (no local path remains). **Keep** the other event_flow tests (multi-backend merge, Host fidelity, slow-client drop) — they exercise the relay path, which stays.

## Debug buffer nuance
- `/api/rpc`'s `handleRPC` fed the `buf` debug ring that `/api/debug/recent` reads (auto-ui/CLAUDE.md: "only sees server ingest events"). Removing `/api/rpc` leaves it with no writer → `/api/debug/recent` goes empty. **Decision (D-3):** wrap the manager event sink to also record into `buf` (relayed events become the debug-recent source), preserving the endpoint. Alternative: drop `/api/debug/recent`'s buffer. Recommend wrap.

## Decisions locked (Requirements tab)
- Retarget hooks → autowatch hook-ingest (auto-ui `/api/rpc` removed).
- **Remove** the eventGate (single path, no dedup needed).
- Discovery: PID-metadata `hookAddr` → env → default (this doc's recommendation).
- Live-update ownership shifts to the autowatch daemon (accepted steady state, D-3 of epic).

## Conventions
- Best-effort/non-breaking: a hook must never error to the agent (`hookscmd.go:65` "never return an error"). Daemon-down = silent drop of the live post; durable append remains.
- `cd auto-ui && go build ./... && go test ./... && go vet ./...`; `cd auto-watch && go test -race ./...`; `cd auto-cli && go test ./...`. pd-lint clean.

## Related Tasks (git history)
- **040 autowatch-event-ingest-relay** (`f3430aa`, PR #95) — created autowatch's `HookIngest` + `bus.subscribe` relay. T10's POST target. Frozen.
- **045 auto-ui-event-aggregation** (`c27d236`, PR #104) — auto-ui subscribes each backend + forwards relayed events to the hub via `SetEventSink`→`eventGate`; deterministic derived ids. T10 removes the `eventGate` (single path) and rewires `SetEventSink` to broadcast straight to the hub. Because 045 already makes auto-ui receive events via the relay, T10's retarget (P1) is sufficient for end-to-end live updates **without** any auto-ui change — the removal (P2) is dead-code cleanup.
- **020–024 hook chain** — `auto-cli/cmd/auto/hookscmd.go` (`uiPort`, `postBusEvent`, durable `hooks.Append`) last touched at `0976a47`; stable, no mid-flight branches.
- **Walking-skeleton note:** P1 (retarget hooks → autowatch) makes the feature work end-to-end via the existing relay; P2 removes the redundant `/api/rpc`+eventGate; P3 docs. This is vertical, not layered.
- **Drift check:** all target files current on `main` post-040/045; no mid-flight branches. The `params.data.path` regression pin currently in auto-ui `rpc_ingest_test.go` is being deleted — confirm autowatch ingest tests assert it before removal.
- **Cross-task:** shares `auto-ui/internal/server/server.go` edits with 046 (046 adds RPC registrations; 047 removes the `/api/rpc` route + gate wiring) — second-merged PR rebases.
