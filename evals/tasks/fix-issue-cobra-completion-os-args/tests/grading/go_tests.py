"""Shared criteria: score `go test -json` output against the task's test lists.

- fail_to_pass: share of the tests that fail before the upstream fix and pass
  after it, which now pass.
- pass_to_pass: share of the tests that already passed, which still pass.
- resolved: 1 only when both are complete. This is the primary reward, as in
  SWE-bench: a half-fix, or a fix that breaks something else, is not a fix.

A test that is missing from the output counts as failed, so a build break
scores 0 rather than being skipped.
"""

import json
from pathlib import Path

from rewardkit import criterion

RESULTS = Path("/logs/verifier/go-test.json")
F2P = Path("/tests/fail_to_pass.json")
P2P = Path("/tests/pass_to_pass.json")


def _passed() -> set[str]:
    passed: set[str] = set()
    if not RESULTS.is_file():
        return passed
    for line in RESULTS.read_text().splitlines():
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        if ev.get("Test") and ev.get("Action") == "pass":
            passed.add(f"{ev['Package']}::{ev['Test']}")
    return passed


def _share(path: Path) -> float:
    wanted = json.loads(path.read_text())
    if not wanted:
        return 1.0
    passed = _passed()
    return sum(t in passed for t in wanted) / len(wanted)


@criterion(shared=True, description="share of fail-to-pass tests that now pass")
def fail_to_pass(workspace: Path) -> float:
    return _share(F2P)


@criterion(shared=True, description="share of previously passing tests that still pass")
def pass_to_pass(workspace: Path) -> float:
    return _share(P2P)


@criterion(shared=True, description="all fail-to-pass and pass-to-pass tests pass")
def resolved(workspace: Path) -> float:
    return 1.0 if _share(F2P) == 1.0 and _share(P2P) == 1.0 else 0.0
