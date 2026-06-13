---
hash: "9343d2a3"
id: "e3001352"
read_when: "improving autograph context pack traversal logic or understanding why graph-based context selection underperforms agent-assembled bundles"
summary: "A/B evaluation comparing autograph static code graph against an agent-assembled context bundle, identifying five graph traversal problems that caused it to score 46/110 vs 92/110 for the agent team."
title: "Context Pack A/B Findings"
---

# Context Pack A/B Findings

Results from an automated A/B evaluation comparing `autograph code context` (static graph) against an agent-assembled context bundle. Run 2026-05-26 against `auto-etl`.

## Test Setup

- **Target project:** auto-etl (Go, ~30 files)
- **Question:** Implement a raw file backup step in the ETL pipeline (backup `.jsonl` files to `~/.auto/etl/raw/` before parsing, gate `--full` rebuilds on backup existence, add `--force` override)
- **Seed files:** `cmd/run.go`, `internal/parser/parser.go`
- **Token budget:** 8,000
- **Score:** Graph CLI 46/110, Agent Team 92/110

## Problem 1: Depth-first into the wrong subtree

The tool traversed **down** from seed files, following Go imports. `cmd/run.go` imports 6 packages, but the tool went depth-first into `internal/git` and included 4 files (discover.go, normalize.go, normalize_test.go, state.go) consuming 2,516 tokens (29% of budget).

None of these files are relevant to the backup feature. The git package handles git-history ETL — a completely separate pipeline path from session backup. The tool has no way to know that `cmd/run.go`'s import of `internal/git` is only exercised under `--only git`, not the session backup path.

**Impact:** Budget exhausted on irrelevant files. Higher-value siblings (`writer`, `progress`, `root.go`) were pushed to the Omitted list.

## Problem 2: No non-code file discovery

The tool only traces Go import edges. The most valuable file for this question was `docs/codex-integration.md` — the authoritative design spec describing the exact backup directory layout, safety gate rules, `--force` semantics, and phased implementation plan.

A developer reading the graph bundle would have to independently discover this spec, potentially making design decisions that contradict it.

**Impact:** For feature-planning questions, the design spec is often more important than any single source file. The tool is blind to it.

## Problem 3: Traversal direction mismatch

The tool went **down** from seeds (following imports). For this question, the natural exploration is **sideways and up**:

- **Sideways from `cmd/run.go`:** `cmd/root.go` (where `--force` flag would be registered), sibling command files
- **Up from `parser.go`:** `internal/transform/transform.go` (what consumes parser output, relevant to understanding the pipeline the backup step inserts into)
- **Sideways to patterns:** `internal/writer/writer.go` (shows `fileExists` skip pattern that backup's append-only semantics should mirror), `internal/progress/progress.go` (UX pattern for the new phase)

None of these made the cut because downward traversal into `internal/git` consumed the budget first.

## Problem 4: No relevance filtering

The tool treats all import edges as equally important. It cannot distinguish between:

- Imports used by the code path relevant to the question (`parser`, `writer`, `progress`)
- Imports used by unrelated code paths in the same file (`git`, `github`)

Since `cmd/run.go` is an orchestrator that imports everything, following all imports equally produces a noisy result.

## Problem 5: Test file inclusion without relevance check

`internal/git/normalize_test.go` (778 tokens) was included as a dependency. It tests URL normalization, which has no bearing on the backup feature. Test files should be deprioritized unless they test functions directly related to the question.

## What the agent team got right

The agent team's bundle included 6 files, all relevant:

| File | Why it matters | Graph CLI status |
|------|---------------|-----------------|
| `cmd/run.go` (full) | Orchestrator, `--full` flag, pipeline phases | Included (seed) |
| `internal/parser/parser.go` (trimmed) | `ScanAndParse`, `findJSONLFiles` discovery | Included (seed) |
| `docs/codex-integration.md` (excerpt) | Authoritative backup design spec | Not discoverable |
| `internal/writer/writer.go` | `fileExists` skip pattern, partition layout | Omitted (budget) |
| `cmd/root.go` | CLI flag structure for adding `--force` | Not in Omitted list |
| `internal/progress/progress.go` | Progress bar pattern for new phase | Omitted (budget) |

## Recommendations

### P1 — Would have changed the outcome

1. **Scan for related markdown docs.** When seed files reference concepts (flag names, directory paths, function names), search `.md` files in the project for mentions. `codex-integration.md` mentions `raw`, `--full`, `ScanAndParse` — all terms from the seeds.

2. **Budget-aware sibling priority.** When a seed is a high-fan-out orchestrator, allocate budget across imported packages before going deep into any one. One file from each of the 6 imported packages (~150-700 tokens each) would have been far more useful than 4 files from `internal/git`.

### P2 — Would have improved quality

3. **Include CLI root when seed is a Cobra command.** When a seed file calls `rootCmd.AddCommand()`, auto-include the file containing `rootCmd`. This is a ~150-token file that's almost always relevant for CLI feature changes.

4. **Deprioritize test files for transitive deps.** `normalize_test.go` is a test for a file that's itself only a transitive dependency. Test files should rank below source files at the same graph distance, and far below source files that are closer to the seeds.

5. **Consider upward/reverse traversal.** Files that import the seeds (dependents) are often as important as files the seeds import (dependencies). `transform.go` imports `parser.go` and shows how parsed output is consumed — critical context for understanding where the backup step fits in the pipeline.

### P3 — Nice to have

6. **Question-aware pruning.** If the tool received the question text, it could use keyword overlap or semantic similarity to score candidate files before budget allocation. `internal/git/normalize.go` shares zero terms with a question about "backup", "raw", "JSONL", or "append-only".

7. **Include `progress.go` when seeds use the progress pattern.** The seed imports `internal/progress` and uses `progress.Bar{}`. Including the 226-token definition file shows the UX convention to follow.
