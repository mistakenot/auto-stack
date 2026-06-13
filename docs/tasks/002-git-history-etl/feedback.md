---
hash: "98dbb609"
id: "6590f497"
read_when: "reviewing lessons from the git history ETL task or understanding credential stripping and date convention requirements"
summary: "Post-task feedback from implementing git history ETL: PAT token leak in remote URLs, --since unit convention conflict between months and minutes, and CI format check failure from skipping gofmt."
title: "Feedback: Git History ETL (Task 002)"
---

# Feedback: Task 002

## Problems faced
1. PAT token leaking into parquet output -- `git remote get-url origin` returned URLs with embedded GitHub PAT tokens (`x-access-token:github_pat_...@github.com`). Both `repo_remote` and `repo_remote_normalized` fields stored credentials in plain text. Fixed by adding `StripCredentials()` to the normalize package and applying it before persisting.
2. `--since` unit convention conflict -- The implementation mapped `m` to months, but the project-wide `CLAUDE.md` convention says `m` = minutes. Caught by automated code review. Fixed by making `m` = minutes and introducing `mo` for months.
3. CI format check failure -- `gofmt` wasn't run on all new files before the initial push. The pre-commit hook should catch this locally but the subagent workflow bypassed it.

## Reflections
- The git command output parsing was the trickiest part. `git diff-tree --raw -z` with rename/copy detection produces variable-length NUL-separated records (1 path for M/A/D, 2 paths for R/C). Getting the index-based join between `--raw`, `--numstat`, and unified diff output correct required careful attention to how each command orders and keys its entries.
- At the start: read the project's `--since` convention in `CLAUDE.md` before implementing unit parsing. Convention conflicts are easy to prevent, annoying to fix after review.
- Almost skipped credential stripping since the spec didn't mention it. Glad E2E testing caught it -- this would have been a real security issue persisting PAT tokens into parquet files on disk.

## Useful context
- `auto-etl/internal/writer/github.go` was the most valuable reference -- the read-merge-write pattern, `readExistingParquet[T]()`, and monthly partitioning logic transferred directly to the git writer.
- `auto-etl/internal/github/syncstate.go` provided the exact pattern for atomic state persistence (temp file + rename).
- Using `git diff-tree --root` (with `--root` flag) was important for initial commits that have no parent -- without it, the first commit in a repo would produce no file entries.
