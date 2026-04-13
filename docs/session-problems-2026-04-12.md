---
hash: "c1eb814f"
id: "98c7c34f"
summary: "Analysis of recurring failures in coding agent sessions over April 12, 2026, with 6 categories of problems and 6 proposed skills to prevent them"
title: "Session Problem Analysis & Skill Proposals — April 12, 2026"
---

# Session Problem Analysis & Skill Proposals — April 12, 2026

Analysis of recent coding agent sessions in auto-stack using `autosearch`, looking for recurring failures and proposing new skills to prevent them.

## Method

Searched session history using `autosearch` with queries targeting common failure modes:
- Build errors, undefined symbols, exit codes
- Retry loops, circular behavior
- Merge conflicts, reverts
- Pre-commit hook failures
- Wrong working directory for Go builds
- "File has not been read" tool errors

## Findings

### 1. "Cannot find main module" — agents running `go build ./...` from the repo root

**Evidence:** 12 message hits across 6+ sessions. Agents run `go build ./...` from `/home/vscode/src/auto-stack` (the monorepo root) instead of `cd`-ing into the correct sub-project first. The root has no `go.mod`, so Go fails immediately.

**Example:**
```
go: cannot find main module, but found .git/config in /home/vscode/src/auto-stack
	to create a module there, run:
	go mod init
```

**Sessions affected:** `1028af22`, `44325cd4`, `51c808a0`, `d031ba37`, `ae60077a`, `045565fa`

**Root cause:** The CLAUDE.md says "run `go build ./...` in the relevant module" but agents don't always internalize which directory they need to be in for a multi-module monorepo.

---

### 2. Undefined symbols from editing multiple files before building

**Evidence:** Found in session `e35ca62d` and others. Agent writes to multiple files (types, implementation, tests) without running `go build` between them. By the time it builds, cascading errors compound.

**Example:**
```
internal/github/fetch.go:252:11: undefined: TransformPR
internal/github/fetch.go:253:17: undefined: TransformComments
```

**Root cause:** Agent writes several files in a batch, then builds. If the first file had a mistake, all downstream files that reference it fail too, creating noisy error output that's harder to diagnose.

---

### 3. Unused imports left behind after refactoring

**Evidence:** 28 messages matching `"imported and not used"` or `"declared and not used"` across multiple sessions.

**Examples:**
```
internal/transform/transform.go:56:2: declared and not used: toolResultContent
internal/cli/cli_integration_test.go:6:2: "encoding/json" imported and not used
internal/config/config.go:6:2: "fmt" imported and not used
```

**Root cause:** Agent removes code that used an import but doesn't clean up the import list. Go treats unused imports as compilation errors, causing build failures that require another round-trip to fix.

---

### 4. "File has not been read" tool errors

**Evidence:** 15 messages across sessions `045565fa`, `07c5ecd3`, `44325cd4`. Agent tries to Write or Edit a file it hasn't Read first, triggering a tool-level error.

**Example:**
```
<tool_use_error>File has not been read yet. Read it first before writing to it.</tool_use_error>
```

**Root cause:** Agent knows the file path and content it wants to write but skips the required Read step, wasting a turn.

---

### 5. Merge conflicts from working directly on main

**Evidence:** Session `44325cd4` shows a rebase conflict in `auto-doc/cmd/autodoc/main.go`. The user had to intervene: "no this isnt on a worktree, you'll conflict with other work, move back to main."

**Root cause:** Feature work done directly on main collides with other concurrent agent work. The user has a feedback memory about this ("use branches + PRs for feature work") but agents don't always follow it.

---

### 6. Pre-commit hook failures causing wasted cycles

**Evidence:** 50+ messages mentioning pre-commit hooks. The pre-commit hook runs `gofmt` and `go vet` across all sub-projects. When an agent commits without formatting first, the hook fails, requiring the agent to format, re-stage, and commit again.

**Root cause:** Agents don't run `gofmt` before committing. The hook catches it but at the cost of an extra turn.

---

### 7. Exit code failures — 44 in the last 14 days

**Evidence:** 44 messages with "Exit code 1" or "Exit code 2" across sessions in this workspace in the last 2 weeks. Many are build failures, but also test failures, curl 404s, and git errors.

**Root cause:** Mix of issues — wrong directory, missing imports, failed tests. The volume suggests agents are trial-and-error coding rather than reasoning about their changes before executing.

---

## Proposed Skills

Based on these findings, here are skills that could prevent these recurring issues:

### Skill 1: `go-monorepo-build` — Build-after-edit enforcer for Go monorepos

**Problem it solves:** #1 (wrong directory), #2 (batch edits without building), #3 (unused imports)

**What it does:**
- After editing a `.go` file, determines which `go.mod` module it belongs to
- Automatically runs `go build ./...` in that module directory
- If build fails, immediately surfaces the error before more files are edited
- Could be implemented as a Claude Code hook on file write

**Implementation:** A hook that watches for Write/Edit tool calls on `*.go` files, resolves the nearest `go.mod` parent, and triggers `go build ./...` + `go vet ./...` in that directory.

---

### Skill 2: `go-import-cleanup` — Auto-remove unused imports after refactoring

**Problem it solves:** #3 (unused imports)

**What it does:**
- After removing code from a Go file, scans for imports that are no longer referenced
- Automatically removes them (or runs `goimports`)
- Prevents the build failure → fix import → rebuild cycle

**Implementation:** Hook or post-edit step that runs `goimports -w` on any modified `.go` file.

---

### Skill 3: `pre-commit-preflight` — Run format checks before committing

**Problem it solves:** #6 (pre-commit hook failures)

**What it does:**
- Before any `git commit`, runs the same checks the pre-commit hook would run
- Automatically fixes formatting issues (`gofmt -w`)
- Only proceeds to commit if all checks pass

**Implementation:** Instruction in CLAUDE.md or a skill that wraps the commit flow: format all staged `.go` files, run `go vet`, then commit.

---

### Skill 4: `monorepo-context` — Working directory awareness for multi-module repos

**Problem it solves:** #1 (wrong directory), #2 (batch edits)

**What it does:**
- At session start, detects the monorepo structure (multiple `go.mod` files)
- Maintains awareness of which sub-project the current work is in
- Warns if a command would run in the wrong directory
- Could emit a brief "you're editing auto-etl, build commands should run in `auto-etl/`" reminder

**Implementation:** A session-start skill that scans for `go.mod` files and injects directory-awareness context.

---

### Skill 5: `branch-guard` — Prevent feature work on main

**Problem it solves:** #5 (merge conflicts from working on main)

**What it does:**
- Detects when significant code changes (not just docs/config) are being made on `main`
- Prompts the agent to create a feature branch before continuing
- Prevents the "oops, should have been on a branch" scenario

**Implementation:** Hook that checks `git branch --show-current` before file writes and warns if on main/master with substantial changes.

---

### Skill 6: `session-health-check` — Detect when an agent is stuck in a loop

**Problem it solves:** #7 (high failure rate), retry loops

**What it does:**
- Monitors the ratio of failed commands to successful ones during a session
- If failure rate exceeds a threshold (e.g., 3 consecutive failures), pauses and suggests a different approach
- Could suggest: "You've had 3 build failures in a row. Consider: reading the error message more carefully, checking you're in the right directory, or asking the user for help."

**Implementation:** Hook that tracks exit codes and triggers after N consecutive failures.

---

## Priority Ranking

| # | Skill | Impact | Ease | Priority |
|---|-------|--------|------|----------|
| 1 | `go-monorepo-build` | High — prevents #1, #2, #3 | Medium | P1 |
| 2 | `pre-commit-preflight` | Medium — prevents #6 | Easy | P1 |
| 3 | `go-import-cleanup` | Medium — prevents #3 | Easy | P2 |
| 4 | `branch-guard` | Medium — prevents #5 | Easy | P2 |
| 5 | `monorepo-context` | Medium — prevents #1 | Medium | P2 |
| 6 | `session-health-check` | Low-Medium — general | Hard | P3 |

## Notes on approach

This analysis was done purely through the CLI tools (`autosearch`) without reading any source code. The search was effective at finding:
- Exact error messages (build failures, tool errors)
- Behavioral patterns (retry loops, reverts)
- Session-level problem concentration (which sessions had the most issues)

What was harder to find:
- *Why* an agent chose a bad approach (would need full transcript reads)
- Silent failures (things that didn't produce error output but still wasted time)
- Token waste from reading large files unnecessarily
