"""Coverage + effect-preview analysis for the oracle pilot.

Answers the go/no-go question from the honest-rigor discussion:
  1. Label density — do queries actually have relevant rules?
  2. Effect preview — does the domain-gate matcher exclude oracle-relevant rules
     that a no-domain pass would have surfaced? (the failure mode under test)

Usage: python -m retrieval_eval.analyze_pilot <oracle_result.json> [--write-qrels]
"""
from __future__ import annotations

import json
import statistics
import sys
from pathlib import Path

from .baseline import match_rules, ordered_ids
from .corpus import load_rules

HERE = Path(__file__).resolve()
PROJECT_ROOT = HERE.parents[2]
QRELS_DIR = PROJECT_ROOT / "data" / "qrels"


def main(argv: list[str]) -> int:
    src = argv[0]
    write_qrels = "--write-qrels" in argv
    data = json.loads(Path(src).read_text())
    qrels = data.get("qrels", data) if isinstance(data, dict) else data

    rules = load_rules()
    by_id = {r.id: r for r in rules}

    per_query = []
    for row in qrels:
        rel = {x["rule_id"]: x["grade"] for x in row.get("relevant", []) if x["rule_id"] in by_id}
        rel1 = {rid for rid, g in rel.items() if g >= 1}
        rel2 = {rid for rid, g in rel.items() if g >= 2}
        intent = row["intent"]
        dom = row.get("domain_guess") or []

        surf_dom = set(ordered_ids(match_rules(rules, intent, domain_filter=dom)))
        surf_nodom = set(ordered_ids(match_rules(rules, intent, domain_filter=[])))

        def recall(relset, surf):
            return (len(relset & surf) / len(relset)) if relset else None

        excluded_by_domain = (rel1 & surf_nodom) - surf_dom  # relevant, surfaced w/o domain, dropped by domain gate
        per_query.append({
            "query_id": row["query_id"],
            "intent": intent[:70],
            "domain_guess": dom,
            "n_rel1": len(rel1), "n_rel2": len(rel2),
            "recall_dom": recall(rel1, surf_dom),
            "recall_nodom": recall(rel1, surf_nodom),
            "excluded_by_domain": sorted(excluded_by_domain),
        })

    n = len(per_query)
    with_rel1 = sum(1 for q in per_query if q["n_rel1"] > 0)
    with_rel2 = sum(1 for q in per_query if q["n_rel2"] > 0)
    rel_counts = [q["n_rel1"] for q in per_query]
    # recall stats over queries that HAVE a relevant rule
    rec_dom = [q["recall_dom"] for q in per_query if q["recall_dom"] is not None]
    rec_nodom = [q["recall_nodom"] for q in per_query if q["recall_nodom"] is not None]
    dom_excl_queries = [q for q in per_query if q["excluded_by_domain"]]
    total_excluded = sum(len(q["excluded_by_domain"]) for q in per_query)

    def mean(xs):
        return round(statistics.mean(xs), 3) if xs else None

    summary = {
        "queries": n,
        "label_density": {
            "with_relevant_grade1+": with_rel1,
            "with_relevant_grade2+": with_rel2,
            "pct_with_relevant": round(100 * with_rel1 / n, 1) if n else 0,
            "mean_relevant_per_query": mean(rel_counts),
            "median_relevant_per_query": statistics.median(rel_counts) if rel_counts else 0,
            "max_relevant": max(rel_counts) if rel_counts else 0,
        },
        "effect_preview_domain_gate": {
            "mean_recall_with_domain_filter": mean(rec_dom),
            "mean_recall_no_filter": mean(rec_nodom),
            "queries_where_domain_gate_excluded_a_relevant_rule": len(dom_excl_queries),
            "total_relevant_rules_excluded_by_domain_gate": total_excluded,
        },
    }
    print(json.dumps(summary, indent=2))
    print("\n--- queries where the domain gate dropped a relevant rule ---")
    for q in dom_excl_queries:
        print(f"  {q['query_id']} dom={q['domain_guess']} recall {q['recall_nodom']}->{q['recall_dom']}  "
              f"excluded={len(q['excluded_by_domain'])}  | {q['intent']}")

    if write_qrels:
        QRELS_DIR.mkdir(parents=True, exist_ok=True)
        out = QRELS_DIR / "pilot.qrels.jsonl"
        with out.open("w") as f:
            for row in qrels:
                f.write(json.dumps(row) + "\n")
        (QRELS_DIR / "pilot.analysis.json").write_text(json.dumps(
            {"summary": summary, "per_query": per_query}, indent=2))
        print(f"\nwrote {out} and pilot.analysis.json")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
