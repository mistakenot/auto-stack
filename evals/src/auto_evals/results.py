"""Load Harbor job directories into flat trial records.

A trial is valid only when Harbor recorded no exception. An errored trial can
still carry a verifier result, for example a rate-limited agent whose empty
answer was graded 0. Harbor's default mean counts it as a failure. Here it is
counted separately, so infrastructure errors never read as agent errors.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path


@dataclass
class Trial:
    job: str
    trial: str
    task: str
    task_checksum: str | None
    valid: bool
    error_type: str | None
    metrics: dict[str, float] = field(default_factory=dict)
    trajectory: Path | None = None


def _seconds(span: dict | None) -> float | None:
    if not span or not span.get("started_at") or not span.get("finished_at"):
        return None
    start = datetime.fromisoformat(span["started_at"].replace("Z", "+00:00"))
    end = datetime.fromisoformat(span["finished_at"].replace("Z", "+00:00"))
    return (end - start).total_seconds()


def load_job(job_dir: Path) -> list[Trial]:
    trials: list[Trial] = []
    for result_path in sorted(job_dir.glob("*/result.json")):
        r = json.loads(result_path.read_text())
        exc = r.get("exception_info")
        rewards = (r.get("verifier_result") or {}).get("rewards") or {}
        agent = r.get("agent_result") or {}
        metrics: dict[str, float] = {k: float(v) for k, v in rewards.items() if isinstance(v, (int, float))}
        for key, value in (
            ("agent_sec", _seconds(r.get("agent_execution"))),
            ("input_tokens", agent.get("n_input_tokens")),
            ("output_tokens", agent.get("n_output_tokens")),
            ("cost_usd", agent.get("cost_usd")),
        ):
            if value is not None:
                metrics[key] = float(value)
        traj = result_path.parent / "agent" / "trajectory.json"
        trials.append(Trial(
            job=job_dir.name,
            trial=result_path.parent.name,
            task=r.get("task_name", ""),
            task_checksum=r.get("task_checksum"),
            valid=exc is None and bool(rewards),
            error_type=(exc or {}).get("exception_type") if exc else (None if rewards else "NoReward"),
            metrics=metrics,
            trajectory=traj if traj.is_file() else None,
        ))
    return trials


def trajectory_matches(path: Path | None, pattern: re.Pattern[str]) -> bool:
    """True when any tool call's arguments in an ATIF trajectory match `pattern`."""
    if path is None:
        return False
    try:
        doc = json.loads(path.read_text())
    except (OSError, ValueError):
        return False
    for step in doc.get("steps", []):
        for call in step.get("tool_calls") or []:
            if pattern.search(json.dumps(call.get("arguments"))):
                return True
    return False


def latest_job(jobs_dir: Path, experiment: str, arm: str) -> Path | None:
    """Newest completed job named <experiment>__<arm>__<timestamp>."""
    prefix = f"{experiment}__{arm}__"
    candidates = sorted(
        (p for p in jobs_dir.glob(prefix + "*") if (p / "result.json").is_file()),
        key=lambda p: p.name,
    )
    return candidates[-1] if candidates else None
