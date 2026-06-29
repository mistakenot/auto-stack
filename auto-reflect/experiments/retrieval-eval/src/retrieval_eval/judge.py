"""Second-judge data collection — re-grade the decision-relevant pool with
independent non-Claude judges (codex, grok). See second-judge/DESIGN.md.

Resumable: each completed (query) grade is appended to
data/results/judge_raw/<model>.jsonl; re-running skips query_ids already present.
Blind (no J1 labels shown) and rule order seeded-shuffled.

Usage:
  PYTHONPATH=src python -m retrieval_eval.judge codex [--limit N] [--workers 5]
  PYTHONPATH=src python -m retrieval_eval.judge grok
"""
from __future__ import annotations

import json
import re
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

from .corpus import load_rules
from .variants import VARIANTS

HERE = Path(__file__).resolve()
PROJECT_ROOT = HERE.parents[2]
DATA = PROJECT_ROOT / "data"
RAW = DATA / "results" / "judge_raw"
GUESS = "guess"
POOL_FLOOR = 25          # ensure enough negatives for an honest kappa
SEED = 90125             # deterministic per-query shuffle
TIMEOUT = 300


# ---- deterministic per-query pseudo-shuffle (no Math.random/Date allowed vibes;
#      stdlib hash-free so it's reproducible across runs) -------------------
def _shuffle(items: list, salt: str) -> list:
    keyed = sorted(items, key=lambda x: _h(f"{salt}:{x}"))
    return keyed


def _h(s: str) -> int:
    h = 1469598103934665603
    for ch in s:
        h ^= ord(ch)
        h = (h * 1099511628211) & 0xFFFFFFFFFFFFFFFF
    return h


def load_inputs():
    queries = {json.loads(l)["query_id"]: json.loads(l)
               for l in (DATA / "queries" / "queries.jsonl").read_text().splitlines() if l.strip()}
    qrels = [json.loads(l) for l in (DATA / "qrels" / "qrels.jsonl").read_text().splitlines() if l.strip()]
    rules = load_rules()
    return queries, qrels, {r.id: r for r in rules}, rules


def build_pool(q: dict, by_id: dict, rules: list) -> list[str]:
    """(J1-relevant) ∪ (top-10 union across all variants, guess) ∪ random J1=0 fill."""
    rel_ids = [x["rule_id"] for x in q.get("relevant", []) if x["rule_id"] in by_id]
    pool = set(rel_ids)
    guess = q.get("domain_guess") or []
    for v in VARIANTS.values():
        pool.update(v.run(rules, q["intent"], guess)[:10])
    if len(pool) < POOL_FLOOR:
        fill = [rid for rid in _shuffle([r.id for r in rules], q["query_id"]) if rid not in pool]
        pool.update(fill[: POOL_FLOOR - len(pool)])
    return _shuffle(list(pool), f"order:{q['query_id']}")


def _rule_obj(r) -> dict:
    return {"id": r.id, "use_when": r.use_when, "content": r.content,
            "causal_note": r.causal_note, "domain": r.domain, "rule_type": r.rule_type}


def build_prompt(q: dict, pool: list[str], by_id: dict) -> str:
    block = json.dumps([_rule_obj(by_id[rid]) for rid in pool])
    return f"""You are a strict RELEVANCE ORACLE building a golden set for evaluating playbook retrieval. Given a coding task intent, decide which playbook rules a competent agent would GENUINELY benefit from retrieving BEFORE starting this work.

TASK INTENT: {q['intent']}
(topic: {q.get('topic', '?')})

Below are {len(pool)} playbook rules as {{id, use_when, content, causal_note, domain, rule_type}}:

{block}

Judge TRUE relevance for THIS intent:
- A rule is relevant only if its actual guidance (content/causal_note) APPLIES to this specific intent — it would change or inform what the agent does. Judge the full content, not just use_when.
- Be STRICT. Do NOT mark a rule relevant merely because it shares a domain/keyword. A rule about "AWS SigV4 signing" is NOT relevant to a tmux-debugging intent even though both might be tagged cli/go.
- Grade each rule: 3 = directly on-point (clearly would help/prevent a mistake here); 2 = relevant; 1 = marginally relevant (tangential but a careful agent might use it); 0 = NOT relevant.
- Most rules will be 0 — that is correct and expected. Do not pad.
- IGNORE any notion of domain filtering; judge relevance unconstrained.

Return ONLY a JSON array, no prose, grading EVERY rule shown exactly once:
[{{"rule_id": "r-...", "grade": 0}}, ...]"""


# ---- CLI adapters -----------------------------------------------------------
def call_codex(prompt: str) -> str:
    p = subprocess.run(["codex", "exec", "-"], input=prompt, capture_output=True,
                       text=True, timeout=TIMEOUT, cwd=str(RAW))
    return p.stdout


def call_grok(prompt: str) -> str:
    p = subprocess.run(["grok", "--output-format", "json", "-p", prompt],
                       capture_output=True, text=True, timeout=TIMEOUT, cwd=str(RAW))
    try:
        return json.loads(p.stdout).get("text", p.stdout)
    except Exception:
        return p.stdout


ADAPTERS = {"codex": call_codex, "grok": call_grok}


def extract_grades(text: str, pool: set[str]) -> dict[str, int] | None:
    """Find the JSON array of {rule_id, grade} in model output; tolerate prose."""
    # candidate spans: outermost [..] (greedy), then any inner [..] (non-greedy).
    spans = []
    if "[" in text and "]" in text:
        spans.append(text[text.index("["): text.rindex("]") + 1])
    spans += [m.group(0) for m in re.finditer(r"\[.*?\]", text, re.DOTALL)]
    for span in spans:
        try:
            arr = json.loads(span)
        except Exception:
            continue
        if not (isinstance(arr, list) and arr and isinstance(arr[0], dict) and "rule_id" in arr[0]):
            continue
        out: dict[str, int] = {}
        for e in arr:
            rid = e.get("rule_id")
            if rid not in pool:
                continue
            try:
                g = int(e.get("grade", 0))
            except (TypeError, ValueError):
                g = 0
            out[rid] = max(0, min(3, g))
        if out:
            return out
    return None


def grade_one(model: str, q: dict, by_id: dict, rules: list) -> dict:
    pool = build_pool(q, by_id, rules)
    prompt = build_prompt(q, pool, by_id)
    poolset = set(pool)
    last_err = ""
    for attempt in range(2):
        try:
            raw = ADAPTERS[model](prompt)
        except subprocess.TimeoutExpired:
            last_err = "timeout"
            continue
        grades = extract_grades(raw, poolset)
        if grades is not None:
            # Missing pool rules default to 0 (judge omitted them ⇒ irrelevant).
            full = {rid: grades.get(rid, 0) for rid in pool}
            return {"query_id": q["query_id"], "pool": pool, "grades": full,
                    "n_pool": len(pool), "n_relevant": sum(1 for g in full.values() if g >= 1)}
        last_err = "parse"
    return {"query_id": q["query_id"], "pool": pool, "grades": None, "error": last_err}


def run(model: str, limit: int | None, workers: int) -> int:
    RAW.mkdir(parents=True, exist_ok=True)
    out_path = RAW / f"{model}.jsonl"
    done = set()
    if out_path.exists():
        for l in out_path.read_text().splitlines():
            if l.strip():
                r = json.loads(l)
                if r.get("grades") is not None:
                    done.add(r["query_id"])

    queries, qrels, by_id, rules = load_inputs()
    clean_with_rel = [q for q in qrels
                      if (queries.get(q["query_id"], {}).get("overlaps_mined_task") in (None, "none", ""))
                      and q.get("relevant")]
    todo = [q for q in clean_with_rel if q["query_id"] not in done]
    if limit:
        todo = todo[:limit]
    print(f"[{model}] {len(clean_with_rel)} clean+rel queries; {len(done)} done; grading {len(todo)} (workers={workers})",
          flush=True)
    if not todo:
        print(f"[{model}] nothing to do", flush=True)
        return 0

    ok = fail = 0
    with out_path.open("a") as fh, ThreadPoolExecutor(max_workers=workers) as ex:
        futs = {ex.submit(grade_one, model, q, by_id, rules): q["query_id"] for q in todo}
        for fut in as_completed(futs):
            r = fut.result()
            fh.write(json.dumps(r) + "\n")
            fh.flush()
            if r.get("grades") is not None:
                ok += 1
            else:
                fail += 1
            print(f"[{model}] {r['query_id']}: "
                  f"{'OK rel=%d/%d' % (r.get('n_relevant', 0), r.get('n_pool', 0)) if r.get('grades') is not None else 'FAIL ' + r.get('error', '')}"
                  f"  ({ok} ok / {fail} fail)", flush=True)
    print(f"[{model}] done: {ok} ok, {fail} fail", flush=True)
    return 0 if fail == 0 else 1


def main(argv: list[str]) -> int:
    if not argv or argv[0] not in ADAPTERS:
        print(f"usage: judge <{'|'.join(ADAPTERS)}> [--limit N] [--workers N]", file=sys.stderr)
        return 2
    model = argv[0]
    limit = int(argv[argv.index("--limit") + 1]) if "--limit" in argv else None
    workers = int(argv[argv.index("--workers") + 1]) if "--workers" in argv else 5
    return run(model, limit, workers)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
