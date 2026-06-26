# Feedback: Task 027

## Problems faced
1. Post-029 architecture shift -- Task 029 (state refactor) merged after 027 planning began, moving all subscriptions and RPC calls into a central store.js. The plan had to be restructured mid-flight: liveness state went into the reducer rather than a standalone module, and the `doc.changed` handler in `initStore()` needed widening to force re-list on known `.html` paths (not just unknown paths).
2. `auto ui emit` lacks branch/type fields -- The existing `emit` CLI only synthesizes `agent.tool.post` with `--project`/`--path`/`--worktree` but does not set `branch` or allow arbitrary event types. Conformance testing for liveness required hand-crafted `curl` POSTs to `/api/rpc` with a full CloudEvents envelope instead.
3. `golang.org/x/net/html` as a new dependency -- Adding an HTML parser for `pd-meta` extraction was a fresh dependency not in any workspace module. Required `go mod tidy` in `auto-ui/` only; `go.work` needed no edit since it pins nothing.

## Reflections
- The 029 state refactor was the right call architecturally (grep gate keeps subscriptions centralized), but it invalidated the original plan's client-side approach. Reading the post-029 codebase before planning would have saved a replanning pass.
- Liveness being purely client-side (no server-side hook recency) means it resets on page reload. This is an acceptable tradeoff for a v1 but should be documented as a known limitation.
- The P2 Codex review comments (stale selector closure when plan branch changes, conformance curl missing bus envelope) are valid edge cases worth addressing in a follow-up but not blocking for the initial feature.

## Useful context
- Task 029 artifacts (`state-harness.sh`) define the grep gate that enforces all `on("...")` and `call(...)` live only in `store.js` -- extend the harness rather than breaking the gate.
- `auto-shared/bus/event.go` `AsNotification()` wraps events as `{jsonrpc, method: e.Type, params: e}` -- `params.branch` is top-level on the wire, which is what the liveness join reads.
- Browser-driven conformance (no JS unit tests) is the established pattern from tasks 024-026 and confirmed by 029 feedback.
