"""Graded verifier: reward = F1 of the agent's /app/answer.json against the
neutral ground truth (tests/expected_importers.json).

Case-agnostic: entries may be file paths (e.g. logrus.go) or package paths
(e.g. plumbing/object, or "." for the module root). Pure stdlib. Tolerant about
answer shape and path spelling so the score reflects the agent's reasoning.
"""

import json
import os
import re
from pathlib import Path

ANSWER = Path(os.environ.get("ANSWER_PATH", "/app/answer.json"))
TRUTH = Path(os.environ.get("TRUTH_PATH", "/tests/expected_importers.json"))
OUT = Path("/logs/verifier")

# strip an absolute in-container repo prefix like /app/<repo>/
_REPO_PREFIX = re.compile(r"^/app/[^/]+/")


def norm(p):
    p = str(p).strip().replace("\\", "/")
    p = _REPO_PREFIX.sub("", p)
    if p in (".", ""):
        return p
    if p.startswith("./"):
        p = p[2:]
    p = p.strip("/")
    return p or "."


def load_answer():
    try:
        data = json.loads(ANSWER.read_text())
    except Exception:
        return set()
    if isinstance(data, dict):
        for k in ("files", "packages", "importers", "answer", "result", "paths"):
            if isinstance(data.get(k), list):
                data = data[k]
                break
        else:
            return set()
    if not isinstance(data, list):
        return set()
    return {norm(x) for x in data if isinstance(x, str) and (norm(x) or norm(x) == ".")}


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    truth = {norm(x) for x in json.loads(TRUTH.read_text())}
    ans = load_answer()

    tp = len(ans & truth)
    fp = len(ans - truth)
    fn = len(truth - ans)
    prec = tp / (tp + fp) if (tp + fp) else 0.0
    rec = tp / (tp + fn) if (tp + fn) else 0.0
    f1 = (2 * prec * rec / (prec + rec)) if (prec + rec) else 0.0

    (OUT / "reward.txt").write_text(f"{f1:.4f}\n")
    (OUT / "reward.json").write_text(
        json.dumps(
            {
                "accuracy": round(f1, 4),
                "f1": round(f1, 4),
                "precision": round(prec, 4),
                "recall": round(rec, 4),
                "true_positives": tp,
                "false_positives": fp,
                "false_negatives": fn,
                "n_answer": len(ans),
                "n_truth": len(truth),
            },
            indent=2,
        )
        + "\n"
    )
    print(f"F1={f1:.4f} precision={prec:.4f} recall={rec:.4f} tp={tp} fp={fp} fn={fn}")


if __name__ == "__main__":
    main()
