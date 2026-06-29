"""BASELINE conformance tests — pin the Python port to the shipped Go matcher.

These are PASS/FAIL regression tests asserting one thing: `baseline.py` reproduces
the *current* behavior of `auto reflect retrieve` (i.e. match.go) exactly. They
describe the BASELINE STATE only — they say nothing about whether that behavior
is *good*. Candidate retrieval variants (domain-as-boost, IDF, semantic, …) are
NOT tested here; they are evaluated against the oracle qrels in Phase 4 and
produce metrics, not pass/fail assertions.

If one of these fails, either match.go changed (resync baseline.py) or the port
drifted. Either way the experiment's baseline is no longer trustworthy until fixed.

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
    """Layer 1: Python baseline == live Go CLI across the real 120-rule playbook."""
    results = harness.run_real_corpus_parity(harness.repo_root())
    mismatches = [r for r in results if not r.ok]
    assert not mismatches, "\n".join(
        f"{r.intent!r} domain={r.domain}\n  go={r.go_ids}\n  py={r.py_ids}"
        for r in mismatches
    )


def test_baseline_matches_go_cli_on_synthetic_edge_cases():
    """Layer 2: Python baseline == Go CLI in a hermetic store built to exercise
    ties, hard-injection-on-domain, domain filtering, --no-drafts, and lifecycle."""
    results = harness.run_synthetic_parity()
    mismatches = [r for r in results if not r.ok]
    assert not mismatches, "\n".join(
        f"{r.intent!r} domain={r.domain} no_drafts={r.no_drafts}\n  go={r.go_ids}\n  py={r.py_ids}"
        for r in mismatches
    )
