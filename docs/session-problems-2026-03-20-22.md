---
hash: "e3102e99"
id: "42163573"
summary: "Recurring problems found in coding sessions over the last 48 hours, classified by severity with prevention recommendations"
title: "Session Problem Analysis: March 20-22, 2026"
---

# Session Problem Analysis: March 20-22, 2026

Analysis of ~20 coding sessions (3,500+ messages) in the auto-stack repo from March 20-22, 2026. Problems were identified by searching session history via `autosearch` for error signals, retries, rewrites, and failure patterns.

## Problems Found

### P1. Go compilation errors from incomplete code generation (Severity: HIGH)

**Occurrences:** 5+ across sessions `17c23bc9`, `07c5ecd3`, `00f0bcac`

**Pattern:** Agent writes code that references functions/types not yet defined, then hits `undefined` errors on build. Examples:
- `undefined: writeFileAtomic`, `undefined: remediationError`, `undefined: normalizeServiceName` in `daemoninstall` (session `17c23bc9`)
- `undefined: fmt` — unused import left after refactor (session `00f0bcac`)
- `cannot use parquet.ReadMode(...)` — wrong API usage (session `00f0bcac`)

**Root cause:** Agent writes files incrementally across multiple tool calls. When a file references helpers not yet written, the build fails. The agent then has to diagnose and fix, wasting 2-5 turns each time.

**Prevention:**
- CLAUDE.md rule: "When writing new Go packages, stub all referenced functions/types before running `go build`. If splitting across files, write the dependency file first."
- Pre-commit `go vet` catches this at commit time but not during iterative development — consider adding a build-check habit after each file write.

---

### P2. Unused imports after refactoring (Severity: MEDIUM)

**Occurrences:** 3+ across sessions `07c5ecd3`, `88943e4d`

**Pattern:** Agent removes or refactors code but leaves behind unused imports. Go refuses to compile.
- Unused `encoding/json`, `runtime`, `strings` in CLI test file after removing planned tests
- Unused `model` import after removing blob tests
- Agent fixes, then later the same file gets unused imports again

**Root cause:** Go's strict unused-import rule catches what other languages silently accept. Agent doesn't always clean up imports after removing code that used them.

**Prevention:**
- CLAUDE.md rule: "After removing Go code, always check the import block and remove any imports that are no longer referenced."
- The pre-commit hook runs `gofmt` but not `goimports`. Adding `goimports -w` to the hook would auto-fix unused imports.

---

### P3. Autodoc linkscan crashes on directories (Severity: HIGH)

**Occurrences:** 4+ across sessions `6c71f534`, `342685f1`

**Pattern:** `autodoc fix` crashes with `read .claude/skills/open-prose: is a directory`. The linkscan source-tag scanner calls `os.ReadFile()` on paths returned by `git ls-files`, but some paths are directories (git submodules, skill directories).

**Error messages:**
- `scanning source tags: read .claude/skills/open-prose: read /home/vscode/src/auto-stack/.claude/skills/open-prose: is a directory`
- `scanning source tags: read .claude/skills/contextual-commit: is a directory`

**Root cause:** `linkscan` iterates `git ls-files` output and assumes every entry is a regular file. Directories (from git submodules or special entries) cause a crash.

**Prevention:**
- Code fix: add `os.Stat()` / `info.IsDir()` check before `os.ReadFile()` in linkscan
- Already noted as a todo in `auto-doc/todo.md` — needs to be prioritized and fixed

---

### P4. Testdata files discovered as real docs (Severity: MEDIUM)

**Occurrences:** 2+ in session `6c71f534`

**Pattern:** `auto-doc/e2e/testdata/two_way_freshness/docs/caching.md` shows up as a real doc in `autodoc fix` output. The `testdata/` directory was added to linkscan's ignore list but the doc discovery (`doctree`) still finds docs inside `testdata/` directories.

**Root cause:** Two separate discovery systems (linkscan for source tags, doctree for docs) with independent ignore lists. Adding an ignore to one doesn't propagate to the other.

**Prevention:**
- Code fix: add `testdata` to doctree's built-in skip list (Go convention — `testdata/` is always test fixture data)
- This was fixed during session `6c71f534` but the pattern reveals a design issue: ignore lists should be shared or centralized

---

### P5. Beads (br) database busy / sync conflicts (Severity: MEDIUM)

**Occurrences:** 6+ across sessions `17c23bc9`, `07c5ecd3`, `6c71f534`

**Pattern:** `br` commands fail with `database is busy` or `Sync conflict: JSONL is newer`. This happens when multiple agents or the pre-commit hook access the beads SQLite DB simultaneously.

**Error messages:**
- `Error: Database error: database is busy`
- `Error: Sync conflict: JSONL is newer (2026-03-22...)`
- Recurring `WAL frame salt mismatch` errors on every `br` invocation

**Root cause:** Multiple concurrent agents (parent + subagents in worktrees) and the pre-commit hook all access `.beads/issues.db`. SQLite doesn't handle concurrent writers well without WAL mode + busy timeout configuration.

**Prevention:**
- Configure SQLite with `PRAGMA busy_timeout=5000` in the beads tool
- The `WAL frame salt mismatch` errors appear to be benign (beads still functions) but noisy — investigate whether this is a beads_rust bug
- For pre-commit hooks: run `br sync --flush-only` with a retry on busy errors

---

### P6. Parquet reader EOF handling (Severity: MEDIUM)

**Occurrences:** 2 in session `07c5ecd3`

**Pattern:** Autosearch `index` command fails with `EOF` when reading valid parquet files. The generic `parquet-go` reader returns `io.EOF` even when rows were successfully read.

**Root cause:** The `parquet-go` library's `Read()` call returns `io.EOF` at end of file, which is normal behavior but was being treated as an error.

**Prevention:**
- Was fixed during the session by adjusting the read loop to handle `io.EOF` correctly
- CLAUDE.md note: "When using parquet-go's generic reader, `io.EOF` from `Read()` is expected — it signals end of data, not an error. Always check rows read before treating EOF as failure."

---

### P7. Agent syntax errors from code edits (Severity: LOW)

**Occurrences:** 3+ across sessions `6c71f534`, `07c5ecd3`

**Pattern:** Agent introduces syntax errors during edits — extra commas, broken indentation, mismatched brackets — then has to do a corrective follow-up.
- "I have a syntax error — extra comma. Let me fix it."
- "The indentation got messed up. Let me rewrite the scanning loop properly."
- "Need to fix all the return statements to return `([]string, error)`"

**Root cause:** When making complex multi-line edits, the agent sometimes produces malformed code, especially when modifying deeply nested structures or changing function signatures across multiple call sites.

**Prevention:**
- Run `go build ./...` after each significant edit to catch immediately
- For signature changes: use a structured approach — change the signature, then find all call sites with `grep`, then update each one

---

### P8. Test fixture race conditions (Severity: LOW)

**Occurrences:** 2 in session `07c5ecd3`

**Pattern:** Test hits unexpected EOF reading fixture parquet files. Root cause: `TestGenerateFixtures` was regenerating fixtures in parallel with another test that reads them.

Agent response: "Passes on retry. Likely a race from parallel test execution."

**Root cause:** Test fixture generation and test reading share the same files without synchronization. Go runs tests in the same package in parallel by default.

**Prevention:**
- Use `TestMain` to generate fixtures once before all tests run
- Or use `sync.Once` in fixture generation
- Or put fixture generation in a separate `_test.go` file with `TestFixtures` that other tests depend on

---

### P9. Autodoc fix gives wrong instructions for stale-hash-only files (Severity: LOW)

**Occurrences:** 1 instance in session `6c71f534`, but affects every `autodoc fix` run

**Pattern:** When a doc file has valid frontmatter but just a stale hash (content changed since last `autodoc fixed`), `autodoc fix` tells the agent to "Set summary to a one-line description" even when the summary is already fine. The stale hash just means content changed, not that the summary is wrong.

**Root cause:** The fix instruction generator doesn't distinguish between "missing/empty summary" and "stale hash" — it emits the same template for both.

**Prevention:**
- Was fixed during session `6c71f534` by adding a separate `EmptySummary` issue type
- For stale-hash-only files, fix now says "review and run `autodoc fixed`" instead of demanding a summary rewrite

---

### P10. Agent reads non-existent files (Severity: LOW)

**Occurrences:** 10+ across sessions `00f0bcac`, `af7815df`, `51f5dc53`

**Pattern:** Agent tries to read files that don't exist, gets "File does not exist" errors. This wastes turns and context window. One session (`af7815df`) had 6+ failed file reads in a row.

**Root cause:** Agent guesses file paths based on naming conventions or prior knowledge rather than listing directory contents first.

**Prevention:**
- CLAUDE.md rule: "Before reading a file, use `ls` or `Glob` to verify it exists, especially when exploring unfamiliar parts of the codebase."
- This is more of a general agent behavior issue than a project-specific bug

---

## Summary by Severity

| Severity | Count | Problems |
|----------|-------|----------|
| HIGH     | 2     | P1 (Go undefined errors), P3 (linkscan directory crash) |
| MEDIUM   | 4     | P2 (unused imports), P4 (testdata discovery), P5 (beads DB busy), P6 (parquet EOF) |
| LOW      | 4     | P7 (syntax errors), P8 (test race), P9 (autodoc fix instructions), P10 (file-not-found reads) |

## Top 3 Actionable Fixes

1. **Fix linkscan directory crash (P3)** — Add `IsDir()` check. This blocks `autodoc fix` entirely.
2. **Add `goimports` to pre-commit hook (P1, P2)** — Would auto-fix unused imports and catch undefined references earlier.
3. **Configure beads SQLite busy timeout (P5)** — Reduce noise and failures from concurrent access.
