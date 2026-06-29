"""Phase-4 retrieval variants: a factored filter×scorer bench.

Each variant is a function `(rules, intent, domain_filter) -> list[rule_id]` —
the surfaced ranking. They share one scaffold (`rank`) so the only thing that
changes between variants is a **filter strategy** (what the agent's `domain_guess`
does) and a **scorer** (how lexical relevance is computed). That isolation is the
whole point: a result is attributable to the axis it varied.

The two domain signals the experiment keeps distinct (see DIARY.md):
  A) a rule's `domain` tags matching the QUERY KEYWORDS — a *scorer* feature
     (baseline: +1 per matched tag), tunable via IDF (`idf-tag` down-weights `go`).
  B) a rule's `domain` tags intersecting the agent's DOMAIN_FILTER guess — the
     *filter* axis (baseline: a hard exclusion gate), the thing under trial.

`hard-gate` is the shipped baseline and MUST reproduce `baseline.match_rules`
ranking exactly (pinned by `tests/test_variant_conformance.py`). Every other
variant changes exactly one axis off that baseline.

Stdlib-only. Scores are kept in *raw* (un-normalized) units: normalization is a
per-query monotonic transform, so it never changes within-query ranking, and all
metrics (recall/precision/nDCG@k, surfaced-count) depend only on the order and the
surfaced set.
"""
from __future__ import annotations

import math
import re
from collections import Counter
from dataclasses import dataclass
from typing import Callable, Sequence

from .baseline import (
    RULE_TYPE_HARD,
    Rule,
    _domains_intersect,
    _normalize_domain_filter,
    _score_rule,
    _surfaceable,
    normalize_keywords,
)

_TOKEN_RE = re.compile(r"[a-z0-9]+")


def _tokens(text: str) -> list[str]:
    return _TOKEN_RE.findall(text.lower())


# --------------------------------------------------------------------------
# Corpus statistics (IDF) — built once per rule set, cached on identity.
# --------------------------------------------------------------------------
@dataclass
class CorpusStats:
    """IDF tables derived from the rule set, for the IDF-based variants."""

    n: int
    tag_idf: dict[str, float]            # over rule.domain tag vocabulary
    term_df: dict[str, int]              # document frequency over use_when token bags
    avgdl: float                         # mean use_when token-bag length
    doc_terms: dict[str, Counter]        # rule_id -> term frequencies in use_when

    def term_idf(self, term: str) -> float:
        df = self.term_df.get(term, 0)
        # BM25 idf, floored at 0 so ubiquitous terms never push scores negative.
        return max(0.0, math.log((self.n - df + 0.5) / (df + 0.5) + 1.0))


_STATS_CACHE: dict[int, CorpusStats] = {}


def corpus_stats(rules: Sequence[Rule]) -> CorpusStats:
    key = id(rules)
    cached = _STATS_CACHE.get(key)
    if cached is not None and cached.n == len(rules):
        return cached
    n = len(rules)
    tag_df: Counter = Counter(d.strip().lower() for r in rules for d in r.domain)
    # IDF over the tag vocabulary: log(N/df). `go` (df 94/120) -> ~0.24; a df-2
    # tag -> ~4.1. This is the lever that makes the near-useless `go` axis cheap.
    tag_idf = {t: math.log(n / df) for t, df in tag_df.items() if df > 0}
    doc_terms: dict[str, Counter] = {}
    term_df: Counter = Counter()
    total_len = 0
    for r in rules:
        tf = Counter(_tokens(r.use_when))
        doc_terms[r.id] = tf
        total_len += sum(tf.values())
        for term in tf:
            term_df[term] += 1
    avgdl = (total_len / n) if n else 0.0
    stats = CorpusStats(n=n, tag_idf=tag_idf, term_df=dict(term_df), avgdl=avgdl, doc_terms=doc_terms)
    _STATS_CACHE[key] = stats
    return stats


# --------------------------------------------------------------------------
# Scorers: (rule, keywords, stats) -> raw lexical score (>0 to surface).
# --------------------------------------------------------------------------
def scorer_lexical(rule: Rule, keywords: Sequence[str], stats: CorpusStats) -> float:
    """Baseline substring scorer: +3 per keyword in use_when, +1 per matched tag."""
    return _score_rule(rule, keywords)


def scorer_idf_tag(rule: Rule, keywords: Sequence[str], stats: CorpusStats) -> float:
    """Baseline use_when (+3 substring), but the rule's-own-tag↔keyword match is
    IDF-weighted instead of a flat +1 (down-weights a `go` tag match). Isolates
    IDF on the *scorer's* tag feature (signal A)."""
    use_when = rule.use_when.lower()
    raw = 0.0
    for kw in keywords:
        if kw in use_when:
            raw += 3.0
        for d in rule.domain:
            if kw in d.lower():
                raw += stats.tag_idf.get(d.strip().lower(), 0.0)
                break
    return raw


# BM25 saturation params — standard defaults, deliberately NOT tuned on this set
# (tuning on the eval queries would overfit; see DIARY Phase-4 note).
_BM25_K1 = 1.5
_BM25_B = 0.75


def scorer_bm25(rule: Rule, keywords: Sequence[str], stats: CorpusStats) -> float:
    """BM25 over use_when tokens, query = the intent's tokens. Rare terms drive
    the score; common terms saturate. Attacks finding #1 (the substring scorer
    surfaces most of the playbook because any keyword overlap counts +3)."""
    tf = stats.doc_terms.get(rule.id)
    if not tf:
        return 0.0
    dl = sum(tf.values())
    denom_norm = _BM25_K1 * (1.0 - _BM25_B + _BM25_B * (dl / stats.avgdl if stats.avgdl else 0.0))
    score = 0.0
    for term in set(keywords):  # keywords are the intent's deduped tokens
        f = tf.get(term, 0)
        if f == 0:
            continue
        score += stats.term_idf(term) * (f * (_BM25_K1 + 1.0)) / (f + denom_norm)
    return score


# --------------------------------------------------------------------------
# Filter / boost strategies for signal B (the domain_guess).
# --------------------------------------------------------------------------
def boost_flat(rule: Rule, filt: Sequence[str], stats: CorpusStats) -> float:
    """+3 (≈ one use_when keyword hit) if the rule is in-domain. A soft nudge,
    never an exclusion. The 'domain-as-boost' the data argues for."""
    return 3.0 if _domains_intersect(rule.domain, filt) else 0.0


def boost_idf_tag(rule: Rule, filt: Sequence[str], stats: CorpusStats) -> float:
    """Sum of tag-IDF over (rule.domain ∩ filt): a rare in-domain tag (e.g. `etl`)
    lifts a rule hard; a `go` match barely moves it. IDF tag weighting as a boost,
    never an exclusion."""
    want = {d.strip().lower() for d in filt}
    return sum(stats.tag_idf.get(d.strip().lower(), 0.0) for d in rule.domain if d.strip().lower() in want)


Scorer = Callable[[Rule, Sequence[str], CorpusStats], float]
Boost = Callable[[Rule, Sequence[str], CorpusStats], float]


def rank(
    rules: Sequence[Rule],
    intent: str,
    domain_filter: Sequence[str] | None,
    *,
    scorer: Scorer,
    gate: bool,
    boost: Boost | None,
    include_drafts: bool = True,
) -> list[str]:
    """Unified ranking scaffold. `gate=True` reproduces the baseline hard
    exclusion; `boost` adds a non-excluding domain signal. Hard-rule injection is
    applied UNIFORMLY (identical to baseline) across all variants so it is never a
    confound — only `gate`/`boost`/`scorer` vary.
    """
    keywords = normalize_keywords(intent)
    filt = _normalize_domain_filter(domain_filter or [])
    stats = corpus_stats(rules)

    candidates = [
        r
        for r in rules
        if _surfaceable(r.lifecycle, include_drafts)
        and (not gate or not filt or _domains_intersect(r.domain, filt))
    ]

    scored: dict[str, float] = {}
    for r in candidates:
        s = scorer(r, keywords, stats)
        if boost is not None and filt:
            s += boost(r, filt, stats)
        if s <= 0:
            continue
        scored[r.id] = s

    # Hard-rule injection — verbatim baseline semantics (injection_set = filt if
    # filt else keywords; exact tag membership; injected at score 0 if absent).
    injection_set = filt if filt else keywords
    for r in rules:
        if r.rule_type != RULE_TYPE_HARD:
            continue
        if not _surfaceable(r.lifecycle, include_drafts):
            continue
        if not _domains_intersect(r.domain, injection_set):
            continue
        scored.setdefault(r.id, 0.0)

    return [rid for rid, _ in sorted(scored.items(), key=lambda kv: (-kv[1], kv[0]))]


# --------------------------------------------------------------------------
# Named variants. Each changes exactly one axis off `hard-gate`.
# --------------------------------------------------------------------------
@dataclass(frozen=True)
class Variant:
    name: str
    desc: str
    scorer: Scorer
    gate: bool
    boost: Boost | None

    def run(self, rules: Sequence[Rule], intent: str, domain_filter: Sequence[str] | None) -> list[str]:
        return rank(rules, intent, domain_filter, scorer=self.scorer, gate=self.gate, boost=self.boost)


VARIANTS: dict[str, Variant] = {
    "hard-gate": Variant(
        "hard-gate", "baseline: lexical scorer + hard domain-exclusion gate",
        scorer_lexical, gate=True, boost=None,
    ),
    "no-filter": Variant(
        "no-filter", "lexical scorer, domain_guess ignored entirely",
        scorer_lexical, gate=False, boost=None,
    ),
    "domain-boost": Variant(
        "domain-boost", "lexical scorer + flat in-domain boost (never excludes)",
        scorer_lexical, gate=False, boost=boost_flat,
    ),
    "idf-tag": Variant(
        "idf-tag", "lexical scorer + IDF-weighted in-domain boost (down-weights `go`; never excludes)",
        scorer_lexical, gate=False, boost=boost_idf_tag,
    ),
    "bm25": Variant(
        "bm25", "BM25 scorer over use_when tokens, no domain filter (scorer variant)",
        scorer_bm25, gate=False, boost=None,
    ),
    "bm25+idf-tag": Variant(
        "bm25+idf-tag", "BM25 scorer + IDF-weighted in-domain boost (predicted best of both)",
        scorer_bm25, gate=False, boost=boost_idf_tag,
    ),
}

BASELINE = "hard-gate"

# SHIPPED names the variant the Go matcher (match.go) now ships. v1 was the
# `hard-gate` baseline; task 054 ported the validated `idf-tag` variant
# (non-excluding IDF-weighted in-domain boost). The Go↔Python conformance harness
# pins the Go CLI against VARIANTS[SHIPPED], while `hard-gate == baseline` stays a
# v1 self-check so the original system of record remains A/B-able.
SHIPPED = "idf-tag"
