# Feedback: Task 041

## Problems faced
1. **Server-only keepalive false-reaps healthy subscribers** — a Codex review (P1) caught that a one-directional keepalive (only the daemon pinging) would reap a perfectly healthy *subscriber* peer that simply had no inbound traffic. Fixed properly with **bidirectional ping/pong** (`fix(041): keepalive ping/pong …`) rather than the weaker "require both sides to opt in" workaround. This is exactly the AC-3 (no-false-reap) failure mode the plan anticipated — the watchdog must see liveness, and a silent-but-alive peer needs the pong to prove it.
2. **`auto-shared` was excluded from every aggregate gate** — it was absent from the Makefile `PROJECTS` list, so its concurrency tests had never run in CI. Adding it to `PROJECTS` (full gating) was a coupled prerequisite for G1's premise ("green CI = trustworthy"); this surfaced in the Grok review during planning, not at execution.
3. **Race detector needs CGO** — `test-race` must run with `CGO_ENABLED=1` and a C toolchain, and is kept as a *separate* CI step so plain `make test` stays cgo-free/fast.

## Reflections
- **What was tricky:** the liveness mechanism is deceptively subtle — "detect a dead peer" and "don't kill a quiet-but-alive peer" pull in opposite directions, and only bidirectional ping/pong satisfies both. The two ACs (AC-2 reap / AC-3 no-false-reap) were the right guard rails.
- **What I'd tell myself at the start:** liveness is the *only* failure detector once you assume reliable transport (GR-N8), so it carries more weight than it looks — design the pong path from day one, don't bolt it on.
- **Almost did but didn't:** almost used `SetReadDeadline` for the watchdog (rejected — `io.ReadWriteCloser` typing + `json.Decoder` mid-frame corruption); the watchdog-closes-conn approach was cleaner and transport-agnostic.

## Useful context
- The conformance harness (`auto-shared/rpc/conformance`, from task 030) was the key enabler — running the hard behaviors across pipe/unix/tcp with one scenario set, plus the new `faultConn` for deterministic connection-drop / drop-on-full.
- `hub.SinkCount()` (added in task 040) made the dead-subscriber-reap test trivial — no new accessor needed.
- The mutation kill-tests (one-time, captured in `artifacts/`) proved the suite actually catches a swapped pending-key / removed drop-on-full / disabled reap — the evidence that converts "tests pass" into "tests would catch a break."
