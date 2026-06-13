---
hash: "0bcb41d3"
id: "281ccb11"
read_when: "reviewing lessons from the binary unification task or understanding the rootcmd.New() seam pattern and concurrent subagent isolation requirements"
summary: "Post-task feedback from unifying separate tool binaries into the auto umbrella: concurrent subagent write leaks into main worktree, non-re-entrant init() refactor causing panic, hallucinated docs-sweep agent, and go.work sync version rewriting."
title: "Feedback: Unify Binaries into Auto (Task 017)"
---

# Feedback: Task 017

## Problems faced
1. **Concurrent subagents leaked writes into the *main* worktree.** The first Phase-2
   fan-out dispatched 14 subagents editing one shared worktree at once; ~half their
   writes "vanished" from the task worktree — and were later found as uncommitted
   changes in the **main** worktree. New files in new dirs (the `rootcmd/` wrappers)
   were the most affected. The harness does not isolate concurrent subagents' file
   writes when they share a worktree. Fix: redo the 7 affected tools **serially**,
   verifying each agent's files landed on disk before the next.
2. **The auto-etl `init()`→`NewRootCmd()` refactor wasn't re-entrant.** It kept a
   package-global `runCmd` and re-registered flags on each build, so building the tree
   twice (as the umbrella integration test does) panicked with "flag redefined". Fix:
   construct fresh commands inside `newRunCmd/newZenCmd/newUpdateCmd`.
3. **A docs-sweep subagent hallucinated** that `user-journey.md` / `autostack-install-daemon.md`
   were "already done" when they were untouched. Always re-verify an agent's claim with
   your own grep before trusting it.
4. **Adding `auto-config` to Makefile `PROJECTS` surfaced a pre-existing lint finding.**
   auto-config had never been linted/tested in CI; the merge pulled it in and CI caught a
   gocritic `stringXbytes` issue (fixed with `bytes.Equal`).
5. **`go work sync` rewrote indirect dep versions** in 4 tool `go.mod` files (MVS
   unification, e.g. auto-watch sqlite v1.39.1→v1.47.0 which auto-search already pinned).
   Kept — the workspace already built against those versions.
6. **`make vulncheck` fails on go1.26.3** (stdlib `crypto/x509`+`textproto`, fixed in
   1.26.4) — pre-existing, reached via auto-doc; CI's `setup-go: "1.26"` resolves to the
   fixed patch, so CI is green.
7. **golangci-lint cache pollution** across sibling worktrees caused a spurious local
   lint failure pointing at a non-existent file path (`auto-stack-task-016/...`);
   `golangci-lint cache clean` resolved it.

## Reflections
- **Tricky:** the lost-writes mystery. Work appeared to vanish; the real cause (leakage
  into the main worktree) only became visible when the user noticed untracked changes on
  main. Don't assume "agent reported success" == "files on disk".
- **Tell myself at the start:** never fan out many subagents that write to one shared
  worktree concurrently — go serial, or give each `isolation: worktree` (but note that
  branches from origin/main, so it won't see uncommitted foundation like a new `go.work`).
  Saved this as a durable memory.
- **Almost did:** trust the hallucinated "already done" docs agent and the agents'
  self-reported builds. Verifying on disk caught both the leak and the hallucination.

## Useful context
- **Pattern:** `go.work` workspace + `auto-cli` umbrella + a public `rootcmd.New(stdout,stderr)`
  seam per tool keeps Go's `internal/` rule satisfied without import-path rewrites.
- **The auto-doc outlier:** module path is `github.com/datadyne-io/autodoc` (not mistakenot);
  `auto-graph/go.mod` was the working `require`+`replace` template.
- **AC-7 guard** (`scripts/check-no-stale-binary-refs.sh`): stem + whitespace + subcommand
  token, with a curated allowlist (service identity, doc-index `Read when:` lines,
  `[autodoc()]` tags, backtick-wrapped design prose). Narrow enough to skip prose/comments.
- **CLAUDE.md JSON-stdout contract** drove the `update.Run` fix (route install.sh chatter
  to stderr so `auto update` emits pure JSON).
