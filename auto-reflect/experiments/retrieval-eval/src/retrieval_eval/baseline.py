"""Faithful Python port of auto-reflect's Go rule matcher.

Mirrors `auto-reflect/internal/rules/match.go` (MatchRules) exactly, so the
experiment harness has a baseline that provably matches the shipped tool. The
conformance test (conformance/) pins this parity against the live Go CLI.

Keep this in lockstep with match.go. If match.go changes, update here and re-run
conformance. Scoring: use_when substring = 3 pts/keyword, domain substring = 1
pt/keyword (once per keyword); normalized by 4*len(keywords). Order: score DESC,
id ASC. Hard rules whose domain intersects the injection set are always included.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Sequence

# Lifecycle constants (match.go / rules package).
LIFECYCLE_DRAFT = "draft"
LIFECYCLE_STALE = "stale"
LIFECYCLE_ENFORCED = "enforced"
RULE_TYPE_HARD = "hard"


@dataclass
class Rule:
    id: str
    use_when: str
    domain: list[str]
    rule_type: str = "soft"
    lifecycle: str = "draft"
    content: str = ""
    causal_note: str = ""
    observation_ids: list[str] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: dict) -> "Rule":
        return cls(
            id=d["id"],
            use_when=d.get("use_when", ""),
            domain=list(d.get("domain") or []),
            rule_type=d.get("rule_type", "soft"),
            lifecycle=d.get("lifecycle", "draft"),
            content=d.get("content", ""),
            causal_note=d.get("causal_note", ""),
            observation_ids=list(d.get("observation_ids") or []),
        )


@dataclass
class Match:
    rule: Rule
    match_score: float
    hard_injected: bool


def normalize_keywords(query: str) -> list[str]:
    """lowercase, split on whitespace, dedupe preserving first-seen order."""
    seen: set[str] = set()
    out: list[str] = []
    for p in query.strip().lower().split():
        if p in seen:
            continue
        seen.add(p)
        out.append(p)
    return out


def _normalize_domain_filter(domains: Sequence[str]) -> list[str]:
    seen: set[str] = set()
    out: list[str] = []
    for d in domains:
        n = d.strip().lower()
        if not n or n in seen:
            continue
        seen.add(n)
        out.append(n)
    return out


def _surfaceable(lifecycle: str, include_drafts: bool) -> bool:
    if lifecycle in (LIFECYCLE_STALE, LIFECYCLE_ENFORCED):
        return False
    if lifecycle == LIFECYCLE_DRAFT:
        return include_drafts
    return True


def _domains_intersect(domain: Sequence[str], sset: Sequence[str]) -> bool:
    if not domain or not sset:
        return False
    want = {s.strip().lower() for s in sset}
    return any(d.strip().lower() in want for d in domain)


def _score_rule(rule: Rule, keywords: Sequence[str]) -> float:
    use_when = rule.use_when.lower()
    domain = [d.lower() for d in rule.domain]
    raw = 0.0
    for kw in keywords:
        if kw in use_when:
            raw += 3
        for d in domain:
            if kw in d:
                raw += 1
                break
    return raw


def match_rules(
    rules: Sequence[Rule],
    intent: str,
    domain_filter: Sequence[str] | None = None,
    include_drafts: bool = True,
) -> list[Match]:
    """Port of MatchRules. Returns surfaced rules ordered score DESC, id ASC."""
    keywords = normalize_keywords(intent)
    filt = _normalize_domain_filter(domain_filter or [])

    candidates = [
        r
        for r in rules
        if _surfaceable(r.lifecycle, include_drafts)
        and (not filt or _domains_intersect(r.domain, filt))
    ]

    max_raw = float(4 * len(keywords))
    scored: list[Match] = []
    in_results: dict[str, int] = {}
    for r in candidates:
        raw = _score_rule(r, keywords)
        if raw <= 0:
            continue
        score = raw / max_raw if max_raw > 0 else 0.0
        scored.append(Match(rule=r, match_score=score, hard_injected=False))
        in_results[r.id] = len(scored) - 1

    injection_set = filt if filt else keywords
    for r in rules:
        if r.rule_type != RULE_TYPE_HARD:
            continue
        if not _surfaceable(r.lifecycle, include_drafts):
            continue
        if not _domains_intersect(r.domain, injection_set):
            continue
        if r.id in in_results:
            scored[in_results[r.id]].hard_injected = True
            continue
        scored.append(Match(rule=r, match_score=0.0, hard_injected=True))
        in_results[r.id] = len(scored) - 1

    scored.sort(key=lambda m: (-m.match_score, m.rule.id))
    return scored


def ordered_ids(matches: Sequence[Match]) -> list[str]:
    return [m.rule.id for m in matches]
