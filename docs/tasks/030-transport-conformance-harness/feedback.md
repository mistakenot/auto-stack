# Feedback: Task 030

## Problems faced
1. Unix stale-socket test initially failed because Go's `net.Listen("unix", ...)` auto-unlinks the socket on close -- had to use `net.ListenUnix` with `SetUnlinkOnClose(false)` to simulate a stale socket left on disk.
2. Async handler dispatch (`go p.handleRequest`) required care to ensure all existing tests still passed under `-race` with the changed timing -- the key was that response enqueueing through the write pump serializes delivery regardless of dispatch concurrency.
3. The peer shutdown sequence (drain → close conn → fail waiters) has a subtle ordering tension: draining before closing lets error responses reach the remote, but closing first prevents pump deadlocks on stalled connections. Resolved by keeping drain-first (safe for all realistic conn types: EOF → writes fail immediately; decode error → remote is still reading; ctx cancel → watcher already closed conn).

## Reflections
- The plan's explicit mention of mirroring `ws.go`'s write-pump pattern was the most valuable design guidance -- it prevented reinventing the bounded-buffer/drop-on-full logic and kept the peer consistent with the existing codebase.
- The Fixture/Scenario seam pays off: writing the conformance test once and running it across three transports caught zero transport-specific bugs (which is the point -- the abstraction works), and the API is ready for Task 3 to plug in a real autowatch fixture.
- The `json.RawMessage` choice for `Response.Result` (preserving `result:null` vs absent) was correctly flagged by the review process during planning -- implementing it from the start was much cleaner than retrofitting.

## Useful context
- `auto-ui/internal/server/rpc.go` -- canonical struct shapes and error codes; mirroring these exactly makes the Task 10 merge mechanical.
- `auto-ui/internal/server/ws.go` -- the write-pump pattern (single goroutine, bounded channel, drop-on-full, cancel-on-overflow) is the reference implementation for the Peer.
- `auto-shared/bus/event.go:AsNotification()` -- the wire shape for bus events; AC-5 ensures byte-compatibility so Task 6 plugs in with zero reshaping.
- `net.Pipe()` satisfies `io.ReadWriteCloser` and returns `net.Conn` -- this made the in-process tier trivial (no transport involvement).
