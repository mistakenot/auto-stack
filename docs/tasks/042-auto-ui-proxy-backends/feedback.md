# Feedback: Task 042

## Problems faced
1. **Redial on disconnect + hostId selector** — a PR review caught two gaps in the BackendManager: a backend that dropped wasn't redialed (the reconcile loop only diffed config, not connection health), and the `hostId` selector wasn't honored on routing. Fixed in `fix(042): address PR review — redial on disconnect, honor hostId selector`. The live-reconcile design handled *config* changes but needed to also reconcile *connection* state (a configured-but-dead backend must be retried).
2. **Atomic P3 cut-over** — the plan's call to land "proxy in + local FS out" as a single phase held up: deleting `docs.go`/`project.go`/`planmeta.go` while switching `server.go` to proxy handlers kept the build/vet green with no intermediate broken state. Splitting it would have failed vet on unused/missing handlers.
3. **`doc.raw` shape mismatch** — autowatch returns base64 (`DocRawResult.ContentBase64`); the browser `/api/doc/raw` contract is raw bytes. Decoding server-side kept the endpoint byte-identical (GR-F1), avoiding any SPA change.

## Reflections
- **What was tricky:** the clean break (GR-F6, no fallback) means there's no "degrade gracefully" — a wrong proxy wiring shows the user *nothing*, so the proxy tests against a fake in-process backend were essential to catch parity gaps before merge.
- **What I'd tell myself at the start:** reconcile must watch connection health, not just the config file — "reread the file each tick" is necessary but not sufficient; a dead-but-still-configured backend needs active redial.
- **Almost did but didn't:** almost kept a transitional local-FS fallback to ease dev; rejected per GR-F6 — fail-fast with remediation is the intended behavior.

## Useful context
- `planmeta.go` was safe to delete (CB3 verdict during planning held) — autowatch's `doc.list` supplies `meta`, so the SPA lost nothing.
- The fake in-process autowatch backend (rpc.Peer over net.Pipe) made ~90% API-level coverage (GR-N7) cheap — no subprocesses, no ports.
- `hub.SinkCount()` / `daemon.status` (from tasks 040/038) were the ready-made hooks for health checks and authoritative hostId.
- Routing is single-backend-default until Task 9 adds multi-host aggregation; the `hostId` selector path is wired now so T9 only adds the aggregation layer.
