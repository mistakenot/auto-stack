# Feedback: Task 064

## Problems faced
1. **auto-shared is stdlib-only by test, not by convention** -- Phase 1 added `doublestar` for `**` globs and its own package tests were green; `rpc/layering_test.go` `TestLayeringGate` (which only runs under `./rpc`) failed on the first full `make test`. Replaced with a 40-line segment matcher on `path.Match`. Phase subagents must run the whole module's tests, not just their package.
2. **A live tmux server broke two pane-identity tests** -- the tests exercised the real `tmux list-panes` probe through `Evaluate`, so whenever a tmux server without the seeded `%3` pane was reachable the fixture lock was reclaimed. Fixed with a swappable `DefaultTmuxPanes` var the tests pin; reproduced with a stub `tmux` on PATH because the command guard blocks starting or stopping real tmux sessions.
3. **Phase 1 keyed unregistered repos on the current worktree root** -- two linked worktrees of one repo resolved to different projects and never serialized. The AC-5 "same branch after restart" test caught it in Phase 2; the fallback is now the main worktree path.
4. **Codex review found six real gaps** (all fixed in one commit): oversized hook payloads (>1 MiB) left `payload` nil and skipped the guard entirely; `handlerExists` ignored the group `matcher`, so a handler under `Bash` counted as enforcement; the worktree probe treated every `Stat` error as "gone"; `Clear` held the host-global flock across the `gh` network call; settings.json decoded leniently so a typo like `identitiy` silently changed identity semantics; `status` demanded an identity even though it is read-only.
5. **The Go snap refreshed to 1.27 mid-PR** -- golangci-lint (built with 1.26) failed typechecking the stdlib and 1.27 `gofmt` flagged an untouched auto-etl file in the pre-commit hook. Lint and commit ran with `GOTOOLCHAIN=go1.26.1` and the 1.26 toolchain's `bin` first on PATH rather than reformatting unrelated files.
6. **The command guard matches prose** -- `gh pr create --body` was blocked because the body text mentioned `tmux kill-server`; PR bodies go through `--body-file`.

## Reflections
- What was tricky? Getting "uncertain" right everywhere recovery touches: a probe error, a network hang, an unreadable payload, a lock that changed between two critical sections. The reviewer's pattern was consistent -- every place the code inferred "dead/absent/allowed" from a failure was a place to instead say "unknown, keep the safe state". Worth applying up front next time rather than after review.
- What would you tell yourself at the start? Run `make test` across the whole monorepo after Phase 1, not only at the end; tell each subagent the module-level test command, not the package-level one. Also decide the "no external process inside the flock" rule when designing the store, since the E2E gh stub made it easy to miss.
- What did you almost do but didn't? Reformat `auto-etl/internal/git/extract_test.go` with the new gofmt to get past the hook -- that would have smuggled an unrelated change into the PR. And enforce Codex despite `apply_patch` carrying no path (D-12 kept v1 honest).

## Useful context
- `auto-env/internal/registry/registry.go` `withLock` was the right flock template; `WriteJSONFileAtomic`'s own doc comment says rename alone does not serialize RMW.
- Task 063's four-actor concurrency discipline paid off: breaking the flock made both AC-8 tests fail 10/10, which is what makes the passing run mean something.
- `hints.go`/`hints_test.go` (`runFire`, `t.Setenv("HOME")`, real temp git repo per test) was the exact harness for the deny path; `mail-flow` was the exact template for a host-global-store E2E with two Workers in one container via `AUTO_LOCK_WORKER`.
- D-7 (lib in `auto-shared/lock`, command in-tree) avoided a new module entirely; the cost is the stdlib-only constraint above.
