---
hash: "ea0afe1b"
id: "3c606818"
read_when: "evaluating autosearch co-change engine performance, choosing between SQLite and duckdb for parquet queries, or understanding co-change query latency scaling"
summary: "Performance spike comparing modernc SQLite vs duckdb for the autosearch co-change query engine, with measurements showing pure-Go SQLite is fast enough at typical repo scale and confirming column projection prunes 98.6% of parquet bytes."
title: "Phase 1 — Co-change Query Latency & Engine Comparison"
---

# Phase 1 — Co-change query latency & engine comparison

**Date:** 2026-05-28
**Status:** DESIGN (findings appended in place after execution)
**Tests:** Task 010 — `autosearch co-change` (`docs/tasks/010-autosearch-co-change/`)

## Hypothesis

> Answering an `autosearch co-change <file>` query by reading the host's
> column-projected git parquet on demand and aggregating in an **ephemeral
> in-memory SQLite (`modernc.org/sqlite`, pure-Go)** is fast enough to ship as a
> per-query command with **no persisted derived index**, and is competitive with
> a duckdb-over-parquet approach — so rejecting duckdb (to keep auto-search a
> pure-Go binary) costs little latency.

This is the exact assumption Task 010's solution defers in *Rejected
Alternatives*: *"per-query parquet read is simpler… Revisit if query latency on
large repos becomes a problem."* The user's follow-up sharpens it into a
head-to-head: **does the duckdb rejection actually cost latency, and where does
the per-query model break as repos grow?**

## What we are actually testing

The chosen engine (solution.md "Engine decision", **Option C**):

1. `parquet-go` reads the **column-projected** slice of `commits` + `commit_files`
   (+ `git_repositories`, `git_refs`) — only `commit_id, file_path, change_type,
   old_path, repo_id` etc., *not* the `diff` text that dominates on-disk size.
2. Rows filtered to one `repo_id`, loaded into an in-memory SQLite DB.
3. Per-commit `weight` (large-commit penalty × time decay) computed **in Go** at
   load time; SQL only `SUM`s it.
4. Joins / co-occurrence self-join / per-path totals / top-N in SQL.
5. Final scalar scores (confidence, lift, `score`) in Go.

The comparison engine (the rejected AC-12-original):

- **duckdb CLI** reads the same parquet directly via `read_parquet(...)` with
  column + predicate pushdown, does the whole aggregation in one SQL pass, Go
  computes the final scalar score. End-to-end **includes duckdb process startup**
  (that cost is part of the shell-out approach and is fair to count).

## Dataset (real host data, `~/.auto/etl/output`)

Measured 2026-05-28. The command must scan projected columns across **all** repos
(parquet is partitioned by date, not repo) then filter to the target `repo_id`.

| Scope | repo_id | commits | commit_files | role |
|---|---|---:|---:|---|
| **auto-stack** | `5fccdd7dc00cceda` | 200 | 1,855 | the user's direct question ("this repo") |
| **opencode** | `4eec6c2a0faf5ddf` | 13,211 | 61,753 | real **33×** large-repo scaling target |
| whole dataset | — | 14,902 | 68,730 | projected-read scan size (445 MB on disk, diff-dominated) |

Target file for auto-stack: `auto-etl/internal/git/extract.go` (AC-19's known file;
it has co-changing model/test files, so the result list is non-empty).

## Metrics & reference bands

User chose **"just measure, no hard pass/fail bar"** — so these bands are
**interpretation reference**, not gates. The verdict is the numbers + the A-vs-B
ratio + the scaling curve.

Reference bands for an interactive CLI that reads data on demand:

- **Instant-ish:** < 1.0 s warm
- **Acceptable:** 1.0–3.0 s warm
- **Slow for a CLI:** > 3.0 s warm

| # | Metric | Reference interpretation |
|---|---|---|
| M1 | Engine A (modernc SQLite) end-to-end **warm** latency, auto-stack | the headline answer to "how long in this repo" |
| M2 | Engine A **stage breakdown** (parquet-read / sqlite-load / SQL-aggregate / Go-score) | if parquet-read > 80% of total, the bottleneck is I/O and engine choice barely matters |
| M3 | Engine A **cold** latency (first run / caches dropped if possible) | upper bound a user feels on first invocation |
| M4 | Engine B (duckdb CLI) warm + cold, auto-stack | comparison baseline |
| M5 | **A / B latency ratio** (warm) | ≤2× → pure-Go choice well justified; 2–5× → duckdb notably faster, note it; >5× **and** A >3s → bring the duckdb decision forward |
| M6 | **Column-projection savings**: projected `parquet-go` read vs full (incl. `diff`) read, time + bytes | projected < 20% of full → columnar pruning works as designed (the per-query model depends on this). ≈ full → architecture flaw |
| M7 | **Scaling**: A & B at auto-stack (1.8k) → opencode (61.7k) → optional 2× synthetic | find the row count where warm crosses 1 s and 3 s |
| C1 | **Correctness equivalence**: A and B produce the same ranked related-file set (top-10, paths + co_commits) within float tolerance on `score` | fairness gate — if they disagree, the latency comparison is apples-to-oranges and is void |

## Decision matrix

| M1 (A warm) | M5 (A/B) | M6 (projection) | C1 | Verdict |
|---|---|---|---|---|
| < 3 s | ≤ 2× | prunes | pass | **Full go.** Ship Task 010 as designed: modernc SQLite, per-query, no index, no duckdb. |
| 1–3 s | ≤ 2× | prunes | pass | **Go, with note.** Add a follow-up: "consider a persisted index for repos ≫ opencode size" gated on M7's curve. |
| > 3 s | > 5× & B fast | prunes | pass | **Reconsider now.** Either persist git data into the index (rejected-alt #2) or revisit duckdb — don't defer to "later". |
| any | any | **does not prune** | any | **Architecture flaw.** parquet-go is reading the diff column → per-query model reads 445 MB every call. Fix the slim reader before anything else. |
| any | any | any | **fail** | **Void.** Engines computed different answers; fix the query equivalence before trusting any latency number. |

## Budget

- **Dollars: $0.** No LLM / API calls — this is a pure performance/feasibility spike.
- **Compute/agent time:** ~1–2 h of worker time to build the harness + run the matrix.
- **Guard:** if building the harness exceeds the budget or hits a hard blocker
  (modernc SQLite can't load the rows; duckdb CLI absent; parquet-go can't read a
  dataset), **stop and report what was learned** — do not thrash. duckdb v1.2.1 is
  confirmed present at `/home/vscode/.local/bin/duckdb`.

## Method

- **Faithful stack:** harness is written in **Go**, using the *same* libraries
  auto-search will ship (`github.com/parquet-go/parquet-go v0.29.0`,
  `modernc.org/sqlite v1.47.0`). Measuring a Python prototype would measure a
  different engine — explicitly out.
- **Warm latency:** median (and p90) of ≥ 10 iterations after ≥ 2 warm-up
  iterations, same process.
- **Cold latency:** attempt `sync && echo 3 > /proc/sys/vm/drop_caches` (needs
  root — will likely fail in this container; if so, report "cold not isolable,
  OS page cache warm" and use the first post-start iteration as a cold-ish proxy
  with that caveat stated).
- **Cache strategy:** every measurement appended as one JSONL row
  `{engine, scope, stage, iter, latency_ms, bytes_read?}` to
  `results.jsonl` so a crash mid-matrix is resumable; final rollups written to
  `results.json`. Harness is idempotent / re-runnable.
- **Fairness:** both engines compute the **same** co-change aggregation and are
  cross-checked (C1) before any latency number is trusted. Rename
  canonicalization may be done identically (Go-side map) before both engines and
  noted as a shared simplification — it is not the bottleneck at these sizes.

## Co-change query spec (both engines must match)

Filter: drop commits with `files_changed > 50`. Default decay ON, τ = 90 days.

Per-commit weight (computed in Go for Engine A; inline SQL for duckdb):
```
filesWeight = 1 / log1p(max(1, files_changed))
decay       = exp(-ageSeconds(author_date) / (90 * 86400))      # age vs time.Now()
weight      = filesWeight * decay
```
Aggregates over the target repo's loaded rows, for target path A:
```
Wa  = Σ weight over commits touching A's lineage
Wb  = Σ weight over commits touching candidate B        # over ALL B's commits, not just A∩B
Wab = Σ weight over commits touching both A and B
Wn  = Σ weight over all loaded commits
co_commits(B) = COUNT(DISTINCT commit) touching both    # raw, threshold + display
```
Scoring (Go): `conf_a_to_b = Wab/Wa`, `conf_b_to_a = Wab/Wb`,
`lift = (Wab*Wn)/(Wa*Wb)`, `score = conf_a_to_b * log1p(lift)`.
Filter `co_commits < 3`; if commits-touching-A `< 5` → metadata-only. Sort by
`score` desc, top 50.

> **Pitfall (must honour):** `Wb` is computed over **all** of B's commits
> (per-path totals), *not* inside the A-co-occurrence self-join — otherwise
> `Wb == Wab`, `conf_b_to_a == 1` for everyone, and lift is inflated. See
> requirements RESOLVED(P1) thread.

## Deliverables (validated on the filesystem before any number is believed)

In `.tmp/experiments/2026-05-28-cochange-query-latency/`:
- `harness/` — Go module: slim parquet readers, Engine A, Engine B (duckdb), the
  benchmark loop, the scaling driver, the C1 equivalence check, the M6 projection
  diagnostic.
- `results.jsonl` — per-measurement append log (resumable cache).
- `results.json` — rolled-up medians/p90 per (engine, scope, stage).
- `notes.md` — honest narrative: what actually happened, caveats, surprises, red
  flags. Cold-cache caveat. Any place a number is a proxy not a true measurement.

Findings are appended to **this file** (below the line) after Phase 3 validation.

## What would change our mind

- If M6 shows parquet-go reads ≈ the full 445 MB (no pruning), the per-query model
  is unviable regardless of the SQLite numbers — we'd pivot to a persisted index.
- If duckdb is > 5× faster *and* Engine A exceeds 3 s on opencode-scale, the
  pure-Go-binary principle is costing real UX and the duckdb rejection should be
  reopened now, not "later".
- If A and B disagree on results (C1 fail), every latency comparison here is void.

---

# FINDINGS (executed 2026-05-28)

Harness: `.tmp/experiments/2026-05-28-cochange-query-latency/harness/` (Go 1.26.3,
parquet-go v0.29.0, modernc.org/sqlite v1.47.0, duckdb CLI v1.2.1). 128 real
measurements in `results.jsonl`; rollup in `results.json`. Validated: harness
builds + vets clean, C1 gate passed, numbers cross-checked against the raw log.

## Headline — "how long does a co-change query take in this repo?"

**auto-stack, true end-to-end ≈ 348 ms**, of which:
- **~338 ms is the parquet read** (one-time, engine-agnostic — scans the slim
  5-column projection of all 15 `commit_files` + 15 `commits` files across *all*
  repos, then filters to the target repo in Go), and
- **~10 ms is the engine** (Engine A, modernc SQLite, warm median 9.9 ms / p90 11.8 ms).

> The single most important finding: **at this repo's scale the engine is ~3% of
> wall-clock; the parquet read is ~97%.** The design question "which engine" barely
> moves latency for a normal repo — I/O dominates 34:1.

## Verdict against the decision matrix

| Lever | Result | Band |
|---|---|---|
| M1 — Engine A end-to-end (auto-stack) | ~348 ms (engine 9.9 ms) | **Instant-ish** (< 1 s) |
| M5 — A/B ratio, "this repo" scale | **A 6.4× faster** than duckdb | A well justified |
| M6 — column projection | **slim reads 1.35% of bytes** (6.10 MB vs 453 MB), 0.31× time | **prunes** ✓ |
| C1 — engine equivalence | byte-identical top-N, Wb-pitfall verified absent | **pass** ✓ |

→ **Decision-matrix row 1: FULL GO.** Ship Task 010 as designed — modernc in-memory
SQLite, per-query, no persisted index, no duckdb — **for typical-repo scale**. The
core feasibility assumption (columnar projection avoids reading the 445 MB of diff
text) is **confirmed and load-bearing**: without it the per-query model would read
445 MB per call; with it, 6 MB.

## But the scaling test (M7) qualifies that — two real caveats

**1. The engines cross over.** duckdb is startup-bound (flat ~55 ms fixed cost),
Engine A is load-bound (per-row SQLite insert + index build):

```
 cf_rows     Engine A (engine only)   Engine B (duckdb, incl. startup)   winner
   1,855            9.9 ms                    63.6 ms                    A  6.4×
  61,753           467   ms                   152   ms                   B  3.1×
```

At **opencode scale (33×, 61.7k commit_files)** Engine A is **3.1× slower** than the
rejected duckdb. The cost is the SQLite *load* stage (62% of engine time — inserting
61.7k rows + building two indexes), NOT I/O and NOT the query. Linear extrapolation:
Engine A crosses **1 s at ~130k cf rows** and **3 s at ~400k cf rows**. duckdb stays
flat and wouldn't cross 1 s until the dataset is enormous.

**2. For small repos the engine is the wrong thing to optimize.** Re-reading all 15
parquet files (and scanning other repos' rows before filtering) every call is the
~338 ms that actually hurts. There is **no parquet-level repo pruning** in the naive
model — it reads every repo's slim columns then filters in Go.

## Recommendations (do NOT reopen the duckdb decision; these keep it pure-Go)

1. **Ship v1 as designed.** Correct, fast (< 350 ms), pure-Go for the common case.
2. **Cheapest scaling fix is dropping SQLite, not adding duckdb.** `go_score` is
   already < 0.2 ms; doing the 5a/5b group-by in **pure-Go maps** would very likely
   beat *both* engines at all scales and keep the no-cgo property — the SQL layer is
   buying notational convenience, not speed. (Alternatively: load the in-memory
   table without indexes and let SQLite table-scan.) Worth a follow-up task gated on
   whether any indexed repo approaches ~130k commit_files.
3. **The real latency win is I/O, not the engine:** partition parquet by `repo_id`,
   or push the `repo_id` filter into the parquet read, or cache the slim per-repo
   slice — any of these collapses the dominant ~338 ms.

## Did the outcome match "what would change our mind"?

- *"If M6 shows no pruning → pivot to a persisted index."* → Pruning works (1.35%);
  per-query model stands. **No pivot.**
- *"If duckdb > 5× faster AND A > 3 s → reopen duckdb now."* → duckdb is 3.1× faster
  only at 33× scale, and A is 467 ms there (< 3 s). Threshold **not** met → duckdb
  stays rejected, but we now know the exact scale (~130k rows) where the question
  reopens, and that the pure-Go *map* alternative pre-empts it.
- *"If A and B disagree (C1) → results void."* → C1 **passed**; comparison valid.

## Caveats (from `notes.md`)

- **Cold cache not measurable** — `drop_caches` needs root (absent in container).
  "Coldish" = first-iter with warm OS page cache; absolute parquet-read numbers are
  *optimistic lower bounds*. The A-vs-B comparison is unaffected (both read the same
  warm files).
- **auto-stack target `extract.go` sits in the metadata-only regime** (touched by
  only 4 commits, < the 5-commit threshold) — so its related list is short (2
  candidates). The opencode run exercises the rich path (1,028 commits, 50
  candidates). Latency conclusion unaffected (work is load-bound, not candidate-bound).
- **Engine B `ARG_MAX` bug** found + fixed mid-run (rename map was passed inline on
  argv; switched to temp CSV + `duckdb -f`). C1 re-verified after the fix. Lesson for
  any real duckdb shell-out: never pass large payloads on argv.
- Optional 2× synthetic opencode point was skipped — the two real points already
  establish the crossover and slope.
