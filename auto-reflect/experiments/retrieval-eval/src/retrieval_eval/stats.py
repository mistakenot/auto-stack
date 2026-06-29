"""Significance machinery for the variant bench — clustered by source session.

Queries from one session are not independent (2–3 share a transcript), so naive
per-query paired tests overstate confidence. Both procedures here respect the
cluster:

  * `cluster_bootstrap_ci` resamples whole SESSIONS with replacement (not
    queries), so the CI reflects between-session variability.
  * `clustered_wilcoxon` aggregates each session to its mean paired delta, then
    runs the Wilcoxon signed-rank test across those session means — one
    observation per cluster, which is the conservative, standard fix.

`holm` corrects the family of variant-vs-baseline comparisons for the primary
metric. numpy/scipy come from the `analysis` extra.
"""
from __future__ import annotations

from collections import defaultdict
from dataclasses import dataclass
from typing import Sequence

import numpy as np
from scipy import stats as sps


@dataclass
class Paired:
    """A baseline/variant metric pair for one query, tagged with its cluster."""

    cluster: str
    base: float
    var: float

    @property
    def delta(self) -> float:
        return self.var - self.base


def _session_mean_deltas(pairs: Sequence[Paired]) -> list[float]:
    by: dict[str, list[float]] = defaultdict(list)
    for p in pairs:
        by[p.cluster].append(p.delta)
    return [float(np.mean(v)) for v in by.values()]


def cluster_bootstrap_ci(
    pairs: Sequence[Paired], *, iters: int = 10_000, alpha: float = 0.05, seed: int = 12345
) -> tuple[float, float, float]:
    """Percentile bootstrap CI for the mean per-query delta, resampling SESSIONS.

    Returns (mean_delta, lo, hi). Deterministic given seed (reproducibility).
    """
    by: dict[str, list[float]] = defaultdict(list)
    for p in pairs:
        by[p.cluster].append(p.delta)
    clusters = list(by.values())
    if not clusters:
        return (float("nan"), float("nan"), float("nan"))
    point = float(np.mean([d for c in clusters for d in c]))
    rng = np.random.default_rng(seed)
    k = len(clusters)
    means = np.empty(iters)
    for i in range(iters):
        pick = rng.integers(0, k, size=k)
        vals = [d for j in pick for d in clusters[j]]
        means[i] = np.mean(vals)
    lo, hi = np.percentile(means, [100 * alpha / 2, 100 * (1 - alpha / 2)])
    return (point, float(lo), float(hi))


def clustered_wilcoxon(pairs: Sequence[Paired]) -> dict:
    """Wilcoxon signed-rank on session-mean deltas (one obs per cluster).

    Returns statistic, p-value, the matched-pairs rank-biserial effect size, the
    cluster count, and the median session delta. p is None when every session
    delta is 0 (no signal) or there are too few non-zero clusters.
    """
    deltas = np.array(_session_mean_deltas(pairs), dtype=float)
    n_clusters = int(deltas.size)
    nonzero = deltas[deltas != 0]
    out = {
        "n_clusters": n_clusters,
        "n_nonzero_clusters": int(nonzero.size),
        "median_session_delta": float(np.median(deltas)) if n_clusters else float("nan"),
        "mean_session_delta": float(np.mean(deltas)) if n_clusters else float("nan"),
        "statistic": None,
        "p_value": None,
        "rank_biserial": None,
    }
    if nonzero.size < 1:
        return out
    res = sps.wilcoxon(nonzero, zero_method="wilcox", alternative="two-sided", correction=False)
    out["statistic"] = float(res.statistic)
    out["p_value"] = float(res.pvalue)
    # Matched-pairs rank-biserial: r = (sum positive ranks - sum negative ranks)
    # / total rank sum. Range [-1, 1]; sign follows the variant's improvement.
    ranks = sps.rankdata(np.abs(nonzero))
    pos = ranks[nonzero > 0].sum()
    neg = ranks[nonzero < 0].sum()
    total = ranks.sum()
    out["rank_biserial"] = float((pos - neg) / total) if total else 0.0
    return out


def holm(pvalues: dict[str, float | None], alpha: float = 0.05) -> dict[str, dict]:
    """Holm–Bonferroni step-down over a family of comparisons. None p-values are
    passed through (not part of the family). Returns per-key {p, p_adj, reject}.
    """
    items = [(k, p) for k, p in pvalues.items() if p is not None]
    out: dict[str, dict] = {k: {"p": None, "p_adj": None, "reject": None} for k, p in pvalues.items() if p is None}
    m = len(items)
    for rank, (k, p) in enumerate(sorted(items, key=lambda kv: kv[1])):
        p_adj = min(1.0, (m - rank) * p)
        # Enforce monotonic non-decreasing adjusted p across the step-down.
        prev = [out[kk]["p_adj"] for kk in out if out[kk].get("p_adj") is not None]
        if prev:
            p_adj = max(p_adj, max(prev))
        out[k] = {"p": float(p), "p_adj": float(p_adj), "reject": bool(p_adj < alpha)}
    return out
