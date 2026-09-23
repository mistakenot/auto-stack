"""Paired comparison of each treatment arm against control.

The unit of pairing is the task. For each metric the effect is the mean over
tasks of (treatment mean - control mean). The 95% interval comes from a
stratified bootstrap that resamples trials within each (task, arm) cell, so a
task with noisy trials widens the interval instead of being averaged away.
Seeded, so the same jobs always produce the same report.
"""

from __future__ import annotations

import random
import re
from collections import Counter, defaultdict
from pathlib import Path
from statistics import fmean

from .layout import Layout, load_experiment
from .results import Trial, latest_job, load_job, trajectory_matches

BOOTSTRAP_SAMPLES = 4000
SEED = 20260923


def _cells(trials: list[Trial]) -> dict[str, list[Trial]]:
    by_task: dict[str, list[Trial]] = defaultdict(list)
    for t in trials:
        if t.valid:
            by_task[t.task].append(t)
    return by_task


def _effect(ctrl: dict[str, list[float]], treat: dict[str, list[float]]) -> float:
    return fmean(fmean(treat[t]) - fmean(ctrl[t]) for t in ctrl)


def _bootstrap(ctrl: dict[str, list[float]], treat: dict[str, list[float]], rng: random.Random) -> tuple[float, float]:
    draws = []
    for _ in range(BOOTSTRAP_SAMPLES):
        c = {t: rng.choices(v, k=len(v)) for t, v in ctrl.items()}
        x = {t: rng.choices(v, k=len(v)) for t, v in treat.items()}
        draws.append(_effect(c, x))
    draws.sort()
    return draws[int(0.025 * len(draws))], draws[int(0.975 * len(draws)) - 1]


def compare(layout: Layout, experiment: str, job_overrides: dict[str, Path] | None = None) -> dict:
    exp = load_experiment(layout, experiment)
    control = layout.control_arm
    overrides = job_overrides or {}
    jobs: dict[str, Path] = {}
    missing = []
    for arm in exp["arms"]:
        job = overrides.get(arm) or latest_job(layout.jobs_dir, experiment, arm)
        if job is None:
            missing.append(arm)
        else:
            jobs[arm] = job
    if control in missing:
        raise SystemExit(
            f"error: no completed '{control}' job for experiment '{experiment}' under jobs/.\n"
            f"hint: run `uv run evals run {experiment}` first, or pass --job {control}=<dir>."
        )

    trials = {arm: load_job(path) for arm, path in jobs.items()}
    metrics = [exp["primary_metric"], *exp.get("secondary_metrics", [])]
    report: dict = {
        "experiment": experiment,
        "question": exp.get("question"),
        "primary_metric": exp["primary_metric"],
        "jobs": {arm: str(p.relative_to(layout.root)) for arm, p in jobs.items()},
        "arms_without_jobs": missing,
        "trials": {
            arm: {
                "total": len(ts),
                "valid": sum(t.valid for t in ts),
                "errors": dict(Counter(t.error_type for t in ts if not t.valid)),
            }
            for arm, ts in trials.items()
        },
        "treatments": {},
        "warnings": [],
    }

    ctrl_cells = _cells(trials[control])
    rng = random.Random(SEED)
    for arm, ts in trials.items():
        if arm == control:
            continue
        treat_cells = _cells(ts)
        tasks = sorted(set(ctrl_cells) & set(treat_cells))
        for task in sorted(set(ctrl_cells) ^ set(treat_cells)):
            report["warnings"].append(f"{arm}: task '{task}' has valid trials in only one arm and is excluded")
        paired = []
        for task in tasks:
            sums = {t.task_checksum for t in ctrl_cells[task] + treat_cells[task]}
            if len(sums) > 1:
                report["warnings"].append(
                    f"{arm}: task '{task}' changed between arms (checksums differ) and is excluded")
            else:
                paired.append(task)

        effects = {}
        for m in metrics:
            c = {t: [x.metrics[m] for x in ctrl_cells[t] if m in x.metrics] for t in paired}
            x = {t: [y.metrics[m] for y in treat_cells[t] if m in y.metrics] for t in paired}
            usable = [t for t in paired if c[t] and x[t]]
            if not usable:
                continue
            c = {t: c[t] for t in usable}
            x = {t: x[t] for t in usable}
            lo, hi = _bootstrap(c, x, rng)
            effects[m] = {
                "control": round(fmean(fmean(v) for v in c.values()), 4),
                "treatment": round(fmean(fmean(v) for v in x.values()), 4),
                "delta": round(_effect(c, x), 4),
                "ci95": [round(lo, 4), round(hi, 4)],
                "ci_excludes_zero": lo > 0 or hi < 0,
                "per_task": {t: round(fmean(x[t]) - fmean(c[t]), 4) for t in usable},
            }

        entry: dict = {"paired_tasks": paired, "effects": effects}
        if pattern := exp["arms"][arm].get("uptake"):
            regex = re.compile(pattern)
            valid = [t for t in ts if t.valid]
            used = sum(trajectory_matches(t.trajectory, regex) for t in valid)
            entry["uptake"] = {"pattern": pattern, "used": used, "of": len(valid),
                               "rate": round(used / len(valid), 4) if valid else None}
        report["treatments"][arm] = entry
    return report


def render_text(report: dict) -> str:
    lines = [f"experiment: {report['experiment']}", f"question:   {report['question']}", ""]
    for arm, s in report["trials"].items():
        errs = ", ".join(f"{k}={v}" for k, v in s["errors"].items()) or "none"
        lines.append(f"{arm:<14} {s['valid']}/{s['total']} valid trials   errors: {errs}")
    for arm, entry in report["treatments"].items():
        lines += ["", f"{arm} vs control   paired tasks: {len(entry['paired_tasks'])}"]
        if "uptake" in entry:
            u = entry["uptake"]
            lines.append(f"uptake: {u['used']}/{u['of']} valid trials matched /{u['pattern']}/")
        lines.append(f"{'metric':<16}{'control':>12}{'treatment':>12}{'delta':>12}   95% CI")
        for m, e in entry["effects"].items():
            mark = " *" if e["ci_excludes_zero"] else ""
            primary = "  (primary)" if m == report["primary_metric"] else ""
            lines.append(f"{m:<16}{e['control']:>12}{e['treatment']:>12}{e['delta']:>12}   "
                         f"[{e['ci95'][0]}, {e['ci95'][1]}]{mark}{primary}")
    if report["arms_without_jobs"]:
        lines += ["", "arms with no completed job: " + ", ".join(report["arms_without_jobs"])]
    for w in report["warnings"]:
        lines.append(f"warning: {w}")
    lines += ["", "* interval excludes zero"]
    return "\n".join(lines)
