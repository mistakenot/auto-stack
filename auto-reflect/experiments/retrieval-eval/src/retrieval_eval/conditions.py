"""Push the pilot further — for free — on the existing qrels.

Answers the two questions the coverage pilot reframed (see DIARY.md):
  1. PRECISION the domain gate buys: removing the gate raises recall, but does it
     flood the agent? Compare precision@k and surfaced-count, gate vs no-filter.
  2. The gate's HARM lives under BAD guesses, which the (competent) mined guesses
     hid. Re-run each query under three domain conditions and measure the recall
     cliff:
       - guess : the agent's mined domain_guess (what we already measured)
       - none  : no --domain (the empty-guess / recall-ceiling case)
       - wrong : a deterministic plausible-but-incorrect domain (a real playbook
                 tag carried by NONE of the query's relevant rules)

No new LLM calls — pure computation over qrels + baseline.py.

Usage: python -m retrieval_eval.conditions <qrels.jsonl> [--write]
"""
from __future__ import annotations

import json
import statistics
import sys
from collections import Counter
from pathlib import Path

from .baseline import match_rules, ordered_ids
from .corpus import load_rules
from . import metrics

HERE = Path(__file__).resolve()
PROJECT_ROOT = HERE.parents[2]
QRELS_DIR = PROJECT_ROOT / "data" / "qrels"
KS = (5, 10)


def _load_qrels(path: str) -> list[dict]:
    p = Path(path)
    if p.suffix == ".jsonl":
        return [json.loads(line) for line in p.read_text().splitlines() if line.strip()]
    data = json.loads(p.read_text())
    return data.get("qrels", data) if isinstance(data, dict) else data


def _wrong_domain(relevant_domains: set[str], vocab_by_freq: list[str]) -> list[str]:
    """A real playbook tag carried by none of the query's relevant rules
    (deterministic: most frequent such tag). Empty if every tag is 'relevant'."""
    for d in vocab_by_freq:
        if d not in relevant_domains:
            return [d]
    return []


def main(argv: list[str]) -> int:
    qrels = _load_qrels(argv[0])
    write = "--write" in argv
    rules = load_rules()
    by_id = {r.id: r for r in rules}
    vocab_by_freq = [d for d, _ in Counter(d for r in rules for d in r.domain).most_common()]

    conditions = ("guess", "none", "wrong")
    rows = []
    for q in qrels:
        intent = q["intent"]
        guess = q.get("domain_guess") or []
        rel = {x["rule_id"]: x["grade"] for x in q.get("relevant", []) if x["rule_id"] in by_id}
        rel_ids = {rid for rid in rel}
        rel_domains = {d for rid in rel_ids for d in by_id[rid].domain}
        wrong = _wrong_domain(rel_domains, vocab_by_freq)

        dom_for = {"guess": guess, "none": [], "wrong": wrong}
        row = {"query_id": q["query_id"], "n_rel": len(rel_ids), "guess": guess, "wrong": wrong, "by_cond": {}}
        for cond in conditions:
            ranking = ordered_ids(match_rules(rules, intent, domain_filter=dom_for[cond]))
            surfaced = set(ranking)
            rec = (len(rel_ids & surfaced) / len(rel_ids)) if rel_ids else None
            full_prec = (len(rel_ids & surfaced) / len(surfaced)) if surfaced else None
            row["by_cond"][cond] = {
                "surfaced": len(ranking),
                "recall": rec,
                "precision_full": full_prec,
                **{f"recall@{k}": metrics.recall_at_k(ranking, rel, k) if rel_ids else None for k in KS},
                **{f"precision@{k}": metrics.precision_at_k(ranking, rel, k) for k in KS},
            }
        rows.append(row)

    def agg(cond, field, *, only_with_rel=True):
        vals = [r["by_cond"][cond][field] for r in rows
                if (not only_with_rel or r["n_rel"] > 0) and r["by_cond"][cond][field] is not None]
        return round(statistics.mean(vals), 3) if vals else None

    summary = {"n_queries": len(rows), "n_with_relevant": sum(1 for r in rows if r["n_rel"] > 0), "by_condition": {}}
    for cond in conditions:
        summary["by_condition"][cond] = {
            "mean_recall": agg(cond, "recall"),
            "mean_recall@5": agg(cond, "recall@5"),
            "mean_recall@10": agg(cond, "recall@10"),
            "mean_precision_full": agg(cond, "precision_full", only_with_rel=False),
            "mean_precision@5": agg(cond, "precision@5", only_with_rel=False),
            "mean_surfaced": agg(cond, "surfaced", only_with_rel=False),
        }

    # The headline contrasts.
    g, n, w = (summary["by_condition"][c] for c in conditions)
    summary["headline"] = {
        "gate_precision_gain_full": _delta(g["mean_precision_full"], n["mean_precision_full"]),
        "gate_recall_cost_vs_nofilter": _delta(g["mean_recall"], n["mean_recall"]),
        "wrong_guess_recall_collapse_vs_nofilter": _delta(w["mean_recall"], n["mean_recall"]),
        "noise_reduction_surfaced": _delta(g["mean_surfaced"], n["mean_surfaced"]),
    }
    print(json.dumps(summary, indent=2))
    print("\n--- per-query recall by condition (queries with a relevant rule) ---")
    for r in rows:
        if r["n_rel"] == 0:
            continue
        bc = r["by_cond"]
        print(f"  {r['query_id']} nrel={r['n_rel']:2d}  guess={_f(bc['guess']['recall'])} "
              f"none={_f(bc['none']['recall'])} wrong={_f(bc['wrong']['recall'])}  "
              f"(wrong-dom={r['wrong']})")

    if write:
        QRELS_DIR.mkdir(parents=True, exist_ok=True)
        out = QRELS_DIR / "pilot.conditions.json"
        out.write_text(json.dumps({"summary": summary, "per_query": rows}, indent=2))
        print(f"\nwrote {out}")
    return 0


def _delta(a, b):
    if a is None or b is None:
        return None
    return round(a - b, 3)


def _f(x):
    return "  -  " if x is None else f"{x:.2f}"


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
