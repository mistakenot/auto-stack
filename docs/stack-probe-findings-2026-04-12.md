---
hash: "7708af0c"
id: "a4a6f2e9"
summary: "Analysis of 14-day session history via autosearch identifying 6 recurring development problems and proposing 11 skills to reduce agent friction and improve coding efficiency."
title: "Stack Probe: Session History Analysis & Skill Opportunities (April 12, 2026)"
---

# Stack Probe: Session History Analysis & Skill Opportunities

**Date:** 2026-04-12
**Method:** Used `autosearch` to search recent (14-day) session history in auto-stack, without reading source code first. All findings derived from tool output.

---

## Recurring Problems Found

### 1. "cannot find main module" — agents run `go build` from repo root

**Occurrences:** 8 messages across 4 sessions
**Query:** `autosearch search '"cannot find main module"' --role tool --since 14d`

Agents repeatedly run `go build ./...` or `go vet ./...` from the repo root (`/home/vscode/src/auto-stack`), which fails because there's no root `go.mod` — each sub-project has its own module. The agent then wastes 1-3 turns realizing it needs to `cd auto-etl && go build ./...` instead.

**Example sessions:** `0e160d60`, `1028af22`, `44325cd4`

**Proposed skill: `go-mono-build`**
A hook or skill that intercepts `go build`/`go vet`/`go test` commands at the repo root and either auto-redirects to all sub-modules or returns an immediate error with the correct command. Could be a Claude Code hook that validates the working directory before executing go commands.

---

### 2. Pre-commit hook lint failures cause commit → fix → recommit cycles

**Occurrences:** 31 messages matching "pre-commit hook" or "gofmt" across 10+ sessions
**Query:** `autosearch search '"pre-commit hook" OR "hook failed" OR "gofmt"' --role tool --since 14d`

Pattern: agent writes code → attempts commit → pre-commit hook catches lint issues (unused vars, `fmt.Errorf` vs `errors.New`, `gosec` warnings) → agent fixes → retries commit. Sometimes 2-3 cycles before a clean commit. The `"let me fix"` pattern shows up 15 times across sessions.

**Example:** Session `9e8c7cca` — `fmt.Errorf("hostId is required")` → lint error → fix to `errors.New`. Session `a8a36427` — `math/rand` vs `crypto/rand` gosec warning.

**Proposed skill: `go-lint-before-commit`**
A skill or hook that runs `make lint` automatically after any Go file edit, *before* the agent attempts to commit. Catches issues at write-time instead of commit-time. Could save 2-4 tool calls per commit cycle.

---

### 3. Missing gcc / cgo build failures

**Occurrences:** Found in session `44325cd4`
**Query:** `autosearch search '"gcc" OR "cgo"' --role tool --since 14d`

`go test ./...` in `auto-etl` fails because CGO is enabled by default but gcc isn't installed in the dev container. The agent sees cascading `[build failed]` across all packages and has to diagnose the root cause.

**Example snippet:**
```
cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in $PATH
FAIL    github.com/mistakenot/auto-etl [build failed]
```

**Proposed skill: `env-doctor`**
Extend the existing `doctor` command pattern to check for build prerequisites (gcc, goimports, golangci-lint) at session start. A startup hook that runs `autoskill doctor` and warns about missing tools before the agent starts coding.

---

### 4. "declared and not used" / "imported and not used" Go compiler errors

**Occurrences:** Found across sessions `0e160d60` and `9e8c7cca`
**Query:** `autosearch search '"declared and not used" OR "imported and not used"' --role tool --since 14d`

Agents introduce unused variables or imports during refactoring, don't notice until `go build` runs, then fix up. The CLAUDE.md says to run `go build ./...` after each file edit, but agents still batch edits and discover errors late.

**Example:** `transform.go:56:2: declared and not used: toolResultContent`

**Proposed skill: `go-build-guard`**
A Claude Code hook that runs `go build ./...` in the correct sub-module directory automatically after any `.go` file write/edit. Enforces the existing CLAUDE.md rule ("run go build after modifying a Go file") mechanically rather than relying on agent compliance.

---

### 5. Excessive file reading — agents re-read files they've already seen

**Occurrences:** 59 messages matching "let me read" from assistant role
**Query:** `autosearch search '"let me read"' --role assistant --since 14d`

Agents frequently announce "Let me read the key files" at session start and re-read the same files across sessions. This is especially wasteful for files like `CLAUDE.md`, `go.mod`, and model definitions that change infrequently.

**Example:** Session `a3457ce0` has 4 separate "let me read" rounds reading progressively deeper into the same codebase.

**Proposed skill: `context-warmup`**
A session-start skill that pre-loads a curated summary of the repo structure, key file locations, and build conventions — like a compressed `recall` but specific to this monorepo. Eliminates the 5-10 file reads agents typically do at session start.

---

### 6. Test failures from flawed test expectations

**Occurrences:** 28 messages matching "test" AND "fail"
**Query:** `autosearch search '"test" AND "fail"' --role tool --since 14d`

Multiple patterns:
- `TestE2E_EmptyInput` — expected no parquet files for empty input but got 3 (session `e35ca62d`)
- `TestSessionSearchRoleFilter` — expected hits with role=tool but query doesn't match (session `0e160d60`)
- `TestCreateHappyWithDirs` — test expects no stderr but skill creation prints guidance text (session `1028af22`)

These are agents writing tests with wrong assertions, not discovering the mismatch until runtime. 

**Proposed skill: `test-assert-reviewer`**
A post-test-write hook that reviews new test assertions against the actual function signatures and return types. Catches "expected 0 but implementation returns N" mismatches before running `go test`.

---

### 7. Meta-search pollution — searching session history returns prior search queries

**Already fixed** in session `0e160d60` by adding the `--role` filter to autosearch. The previous session doing this exact task discovered that searching for error patterns returned its own prior searches as top hits. The `--role tool` filter solved this.

**Status:** Resolved — `--role` flag shipped in commit `6b9e231`.

---

## Skills Not Used

`autosearch skills` returns an empty list — no skills have been detected in any indexed sessions. This confirms the stack is early-stage for skill adoption and validates the need for skill creation.

---

## Meta-Observation: This Task Was Already Done

Session `0e160d60` (326 messages, the highest-scoring error session) was a previous run of *this exact same task* — probing session history to find problems. That session:
1. Searched for the same error patterns I searched for
2. Found the meta-search pollution problem
3. Actually wrote code to fix it (added `--role` filter, `--limit` flag, pagination)
4. Committed the fix as `6b9e231`

This is a concrete demonstration of the auto-stack's feedback loop working: search history → find problem → fix it → ship. The fact that my session can now use `--role tool` to filter out noise is direct evidence of the loop closing.

---

## Summary: Proposed Skills

| # | Skill Name | Problem Solved | Est. Savings |
|---|-----------|---------------|--------------|
| 1 | `go-mono-build` | Wrong-directory go build in monorepo | 1-3 turns/session |
| 2 | `go-lint-before-commit` | Pre-commit hook fix cycles | 2-4 calls/commit |
| 3 | `env-doctor` | Missing build tools (gcc etc) | Entire failed session |
| 4 | `go-build-guard` | Unused vars caught late | 1-2 turns/edit |
| 5 | `context-warmup` | Redundant file reads at session start | 5-10 reads/session |
| 6 | `test-assert-reviewer` | Wrong test expectations | 2-3 fix cycles/test |

---

## Addendum: Second Probe Pass (Codex, 2026-04-12)

This second pass was run directly against recent indexed sessions in `/home/vscode/src/auto-stack` (rolling 14 days) without source-code inspection.

### Additional recurring issues found

### A) Edit tool state failures during patching
- **Times seen:** 9 incidents
- **Window:** first `2026-04-05T15:16:46Z` (`a1a34b0915fc65f5b-49`), most recent `2026-04-12T19:52:12Z` (`0e160d60-06ee-4e5b-8bff-fccdc0138c9a-77`)
- **Signals:** `"File has not been read yet"`, `"File has been modified since read"`, `"String to replace not found in file"`
- **Impact:** repeated edit retries and interrupted implementation flow.

### B) Environment prereq / install blockers
- **Times seen:** 3 high-precision incidents
- **Window:** first `2026-04-09T14:53:11Z` (`e35ca62d-f692-4e3e-8b62-bd8674e9726a-499`), most recent `2026-04-09T15:34:39Z` (`44325cd4-d81a-44ee-bc56-ccb77c319af7-99`)
- **Signals:** `cgo: C compiler "gcc" not found`, `cp ... autowatch: Text file busy`
- **Impact:** build/test/install failures that force detours.

### C) Search-signal quality gaps in reflection workflows
- **Times seen:** 10 incidents
- **Window:** first `2026-04-12T00:38:07Z` (`b815b122-99ad-48b0-9717-ba8577c7387d-308`), most recent `2026-04-12T20:19:36Z` (`0e160d60-06ee-4e5b-8bff-fccdc0138c9a-274`)
- **Signals:** recurring complaints about saturated hit counts (`50` cap), missing highlight utility, and lack of structured aggregation.
- **Impact:** weak ranking confidence when prioritizing issues from session history.

### D) User correction churn
- **Times seen:** 10 correction-language hits
- **Window:** first `2026-04-05T15:20:10Z` (`941bcf49-5646-4aeb-9049-b094ecf5cefc-139`), most recent `2026-04-12T00:38:07Z` (`b815b122-99ad-48b0-9717-ba8577c7387d-308`)
- **Representative corrections:** `no add to todo.md`, `no this isnt on a worktree...`, `no the remote install script`
- **Impact:** avoidable execution drift and extra turns.

### Additional proposed skills

| # | Skill Name | Problem Solved | Est. Savings |
|---|-----------|---------------|--------------|
| 7 | `tool-edit-recovery` | Stale-read and replace-target edit failures | 1-3 failed edit attempts/task |
| 8 | `env-preflight-and-safe-install` | Missing toolchain + busy-binary install errors | 1 failed build/install cycle/task |
| 9 | `autosearch-evidence-hardening` | Saturated/noisy evidence during reflection | Better issue ranking per analysis run |
| 10 | `correction-aware-execution` | User redirection churn after drift | 1-2 correction turns/task |
| 11 | `session-pattern-aggregator` | Missing structured frequency views (file/doc hot spots) | Faster root-cause triage |

### Query evidence used for this addendum
- `autosearch search '"File has not been read yet" OR "modified since read" OR "String to replace not found in file"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --limit 200`
- `autosearch search '"cannot create regular file '/home/vscode/.local/bin/autowatch': Text file busy" OR "cgo: C compiler \"gcc\" not found"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --limit 200`
- `autosearch search '"hits 50" OR "highlights were always null" OR "--count mode" OR "can''t do structured queries"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --limit 200`
- `autosearch search '"no, " OR "didn''t ask" OR "undo" OR "stop" OR "not what I asked" OR "wrong"' --scope messages --cwd /home/vscode/src/auto-stack --since 14d --role user --limit 200`
