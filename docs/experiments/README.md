# Experiments

Long-lived writeups for research-style experiments run against auto-stack. Code, data, embeddings, and plots live in `.tmp/experiments/<name>/`; the markdown findings live here and are checked into git.

## Conventions

- One folder per experiment program, prefixed with the start date: `YYYY-MM-DD-<name>/`
- Each folder contains a `README.md` synthesizing the whole experiment
- Phase or sub-experiment docs are named `phaseN-<topic>.md`
- See [PATTERNS.md](PATTERNS.md) for the dispatch and end-of-experiment checklists and the patterns/anti-patterns observed across runs

## Local setup

Code, data, and intermediate artifacts for an experiment live under `.tmp/experiments/<name>/` (git-ignored); only the markdown findings are checked in here.

- Python spike scripts go in `.tmp/experiments/`. Use [`uv`](https://docs.astral.sh/uv/) to keep each experiment self-contained (inline script dependencies or per-script venvs).
- `.tmp/experiments/.env` holds `OPENAI_API_KEY` (~$30 of credit). Spread API usage across experiments rather than burning it on a single run.

## Experiments

- **[2026-05-26 — Orthogonal Questioning](2026-05-26-orthogonal-questioning/README.md)**: Tested whether requirements could be modeled as a vector space and compressed to ~3 questions via cosine-geometry orthogonal probing. Four phases. Conclusion: the geometric framework as originally proposed doesn't work, but a relaxed version using per-dimension classifiers + active learning hits the same 3-5 question budget on linguistically-legible preference dimensions.

- **[2026-05-28 — Co-change query latency](2026-05-28-cochange-query-latency/phase1-engine-latency.md)**: Tested Task 010's in-memory-SQLite-over-parquet engine for `autosearch co-change`. A query in this repo takes ~348 ms end-to-end, but ~97% is the parquet read — the engine is only ~10 ms. Column projection works (reads 1.35% of the 445 MB). modernc-SQLite beats duckdb 6.4× at this repo's scale but *loses* 3.1× at 33× scale (opencode) due to its row-insert tax; they cross around ~130k commit_files. Verdict: ship v1 as designed (pure-Go, per-query, no duckdb); the cheapest scaling fix is a pure-Go map group-by, not duckdb.

- **[Quint Sync Protocol](quint-sync-protocol/)**: Formal verification of ETL CRDT merge semantics using Quint specification language. Phase 1 (tech spike) validated Quint as viable for modeling merge operations — all 11 CRDT properties pass, tombstone resurrection caught in 63ms. Phase 2 (MBT verification) replayed Quint-generated ITF traces against Go merge functions — spec-aligned merge matches Quint perfectly (0 divergences / 5200 steps), while the current naive "incoming wins" merge diverges on 100% of traces (3889 divergences across tombstone, schema_version, and LWW gaps). Experiment artifact only — no merge code is in the live tree; the merge logic is reproduced as inline examples in the writeup.

- **[Structured Compiler](structured-compiler/)**: (separate experiment, see folder)

## See also

- [PATTERNS.md](PATTERNS.md) — patterns and anti-patterns for running experiments
- `.tmp/experiments/` — code, data, and intermediate artifacts (not in git)
- `docs/spikes/` — earlier-style spike docs (predates this folder convention; new work should go here instead)
