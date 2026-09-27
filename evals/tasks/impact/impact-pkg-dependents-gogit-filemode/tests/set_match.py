"""Shared set-match criteria: score /app/answer.json against /tests/expected.json.

The answer must be a JSON array of strings, as the instruction specifies. Any
other shape scores 0. Path spelling is normalised, so `./a`, `a/`, and
`/app/<repo>/a` all match `a`. The score reflects the agent's reasoning, not
its formatting.

Registered with shared=True, so this file produces no score of its own. The
f1/, precision/, and recall/ dimension directories each call one criterion.
"""

import json
import re
from pathlib import Path

from rewardkit import criterion

TRUTH = Path("/tests/expected.json")
ANSWER_NAME = "answer.json"

_REPO_PREFIX = re.compile(r"^/app/[^/]+/")


def _norm(path: str) -> str:
    p = path.strip().replace("\\", "/")
    p = _REPO_PREFIX.sub("", p)
    if p.startswith("./"):
        p = p[2:]
    p = p.strip("/")
    return p or "."


def _load_answer(workspace: Path) -> set[str]:
    try:
        data = json.loads((workspace / ANSWER_NAME).read_text())
    except (OSError, ValueError):
        return set()
    if not isinstance(data, list) or not all(isinstance(x, str) for x in data):
        return set()
    return {_norm(x) for x in data}


def _precision_recall(workspace: Path) -> tuple[float, float]:
    truth = {_norm(x) for x in json.loads(TRUTH.read_text())}
    answer = _load_answer(workspace)
    tp = len(answer & truth)
    precision = tp / len(answer) if answer else 0.0
    recall = tp / len(truth) if truth else 0.0
    return precision, recall


@criterion(shared=True, description="precision of the answer set against the ground truth")
def set_precision(workspace: Path) -> float:
    return _precision_recall(workspace)[0]


@criterion(shared=True, description="recall of the answer set against the ground truth")
def set_recall(workspace: Path) -> float:
    return _precision_recall(workspace)[1]


@criterion(shared=True, description="F1 of the answer set against the ground truth")
def set_f1(workspace: Path) -> float:
    precision, recall = _precision_recall(workspace)
    if precision + recall == 0:
        return 0.0
    return 2 * precision * recall / (precision + recall)
