---
hash: "1f5eaa3e"
id: "7b8a3064"
read_when: "implementing commit-to-session linking in auto-etl or setting up the prepare-commit-msg hook"
summary: "Implementation plan for linking git commits to agent sessions: add session_id to the Commit parquet row via trailer extraction and fallback message matching, plus a new auto-config package with prepare-commit-msg hook installation."
title: "Plan: Task 008 — Commit-Session Link"
---

# Plan: Task 008

## Summary

Add `session_id` to the `Commit` parquet row (extracted from trailer or fallback-matched from messages), and create a minimal `auto-config` package that installs the `prepare-commit-msg` hook via `autoconfig init --project`.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| ~ | auto-etl/internal/model/git.go | Add `SessionID` field to `Commit` struct |
| ~ | auto-etl/internal/git/extract.go | Extract `Session-Id` from `TrailersJSON` during commit parsing |
| ~ | auto-etl/internal/git/extract_test.go | Tests for trailer → SessionID extraction |
| + | auto-etl/internal/git/session_link.go | `LinkSessionIDs`: fallback matching via messages parquet |
| + | auto-etl/internal/git/session_link_test.go | Tests for fallback matching and false-positive filtering |
| ~ | auto-etl/cmd/run.go | Call `LinkSessionIDs` between `ExtractRepo` and `WriteGit` |
| + | auto-config/go.mod | New Go module |
| + | auto-config/go.sum | Generated |
| + | auto-config/CLAUDE.md | Build/test instructions |
| + | auto-config/cmd/autoconfig/main.go | CLI entry point with init command |
| + | auto-config/internal/cli/root.go | Root cobra command |
| + | auto-config/internal/cli/init.go | Init command with --project flag |
| + | auto-config/internal/hooks/install.go | `SetupGitHooks`: check + write + chmod |
| + | auto-config/internal/hooks/install_test.go | Tests for hook installation |
| + | auto-config/internal/hooks/prepare-commit-msg | Hook script (embedded via go:embed) |

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test

- [x] `auto-etl/internal/git/extract_test.go` — trailer extraction, precedence, empty field
- [x] `auto-etl/internal/git/session_link_test.go` — fallback matching, false positive filtering, no-match case
- [x] `auto-config/internal/hooks/install_test.go` — fresh install, existing hook error
- [x] `cd auto-etl && go build ./...` — compiles
- [x] `cd auto-config && go build ./...` — compiles
- [x] `cd auto-etl && go test ./...` — all tests pass
- [x] `cd auto-config && go test ./...` — all tests pass

## Execution Sequence

```
Phase 1 (Model + Trailer) --> Phase 2 (Fallback)
Phase 3 (Auto-config) [independent]
```

Phase 3 has no dependency on Phases 1-2 and can run in parallel. Phase 2 depends on Phase 1 (needs the `SessionID` field on `Commit`).

## Plan

### Phase 1: Commit model + trailer extraction

Add the `SessionID` field and populate it from `TrailersJSON` during parsing.

- [x] Step 1.1: Add `SessionID string` field to `Commit` struct in `auto-etl/internal/model/git.go`, after `TrailersJSON` (line 74). Parquet tag: `parquet:"session_id,dict"`.
  - Verify: `cd auto-etl && go build ./...` compiles cleanly.

- [x] Step 1.2: In `auto-etl/internal/git/extract.go`, add a helper function `extractSessionID(trailersJSON string) string` that unmarshals the JSON as `map[string][]string` and returns the first value of the `"Session-Id"` key (or empty string if absent/empty).
  - Verify: function compiles, returns "" for `"{}"` input.

- [x] Step 1.3: In `parseCommitLog` (extract.go ~line 417), after `trailersJSON := parseTrailers(messageBody)`, call `sessionID := extractSessionID(trailersJSON)`. Set `SessionID: sessionID` in the Commit struct literal (~line 454).
  - Verify: `cd auto-etl && go build ./...` compiles cleanly.

- [x] Step 1.4: Add tests to `auto-etl/internal/git/extract_test.go`:
  - Test `extractSessionID` with trailer present → returns UUID (AC-1)
  - Test `extractSessionID` with no trailer → returns "" (AC-5)
  - Test `extractSessionID` with multiple Session-Id values → returns first
  - Test `parseCommitLog` end-to-end with a commit message containing `Session-Id` trailer → `commit.SessionID` populated (AC-1, AC-4)
  - Test `parseCommitLog` with commit that has no `Session-Id` trailer → `commit.SessionID` is "" (AC-5)
  - Verify: `cd auto-etl && go test ./internal/git/ -run SessionID -v` — all pass.

- [x] Step 1.5: Commit: `feat(008): phase 1 — session_id field and trailer extraction`

### Phase 2: Fallback extraction from messages

Add `LinkSessionIDs` that enriches commits without trailer matches by scanning messages parquet.

- [x] Step 2.1: Create `auto-etl/internal/git/session_link.go` with:
  - A lightweight struct `messageRow` with 4 fields: `SessionID`, `BashCommand`, `Content`, `GitRemote` (with matching parquet tags). Parquet-go silently ignores missing struct fields, so this reads only the columns we need.
  - A compiled regex `commitOutputRe = regexp.MustCompile(\[[\w/.-]+ ([0-9a-f]{7,})\])` for matching git commit output lines.
  - A compiled regex or string-match list for commit-creating bash commands: `git commit`, `git merge`, `git cherry-pick`.
  - Function `LinkSessionIDs(commits []model.Commit, messagesDir string, repoRemoteNormalized string) error` that:
    1. Skips early if no commits need linking (all have SessionID set)
    2. Discovers messages parquet files under `messagesDir` using `filepath.Glob`
    3. Reads each parquet file using the local `readParquet[messageRow]` helper
    4. Filters rows: `BashCommand` must contain a commit-creating command (AC-3)
    5. Filters rows: `GitRemote` must match `repoRemoteNormalized` (repo scoping to avoid cross-repo false matches)
    6. Extracts short SHAs from `Content` using the regex
    7. Builds index: `captured_sha → sessionID` (first match wins)
    8. For each commit with empty `SessionID`, extracts full SHA from `Commit.ID` (strip `repoID-` prefix) and checks if any index key is a prefix of the full SHA. If exactly one match, sets `SessionID`. If multiple matches (ambiguous), skips (AC-5).
  - Verify: `cd auto-etl && go build ./...` compiles cleanly.

- [x] Step 2.2: Move or export `readExistingParquet` so it's accessible from the `git` package. Options: (a) move to a shared `parquet` or `reader` package under `internal/`, or (b) duplicate the small function in `session_link.go`. Choose the simpler option — if the function is only ~20 lines, duplicating avoids import cycle risk.
  - Verify: `cd auto-etl && go build ./...` compiles cleanly.

- [x] Step 2.3: In `auto-etl/cmd/run.go`, after `ExtractRepo` returns (line 414) and before `WriteGit` (line 420), insert:
  ```go
  messagesDir := filepath.Join(outputDir, "messages")
  if err := gitextract.LinkSessionIDs(result.Commits, messagesDir, normalized); err != nil {
      fmt.Fprintf(os.Stderr, "warning: session link fallback: %v\n", err)
  }
  ```
  The `normalized` variable (repo remote URL) is already in scope from line 397.
  - Verify: `cd auto-etl && go build ./...` compiles cleanly.

- [x] Step 2.4: Create `auto-etl/internal/git/session_link_test.go` with:
  - Test: commit-creating bash command with matching short SHA → SessionID set (AC-2)
  - Test: `git log` bash command with matching short SHA → SessionID NOT set (AC-3)
  - Test: `cat` command with matching SHA pattern → SessionID NOT set (AC-3)
  - Test: commit already has SessionID from trailer → fallback does not overwrite (AC-4)
  - Test: no matching message rows → SessionID stays empty (AC-5)
  - Test: messages directory doesn't exist → no error, graceful skip
  - Test: message from different repo (different `git_remote`) → not matched (repo scoping)
  - Test: 7-char captured SHA matches commit with 40-char full SHA → matched (prefix matching)
  - Test: ambiguous prefix (two commits share captured prefix) → neither matched (AC-5)
  - Use in-memory test data or write temp parquet files with parquet-go.
  - Verify: `cd auto-etl && go test ./internal/git/ -run Link -v` — all pass.

- [x] Step 2.5: Run full test suite: `cd auto-etl && go test ./...` — all pass.

- [x] Step 2.6: Commit: `feat(008): phase 2 — fallback session linking from messages parquet`

### Phase 3: Auto-config hook installation

Create the minimal `auto-config` package with `init --project` and git hooks setup.

- [x] Step 3.1: Create `auto-config/go.mod`:
  ```
  module github.com/mistakenot/auto-config
  go 1.26.1
  require (
      github.com/mistakenot/auto-shared v0.0.0
      github.com/spf13/cobra v1.10.2
  )
  replace github.com/mistakenot/auto-shared => ../auto-shared
  ```
  Run `cd auto-config && go mod tidy`.

<!-- RESOLVED(P1): go.mod omits the local auto-shared dependency
REVIEW: Step 3.4 imports `github.com/mistakenot/auto-shared/version`, and `docs/auto-package-patterns.md` says new auto-* modules should use `replace ../auto-shared`. Existing modules such as `auto-etl/go.mod` require `github.com/mistakenot/auto-shared v0.0.0` and replace it with `../auto-shared`. With only Cobra in `go.mod`, `cd auto-config && go build ./...` cannot resolve the local shared module reliably. Add the `auto-shared` require and replace entries in Step 3.1.
AUTHOR: Added `auto-shared v0.0.0` require and `replace ../auto-shared` directive to the go.mod template in Step 3.1.
-->

  - Verify: go.mod and go.sum exist with valid content.

- [x] Step 3.2: Copy the existing `hooks/prepare-commit-msg` script to `auto-config/internal/hooks/prepare-commit-msg` (the file that will be embedded).
  - Verify: file exists and content matches the original.

- [x] Step 3.3: Create `auto-config/internal/hooks/install.go`:
  - `//go:embed prepare-commit-msg` directive with `var hookScript []byte`
  - Function `SetupGitHooks(gitDir string) error` that:
    1. Constructs path: `filepath.Join(gitDir, "hooks", "prepare-commit-msg")`
    2. Checks if file exists — if so, returns `fmt.Errorf("prepare-commit-msg hook already exists at %s — not modified (remove it manually to reinstall)", path)`
    3. Creates the `hooks/` directory with `os.MkdirAll`
    4. Writes the embedded script with `os.WriteFile(path, hookScript, 0755)`
    5. Returns nil on success
  - Verify: `cd auto-config && go build ./...` compiles cleanly.

- [x] Step 3.4: Create `auto-config/internal/cli/root.go`:
  - Root cobra command `autoconfig` with version from `auto-shared/version`
  - `Execute()` function
  - Verify: compiles.

- [x] Step 3.5: Create `auto-config/internal/cli/init.go`:
  - `--project` flag (bool)
  - When `--project` is set:
    1. Run `git rev-parse --git-dir` to find the git directory
    2. Call `hooks.SetupGitHooks(gitDir)`
    3. Print success message to stderr
  - When `--project` is not set: print "global init not yet implemented" and return nil (placeholder for future work)
  - Verify: `cd auto-config && go build ./...` compiles cleanly.

<!-- REJECTED(P2): init behavior conflicts with existing autoconfig requirements
REVIEW: `auto-config/docs/requirements.md` already defines `autoconfig init` as creating `~/.auto/settings.json` and `~/.auto/config/settings.json`, with `init --project` also creating `.auto/config/settings.json` (R2, lines 139-144), and the same doc says command output defaults to JSON. This plan would ship `init` with a successful no-op placeholder and `init --project` with only hook installation plus a stderr success message. Either include the documented settings bootstrap/JSON behavior in this task or update the requirements/scope so the new CLI does not immediately contradict its own docs.
AUTHOR: The auto-config requirements doc is a roadmap for the full package — it hasn't been implemented yet and is not a contract for this task. Task 008 creates a minimal bootstrap with just enough for hook installation (AC-7/AC-8). The placeholder `init` (without `--project`) explicitly prints "not yet implemented" rather than silently succeeding, so it doesn't contradict the roadmap — it signals incomplete. The full init behavior (settings bootstrap, JSON output, scaffold) belongs in a dedicated auto-config implementation task.
-->

- [x] Step 3.6: Create `auto-config/cmd/autoconfig/main.go`:
  - Minimal entry point calling `cli.Execute()`
  - Verify: `cd auto-config && go build ./cmd/autoconfig/` produces a binary.

- [x] Step 3.7: Create `auto-config/CLAUDE.md` with build/test instructions.

- [x] Step 3.8: Create `auto-config/internal/hooks/install_test.go`:
  - Test: fresh install to temp dir → hook file exists with correct content and 0755 permissions (AC-7)
  - Test: existing hook file → returns error with descriptive message, file unchanged (AC-8)
  - Test: git dir has no hooks subdirectory → creates it and installs (AC-7)
  - Verify: `cd auto-config && go test ./internal/hooks/ -v` — all pass.

- [x] Step 3.9: Run full test suite: `cd auto-config && go test ./...` — all pass.

- [x] Step 3.10: Commit: `feat(008): phase 3 — autoconfig init --project with git hook setup`

## Success Criteria

- [x] `cd auto-etl && go build ./...` compiles
- [x] `cd auto-etl && go test ./...` passes (including new session ID tests)
- [x] `cd auto-config && go build ./cmd/autoconfig/` produces binary
- [x] `cd auto-config && go test ./...` passes (including hook install tests)
- [x] Commits with `Session-Id` trailer → `session_id` field populated (AC-1)
- [x] Commits without trailer but with matching message row → `session_id` from fallback (AC-2)
- [x] Non-commit bash commands don't produce false matches (AC-3)
- [x] Trailer always takes precedence over fallback (AC-4)
- [x] No match → empty string, no guess (AC-5)
- [x] `session_id` column present in parquet output (AC-6)
- [x] `autoconfig init --project` installs hook when none exists (AC-7)
- [x] `autoconfig init --project` errors when hook already exists (AC-8)

## Open Questions

None.
