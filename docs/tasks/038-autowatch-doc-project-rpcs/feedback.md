# Feedback: Task 038

## Problems faced
1. Path validation parity — auto-ui's `cleanDocPath` had subtle traversal-guard behavior (reject `..`, enforce extension) that needed exact reimplementation in autowatch's `rpcmethods/docfs.go` rather than extraction to a shared package (per GR-N3 layering).
2. `doc.raw` over JSON-RPC — returning raw HTML bytes + content type over a JSON-RPC envelope required a deliberate response shape (`{content, contentType, path}`) so the future Task 7 proxy can reconstruct the HTTP GET without re-deriving guards.

## Reflections
- The parity test approach (conformance fixtures checking response shapes match auto-ui's current output) was the right call — it caught two edge cases in doc-walk ordering.
- Starting from auto-ui's existing `docs.go` / `raw.go` / `project.go` as the behavioral spec kept scope tight; resisting the urge to "improve" the API surface avoided scope creep.
- Stamping `host` into `project.list` entries (the one decided addition) was straightforward since `config.HostIDQuietly()` was already available.

## Useful context
- `auto-ui/internal/server/docs.go` is the behavioral reference for doc.list/doc.get path handling.
- `auto-watch/internal/rpcmethods/parity_test.go` validates response-shape parity and is the fastest way to catch regressions if either side changes.
- The 031 conformance test pattern (in-process + binary tiers) scaled cleanly to four new methods.
