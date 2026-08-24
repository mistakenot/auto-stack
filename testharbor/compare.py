#!/usr/bin/env python3
"""Compare two Harbor jobs — treatment (with auto graph) vs baseline — on the
three axes we care about: quality (reward/F1), agent wall-clock time, and tokens.

    python3 compare.py <treatment_job_dir> <baseline_job_dir>

Job dirs are the ones Harbor writes under jobs/<timestamp>/. Each trial's
result.json carries the reward, per-phase timestamps, and token counts.
"""

import glob
import json
import os
import statistics
import sys
from datetime import datetime


def _dur(section):
    a = (section or {}).get("started_at")
    b = (section or {}).get("finished_at")
    if not a or not b:
        return None
    f = lambda s: datetime.fromisoformat(s.replace("Z", "+00:00"))
    return (f(b) - f(a)).total_seconds()


def _trial_errored(trial_dir):
    """True if the agent run failed (e.g. rate-limited / out of credits) rather
    than genuinely attempting the task. Such trials must not pollute the means.
    Claude Code writes a final {"type":"result",...} line with is_error/api_error.
    """
    cc = os.path.join(trial_dir, "agent", "claude-code.txt")
    try:
        lines = [l for l in open(cc).read().splitlines() if l.startswith("{")]
    except OSError:
        return False
    for line in reversed(lines):
        try:
            ev = json.loads(line)
        except Exception:
            continue
        if ev.get("type") == "result":
            return bool(ev.get("is_error")) or ev.get("api_error_status") not in (None, 200)
    # No result line at all → the run didn't complete cleanly.
    return any('"out_of_credits"' in l or '"rate_limit"' in l for l in lines)


def load_job(job_dir):
    rows = []
    for p in glob.glob(f"{job_dir.rstrip('/')}/*/result.json"):
        try:
            r = json.load(open(p))
        except Exception:
            continue
        if "verifier_result" not in r:
            continue
        ar = r.get("agent_result") or {}
        rewards = (r.get("verifier_result") or {}).get("rewards") or {}
        # Graded verifiers emit f1/accuracy; a plain pass/fail verifier emits reward.
        quality = next(
            (rewards[k] for k in ("reward", "f1", "accuracy") if rewards.get(k) is not None),
            None,
        )
        rows.append(
            {
                "errored": _trial_errored(os.path.dirname(p)),
                "reward": quality,
                "agent_sec": _dur(r.get("agent_execution")),
                "out_tok": ar.get("n_output_tokens"),
                "in_tok": ar.get("n_input_tokens"),
                "cache_tok": ar.get("n_cache_tokens"),
                "cost": ar.get("cost_usd"),
            }
        )
    return rows


def agg(rows, key):
    vals = [r[key] for r in rows if r.get(key) is not None]
    return statistics.mean(vals) if vals else None


def fmt(v, unit=""):
    if v is None:
        return "—"
    if isinstance(v, float):
        return f"{v:.3f}{unit}"
    return f"{v}{unit}"


def main():
    if len(sys.argv) < 3:
        print("usage: compare.py <treatment_job_dir> <baseline_job_dir>")
        sys.exit(2)
    t_all = load_job(sys.argv[1])
    b_all = load_job(sys.argv[2])
    # Exclude errored (e.g. rate-limited / out-of-credits) trials from the means.
    t = [r for r in t_all if not r["errored"]]
    b = [r for r in b_all if not r["errored"]]
    t_err = len(t_all) - len(t)
    b_err = len(b_all) - len(b)

    metrics = [
        ("reward", "quality (F1)", ""),
        ("agent_sec", "agent time", "s"),
        ("out_tok", "output tokens", ""),
        ("in_tok", "input tokens", ""),
        ("cost", "cost", "$"),
    ]
    print(f"{'metric':<16}{'with auto graph':>18}{'baseline':>14}{'delta':>14}")
    print("-" * 62)
    for key, label, unit in metrics:
        tv, bv = agg(t, key), agg(b, key)
        delta = (tv - bv) if (tv is not None and bv is not None) else None
        print(f"{label:<16}{fmt(tv, unit):>18}{fmt(bv, unit):>14}{fmt(delta, unit):>14}")
    print(f"\nvalid trials: with={len(t)}  baseline={len(b)}")
    if t_err or b_err:
        print(f"EXCLUDED errored/rate-limited trials: with={t_err}  baseline={b_err} "
              f"— means above are over valid trials only.")
    if not t or not b:
        print("WARNING: an arm has zero valid trials (all errored) — no comparison possible. "
              "Check auth/quota and re-run.")
    if any(r["out_tok"] is None for r in t + b):
        print("note: token/cost fields are None for the Oracle agent (no LLM) — "
              "run -a claude-code -m <model> (with ANTHROPIC_API_KEY or "
              "CLAUDE_CODE_OAUTH_TOKEN set) to populate them.")


if __name__ == "__main__":
    main()
