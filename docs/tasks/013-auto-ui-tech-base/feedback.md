# Feedback: Task 013

## Problems faced

1. **Go tests passed but the SPA was broken in the browser.** The whole proof rendered a blank
   page because esm.sh's `*` external-all import-map prefix makes `htm/preact` import a *bare* `htm`
   specifier that wasn't in the map → module resolution failed silently. No Go test could catch this;
   only the agent-browser conformance run surfaced it. Lesson: for a no-build browser frontend, the
   browser conformance step is not optional polish — it's the only real test of AC-1/3/5.
2. **Dev edit→reload served stale assets.** `http.FileServer` sends only `Last-Modified` (no
   `Cache-Control`), so a plain browser refresh reused the cached ES module even though the server
   served fresh bytes (confirmed via curl). AC-2 looked broken until we added `Cache-Control:
   no-store` in disk mode. The server-vs-browser split (curl works, browser doesn't) was the key
   diagnostic.
3. **`go run` child outlived the parent.** Killing the `go run -tags dev` PID left the compiled
   `autoui` child holding the port, so the next dev server failed to bind. Building an explicit
   `-tags dev` binary (rather than `go run`) gave a clean process lifecycle for the conformance run.
4. **`pkill -f "autoui serve"` killed my own shell.** The pattern matched the bash command line
   running it (exit 144 = SIGTERM). Kill listeners by PID via `ss`, never `pkill -f` on a string
   that appears in your own command.
5. **agent-browser `find role button <name> click` silently didn't click.** It first looked like an
   off-by-one in the counter. The reliable pattern is `snapshot -i` → `click @ref`, re-snapshotting
   after every re-render (a stale ref drops the click).
6. **Graceful shutdown was dead code, twice.** First because `main.go` uses `context.Background()`
   (fixed by moving signal handling into the `serve` command, mirroring auto-watch); then the review
   re-flagged that `srv.Shutdown(context.Background())` had no deadline (fixed with a 10s timeout).

## Reflections

- **What was tricky:** every defect in this task was invisible to the Go toolchain — they lived in
  the browser/runtime layer (import maps, HTTP caching, process lifecycle). The build was green the
  whole time. Trust the conformance run over the test suite for the frontend.
- **What I'd tell myself at the start:** when you adopt the esm.sh `*` prefix, *every* transitive
  bare specifier (here, `htm`) must be in the import map — add the leaf entries up front. And plan
  for `Cache-Control: no-store` in dev from the beginning; it's the actual enabler of "edit→refresh".
- **What I almost did but didn't:** almost marked AC-5 as a real off-by-one bug in the counter — it
  was test-harness timing (stale refs), not product code. Verifying with a deterministic re-snapshot
  loop before "fixing" production code saved a wrong change.

## Useful context

- `docs/auto-package-patterns.md` + the auto-graph scaffold (commit `78d2616`) — the exact template
  for a new auto-* package; hand-copying auto-graph was cleaner than the `new-package` skill here
  because the skill also edits the root Makefile/CLAUDE.md (our Phase 5) and prompts interactively.
- `auto-shared/config` (`AutoDir`, `EnsureAutoDir`, `DecodeJSONFileStrict`, `WriteJSONFile`,
  `ValidationError`) and `auto-shared/update.Run` — drop-in, matched the context.md signatures.
- `auto-watch/internal/cli/ops.go` — the precedent for `signal.NotifyContext(cmd.Context(), ...)`
  inside a long-running command (since every `main.go` passes `context.Background()`).
- The build-tag split lives in a dedicated `web` package next to `static/` because `//go:embed`
  can't reference parent directories — worth remembering for any future embedded-asset package.
- `conformance.md` + `evidence/` — the agent-browser script and screenshots; re-run it after any
  frontend or asset-serving change, not just Go tests.
