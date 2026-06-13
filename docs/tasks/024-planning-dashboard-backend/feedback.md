# Feedback: Task 024

## Problems faced

1. **`gosec G705` XSS taint on the raw HTML route (Phase 3)** — `golangci-lint` flagged
   `w.Write(data)` in `handleDocRaw` as an XSS sink. Serving verbatim HTML *is* the route's
   contract (self-contained planning docs render only inside a sandboxed iframe, never inlined),
   and the path is already constrained by `cleanDocPath(path, ".html")` to a `docs/**/*.html` file
   under a registered root. Resolved with a scoped `//nolint:gosec` plus an explanatory comment —
   there was no prior gosec-suppression precedent in `auto-ui`/`auto-shared`, so the suppression
   style had to be chosen fresh.

2. **Autodoc freshness churn on the epic doc** — every phase's pre-commit hook emitted an
   informational "needs frontmatter attention" note for `docs/epics/002-planning-docs-dashboard.md`.
   The hook *allows* the commit (informational only), so it was noise until Phase 8, where editing
   the epic's sub-task index and running `auto doc fixed docs/epics/002-planning-docs-dashboard.md`
   refreshed the hash and cleared it. Worth knowing: the freshness note is not a failure and does
   not block phase commits.

3. **`abs` field had no defined source in `emit` (Phase 7)** — the plan originally wrote
   `abs: filepath.Join(root, path)` without saying where `root` came from for a client-side command
   that only takes `--project`/`--path`/`--worktree`/`--port`. This was already caught and resolved
   in the planning review: `DeriveDocChanged` carries `p.Abs` into `DocChanged.AbsPath` *without
   validating it*, so exactly one `doc.changed` derives regardless of `abs`. `emit` therefore sets
   `abs = filepath.Join(worktree, path)` only when `--worktree` is given (else empty) and never
   loads the registry. The pre-resolved review thread saved an implementation dead-end.

## Reflections

- **What was tricky:** keeping the task-021-owned signal path (`Hub`, `Event` envelope,
  `DeriveDocChanged` rules) untouched while still extending coverage. The discipline of "the only
  allowed change to `auto-shared/bus` is the one-line `isDocPath` widening" held cleanly — the debug
  buffer taps `handleRPC` (the ingest point), not the Hub, which is the right seam.
- **What I'd tell myself at the start:** the parameterized `cleanDocPath(p, allowed...)` had to land
  first (Phase 1) so the raw route (Phase 3) builds on it rather than re-editing the same function.
  Sequencing the shared validator before its consumers avoided a merge-within-branch headache.
- **What I almost did but didn't:** almost ran Phase 5 (`auto-shared/bus`, fully independent module)
  in parallel with the `auto-ui` chain to save wall-clock. Held off because concurrent subagents
  sharing one worktree leak writes into the main worktree (project rule); serial dispatch kept the
  branch clean and was worth the small time cost.

## Useful context

- **`git.NormalizeRemoteURL` (`auto-shared/git/normalize.go:16`)** is the canonical credential-strip
  for any UI/bus/log boundary. `project.list` re-emits the stored registry `remote`, so it must pass
  through this before reaching the browser — matching `FindProjectByRemote` which already normalizes
  both sides. The test asserts a `https://user:token@github.com/...` entry is emitted credential-free.
- **Server test idiom** (`server_test.go`/`rpc_ingest_test.go`): `server.New(newTestFS(), "test",
  WithRegistryProvider(fn), WithDebug(true))` + `httptest`. But the raw route reads from the *real*
  OS filesystem via `resolveRoot` (not the embedded `web.FS`), so `raw_test.go` needs a real temp
  `docs/` tree with a fixture registry pointing at it — a different fixture shape from the in-memory
  `fstest.MapFS` used elsewhere.
- **`cmd.Context()` + `signal.NotifyContext`** is how `serve` derives its shutdown signal, so
  `serve_test.go` triggers graceful shutdown by cancelling a context set via `cmd.SetContext` — no
  OS signal needed. This made the blocking-server test tractable.
- **The `--ready-file` JSON line (`{"addr":"127.0.0.1:NNNN"}`)** plus `net.Listen` + `srv.Serve(ln)`
  (instead of `ListenAndServe`) is what makes `--port 0` deterministic for a harness: read the real
  bound port from `ln.Addr()` rather than scraping the stderr banner.
