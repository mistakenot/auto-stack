"""Phase-4 variant bench — the run that pays off the setup.

Scores every variant in `variants.VARIANTS` against the frozen golden qrels and
emits the full metric panel, then runs the **pre-registered** significance test.

Pre-registration (fixed BEFORE looking at variant results; see DIARY.md):
  * Primary metric  : nDCG@10 — graded (qrels are 1–3), captures both whether
                      relevant rules surface AND how high they rank; recall alone
                      is already saturated (~0.9), so it can't discriminate.
  * Primary slice   : the clean 64 (overlaps_mined_task == none). Flagged-36 and
                      all-100 are reported as sensitivity only.
  * Primary condition: `guess` — the realistic case (agent supplies its mined
                      domain_guess). `none`/`wrong` report robustness.
  * Primary contrast: each variant vs `hard-gate` (the shipped baseline).
  * Inference       : Wilcoxon signed-rank on session-mean deltas (clustered by
                      source_session) + session-cluster bootstrap 95% CI + Holm
                      correction across the variant family.

Metrics for queries with ≥1 relevant rule only (recall/nDCG undefined otherwise);
surfaced-count and precision@k are reported over all queries.

Usage:
  PYTHONPATH=src python -m retrieval_eval.evaluate [--write] [--slice clean|all]
"""
from __future__ import annotations

import json
import statistics
import sys
from collections import Counter
from dataclasses import dataclass
from pathlib import Path

from . import metrics
from .conditions import _wrong_domain
from .corpus import load_rules
from .stats import Paired, cluster_bootstrap_ci, clustered_wilcoxon, holm
from .variants import BASELINE, VARIANTS

HERE = Path(__file__).resolve()
PROJECT_ROOT = HERE.parents[2]
DATA = PROJECT_ROOT / "data"
RESULTS = DATA / "results"

PRIMARY_METRIC = "ndcg@10"
PRIMARY_CONDITION = "guess"
CONDITIONS = ("guess", "none", "wrong")
KS = (5, 10)


@dataclass
class Query:
    query_id: str
    intent: str
    cluster: str           # source_session
    clean: bool            # overlaps_mined_task == none
    rel: dict[str, int]    # rule_id -> grade
    dom: dict[str, list]   # condition -> domain_filter

    @property
    def has_rel(self) -> bool:
        return any(self.rel.values())


def _load() -> list[Query]:
    queries = {
        json.loads(l)["query_id"]: json.loads(l)
        for l in (DATA / "queries" / "queries.jsonl").read_text().splitlines()
        if l.strip()
    }
    qrels = [json.loads(l) for l in (DATA / "qrels" / "qrels.jsonl").read_text().splitlines() if l.strip()]
    rules = load_rules()
    by_id = {r.id: r for r in rules}
    vocab_by_freq = [d for d, _ in Counter(d for r in rules for d in r.domain).most_common()]

    out: list[Query] = []
    for q in qrels:
        qid = q["query_id"]
        meta = queries.get(qid, {})
        rel = {x["rule_id"]: x["grade"] for x in q.get("relevant", []) if x["rule_id"] in by_id}
        rel_domains = {d for rid in rel for d in by_id[rid].domain}
        guess = q.get("domain_guess") or []
        flag = q.get("overlaps_mined_task")
        out.append(
            Query(
                query_id=qid,
                intent=q["intent"],
                cluster=meta.get("source_session", qid),
                clean=flag in (None, "none", ""),
                rel=rel,
                dom={"guess": guess, "none": [], "wrong": _wrong_domain(rel_domains, vocab_by_freq)},
            )
        )
    return out


def _panel(ranking: list[str], rel: dict[str, int]) -> dict:
    return {
        "surfaced": len(ranking),
        "recall": metrics.recall_at_k(ranking, rel, 10**9),
        "recall@5": metrics.recall_at_k(ranking, rel, 5),
        "recall@10": metrics.recall_at_k(ranking, rel, 10),
        "precision@5": metrics.precision_at_k(ranking, rel, 5),
        "precision@10": metrics.precision_at_k(ranking, rel, 10),
        "ndcg@5": metrics.ndcg_at_k(ranking, rel, 5),
        "ndcg@10": metrics.ndcg_at_k(ranking, rel, 10),
        "mrr": metrics.mrr(ranking, rel),
        "excluded_relevant_rate": metrics.excluded_relevant_rate(ranking, rel),
    }


def _is_num(x) -> bool:
    return isinstance(x, (int, float)) and x == x  # excludes NaN


def _mean(vals: list[float]) -> float | None:
    vals = [v for v in vals if _is_num(v)]
    return round(statistics.mean(vals), 4) if vals else None


def evaluate(queries: list[Query], *, primary_slice_clean: bool = True) -> dict:
    rules = load_rules()

    # per (variant, condition, query) panels.
    raw: dict[str, dict[str, dict[str, dict]]] = {}
    for vname, variant in VARIANTS.items():
        raw[vname] = {}
        for cond in CONDITIONS:
            per_q = {}
            for q in queries:
                ranking = variant.run(rules, q.intent, q.dom[cond])
                per_q[q.query_id] = _panel(ranking, q.rel)
            raw[vname][cond] = per_q

    qmap = {q.query_id: q for q in queries}

    def aggregate(slice_clean: bool | None) -> dict:
        """slice_clean True=clean64, False=flagged36, None=all100."""
        def in_slice(q: Query) -> bool:
            if slice_clean is None:
                return True
            return q.clean == slice_clean

        agg: dict[str, dict[str, dict]] = {}
        for vname in VARIANTS:
            agg[vname] = {}
            for cond in CONDITIONS:
                rows = [(qid, p) for qid, p in raw[vname][cond].items() if in_slice(qmap[qid])]
                rel_rows = [(qid, p) for qid, p in rows if qmap[qid].has_rel]
                agg[vname][cond] = {
                    "n": len(rows),
                    "n_with_rel": len(rel_rows),
                    "mean_surfaced": _mean([p["surfaced"] for _, p in rows]),
                    "mean_precision@5": _mean([p["precision@5"] for _, p in rows]),
                    "mean_recall": _mean([p["recall"] for _, p in rel_rows]),
                    "mean_recall@10": _mean([p["recall@10"] for _, p in rel_rows]),
                    "mean_ndcg@10": _mean([p["ndcg@10"] for _, p in rel_rows]),
                    "mean_ndcg@5": _mean([p["ndcg@5"] for _, p in rel_rows]),
                    "mean_mrr": _mean([p["mrr"] for _, p in rel_rows]),
                    "mean_excluded_relevant_rate": _mean([p["excluded_relevant_rate"] for _, p in rel_rows]),
                }
        return agg

    summary = {
        "clean64": aggregate(True),
        "flagged36": aggregate(False),
        "all100": aggregate(None),
    }

    # ---- inference: primary metric, vs baseline, clustered by session ----
    def stats_for(subset: list[Query], condition: str) -> dict:
        base_cond = raw[BASELINE][condition]
        pvals: dict[str, float | None] = {}
        block: dict[str, dict] = {}
        for vname in VARIANTS:
            if vname == BASELINE:
                continue
            var_cond = raw[vname][condition]
            pairs = [
                Paired(
                    cluster=q.cluster,
                    base=base_cond[q.query_id][PRIMARY_METRIC],
                    var=var_cond[q.query_id][PRIMARY_METRIC],
                )
                for q in subset
                if _is_num(base_cond[q.query_id][PRIMARY_METRIC]) and _is_num(var_cond[q.query_id][PRIMARY_METRIC])
            ]
            wil = clustered_wilcoxon(pairs)
            point, lo, hi = cluster_bootstrap_ci(pairs)
            block[vname] = {
                "n_queries": len(pairs),
                "wilcoxon": wil,
                "mean_delta": round(point, 4),
                "ci95": [round(lo, 4), round(hi, 4)],
            }
            pvals[vname] = wil["p_value"]
        for vname, a in holm(pvals).items():
            block[vname]["holm"] = a
        return block

    # Pre-registered primary: clean 64, guess condition. Sensitivity: all 100, and
    # the wrong-guess condition (the gate's catastrophe axis — decisive for the
    # filter recommendation, even though robustness wasn't the pre-reg primary).
    primary_qs = [q for q in queries if q.clean and q.has_rel]
    all_qs = [q for q in queries if q.has_rel]
    stats_block = stats_for(primary_qs, PRIMARY_CONDITION)

    return {
        "preregistration": {
            "primary_metric": PRIMARY_METRIC,
            "primary_condition": PRIMARY_CONDITION,
            "primary_slice": "clean64",
            "baseline": BASELINE,
            "inference": "clustered Wilcoxon on session-mean deltas + cluster bootstrap CI + Holm",
            "n_primary_queries": len(primary_qs),
            "n_clusters": len({q.cluster for q in primary_qs}),
        },
        "variants": {v.name: v.desc for v in VARIANTS.values()},
        "summary": summary,
        "primary_stats": stats_block,
        "sensitivity_stats": {
            "all100_guess": stats_for(all_qs, "guess"),
            "clean64_wrong": stats_for(primary_qs, "wrong"),
        },
    }


def _fmt(x, w=6):
    if x is None or (isinstance(x, float) and x != x):
        return f"{'-':>{w}}"
    return f"{x:>{w}.3f}" if isinstance(x, float) else f"{x:>{w}}"


def _print(result: dict) -> None:
    pre = result["preregistration"]
    print(f"\n=== Phase-4 variant bench (primary: {pre['primary_metric']} | {pre['primary_condition']} | "
          f"{pre['primary_slice']}; {pre['n_primary_queries']}q / {pre['n_clusters']} sessions) ===\n")
    clean = result["summary"]["clean64"]
    for cond in CONDITIONS:
        print(f"--- clean64 / condition={cond} ---")
        print(f"  {'variant':<14} {'ndcg@10':>8} {'ndcg@5':>7} {'p@5':>6} {'rec@10':>7} {'recall':>7} "
              f"{'surf':>6} {'excl':>6}")
        for vname in VARIANTS:
            c = clean[vname][cond]
            print(f"  {vname:<14} {_fmt(c['mean_ndcg@10'],8)} {_fmt(c['mean_ndcg@5'],7)} "
                  f"{_fmt(c['mean_precision@5'])} {_fmt(c['mean_recall@10'],7)} {_fmt(c['mean_recall'],7)} "
                  f"{_fmt(c['mean_surfaced'])} {_fmt(c['mean_excluded_relevant_rate'])}")
        print()
    def _stats_table(title, block):
        print(f"--- {title} ---")
        print(f"  {'variant':<14} {'Δmean':>8} {'ci95':>20} {'p':>8} {'p_holm':>8} {'rank_bis':>9} {'sig':>4}")
        for vname, s in block.items():
            ci = s["ci95"]
            wil = s["wilcoxon"]
            print(f"  {vname:<14} {_fmt(s['mean_delta'],8)} "
                  f"[{_fmt(ci[0],7)},{_fmt(ci[1],7)}]  {_fmt(wil['p_value'],8)} "
                  f"{_fmt(s['holm']['p_adj'],8)} {_fmt(wil['rank_biserial'],9)} "
                  f"{'✓' if s['holm']['reject'] else '·':>4}")
        print()

    _stats_table(f"PRIMARY (pre-reg): Δ{pre['primary_metric']} vs {pre['baseline']} "
                 f"(clean64 / {pre['primary_condition']}, clustered)", result["primary_stats"])
    _stats_table(f"sensitivity: Δ{pre['primary_metric']} vs {pre['baseline']} (all100 / guess)",
                 result["sensitivity_stats"]["all100_guess"])
    _stats_table(f"robustness: Δ{pre['primary_metric']} vs {pre['baseline']} (clean64 / WRONG guess)",
                 result["sensitivity_stats"]["clean64_wrong"])


def main(argv: list[str]) -> int:
    queries = _load()
    result = evaluate(queries)
    _print(result)
    if "--write" in argv:
        RESULTS.mkdir(parents=True, exist_ok=True)
        out = RESULTS / "phase4.json"
        out.write_text(json.dumps(result, indent=2))
        print(f"wrote {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
