---
hash: "a1679137"
id: "bf69a693"
read_when: "evaluating incremental decision graph recompilation, understanding structured compiler safety metrics, or planning full-recompile vs incremental modes"
summary: "Experiment report validating that incremental recompilation of decision graphs is safe (IR=1.0, SDLR=0.0) with 69% token savings, while noting interface-mutation over-invalidation as a known conservative trade-off."
title: "Structured Compiler Assumption 3 Report — Incremental Recompilation Is Sound"
---

# Assumption 3 Report — Incremental Recompilation Is Sound

**Verdict:** PARTIAL — safety thresholds met; ship full-recompile mode first

**Scope:** 120 mutation runs across 40 cases.
Each case received up to 3 root mutations (storage, interface, validation).
Model: `gpt-4o-mini`, temperature=0, seed=11. Total OpenAI spend so far: **$0.0231** (graph extraction + recompiles).

## Headline metrics

| Metric | Threshold | Mean | Median | 95% bootstrap CI | Pass (CI side) |
|--------|-----------|------|--------|------------------|----------------|
| IR (Invalidation Recall) | >= 0.95 | 1.000 | 1.000 | [1.000, 1.000] | PASS |
| IP (Invalidation Precision) | >= 0.80 | 0.814 | 1.000 | [0.763, 0.863] | FAIL |
| SDLR (Stale Decision Leak Rate) | <= 0.02 | 0.000 | 0.000 | [0.000, 0.000] | PASS |
| RS (Recompute Savings) | >= 0.40 | 0.694 | 0.667 | [0.674, 0.714] | PASS |

Safety pair = IR + SDLR. Efficiency pair = IP + RS. CI side = lower bound for `>=` metrics, upper bound for `<=` metrics.

## Breakdown by mutation type

| Mutation | N | IP | IR | SDLR | RS | Cases with stale | Cases with missed |
|----------|----|----|----|------|----|------------------|-------------------|
| interface | 40 | 0.451 | 1.000 | 0.000 | 0.599 | 0 | 0 |
| storage | 40 | 1.000 | 1.000 | 0.000 | 0.792 | 0 | 0 |
| validation | 40 | 0.992 | 1.000 | 0.000 | 0.691 | 0 | 0 |

## Case studies

### Clean incremental: `sc_002` / mutation `storage`

- Mutation: `storage` 'parquet' -> 'postgres'
- 3 of 9 nodes truly changed; incremental invalidated 3 (TP=3, FP=0, FN=0)
- Truly changed nodes: ['incremental', 'schema', 'storage']
- Invalidated nodes: ['incremental', 'schema', 'storage']
- IR=1.0, SDLR=0, RS=0.667

No stale leaks observed — none of the 120 mutation runs produced a node that
retained a pre-mutation value while the oracle had changed it.

## Failure modes (dependency extraction misses)

No missed edges across the corpus. Dependency extraction was sufficient at the axis granularity tested.

## Interpretation

**Safety is intact.** IR=1.0 across all 120 mutation runs with zero stale leaks. Every node the oracle changed was also marked invalid by the incremental walker. Of the 120 runs, 62 were trivially recall-perfect (only the root node changed in the oracle, so any incremental scheme would pass), but the remaining 58 runs had real downstream propagation and incremental still caught all of it. Incremental will never silently retain a pre-mutation value in this corpus.

**The IP shortfall is concentrated in `interface` mutations** (IP=0.45 vs ~1.0 for `storage` and `validation`). What's actually happening: the dependency graph correctly lists `output_format`, `testing_strategy`, and similar nodes as downstream of `interface_contract`. When `single_filter` flips to `composable_filters`, the incremental walker dutifully invalidates them. But the recompile LLM concludes that, e.g., `output_format = json` is fine under both filter contracts. So the edge IS semantically valid (it's plausible the answer would change); it just didn't *actually* change in this particular pair. This is conservative over-invalidation, not a bug. It costs LLM tokens, not correctness.

**Storage mutations are essentially perfect** (IP=1.00, IR=1.00, RS=0.79). Schema/partitioning/indexing decisions really do shift when storage changes, and the graph captures that cleanly.

**Validation mutations are nearly perfect** (IP=0.99, RS=0.69). One outlier in the corpus drops the mean — most validation flips ripple only into error_handling, exactly as predicted.

## Note on LLM nondeterminism

Temperature is 0 and seed is fixed (11 for compile, 7 for graph extraction), but gpt-4o-mini still produces small format jitter. Stale-leak measurement is the metric most sensitive to that jitter: a node that should compile to the same value under both oracle and incremental can differ by whitespace/casing, masquerading as a real change. We dedupe via exact string match. If you observe SDLR > 0, sample the stale node and verify it isn't pure cosmetic drift.

## Cost

- Graph extraction: ~$0.017 (see `artifacts/a3_graph_summary.json`)
- Recompile + mutations: ~$0.006 (see `artifacts/a3_recompile_summary.json`)
- **Total: $0.0231** (well under the $10 budget)
