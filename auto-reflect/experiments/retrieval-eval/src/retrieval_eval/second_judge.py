"""Second-judge analysis — inter-rater reliability + decision robustness.

Consumes data/results/judge_raw/{codex,grok}.jsonl (from judge.py) and the frozen
J1 qrels, computes Cohen's κ across all judge pairs, and re-runs the Phase-4 metric
panel under each judge to test whether the variant verdict survives a judge swap.

See second-judge/DESIGN.md for the pre-registered go/no-go.

Usage: PYTHONPATH=src python -m retrieval_eval.second_judge [--write]
"""
from __future__ import annotations

import json
import statistics
import sys
from pathlib import Path

from scipy import stats as sps

from . import metrics
from .corpus import load_rules
from .stats import cohen_kappa, landis_koch
from .variants import BASELINE, VARIANTS

HERE = Path(__file__).resolve()
PROJECT_ROOT = HERE.parents[2]
DATA = PROJECT_ROOT / "data"
RAW = DATA / "results" / "judge_raw"
GUESS_K = (5, 10)


def _load_judge(model: str) -> dict[str, dict]:
    """query_id -> {pool, grades{rid:grade}} for successfully graded queries."""
    path = RAW / f"{model}.jsonl"
    out = {}
    if path.exists():
        for l in path.read_text().splitlines():
            if not l.strip():
                continue
            r = json.loads(l)
            if r.get("grades") is not None:
                out[r["query_id"]] = {"pool": r["pool"], "grades": r["grades"]}
    return out


def _j1_on_pool(qrels_by_id: dict, qid: str, pool: list[str]) -> dict[str, int]:
    rel = {x["rule_id"]: x["grade"] for x in qrels_by_id[qid].get("relevant", [])}
    return {rid: rel.get(rid, 0) for rid in pool}


def main(argv: list[str]) -> int:
    write = "--write" in argv
    rules = load_rules()
    queries = {json.loads(l)["query_id"]: json.loads(l)
               for l in (DATA / "queries" / "queries.jsonl").read_text().splitlines() if l.strip()}
    qrels_by_id = {json.loads(l)["query_id"]: json.loads(l)
                   for l in (DATA / "qrels" / "qrels.jsonl").read_text().splitlines() if l.strip()}

    codex = _load_judge("codex")
    grok = _load_judge("grok")
    common = sorted(set(codex) & set(grok))
    print(f"queries graded by both second judges: {len(common)} "
          f"(codex {len(codex)}, grok {len(grok)})")
    if not common:
        print("no common queries yet — judges still running?")
        return 1

    # Per-query pool is identical across judges (deterministic builder); take codex's.
    # Build aligned label vectors per judge over the union of (query,rule) items.
    j1v, j2v, j3v, csv = [], [], [], []   # cs = consensus (median)
    per_query = {}
    for qid in common:
        pool = codex[qid]["pool"]
        g1 = _j1_on_pool(qrels_by_id, qid, pool)
        g2 = codex[qid]["grades"]
        g3 = grok[qid]["grades"]
        for rid in pool:
            a, b, c = g1[rid], int(g2.get(rid, 0)), int(g3.get(rid, 0))
            j1v.append(a); j2v.append(b); j3v.append(c)
            csv.append(int(statistics.median([a, b, c])))
        per_query[qid] = {"pool": pool, "g": {"j1": g1, "codex": g2, "grok": g3}}

    def kpair(x, y):
        return {
            "binary_kappa": round(cohen_kappa([int(v >= 1) for v in x], [int(v >= 1) for v in y]), 3),
            "weighted_kappa": round(cohen_kappa(x, y, weights="quadratic", labels=[0, 1, 2, 3]), 3),
            "pct_exact": round(sum(1 for a, b in zip(x, y) if a == b) / len(x), 3),
        }

    agreement = {
        "n_judgments": len(j1v),
        "J1-codex": kpair(j1v, j2v),
        "J1-grok": kpair(j1v, j3v),
        "codex-grok": kpair(j2v, j3v),
        "mean_grade": {"j1": round(statistics.mean(j1v), 3), "codex": round(statistics.mean(j2v), 3),
                       "grok": round(statistics.mean(j3v), 3)},
        "relevant_rate": {"j1": round(sum(v >= 1 for v in j1v) / len(j1v), 3),
                          "codex": round(sum(v >= 1 for v in j2v) / len(j2v), 3),
                          "grok": round(sum(v >= 1 for v in j3v) / len(j3v), 3)},
    }
    for k in ("binary_kappa", "weighted_kappa"):
        agreement.setdefault("interpretation", {})[k] = {
            "J1-codex": landis_koch(agreement["J1-codex"][k]),
            "J1-grok": landis_koch(agreement["J1-grok"][k]),
            "codex-grok": landis_koch(agreement["codex-grok"][k]),
        }

    # ---- decision robustness: re-run the panel under each judge ----
    def panel_under(judge_grades: dict[str, dict[str, int]]) -> dict[str, dict]:
        """judge_grades: qid -> {rid: grade} over that query's pool."""
        res = {}
        for vname, variant in VARIANTS.items():
            nd, p5, r10, rec = [], [], [], []
            for qid in common:
                rel = judge_grades[qid]
                if not any(g >= 1 for g in rel.values()):
                    continue
                ranking = variant.run(rules, queries[qid]["intent"], queries[qid].get("domain_guess") or [])
                nd.append(metrics.ndcg_at_k(ranking, rel, 10))
                p5.append(metrics.precision_at_k(ranking, rel, 5))
                r10.append(metrics.recall_at_k(ranking, rel, 10))
                rec.append(metrics.recall_at_k(ranking, rel, 10**9))
            def m(v):
                v = [x for x in v if x == x]
                return round(statistics.mean(v), 4) if v else None
            res[vname] = {"n": len(nd), "ndcg@10": m(nd), "precision@5": m(p5),
                          "recall@10": m(r10), "recall": m(rec)}
        return res

    grades_by_judge = {
        "j1": {qid: per_query[qid]["g"]["j1"] for qid in common},
        "codex": {qid: per_query[qid]["g"]["codex"] for qid in common},
        "grok": {qid: per_query[qid]["g"]["grok"] for qid in common},
        "consensus": {qid: {rid: int(statistics.median([per_query[qid]["g"]["j1"][rid],
                                                        per_query[qid]["g"]["codex"].get(rid, 0),
                                                        per_query[qid]["g"]["grok"].get(rid, 0)]))
                            for rid in per_query[qid]["pool"]} for qid in common},
    }
    panels = {j: panel_under(g) for j, g in grades_by_judge.items()}

    # ordering stability: Spearman of variant nDCG@10 vs J1; sign of key deltas.
    order = list(VARIANTS)
    j1_nd = [panels["j1"][v]["ndcg@10"] for v in order]
    robustness = {"variant_order": order, "ndcg@10_by_judge": {j: [panels[j][v]["ndcg@10"] for v in order] for j in panels}}
    for j in panels:
        if j == "j1":
            continue
        rho = sps.spearmanr(j1_nd, [panels[j][v]["ndcg@10"] for v in order]).correlation
        robustness.setdefault("spearman_vs_j1", {})[j] = round(float(rho), 3)

    def deltas(j):
        base = panels[j][BASELINE]["ndcg@10"]
        return {v: round(panels[j][v]["ndcg@10"] - base, 4) for v in order if v != BASELINE}
    robustness["key_deltas_vs_hard_gate"] = {j: deltas(j) for j in panels}
    # sign agreement of each variant's delta-sign across judges
    sign = lambda x: 0 if abs(x) < 0.005 else (1 if x > 0 else -1)
    robustness["delta_sign_consistent"] = {
        v: len({sign(robustness["key_deltas_vs_hard_gate"][j][v]) for j in panels}) == 1
        for v in order if v != BASELINE
    }

    result = {"agreement": agreement, "robustness": robustness, "panels": panels,
              "n_common_queries": len(common)}
    _print(result)
    if write:
        out = DATA / "results" / "second_judge.json"
        out.write_text(json.dumps(result, indent=2))
        print(f"\nwrote {out}")
    return 0


def _f(x, w=7):
    return f"{'-':>{w}}" if x is None else f"{x:>{w}.3f}"


def _print(r: dict) -> None:
    a = r["agreement"]
    print(f"\n=== AGREEMENT ({a['n_judgments']} judgments over {r['n_common_queries']} queries) ===")
    print(f"  {'pair':<12} {'binary κ':>9} {'weighted κ':>11} {'%exact':>7}  interpretation(binary)")
    for pair in ("J1-codex", "J1-grok", "codex-grok"):
        print(f"  {pair:<12} {a[pair]['binary_kappa']:>9} {a[pair]['weighted_kappa']:>11} "
              f"{a[pair]['pct_exact']:>7}  {a['interpretation']['binary_kappa'][pair]}")
    print(f"  mean grade: {a['mean_grade']}   relevant-rate: {a['relevant_rate']}")
    rb = r["robustness"]
    print("\n=== DECISION ROBUSTNESS: nDCG@10 by judge ===")
    print(f"  {'variant':<14} " + " ".join(f"{j:>9}" for j in rb['ndcg@10_by_judge']))
    for i, v in enumerate(rb["variant_order"]):
        print(f"  {v:<14} " + " ".join(_f(rb['ndcg@10_by_judge'][j][i], 9) for j in rb['ndcg@10_by_judge']))
    print(f"\n  Spearman(nDCG@10 ordering) vs J1: {rb['spearman_vs_j1']}")
    print(f"  key Δ vs hard-gate sign-consistent across judges: {rb['delta_sign_consistent']}")
    print(f"  Δ detail: {json.dumps(rb['key_deltas_vs_hard_gate'], indent=0)}")


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
