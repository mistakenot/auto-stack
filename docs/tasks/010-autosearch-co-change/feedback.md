---
hash: "f284119e"
id: "5c4ffedf"
read_when: "reviewing lessons from the co-change implementation or debugging co-change scoring correctness"
summary: "Post-implementation feedback for task 010 (autosearch co-change): four problems found including pre-existing lint debt masking CI, Wn large-commit filter bug, and lessons about asserting shared denominators in tests."
title: "Feedback: Task 010 — Autosearch Co-Change"
---

# Feedback: Task 010

`autosearch co-change <path>` — read git parquet into ephemeral in-memory SQLite,
aggregate temporal coupling, score in Go, emit JSON. Merged to `main` as `9dff600`
(PR #44), squash-merged via `--admin` over a red CI (see Problem 4).

## Problems faced

1. **CI was red on pre-existing lint debt, not this PR's code** — `make check`
   runs `fmt-check → vet → lint`, and `make lint` stops at the **first** failing
   project. CI had been red for many commits on gofmt debt, which masked
   everything downstream. Fixing the 7 gofmt files unblocked `fmt-check`, which
   then exposed ~54 golangci-lint issues across the monorepo (auto-graph 28,
   auto-search/cochange 19, auto-shared 6, auto-config 1). The "8 auto-etl
   issues" first reported were just the tip — the linter never reached the rest
   because it aborted on the first failing project. Lesson: when CI is red,
   confirm *which step* fails and remember the per-project loop hides later
   debt; a green `fmt-check` is not a green `check`.

2. **`Wn` silently ignored the large-commit drop (caught in review)** — the lift
   denominator's global term (`Wn = Σ weight over all commits`) was queried from
   `c` with no `files_changed <= 50` filter, while every other pass honoured
   AC-3b. Relative ranking was unaffected (shared factor), so all tests passed —
   but absolute `lift` was inflated. Existing tests asserted `CommitsA` and
   candidate presence but never inspected `Wn`. Fix: add the filter; lock it in
   with a `Wn`-equality assertion in `TestScore_LargeCommitDropped` (fails at
   5.564 vs 4.551 without the filter). Lesson: a quantity used only as a shared
   denominator can be wrong without breaking any ordering test — assert its
   absolute value, not just downstream ranks.

3. **Per-candidate detail ran the recursive rename CTE before filtering (review)**
   — `Aggregate` fetched top authors/sessions/sample-commits for *every*
   co-occurring path, each re-running `pathCanonCTE` ~3×, before `MinCoCommits`
   and `--limit` were applied in `ScoreAndRank`. Tiny fixtures hid it; on a real
   ~1 GB dataset it's the hot path. Fix: split detail into `FillCandidateDetails`
   called *after* score+sort+limit, so the expensive CTEs run only for the
   surviving top-N.

4. **Two transient API 500s killed the Phase 3 subagent mid-run** — the agent had
   written `load.go`/`query.go` but not built/tested/committed. Retrying the
   subagent failed again. The coordinator finished it directly (build, write
   `query_test.go`, commit). Lesson: subagent output is checkpointed in the
   worktree files, not the agent session — when an agent dies, inspect the
   worktree and finish from there rather than blindly re-dispatching.

5. **Ref-tip join key is asymmetric** — `commits.id`/`commit_files.commit_id` are
   `<repoID>-<sha>` but `git_refs.commit_id` is the **raw** sha. A naive
   `ON commit_id` join matches nothing. The query must strip the prefix:
   `refs.commit_id = substr(c.commit_id, length(repoID)+2)`. Flagged in
   requirements review and load-bearing — verify against `extract.go`, not the
   prose docs (`git-history-etl.md` describes `commits.id` loosely as a full
   sha; the code is authoritative).

6. **Fixture determinism needed an isolated HOME** — `autoetl run --only git`
   filters commits against `~/.auto/etl/git/sync-state.json`, so on a dev machine
   that already indexed this repo, `make fixtures` would emit a partial/empty
   snapshot. `--full` is worse: it deletes the user's real sync-state. Fix: run
   autoetl under a fresh temp `HOME` so the sync-state path is empty → guaranteed
   full, deterministic extraction, real `~/.auto` untouched.

## Reflections

- **What was tricky?** The subtle correctness bugs (`Wn`, candidate-path
  canonicalisation, the `Wb`-outside-the-self-join requirement) all *passed
  tests* while being wrong. The planning-doc review threads caught most of these
  before code; the rest came from PR review. The SQL aggregation split
  (per-path totals independent of A, co-occurrence producing only `Wab`) is easy
  to collapse incorrectly into one self-join that makes `Wb == Wab`.
- **What I'd tell myself at the start:** the lint/CI debt is repo-wide and
  long-standing — don't assume a feature branch can land green without a separate
  cleanup. Surface the merge-over-red-CI decision early rather than discovering
  the scope mid-merge.
- **What I almost did but didn't:** (a) almost suppressed the gosec G602 with a
  `//nolint` — instead verified it's a false positive (`i` bounded by
  `range commits`) and cleared it with a pointer consolidation; (b) almost
  ran Phase 3 and Phase 5 in parallel — backed off because they share the
  auto-search Go module and a half-written `cochange` package would race
  `go build ./...` between the two agents.

## Useful context

- **Engine decision (solution.md):** auto-search is pure-Go with zero external
  runtime deps. The original duckdb-CLI plan was replaced by parquet-go +
  ephemeral in-memory `modernc.org/sqlite` + Go scalar scoring. A `:memory:` DB
  is per-connection — **`db.SetMaxOpenConns(1)`** is mandatory or queries hit
  empty tables.
- **`DiscoverDatasets` vs `Discover`:** the indexer writes `index_state`/
  `FilesProcessed` for *any* discovered dataset, so broadening the global
  `Discover` would make `autosearch index` silently churn git parquet. Keep
  `Discover` scoped to `{messages,sessions}`; co-change calls
  `DiscoverDatasets(root, {commits,commit_files,git_repositories,git_refs})`.
- **Privacy boundary is structural:** fixtures are *regenerated* from this repo
  (never sliced from `~/.auto`), column-projected to drop `message`/`diff`/
  `trailers_json`/host-volatile fields, and guarded by parquet-schema
  introspection in `make verify-fixtures`. The raw `repo_remote` is safe because
  ETL runs `StripCredentials` before writing (verified: no PAT in the fixture).
- **`--decay-tau` parser:** reuse `search.ParseDurationMs` (units `m|h|d|w`, no
  `y`), **not** `time.ParseDuration` (rejects `d`/`w`).

## Follow-ups left open

- Repo-wide golangci-lint debt (~54 issues) keeps `main` CI red; deferred by
  user decision. The auto-etl portion (8 issues, behavior-preserving fixes,
  verified `0 issues`) is saved in a git stash titled
  "task010: auto-etl lint fixes" for whoever does the cleanup.
- `top_sessions` is empty against the current real dataset because indexed
  commits lack linked `session_id`s yet (Task 008 trailer linkage) — correct
  behavior, not a bug.
