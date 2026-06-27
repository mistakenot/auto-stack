# Feedback: Task 045

## Problems faced
1. **id-only dedup missed the separately-derived `doc.changed`** — caught by Codex review during planning, not execution. Both auto-ui's local `/api/rpc` derive and autowatch's derive call `NewEvent` with a fresh random id, so the two `doc.changed` events wouldn't collide. Fixed at the root: `bus.DeriveDocChanged` now mints a **deterministic** id (hash of source id + type + path), so both derive sites produce the same id and the consumer-side id-dedup catches the derived pair too.
2. **Relay-degraded conns needed an explicit retry path** — `Reconcile` only redialed on disconnect, so a connected-but-degraded conn (subscribe failed) would never retry. Added a dedicated branch that re-attempts the (bounded) `bus.subscribe` on `connected && relayDegraded` conns. Also made the `bus.subscribe` call deadline-bounded so a silent backend can't wedge the reconcile wait group.
3. **The event sink raced the read loop** — a plain `func` field set after `Run` started would race the `OnNotify` reader. Stored it in an `atomic.Value`, loaded+copied per notification, invoked outside `m.mu`. `go test -race` is the guard.

## Reflections
- **What was tricky:** the dedup correctness was deceptively subtle — the obvious "dedup by id" silently fails for derived events because each derive site mints its own id. The deterministic-id fix is the clean root cause, not a band-aid at the consumer.
- **What I'd tell myself at the start:** a derived event is a pure function of its source — give it a deterministic id from day one; random ids for derived events are the actual bug.
- **Almost did but didn't:** almost did semantic dedup (collapse `doc.changed` by type+project+path) at the consumer; rejected for the deterministic-id approach which keeps dedup uniformly id-based and benefits any consumer.

## Useful context
- The task-042 `fakeBackend`/`fakeFleet` harness (rpc.Peer over net.Pipe) extended cleanly to host a `bus.Hub` + `bus.subscribe`→peerSink, making the end-to-end relay→Hub→WS test API-level (GR-N7) with no subprocesses.
- The dedup lives in an auto-ui `eventGate`, not the shared `bus.Hub`, so autowatch's hub contract is unchanged (bus-spec §5 note added documenting the consumer-side extension + the deterministic derived id in §2).
- Scope held: SPA host-aware filtering is Task 9 (SPA filters by project only today); the `/api/rpc` path + hook dual-post are removed in Task 10 — the dedup guards the transition window until then.
