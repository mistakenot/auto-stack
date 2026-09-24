"""Paired comparison of each treatment arm against control.

The unit of pairing is the task. For each metric the effect is the mean over
tasks of (treatment mean - control mean). Two bootstrap intervals answer two
different questions:

- `ci95_these_tasks` resamples trials within each (task, arm) cell. It says how
  stable the effect is on exactly these tasks, i.e. agent randomness only.
- `ci95_generalized` also resamples the tasks themselves. It says whether the
  effect should hold on similar tasks. It is only computed once there are at
  least MIN_TASKS_TO_GENERALIZE paired tasks, because with fewer the resampled
  task sets are too few to mean anything.

The verdict comes from the experiment's pre-registered primary metric alone,
using the generalized interval when available. Secondary metrics are context.
They are flagged significant only after a Holm correction across all of them,
because testing many metrics at 95% makes at least one fluke likely.
Everything is seeded, so the same jobs always produce the same report.
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
ALPHA = 0.05
MIN_TRIALS_FOR_CI = 2
MIN_TASKS_TO_GENERALIZE = 8

Cells = dict[str, list[float]]


def _cells(trials: list[Trial]) -> dict[str, list[Trial]]:
    by_task: dict[str, list[Trial]] = defaultdict(list)
    for t in trials:
        if t.valid:
            by_task[t.task].append(t)
    return by_task


def _effect(ctrl: Cells, treat: Cells, tasks: list[str]) -> float:
    return fmean(fmean(treat[t]) - fmean(ctrl[t]) for t in tasks)


def _draws(ctrl: Cells, treat: Cells, rng: random.Random, *, resample_tasks: bool) -> list[float]:
    tasks = list(ctrl)
    out = []
    for _ in range(BOOTSTRAP_SAMPLES):
        picked = rng.choices(tasks, k=len(tasks)) if resample_tasks else tasks
        # sorted(): set order depends on per-process string hashing, which would
        # change the order of random draws and make reports irreproducible.
        c = {t: rng.choices(ctrl[t], k=len(ctrl[t])) for t in sorted(set(picked))}
        x = {t: rng.choices(treat[t], k=len(treat[t])) for t in sorted(set(picked))}
        out.append(_effect(c, x, picked))
    out.sort()
    return out


def _interval(draws: list[float]) -> list[float]:
    return [round(draws[int(0.025 * len(draws))], 4), round(draws[int(0.975 * len(draws)) - 1], 4)]


def _p_value(draws: list[float]) -> float:
    """Two-sided bootstrap p-value for 'no effect'."""
    below = sum(d <= 0 for d in draws) / len(draws)
    above = sum(d >= 0 for d in draws) / len(draws)
    return max(min(1.0, 2 * min(below, above)), 1 / len(draws))


def _holm(pvalues: dict[str, float]) -> dict[str, bool]:
    """Holm step-down: which hypotheses survive family-wise error control at ALPHA."""
    ordered = sorted(pvalues.items(), key=lambda kv: kv[1])
    m = len(ordered)
    result: dict[str, bool] = {}
    rejecting = True
    for i, (name, p) in enumerate(ordered):
        rejecting = rejecting and p <= ALPHA / (m - i)
        result[name] = rejecting
    return result


def compare(layout: Layout, experiment: str, job_overrides: dict[str, Path] | None = None) -> dict:
    exp = load_experiment(layout, experiment)
    control = layout.control_arm
    primary = exp["primary_metric"]
    secondaries = [m for m in exp.get("secondary_metrics", []) if m != primary]
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
    report: dict = {
        "experiment": experiment,
        "question": exp.get("question"),
        "primary_metric": primary,
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
        for task in sorted(set(ctrl_cells) ^ set(treat_cells)):
            report["warnings"].append(f"{arm}: task '{task}' has valid trials in only one arm and is excluded")
        paired = []
        for task in sorted(set(ctrl_cells) & set(treat_cells)):
            if len({t.task_checksum for t in ctrl_cells[task] + treat_cells[task]}) > 1:
                report["warnings"].append(
                    f"{arm}: task '{task}' changed between arms (checksums differ) and is excluded")
            else:
                paired.append(task)

        effects: dict[str, dict] = {}
        pvals: dict[str, float] = {}
        for m in [primary, *secondaries]:
            c = {t: [x.metrics[m] for x in ctrl_cells[t] if m in x.metrics] for t in paired}
            x = {t: [y.metrics[m] for y in treat_cells[t] if m in y.metrics] for t in paired}
            usable = [t for t in paired if c[t] and x[t]]
            if not usable:
                continue
            c, x = {t: c[t] for t in usable}, {t: x[t] for t in usable}
            e: dict = {
                "control": round(fmean(fmean(v) for v in c.values()), 4),
                "treatment": round(fmean(fmean(v) for v in x.values()), 4),
                "delta": round(_effect(c, x, usable), 4),
                "n_tasks": len(usable),
                "ci95_these_tasks": None,
                "ci95_generalized": None,
                "p": None,
                "significant": False,
                "per_task": {t: round(fmean(x[t]) - fmean(c[t]), 4) for t in usable},
            }
            # A one-trial cell hides its own noise: resampling it always returns the same value.
            if all(len(c[t]) >= MIN_TRIALS_FOR_CI and len(x[t]) >= MIN_TRIALS_FOR_CI for t in usable):
                fixed = _draws(c, x, rng, resample_tasks=False)
                e["ci95_these_tasks"] = _interval(fixed)
                deciding = fixed
                if len(usable) >= MIN_TASKS_TO_GENERALIZE:
                    general = _draws(c, x, rng, resample_tasks=True)
                    e["ci95_generalized"] = _interval(general)
                    deciding = general
                e["p"] = round(_p_value(deciding), 4)
                pvals[m] = e["p"]
            effects[m] = e

        if primary in effects and effects[primary]["p"] is not None:
            effects[primary]["significant"] = effects[primary]["p"] <= ALPHA
        survived = _holm({m: p for m, p in pvals.items() if m != primary})
        for m, ok in survived.items():
            effects[m]["significant"] = ok

        entry: dict = {"paired_tasks": paired, "verdict": _verdict(effects.get(primary)), "effects": effects}
        if len(paired) < MIN_TASKS_TO_GENERALIZE:
            report["warnings"].append(
                f"{arm}: {len(paired)} paired task(s); results describe these tasks only. "
                f"Add tasks until there are >= {MIN_TASKS_TO_GENERALIZE} to test generalization.")
        if any(e["ci95_these_tasks"] is None for e in effects.values()):
            report["warnings"].append(
                f"{arm}: too few trials for intervals; every task needs >= {MIN_TRIALS_FOR_CI} valid trials per arm")
        if pattern := exp["arms"][arm].get("uptake"):
            regex = re.compile(pattern)
            valid = [t for t in ts if t.valid]
            used = sum(trajectory_matches(t.trajectory, regex) for t in valid)
            entry["uptake"] = {"pattern": pattern, "used": used, "of": len(valid),
                               "rate": round(used / len(valid), 4) if valid else None}
        report["treatments"][arm] = entry
    return report


def _verdict(e: dict | None) -> dict:
    if e is None or e["p"] is None:
        return {"result": "no-data", "scope": None}
    scope = "generalizes" if e["ci95_generalized"] is not None else "these-tasks-only"
    if not e["significant"]:
        result = "inconclusive"
    else:
        result = "improves" if e["delta"] > 0 else "worsens"
    return {"result": result, "scope": scope, "delta": e["delta"], "p": e["p"]}


def render_text(report: dict) -> str:
    lines = [f"experiment: {report['experiment']}", f"question:   {report['question']}", ""]
    for arm, s in report["trials"].items():
        errs = ", ".join(f"{k}={v}" for k, v in s["errors"].items()) or "none"
        lines.append(f"{arm:<14} {s['valid']}/{s['total']} valid trials   errors: {errs}")
    for arm, entry in report["treatments"].items():
        v = entry["verdict"]
        lines += ["", f"{arm} vs control   paired tasks: {len(entry['paired_tasks'])}",
                  f"VERDICT on {report['primary_metric']}: {v['result']}"
                  + (f"  (delta {v['delta']}, p={v['p']}, scope: {v['scope']})" if v.get("scope") else "")]
        if "uptake" in entry:
            u = entry["uptake"]
            lines.append(f"uptake: {u['used']}/{u['of']} valid trials matched /{u['pattern']}/")
        num = lambda v: f"{v:,.0f}" if abs(v) >= 1000 else f"{v:.4g}"
        ci = lambda c: f"[{num(c[0])}, {num(c[1])}]" if c else "n/a"
        rows = [("metric", "control", "treatment", "delta", "95% CI these tasks", "95% CI generalized", "p")]
        for m, e in entry["effects"].items():
            rows.append((m, num(e["control"]), num(e["treatment"]), num(e["delta"]), ci(e["ci95_these_tasks"]),
                         ci(e["ci95_generalized"]), "n/a" if e["p"] is None else f"{e['p']:.3g}"))
        widths = [max(len(r[i]) for r in rows) for i in range(len(rows[0]))]
        metrics = list(entry["effects"])
        for i, r in enumerate(rows):
            cells = [r[0].ljust(widths[0])] + [c.rjust(w) for c, w in zip(r[1:4], widths[1:4])] \
                + [c.ljust(w) for c, w in zip(r[4:6], widths[4:6])] + [r[6].rjust(widths[6])]
            suffix = ""
            if i:
                e = entry["effects"][metrics[i - 1]]
                suffix = (" *" if e["significant"] else "") + ("  (primary)" if metrics[i - 1] == report["primary_metric"] else "")
            lines.append("  ".join(cells) + suffix)
    if report["arms_without_jobs"]:
        lines += ["", "arms with no completed job: " + ", ".join(report["arms_without_jobs"])]
    if report["warnings"]:
        lines.append("")
    for w in report["warnings"]:
        lines.append(f"warning: {w}")
    lines += ["", "* significant: primary at p<=0.05; secondary metrics after Holm correction across secondaries"]
    return "\n".join(lines)
