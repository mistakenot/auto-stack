#!/usr/bin/env python3
"""BASELINE conformance runner — assert the Python baseline matches the Go CLI.

This checks the BASELINE STATE only (does baseline.py reproduce the shipped
match.go?). It is not a variant evaluation; candidate methods are scored against
oracle qrels in Phase 4, separately. Exits non-zero on any ranking mismatch.

Usage:
    python conformance/run_conformance.py            # both layers
    python conformance/run_conformance.py --synthetic-only
    python conformance/run_conformance.py --real-only
"""
from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))  # for `fixtures`

import harness  # noqa: E402


def _report(title: str, results) -> int:
    fails = [r for r in results if not r.ok]
    print(f"\n=== {title}: {len(results) - len(fails)}/{len(results)} match ===")
    for r in fails:
        dom = f" --domain {','.join(r.domain)}" if r.domain else ""
        nd = " --no-drafts" if r.no_drafts else ""
        print(f"  MISMATCH  intent={r.intent!r}{dom}{nd}")
        print(f"    go: {r.go_ids}")
        print(f"    py: {r.py_ids}")
    return len(fails)


def main(argv: list[str]) -> int:
    synthetic_only = "--synthetic-only" in argv
    real_only = "--real-only" in argv
    fails = 0

    if not synthetic_only:
        fails += _report("Layer 1 — real 120-rule playbook parity",
                         harness.run_real_corpus_parity(harness.repo_root()))
    if not real_only:
        fails += _report("Layer 2 — synthetic hermetic edge cases",
                         harness.run_synthetic_parity())

    print()
    if fails:
        print(f"BASELINE CONFORMANCE FAILED: {fails} mismatch(es) — baseline.py diverges "
              f"from the shipped Go matcher (match.go changed, or the port drifted).")
        return 1
    print("BASELINE CONFORMANCE PASSED: Python baseline ranks identically to the Go CLI.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
