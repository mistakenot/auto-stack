---
hash: "ea638198"
id: "eee90a40"
summary: "System improvement opportunities found by searching recent coding session history with autosearch, extending the earlier session-problems analysis"
title: "System Improvement Opportunities — March 22, 2026"
---

# System Improvement Opportunities — March 22, 2026

Analysis of 50 coding sessions (~9,000 indexed messages) in the auto-stack repo, searching for recurring waste, friction, and failure patterns. This extends the earlier [session-problems-2026-03-20-22.md](session-problems-2026-03-20-22.md) with a focus on systemic improvement opportunities rather than individual bugs.

## Search Methodology

All findings below were discovered using `autosearch search` with various queries. The index was rebuilt at session start with `autosearch index` (271 sessions, 9,184 messages). Searches were scoped to `--cwd /home/vscode/src/auto-stack` and filtered by `--since 7d` or `--since 14d`.

### Queries Run

| # | Query | Scope | Hits | What it revealed |
|---|-------|-------|------|------------------|
| 1 | `"build failed" OR "does not compile" OR "undefined:" OR "compilation error"` | sessions, 7d | 37 | Go build failures are the #1 error category |
| 2 | `"test failed" OR "FAIL:" OR "panic:" OR "assertion failed"` | sessions, 7d | 50 | Test failures widespread, especially nil-pointer panics |
| 3 | `"retry" OR "retrying" OR "try again" OR "attempt failed"` | sessions, 7d | 50 | Retry loops common, some from rate limits, some from code bugs |
| 4 | `"wrong" OR "mistake" OR "actually" OR "sorry" OR "let me fix"` | sessions, 7d | 50 | Agent self-corrections frequent — signals wasted turns |
| 5 | `"pre-commit" OR "hook failed" OR "lint" OR "gofmt"` | sessions, 7d | 50 | Pre-commit hook is a consistent friction point |
| 6 | `"WAL frame salt" OR "database is locked" OR "database is busy"` | messages, 14d | 50 | Beads SQLite corruption is pervasive |
| 7 | `"redeclared in this block" OR "already declared" OR "duplicate"` | messages, 7d | 50 | Symbol redeclarations from agent writing duplicate code |
| 8 | `"nil pointer" OR "segfault" OR "SIGSEGV" OR "panic:"` | messages, 14d | 24 | Autodoc doctor command has an unfixed nil-pointer crash |
| 9 | `"File has been modified since read"` | messages, 7d | 3 | Linter modifying files between agent read/write (session 17c23bc9) |
| 10 | `"Exit code 1" OR "Exit code 2"` | messages, 5d | 50 | Top offenders: session 17c23bc9 (12), 045565fa (8), 6c71f534 (6) |
| 11 | `"no not" OR "didnt ask" OR "stop" OR "undo that" OR "thats wrong"` | messages, 14d | 49 | At least 1 instance of user having to undo unwanted agent action |
| 12 | `"golangci-lint" OR "linter" OR "staticcheck"` | messages, 3d | 16 | Linter setup caused "file modified since read" errors |
| 13 | `"parquet" AND ("error" OR "corrupt" OR "EOF" OR "invalid")` | messages, 14d | 50 | Parquet EOF handling still brittle |
| 14 | `"autosearch search" OR "session problem" OR "problem analysis"` | messages, 3d | 45 | 16 sessions doing meta-analysis — significant overhead |
| 15 | `"TestDoctorInvalidProjectConfig" OR "doctor" AND "panic"` | messages, 7d | 6 | Unfixed nil-pointer in autodoc doctor |

## Improvement Opportunities

### OPP-1: Reduce Go compilation churn (HIGH IMPACT)

**Evidence:** 37 sessions with build failures (query #1), 50 with redeclarations (query #7). Session `17c23bc9` had 12 non-zero exits alone — a 711-message, 44M-token session with 352 bash commands. Much of that was compile-fix-retry loops.

**Specific patterns found:**
- `ParquetMessageRow redeclared in this block` — agent wrote duplicate type definitions across files
- `TestResolveSpec_Defaults redeclared` — duplicate test function names
- `undefined: fmt` — imports left dangling after refactors
- `undefined: writeFileAtomic`, `undefined: remediationError` — references to not-yet-written helpers

**What to do:**
1. **Add `goimports` to the pre-commit hook.** Currently only `gofmt` runs. `goimports` would auto-fix unused imports, eliminating the "unused import" → "fix import" → "re-commit" cycle.
2. **Add a CLAUDE.md rule:** "After writing a new Go file, run `go build ./...` in the module before moving to the next file. Don't accumulate unbuilt files."
3. **Consider `golangci-lint` in the pre-commit hook** (already added per recent commits) — but watch for the "file modified since read" issue it causes (query #12).

---

### OPP-2: Fix beads SQLite concurrency (HIGH IMPACT)

**Evidence:** 50 messages with WAL/busy errors (query #6). Every `br` invocation logs `WAL frame salt mismatch`. Multiple sessions show `database is busy` when the pre-commit hook runs `br sync --flush-only` while an agent is also using beads.

**Specific errors found:**
- `WAL frame salt mismatch — chain terminated frame_index=5141` (every single `br` call)
- `Error: Database error: database is busy`
- `Error: Sync conflict: JSONL is newer`

**What to do:**
1. **Report the WAL frame salt mismatch upstream** to beads_rust. It's logged as ERROR on every invocation, suggesting either a persistent corruption or a bug in WAL handling.
2. **Add `PRAGMA busy_timeout=5000`** to the beads SQLite connection (if configurable).
3. **Make the pre-commit hook's `br sync` resilient** — wrap it in a retry with a short backoff, or skip sync if the DB is locked (non-blocking check).

---

### OPP-3: Fix autodoc doctor nil-pointer crash (MEDIUM IMPACT)

**Evidence:** 6 messages (query #15) showing `TestDoctorInvalidProjectConfig` panics with `invalid memory address or nil pointer dereference`. Found in sessions `6c71f534`, `045565fa` — two separate sessions hitting the same crash.

**Root cause identified in transcript:** When `config.Load` fails with invalid JSON, `cfg` is nil, but the code dereferences it on line 65.

**What to do:**
1. **Fix the nil check in the doctor command** — this is a one-line fix (check `cfg != nil` before accessing fields).
2. The fix was identified during session `6c71f534` but may not have been committed — verify and fix.

---

### OPP-4: Reduce meta-analysis overhead (MEDIUM IMPACT)

**Evidence:** 45 messages across 16 sessions (query #14) running `autosearch search` for problem analysis. This is the third time this exact analysis workflow has been run, each time producing similar findings. Sessions `a810fc8e`, `ae60077a`, `045565fa`, `f2e2e033`, and the current session all do variations of "search for problems in logs."

**Pattern:** User asks agent to analyze session history → agent runs 8-15 search queries → reads transcripts → writes a doc → next session does the same thing again.

**What to do:**
1. **Consolidate findings into one living doc** rather than creating new analysis docs each time. This doc should become that living doc.
2. **Create a simple script or makefile target** that runs the standard set of diagnostic queries and outputs a summary, so the analysis doesn't need to be re-driven by an agent each time.
3. **Track fixes** — the existing `session-problems-2026-03-20-22.md` identified 10 problems but doesn't track which ones were fixed.

---

### OPP-5: Linter vs agent file-write conflicts (MEDIUM IMPACT)

**Evidence:** 3 occurrences of `File has been modified since read` in session `17c23bc9` (query #9), 16 linter-related messages in 3 days (query #12).

**Pattern:** Agent reads a file → writes an edit → linter (golangci-lint or gofmt via hook) modifies the file → agent's next edit fails because the file changed. The agent then has to re-read and retry.

**What to do:**
1. **Ensure linters run only on commit**, not as background watchers. If IDE linters are modifying files on save, they conflict with agent writes.
2. **Add a CLAUDE.md note:** "If you get 'File has been modified since read', re-read the file before retrying the edit — a linter may have reformatted it."

---

### OPP-6: Parquet reader brittleness (LOW-MEDIUM IMPACT)

**Evidence:** 50 messages (query #13) referencing parquet errors. Recurring `EOF` errors when reading valid parquet files. The `parquet-go` library returns `io.EOF` at end of file, which was being treated as an error.

**What to do:**
1. The core EOF fix was applied, but verify it's in all code paths — search for `parquet.NewGenericReader` usage and ensure all read loops handle `io.EOF` correctly.
2. **Add a test with a minimal parquet fixture** that exercises the EOF boundary condition.

---

### OPP-7: Autodoc linkscan directory crash (LOW — may be fixed)

**Evidence:** Found in earlier analysis (sessions `6c71f534`, `342685f1`). `autodoc fix` crashes with `read .claude/skills/open-prose: is a directory` because linkscan calls `os.ReadFile()` on directories.

**What to do:**
1. Verify this was fixed — the earlier doc identified it but didn't confirm the fix shipped.
2. If not fixed, add an `info.IsDir()` check before `os.ReadFile()` in the linkscan walker.

---

### OPP-8: Large orchestration sessions are expensive and fragile (SYSTEMIC)

**Evidence:** Session `17c23bc9` — 711 messages, 44M tokens, 5+ hours, 12 error exits. Session `07c5ecd3` — 529 messages. Session `6c71f534` — 782 messages. These are multi-issue orchestration sessions started from beads `br ready` that try to complete many blocked tasks in sequence.

**Pattern:** Session starts with "Reread AGENTS.md and continue from where you left off" → agent picks up multiple blocked issues → works on them sequentially → accumulates context → errors compound → later work in the session is degraded.

**What to do:**
1. **Prefer smaller, single-issue sessions** over marathon multi-issue sessions. The error rate clearly increases in the later portion of long sessions.
2. **Use worktrees for parallel independent issues** rather than sequential work in the same session.
3. **Set a soft cap** — if a session exceeds ~200 messages, consider wrapping up and starting fresh.

---

### OPP-9: Agent acts before being asked (LOW)

**Evidence:** 1 clear instance (query #11): user says "no didnt ask you to start yet. undo that change" in session `51f5dc53`.

**What to do:**
1. Already addressed in CLAUDE.md interaction rules ("When you need user input, break it down into single numbered items").
2. Consider adding: "For ambiguous or multi-step tasks, confirm the plan before starting implementation."

---

## Summary

| # | Opportunity | Impact | Effort | Status |
|---|------------|--------|--------|--------|
| OPP-1 | Reduce Go compilation churn | HIGH | LOW | `goimports` + CLAUDE.md rule |
| OPP-2 | Fix beads SQLite concurrency | HIGH | MEDIUM | Upstream report + busy_timeout |
| OPP-3 | Fix autodoc doctor nil-pointer | MEDIUM | LOW | One-line fix |
| OPP-4 | Reduce meta-analysis overhead | MEDIUM | LOW | Living doc + script |
| OPP-5 | Linter vs agent write conflicts | MEDIUM | LOW | Config change + CLAUDE.md note |
| OPP-6 | Parquet reader brittleness | LOW-MED | LOW | Verify fix + add test |
| OPP-7 | Autodoc linkscan directory crash | LOW | LOW | Verify fix shipped |
| OPP-8 | Large sessions are fragile | SYSTEMIC | PROCESS | Scope prompts, session budgets, commit checkpoints (see Deep Dive below) |
| OPP-9 | Agent acts before asked | LOW | LOW | Already addressed in rules |

## Deep Dive: Why Long Sessions Happen

The 5 longest sessions (374–782 messages) were analyzed in detail by reading their transcripts, counting task transitions (`br close`/`br update`), subagent spawns, context compactions, and user interventions.

| Session | Messages | Tokens | Tasks closed | Subagents | Compactions | Files written | Pattern |
|---------|----------|--------|-------------|-----------|-------------|---------------|---------|
| `6c71f534` | 782 | 65M | 10 | 0 | 0 | 174 | Interactive v1 implementation |
| `17c23bc9` | 711 | 44M | 10+ | 12 | 1 | 50 | "Continue from where you left off" |
| `07c5ecd3` | 529 | 30M | 12 | 0 | 0 | 80 | "Continue from where you left off" |
| `6250c45c` | 457 | 28M | 0 | 0 | 0 | 178 | Iterative design doc |
| `88943e4d` | 374 | 21M | — | 0 | 0 | 76 | Cross-repo spec comparison |

### Pattern A: Open-ended "continue" prompt (sessions `17c23bc9`, `07c5ecd3`)

Both start with the same prompt:
```
# Session Recovery Context
## Current Task Status
- [ ] Blocked: [auto-k8z.11] ...
- [ ] Blocked: [auto-ocd.7] ...
[10 blocked issues listed]

Reread AGENTS.md and continue from where you left off.
```

The agent reads AGENTS.md, runs `br ready`, then **sequentially grinds through every unblocked beads issue**. Session `07c5ecd3` closed 12 tasks across 3 sub-projects (autosearch, autowatch, autostack) and produced 17 commits. Session `17c23bc9` closed 10+ tasks, spawned 12 subagents, hit context compaction once, and ended 46 files changed with 5,624 insertions.

**Root cause:** "Continue from where you left off" is an open-ended directive with no stopping condition. The agent interprets it as "do everything on the backlog until done." The beads `br ready` queue keeps feeding it more work.

**Queries used:** `autosearch session describe <id>`, then `autosearch session get <id>` with grep for `br close`, `task-notification`, and `being continued from a previous conversation`.

### Pattern B: User-driven scope accumulation (sessions `6c71f534`, `6250c45c`)

Session `6c71f534` (782 msgs) starts with "create a doc describing changes needed for autodoc v1," then the user iterates: "also move config to .auto/doc/" → "add acceptance criteria" → "use beads to plan the work" → "get working" → "commit all your work." What began as a gap-analysis doc turned into a full v1 implementation — 10 beads tasks closed, 174 files written.

Session `6250c45c` (457 msgs) follows the same arc: starts designing an autowatch solution doc, then the user adds diagrams, sections, and refinements. 178 file writes, almost entirely to one solution doc.

**Root cause:** These sessions are productive — real work is getting done. But the user keeps expanding scope in the same session rather than committing and starting fresh. By the end, the agent is carrying a huge context of earlier edits it may no longer remember accurately.

### Pattern C: Cross-project exploration (session `88943e4d`)

Starts with comparing auto-etl to the user-journey spec, then pivots into implementation. Touches auto-etl, auto-search, and auto-watch models and schemas. 374 messages with 114 bash commands and 38 file reads.

**Root cause:** Comparing spec to implementation across multiple sub-projects with separate Go modules naturally requires reading many files. No single stopping point.

### Consequences of long sessions

1. **Error accumulation** — session `17c23bc9` had 12 non-zero exits (highest of any session). Error rate is higher in the second half of long sessions.
2. **Context compaction** — when the context window fills, earlier specifics are lost. The agent must re-read files it already saw, wasting turns.
3. **Linter/file conflicts** — session `17c23bc9` had 3 "File has been modified since read" errors, all from linters modifying files between agent read/write cycles.
4. **Beads DB contention** — long sessions with subagents cause concurrent `br` access, triggering "database is busy" and WAL errors.

### Recommended changes

| # | Change | Addresses |
|---|--------|-----------|
| 1 | **Scope the "continue" prompt** — "work on the next 2-3 ready issues, then commit and stop" instead of open-ended "continue from where you left off" | Pattern A |
| 2 | **Add a session budget guideline to CLAUDE.md** — "After closing 3 beads issues or reaching ~150 messages, commit your work and suggest starting a fresh session" | Patterns A, B |
| 3 | **Split work by sub-project** — run separate sessions for autosearch, autowatch, autostack rather than one session spanning all three | Patterns A, C |
| 4 | **Commit after each completed task** — don't accumulate 17 commits for a single session-end push. Commit and push after each beads issue closes | Patterns A, B |
| 5 | **Design then implement in separate sessions** — when a design doc is done, commit it and start a fresh session for implementation | Pattern B |

## Relationship to Prior Analysis

The [session-problems-2026-03-20-22.md](session-problems-2026-03-20-22.md) doc identified 10 specific problems (P1-P10). This doc overlaps on the root causes but focuses on systemic improvements rather than individual bug fixes:

- P1 (Go undefined errors) → OPP-1
- P2 (unused imports) → OPP-1
- P3 (linkscan crash) → OPP-7
- P5 (beads DB busy) → OPP-2
- P6 (parquet EOF) → OPP-6
- P9 (autodoc fix instructions) → OPP-3
- P10 (file-not-found reads) → OPP-8 (symptom of long sessions)
