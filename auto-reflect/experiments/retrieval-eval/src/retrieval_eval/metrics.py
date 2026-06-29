"""IR metrics for comparing retrieval variants against oracle qrels.

qrels: dict[query_id, dict[rule_id, relevance]] where relevance is graded
(0 = irrelevant, higher = more relevant). A ranking is the ordered list of
rule ids a variant surfaced for that query.

Stdlib-only (no numpy) so the baseline/conformance layer has zero heavy deps;
statistical tests (Wilcoxon etc.) live in analysis.py once the oracle lands.
"""
from __future__ import annotations

import math
from typing import Mapping, Sequence


def recall_at_k(ranking: Sequence[str], rel: Mapping[str, int], k: int, *, threshold: int = 1) -> float:
    relevant = {rid for rid, g in rel.items() if g >= threshold}
    if not relevant:
        return float("nan")
    hits = sum(1 for rid in ranking[:k] if rid in relevant)
    return hits / len(relevant)


def precision_at_k(ranking: Sequence[str], rel: Mapping[str, int], k: int, *, threshold: int = 1) -> float:
    if k <= 0:
        return float("nan")
    relevant = {rid for rid, g in rel.items() if g >= threshold}
    hits = sum(1 for rid in ranking[:k] if rid in relevant)
    return hits / k


def mrr(ranking: Sequence[str], rel: Mapping[str, int], *, threshold: int = 1) -> float:
    for i, rid in enumerate(ranking, start=1):
        if rel.get(rid, 0) >= threshold:
            return 1.0 / i
    return 0.0


def dcg_at_k(ranking: Sequence[str], rel: Mapping[str, int], k: int) -> float:
    return sum(
        (2 ** rel.get(rid, 0) - 1) / math.log2(i + 1)
        for i, rid in enumerate(ranking[:k], start=1)
    )


def ndcg_at_k(ranking: Sequence[str], rel: Mapping[str, int], k: int) -> float:
    ideal = sorted(rel.values(), reverse=True)
    idcg = sum((2 ** g - 1) / math.log2(i + 1) for i, g in enumerate(ideal[:k], start=1))
    if idcg == 0:
        return float("nan")
    return dcg_at_k(ranking, rel, k) / idcg


def excluded_relevant_rate(ranking: Sequence[str], rel: Mapping[str, int], *, threshold: int = 1) -> float:
    """Fraction of relevant rules that the variant did NOT surface at all.

    Directly measures the failure mode driving this experiment: good rules
    silently excluded (by the domain gate or the keyword-score>0 threshold).
    """
    relevant = {rid for rid, g in rel.items() if g >= threshold}
    if not relevant:
        return float("nan")
    surfaced = set(ranking)
    missed = sum(1 for rid in relevant if rid not in surfaced)
    return missed / len(relevant)
