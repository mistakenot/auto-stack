"""Load the pinned corpus snapshot and build lookup maps."""
from __future__ import annotations

import json
from pathlib import Path

from .baseline import Rule

# repo .../auto-reflect/experiments/retrieval-eval/src/retrieval_eval/corpus.py
_HERE = Path(__file__).resolve()
PROJECT_ROOT = _HERE.parents[2]  # .../retrieval-eval
DEFAULT_SNAPSHOT = PROJECT_ROOT / "data" / "corpus" / "rules.snapshot.json"


def load_rules(path: Path | str | None = None) -> list[Rule]:
    p = Path(path) if path else DEFAULT_SNAPSHOT
    data = json.loads(p.read_text())
    return [Rule.from_dict(d) for d in data]


def use_when_to_id(rules: list[Rule]) -> dict[str, str]:
    m: dict[str, str] = {}
    for r in rules:
        if r.use_when in m:
            raise ValueError(f"duplicate use_when (not a unique key): {r.use_when!r}")
        m[r.use_when] = r.id
    return m
