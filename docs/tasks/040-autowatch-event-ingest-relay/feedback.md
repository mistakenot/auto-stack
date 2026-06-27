# Feedback: Task 040

## Problems faced
1. Per-connection subscription state vs shared handler -- The `rpcmethods.Handlers` struct is a single shared instance registered onto every peer, so the `bus.subscribe` cancel func had to live in the per-connection accept-loop scope inside `rpcserver`, not in `rpcmethods`. Getting this layering right was the key design decision.

## Reflections
- The context.md grounding was very effective — file:line references to the exact seams 031 left open made it straightforward to know where each piece plugged in.
- The `--ctl-events` gating strategy from 031 (gate at emission, not relay) simplified the bridge: it relays everything on the hub unconditionally since ctl events are only broadcast when the flag is on.
- The ingest derivation mirrors the auto-ui pattern almost exactly (`DeriveDocChanged` → broadcast each derived event), confirming the shared-bus architecture works across both servers.

## Useful context
- `auto-ui/internal/server/rpc_ingest.go:75-88` was the canonical pattern for ingest derivation — mirror it rather than inventing a new approach.
- `rpc.Peer.enqueue` already implements drop-on-full + disconnect, so the bridge sink gets at-most-once delivery for free without custom backpressure logic.
- The layering test (`TestLayering_NoTransportImport`) from 031 continues to enforce that `rpcmethods` stays transport-free.
