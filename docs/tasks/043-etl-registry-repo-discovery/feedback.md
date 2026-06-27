# Feedback: Task 043

## Problems faced
1. **The accepted plan (AC-3) encoded the very leak the task exists to close.** AC-3 case (a)
   was specified and unit-tested as "a workspace nested under a registered project but with a
   foreign remote is KEPT (via `FindProjectByPath` longest-prefix)." Combined with the GitHub
   phase keying off the cached remote, that meant a clone vendored/experimented with *inside*
   a registered project (`~/myproject/vendor/foreign`) would have its PRs and git history
   indexed — exactly the data-scope leak the gate was meant to stop. Codex flagged it as P1
   on the PR. The fix required reversing an accepted, tested AC, so it was escalated to the
   user before changing it.
2. **`path-OR-remote` is too coarse a gate.** The cache holds `path → origin-remote`, where
   the remote is whatever `git remote get-url origin` resolves from the *enclosing* repo. So a
   genuine subdir/worktree already matches by remote; the only thing a bare path-prefix match
   adds is foreign nested repos. The corrected rule keys primarily on remote, treats *exact*
   path registration as explicit intent, and only allows a prefix match when the workspace has
   no distinct remote of its own.

## Reflections
- **What was tricky?** Distinguishing the three legitimate "keep" cases from the one leak case.
  The key realization: a real subdirectory of a registered repo resolves the *enclosing* repo's
  origin, so it's kept by `FindProjectByRemote` anyway — the path branch is only needed for
  remote-less workspaces. That makes "drop nested paths with a foreign remote" safe without
  losing genuine subdirs/worktrees.
- **What would you tell yourself at the start?** When a gate's purpose is to *exclude* something,
  trace a concrete instance of the thing-to-exclude through every branch of the keep condition
  before trusting the ACs. The plan's own Problem statement ("random GitHub projects I cloned to
  experiment") was the counterexample to its own AC-3 — reading goals and ACs against each other
  would have caught it at planning time.
- **What did you almost do but didn't?** Almost fixed the gate unilaterally during
  address-feedback. Held off because it reversed an accepted + tested AC — that's a
  requirements decision, so I asked the user first (they chose to tighten).
- The existing `FindProjectByExactPath` (added for re-registering nested repos) was exactly the
  primitive needed for the "explicit registration wins" branch — no new lookup required.

## Useful context
- `auto-shared/config/projects.go` — `FindProjectByRemote` (normalizes both sides via
  `git.NormalizeRemoteURL`: SSH↔HTTPS, strip creds, lowercase host), `FindProjectByPath`
  (longest-prefix), and `FindProjectByExactPath` (no parent match). The three-way keep rule
  composes these directly.
- `auto-cli/cmd/auto/hookscmd.go` `loadRegistryQuietly()` — the read-only registry-load pattern
  to mirror (never `EnsureProjects`, so the ETL never mutates the registry just by running).
- The cache semantics matter: `resolveGitRemote` stores an **empty string** as a valid cached
  remote (workspace with no origin), which is what makes "empty remote → keep by path" a real,
  safe case rather than dead code.
- Gate at the `cmd` boundary on the `map[path]remote` — the only place holding both path and
  remote together. The `internal/github` and `internal/git` discovery packages stay
  registry-agnostic; `saveRemotesCache` writes the unfiltered original by construction.
