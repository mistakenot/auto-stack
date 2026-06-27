# Feedback: Task 039

## Problems faced
1. Concurrency between RPC handlers and the tick loop — `rpc.Peer` dispatches each request in its own goroutine, so `task.dispatch` and `task.cancel` run concurrently with Tick/Reap. Required adding a mutex to `daemon.Service` and proving single-reaper/single-mutex dispatch with `-race` tests.
2. Getting `watch.task.*` bus events to fire from both the existing tick-loop path and the new RPC path without duplicating emit logic — solved by emitting at the `daemon.Service` layer (startWorker/Reap) rather than in handlers.

## Reflections
- The biggest risk was concurrency: the daemon was single-threaded before this task. Adding a mutex and proving it under `-race` was the right call; would have been painful to retrofit later.
- The context.md grounding doc was valuable — having exact file:line pointers to `startWorker`, `ReserveRun`, and `Reap` meant no time wasted re-discovering the dispatch flow.
- Keeping RPC handlers thin (validate params → call exported `Service` methods) kept the layering test passing and made unit tests straightforward.

## Useful context
- `store.ErrActiveRunExists` is the only dedup mechanism — no need for application-level locking on dispatch.
- `rpcmethods` must never import `transport` (GR-N3, enforced by layering test from task 031).
- `watch.task.*` events are data-plane (always-on), not gated by `--ctl-events`.
