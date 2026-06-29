"""Conformance tests — pin the shipped Python variant to the live Go matcher.

These are PASS/FAIL regression tests asserting one thing: `variants[SHIPPED]`
(the `idf-tag` variant the Go matcher ships after task 054) reproduces the
*current* behavior of `auto reflect retrieve` (i.e. match.go) exactly. They
describe the SHIPPED STATE only — they say nothing about whether that behavior
is *good*. The frozen v1 self-check (`hard-gate == baseline.py`) lives in
`test_variant_conformance.py`; candidate variants are evaluated against the oracle
qrels in Phase 4 and produce metrics, not pass/fail assertions.

If one of these fails, either match.go changed (resync the shipped variant / the
SHIPPED pointer) or the port drifted. Either way the experiment's conformance gate
is no longer trustworthy until fixed.

Run just these:  pytest -m baseline
"""
from __future__ import annotations

import shutil
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "conformance"))

import harness  # noqa: E402

pytestmark = [
    pytest.mark.baseline,
    pytest.mark.skipif(shutil.which("auto") is None, reason="`auto` binary not on PATH"),
]


def test_baseline_matches_go_cli_on_real_playbook():
    """Layer 1: shipped Python variant == live Go CLI across the real 120-rule playbook."""
    results = harness.run_real_corpus_parity(harness.repo_root())
    mismatches = [r for r in results if not r.ok]
    assert not mismatches, "\n".join(
        f"{r.intent!r} domain={r.domain}\n  go={r.go_ids}\n  py={r.py_ids}"
        for r in mismatches
    )


def test_baseline_matches_go_cli_on_synthetic_edge_cases():
    """Layer 2: shipped Python variant == Go CLI in a hermetic store built to exercise
    ties, hard-injection-on-domain, domain boost, --no-drafts, and lifecycle."""
    results = harness.run_synthetic_parity()
    mismatches = [r for r in results if not r.ok]
    assert not mismatches, "\n".join(
        f"{r.intent!r} domain={r.domain} no_drafts={r.no_drafts}\n  go={r.go_ids}\n  py={r.py_ids}"
        for r in mismatches
    )
