# Feedback: Task 028

## Problems faced
1. None significant -- the task was well-scoped with clear context.md and plan.html. All three phases completed without blockers.

## Reflections
- The Codex reviewer caught a valid gap: `HostIDQuietly()` wasn't validating the configured hostId against the format regex before returning it, so a hand-edited `host.json` with invalid format would silently leak onto bus events. Good catch from automated review.
- The `hostname.username` default in `EnsureHost()` needed a fallback path for when `user.Current()` fails or the generated default doesn't pass validation -- important for edge cases like containerized environments without a proper user database.
- `newDerived()` already calls `NewEvent()` which auto-populates Host, but we still override with `src.Host` to preserve the source event's identity. This distinction matters for correctness but is easy to overlook.

## Useful context
- context.md's file-level code pointers (with line numbers) made subagent dispatch very efficient -- each phase agent could start coding immediately without exploration.
- The existing table-driven test patterns in `event_test.go` and `doctor_test.go` made extending tests straightforward.
- The `config` package already imported in `bus/event.go` (from task 021) meant no circular dependency concerns when calling `config.HostIDQuietly()` from `NewEvent()`.
