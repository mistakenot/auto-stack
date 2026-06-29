"""Pin: the `hard-gate` variant scaffold reproduces `baseline.match_rules`.

The Phase-4 bench introduces a new ranking scaffold (`variants.rank`). If it
doesn't reproduce the shipped matcher for the baseline configuration, every
variant delta is measured against the wrong zero. This asserts byte-identical
ranking across all 100 real queries, under both the guess and no-filter
conditions. Marked `baseline` so it runs with the other regression pins.
"""
from __future__ import annotations

import json
from pathlib import Path

import pytest

from retrieval_eval.baseline import match_rules, ordered_ids
from retrieval_eval.corpus import load_rules
from retrieval_eval.variants import VARIANTS

PROJECT_ROOT = Path(__file__).resolve().parents[1]
QUERIES = PROJECT_ROOT / "data" / "queries" / "queries.jsonl"


def _queries():
    return [json.loads(line) for line in QUERIES.read_text().splitlines() if line.strip()]


@pytest.mark.baseline
@pytest.mark.parametrize("use_guess", [True, False], ids=["guess", "no-filter"])
def test_hard_gate_variant_matches_match_go(use_guess):
    rules = load_rules()
    hard_gate = VARIANTS["hard-gate"]
    n = 0
    for q in _queries():
        dom = (q.get("domain_guess") or []) if use_guess else []
        expected = ordered_ids(match_rules(rules, q["intent"], domain_filter=dom))
        got = hard_gate.run(rules, q["intent"], dom)
        assert got == expected, f"ranking drift on {q['query_id']} (guess={use_guess})"
        n += 1
    assert n == 100
