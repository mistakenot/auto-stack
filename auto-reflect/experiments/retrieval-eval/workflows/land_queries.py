#!/usr/bin/env python3
"""Land mined retrieval queries into the eval harness as data/queries/queries.jsonl.

Reads the query-mining workflow result ({"queries":[...]}), assigns stable ids,
dedupes near-identical intents, and writes JSONL + a provenance README.
"""
import json
import sys
import hashlib
import re
from pathlib import Path

src = sys.argv[1]
data = json.loads(Path(src).read_text())
queries = data.get("queries", data) if isinstance(data, dict) else data

OUT_DIR = Path("/home/vscode/src/auto-stack/auto-reflect/experiments/retrieval-eval/data/queries")
OUT_DIR.mkdir(parents=True, exist_ok=True)


def norm(s: str) -> str:
    return re.sub(r"\s+", " ", (s or "").strip().lower())


def qid(intent: str) -> str:
    return "q-" + hashlib.sha1(norm(intent).encode()).hexdigest()[:8]


seen: dict[str, dict] = {}
dupes = 0
for q in queries:
    intent = (q.get("intent") or "").strip()
    if not intent:
        continue
    qi = qid(intent)
    if qi in seen:
        dupes += 1
        continue
    dom = q.get("domain_guess") or []
    seen[qi] = {
        "query_id": qi,
        "intent": intent,
        "domain_guess": [d.strip().lower() for d in dom if d and d.strip()],
        "topic": q.get("topic", ""),
        "overlaps_mined_task": q.get("overlaps_mined_task", "none"),
        "held_out": bool(q.get("held_out", True)),
        "source_session": q.get("source_session", ""),
        "rationale": q.get("rationale", ""),
    }

rows = list(seen.values())
out = OUT_DIR / "queries.jsonl"
with out.open("w") as f:
    for r in rows:
        f.write(json.dumps(r) + "\n")

# stats
no_overlap = [r for r in rows if r["overlaps_mined_task"] == "none"]
with_dom = [r for r in rows if r["domain_guess"]]
empty_dom = [r for r in rows if not r["domain_guess"]]
from collections import Counter
dom_counter = Counter(d for r in rows for d in r["domain_guess"])

summary = {
    "total_queries": len(rows),
    "dropped_dupes": dupes,
    "clean_holdout_no_task_overlap": len(no_overlap),
    "flagged_overlap_with_mined_task": len(rows) - len(no_overlap),
    "with_domain_guess": len(with_dom),
    "empty_domain_guess": len(empty_dom),
    "top_domain_guesses": dom_counter.most_common(12),
    "out": str(out),
}
print(json.dumps(summary, indent=2))
