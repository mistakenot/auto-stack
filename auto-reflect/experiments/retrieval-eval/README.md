# retrieval-eval

A durable, returnable **offline IR experiment** for `auto reflect` playbook
retrieval. Goal: measure which retrieval method best surfaces the rules a task
actually needs — to de-risk changing the Go matcher
(`auto-reflect/internal/rules/match.go`) with data instead of intuition.

Motivating concern: the shipped matcher uses `--domain` as a **hard exclusion**
and drops any rule with a zero keyword score. A near-universal tag like `go`
(78% of rules) makes domain filtering near-useless, and a wrong/absent domain
guess can silently exclude good rules. Before changing that, we want a golden
set and statistical comparison.

## Status

- [x] **Phase 1 — baseline infrastructure** (this commit)
  - Pinned corpus snapshot (`data/corpus/`), independent of the live event store.
  - Faithful Python port of the Go matcher (`src/retrieval_eval/baseline.py`).
  - **Conformance harness** proving the baseline ranks identically to the Go CLI
    (`conformance/`) — two **hermetic** layers (real-120-rule parity rebuilt from
    the snapshot + synthetic edge cases), each in a throwaway `auto reflect`
    store. Nothing touches the live store: `auto reflect retrieve` appends a
    `retrieval` event on every call, so running it against the project store
    would pollute the canonical log with experiment artifacts.
  - Metric scaffolding (`src/retrieval_eval/metrics.py`).
- [x] **Phase 2 — query mining from held-out sessions** → `data/queries/queries.jsonl`
  (100 queries from 48 held-out sessions; 64 clean / 36 leakage-flagged; see
  `data/queries/QUERIES.md`).
- [x] **Phase 3 — LLM oracle**: full 100-query golden set in `data/qrels/qrels.jsonl`
  (89% coverage, mean 5.85 relevant/query, 585 labels). Reusable: any variant now
  scores with zero new oracle calls. See `data/qrels/QRELS.md` + `DIARY.md`.
- [ ] Phase 4 — variant registry (domain-as-boost, IDF tag weighting, no-filter,
      semantic) + statistical comparison (paired Wilcoxon, bootstrap CIs).
- [ ] Phase 5 — port the winning method into `match.go`.

See **`DIARY.md`** for the running research log: decisions, pilot findings, the
Codex + IIR Ch 8 second reads, and the open issues that gate the full oracle.

## Design decisions

- **Python harness, port winner later.** Variants + stats + oracle are
  Python-native; each variant is ~20 lines here vs a Go rebuild. Only the
  winner gets ported to Go.
- **Baseline fidelity is asserted, not assumed.** `baseline.py` mirrors
  `match.go`; the conformance harness pins it against the real CLI. If `match.go`
  changes, update `baseline.py` and re-run conformance.
- **Pinned, independent data.** The corpus snapshot lives here and does not
  auto-sync with `.auto/reflect/`, so runs are reproducible. Re-snapshot
  intentionally (see `data/corpus/SNAPSHOT.md`).

## Two kinds of checks (don't conflate them)

| | **Baseline conformance** (`conformance/`, `tests/test_baseline_conformance.py`) | **Variant evaluation** (Phase 4, `variants/` — not built yet) |
|---|---|---|
| Question | Does `baseline.py` reproduce the *shipped* matcher exactly? | Which retrieval method surfaces the right rules best? |
| Output | **pass/fail** regression | **metrics** (recall@k, nDCG, excluded-relevant-rate) + significance |
| Asserts | Python == Go *today* (the BASELINE STATE) | nothing pass/fail — it ranks candidates |
| Run | `pytest -m baseline` / `python conformance/run_conformance.py` | (later) `python -m retrieval_eval.evaluate` |

Everything in this commit is **baseline conformance** — it only pins the current
state. No variant has been proposed or judged yet.

## Run

```bash
cd auto-reflect/experiments/retrieval-eval

# Baseline conformance (needs the `auto` binary on PATH). Both layers build
# throwaway stores; the live project store is never read or written.
python conformance/run_conformance.py
python conformance/run_conformance.py --synthetic-only   # just the edge cases
pytest -m baseline                                       # same, via pytest

# Re-snapshot the corpus after the playbook materially changes:
#   (from repo root) regenerate data/corpus/rules.snapshot.json — see SNAPSHOT.md
```

`conformance` is stdlib-only. `uv sync --extra analysis --extra oracle` pulls the
heavier deps once Phases 3–4 land.

## Layout

```
data/corpus/      pinned rule snapshot + provenance (committed)
data/queries/     mined query set (Phase 2)
data/qrels/       oracle relevance judgments (Phase 3)
data/results/     run outputs (gitignored, reproducible)
src/retrieval_eval/
  baseline.py     faithful port of match.go (the conformance baseline)
  gocli.py        wrapper over the real `auto reflect` CLI
  corpus.py       snapshot loader + use_when→id map
  metrics.py      recall@k, ndcg@k, mrr, excluded-relevant-rate
conformance/      Go-vs-Python ranking parity (fixtures + harness + runner)
tests/            pytest entrypoint for conformance
```
