---
hash: "c6b15909"
id: "6f8fd0e0"
read_when: "implementing systemd daemon installation in auto-watch or debugging worktree subagent isolation"
summary: "Post-implementation reflections on the autowatch easy daemon: subagent main-worktree leaks, golangci-lint cross-worktree cache pollution, and scope-switch pattern for user-level systemd."
title: "Feedback: Task 018 — Auto Watch Easy Daemon"
---

# Feedback: Task 018

## Problems faced
1. **Subagents leaked edits into the *main* worktree — twice.** Same failure mode as task 017:
   a phase subagent given the 018 worktree path wrote some edits into `/home/vscode/src/auto-stack`
   (main) instead. The Phase 5 agent caught and recovered its own leak (stash + drop); I verified
   main was clean after every phase. Dispatching phases **serially** (not concurrently) kept it to
   recoverable one-offs rather than the half-lost mess of 017.
2. **`golangci-lint` cross-worktree cache pollution (again).** `make check` reported phantom
   `gosec`/`nilerr` findings in `auto-reflect` pointing at `../017-unify-binaries-into-auto/…` — a
   worktree that no longer exists. `golangci-lint cache clean` cleared them; the branch was actually
   clean. Worth knowing this recurs whenever a sibling worktree is removed mid-stream.
3. **A real `modernize` lint finding** in the new `doctor.go` (`strings.Split` → `strings.SplitSeq`)
   only surfaced from `make check` lint, not `go vet`/`go test`. Run the actual `make check` (lint),
   not just build+test, before declaring a Go phase done.
4. **`make vulncheck` fails locally on go1.26.3** — the same 2 stdlib vulns (crypto/x509 + textproto,
   fixed in 1.26.4) as task 017. Not introduced here (018 adds no deps); CI's toolchain passes.

## Reflections
- **Tricky:** distinguishing "real lint failure" from "stale-cache phantom." The tell is the file
  path — if it points at a removed/sibling worktree, it's cache pollution, not your code.
- **Tell myself at the start:** the review surfaced the highest-value catch — AC-1 (no-flag install
  must *enable+start*, not just write the unit). The plan's enable/start defaults were `false`;
  flipping them to `true` (Step 2.1a) was the difference between the headline AC passing or silently
  failing. Trust the doc-review pass to find AC/impl gaps before execution.
- **What worked:** serial phase dispatch + verifying files on disk (and `git -C <main> status`)
  after every phase. Given the recurring main-worktree leak, this is non-negotiable for this repo.
- **Almost did:** unilaterally "slim" the user unit (drop `User=`/`Group=`) per a review nit — but
  it touches `parseInstalledUnit` and the reviewer flagged regression risk, so it stayed a follow-up.

## Useful context
- **Scope-switch pattern:** the cleanest way to add user-level systemd was to parameterize the
  existing `daemoninstall` package by a `Scope` enum (unit dir, `systemctl [--user]`, `WantedBy`,
  validate) rather than fork a parallel implementation. System behavior stayed byte-identical;
  existing tests were pinned to `Scope: ScopeSystem`.
- **`ExecRunner` inherits env by default** (`exec.Command` with nil `Env`), so `systemctl --user`
  gets `XDG_RUNTIME_DIR`/`DBUS_SESSION_BUS_ADDRESS` for free — no Runner change needed. A
  `checkUserBus` preflight still guards the "no user D-Bus" case with a clear remediation.
- **AC-2 update-restart lives in `install.sh`, not Go** — because `auto-cli` can't import
  `auto-watch/internal/daemoninstall` (Go `internal/` rule). The restart is failure-tolerant under
  `set -euo pipefail` (binary already replaced → never abort the update).
- **Follow-ups captured (non-blocking, from review):** (1) omit `User=`/`Group=` from user units
  (+ adjust `parseInstalledUnit`); (3) make `install.sh` iterate `RESTART_SERVICES` if `$BINARIES`
  ever expands. Both deferred intentionally.
- The new `auto watch doctor` check immediately flagged a **real** stale `autowatch.service` on the
  dev host (`ExecStart=…/autowatch start`) — the exact task-017→018 migration case it's meant for.
