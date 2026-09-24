"""Offline tests for the tooling layer. No Docker, no model calls.

Synthetic Harbor job directories are written to pytest's tmp_path and removed
by pytest afterwards.
"""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from auto_evals.compare import MIN_TASKS_TO_GENERALIZE, _holm, compare
from auto_evals.layout import Layout
from auto_evals.lint import _diff_paths, validate
from auto_evals.results import load_job

EVALS_ROOT = Path(__file__).resolve().parents[1]


def _trial(job: Path, name: str, task: str, reward: float | None, *, error: str | None = None,
           checksum: str = "c1", uses_tool: bool = False) -> None:
    d = job / name
    (d / "agent").mkdir(parents=True)
    result = {
        "task_name": task,
        "task_checksum": checksum,
        "exception_info": {"exception_type": error, "exception_message": "x"} if error else None,
        "verifier_result": {"rewards": {"reward": reward}} if reward is not None else None,
        "agent_result": {"n_input_tokens": 100, "n_output_tokens": 10, "cost_usd": 0.01},
        "agent_execution": {"started_at": "2026-09-23T00:00:00Z", "finished_at": "2026-09-23T00:01:00Z"},
    }
    (d / "result.json").write_text(json.dumps(result))
    command = "auto graph code graph ." if uses_tool else "grep -r import ."
    traj = {"steps": [{"tool_calls": [{"function_name": "Bash", "arguments": {"command": command}}]}]}
    (d / "agent" / "trajectory.json").write_text(json.dumps(traj))


def _job(root: Path, name: str) -> Path:
    job = root / "jobs" / name
    job.mkdir(parents=True)
    (job / "result.json").write_text("{}")
    return job


@pytest.fixture
def layout(tmp_path: Path) -> Layout:
    (tmp_path / "conventions.toml").write_text((EVALS_ROOT / "conventions.toml").read_text())
    exp = tmp_path / "experiments" / "demo"
    exp.mkdir(parents=True)
    (exp / "experiment.toml").write_text(
        'question = "q"\nhypothesis = "h"\ndataset = "d"\nprimary_metric = "reward"\n'
        'secondary_metrics = ["agent_sec"]\n'
        '[arms.control]\ndescription = "c"\n'
        '[arms.tool]\ndescription = "t"\nchanges = ["x"]\nuptake = "\\\\bauto\\\\s+graph\\\\b"\n'
    )
    return Layout(tmp_path)


def test_errored_trial_with_reward_is_invalid(tmp_path: Path) -> None:
    job = _job(tmp_path, "j")
    _trial(job, "a", "t1", 0.0, error="ApiRateLimitError")
    _trial(job, "b", "t1", 1.0)
    trials = {t.trial: t for t in load_job(job)}
    assert not trials["a"].valid and trials["a"].error_type == "ApiRateLimitError"
    assert trials["b"].valid and trials["b"].metrics["agent_sec"] == 60.0


def test_compare_pairs_by_task_and_excludes_errors(layout: Layout) -> None:
    ctrl = _job(layout.root, "demo__control__20260923T000000Z")
    treat = _job(layout.root, "demo__tool__20260923T000000Z")
    for i, r in enumerate([0.5, 0.5, 0.5]):
        _trial(ctrl, f"c{i}", "t1", r)
    _trial(ctrl, "cerr", "t1", 0.0, error="ApiRateLimitError")  # must not drag control down
    for i, r in enumerate([0.9, 0.9, 0.9]):
        _trial(treat, f"x{i}", "t1", r, uses_tool=i < 2)

    report = compare(layout, "demo")
    eff = report["treatments"]["tool"]["effects"]["reward"]
    assert eff["control"] == 0.5 and eff["treatment"] == 0.9 and eff["delta"] == 0.4
    assert eff["significant"] and eff["ci95_generalized"] is None  # one task cannot generalize
    assert report["treatments"]["tool"]["verdict"]["result"] == "improves"
    assert report["treatments"]["tool"]["verdict"]["scope"] == "these-tasks-only"
    assert report["trials"]["control"]["errors"] == {"ApiRateLimitError": 1}
    assert report["treatments"]["tool"]["uptake"] == {
        "pattern": "\\bauto\\s+graph\\b", "used": 2, "of": 3, "rate": 0.6667}


def test_compare_excludes_task_changed_between_arms(layout: Layout) -> None:
    ctrl = _job(layout.root, "demo__control__20260923T000000Z")
    treat = _job(layout.root, "demo__tool__20260923T000000Z")
    _trial(ctrl, "c", "t1", 0.5, checksum="old")
    _trial(treat, "x", "t1", 0.9, checksum="new")
    report = compare(layout, "demo")
    assert report["treatments"]["tool"]["paired_tasks"] == []
    assert any("checksums differ" in w for w in report["warnings"])


def test_compare_is_deterministic_across_processes(layout: Layout) -> None:
    """String hashing is randomized per process, so compare must not depend on set order."""
    import subprocess
    import sys

    ctrl = _job(layout.root, "demo__control__20260923T000000Z")
    treat = _job(layout.root, "demo__tool__20260923T000000Z")
    for k in range(MIN_TASKS_TO_GENERALIZE):
        for i, r in enumerate([0.2, 0.6, 0.4]):
            _trial(ctrl, f"c{k}-{i}", f"t{k}", r)
            _trial(treat, f"x{k}-{i}", f"t{k}", r + 0.05 * (k % 3))
    code = ("import json,sys; from pathlib import Path; from auto_evals.layout import Layout; "
            "from auto_evals.compare import compare; print(json.dumps(compare(Layout(Path(sys.argv[1])), 'demo')))")
    outs = {subprocess.run([sys.executable, "-c", code, str(layout.root)], capture_output=True, text=True,
                           env={"PYTHONHASHSEED": seed}, check=True).stdout for seed in ("1", "2", "3")}
    assert len(outs) == 1


def test_single_trial_cells_get_no_interval(layout: Layout) -> None:
    ctrl = _job(layout.root, "demo__control__20260923T000000Z")
    treat = _job(layout.root, "demo__tool__20260923T000000Z")
    _trial(ctrl, "c", "t1", 0.5)
    _trial(treat, "x", "t1", 0.9)
    eff = compare(layout, "demo")["treatments"]["tool"]["effects"]["reward"]
    assert eff["delta"] == 0.4 and eff["ci95_these_tasks"] is None and not eff["significant"]


def test_generalized_interval_needs_enough_tasks(layout: Layout) -> None:
    ctrl = _job(layout.root, "demo__control__20260923T000000Z")
    treat = _job(layout.root, "demo__tool__20260923T000000Z")
    for k in range(MIN_TASKS_TO_GENERALIZE):
        for i in range(2):
            _trial(ctrl, f"c{k}-{i}", f"t{k}", 0.5 + 0.01 * i)
            _trial(treat, f"x{k}-{i}", f"t{k}", 0.8 + 0.01 * i)
    tool = compare(layout, "demo")["treatments"]["tool"]
    assert tool["effects"]["reward"]["ci95_generalized"] is not None
    assert tool["verdict"] == {"result": "improves", "scope": "generalizes", "delta": 0.3,
                               "p": tool["effects"]["reward"]["p"]}


def test_holm_is_stricter_than_raw_threshold() -> None:
    # 0.03 passes a raw 0.05 bar, but not Holm's 0.05/2 when it is the smaller of two.
    assert _holm({"a": 0.03, "b": 0.04}) == {"a": False, "b": False}
    assert _holm({"a": 0.001, "b": 0.04}) == {"a": True, "b": True}


def test_compare_pairs_arms_from_the_same_run(layout: Layout) -> None:
    old_c = _job(layout.root, "demo__control__20260923T000000Z")
    old_t = _job(layout.root, "demo__tool__20260923T000000Z")
    _trial(old_c, "c", "t1", 0.5)
    _trial(old_c, "c2", "t1", 0.5)
    _trial(old_t, "x", "t1", 0.9)
    _trial(old_t, "x2", "t1", 0.9)
    rerun = _job(layout.root, "demo__control__20260924T000000Z")  # control rerun alone
    _trial(rerun, "c", "t1", 1.0)
    report = compare(layout, "demo")
    assert report["cohort"] == "20260923T000000Z"
    assert report["jobs"]["control"] == "jobs/demo__control__20260923T000000Z"
    assert any("20260924T000000Z" in w and "ignored" in w for w in report["warnings"])


def test_compare_overrides_must_cover_every_arm_and_may_live_outside(layout: Layout, tmp_path_factory) -> None:
    outside = tmp_path_factory.mktemp("archive")
    ctrl, treat = _job(outside, "c"), _job(outside, "t")
    _trial(ctrl, "c", "t1", 0.5)
    _trial(treat, "x", "t1", 0.9)
    with pytest.raises(SystemExit, match="must name every arm"):
        compare(layout, "demo", {"control": ctrl})
    report = compare(layout, "demo", {"control": ctrl, "tool": treat})
    assert report["cohort"] == "explicit" and report["jobs"]["control"] == str(ctrl.resolve())


def test_malformed_dataset_yaml_is_a_lint_error(tmp_path: Path) -> None:
    (tmp_path / "conventions.toml").write_text((EVALS_ROOT / "conventions.toml").read_text())
    (tmp_path / "datasets").mkdir()
    (tmp_path / "datasets" / "broken.yaml").write_text("datasets: [unclosed\n")
    (tmp_path / "datasets" / "scalar.yaml").write_text("just a string\n")
    codes = sorted((e.path, e.code) for e in validate(Layout(tmp_path), resolve_arms=False))
    assert codes == [("datasets/broken.yaml", "dataset_invalid"), ("datasets/scalar.yaml", "dataset_invalid")]


def test_diff_paths_treats_missing_section_as_empty() -> None:
    assert _diff_paths({"a": 1}, {"a": 1, "environment": {"mounts": [1]}}) == ["environment.mounts"]
    assert _diff_paths({"agents": [1]}, {"agents": [2]}) == ["agents"]


def test_real_suite_passes_static_lint() -> None:
    errors = validate(Layout(EVALS_ROOT), resolve_arms=False)
    assert errors == [], [e.to_dict() for e in errors]
