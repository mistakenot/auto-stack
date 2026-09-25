"""Static checks for tasks, datasets, and experiments.

`validate()` is the single shared validator. Every command that needs to know
whether the suite is well-formed calls it. It returns structured errors and
never raises for a content problem.
"""

from __future__ import annotations

import fnmatch
import re
import tomllib
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

import yaml

from . import harbor
from .layout import Layout, load_experiment


@dataclass
class LintError:
    code: str
    path: str
    field: str
    message: str
    value: Any = None

    def to_dict(self) -> dict:
        d = asdict(self)
        if d["value"] is None:
            d.pop("value")
        return d


def validate(layout: Layout, *, resolve_arms: bool = True) -> list[LintError]:
    errors: list[LintError] = []
    task_names = [d.name for d in layout.task_dirs()]
    for task_dir in layout.task_dirs():
        errors += _check_task(layout, task_dir)
    for cfg in sorted(layout.datasets_dir.glob("*.yaml")):
        errors += _check_dataset(layout, cfg, task_names)
    if layout.experiments_dir.is_dir():
        for exp_dir in sorted(p for p in layout.experiments_dir.iterdir() if p.is_dir()):
            errors += _check_experiment(layout, exp_dir, resolve_arms=resolve_arms)
    return errors


# --- tasks -------------------------------------------------------------------


def _check_task(layout: Layout, task_dir: Path) -> list[LintError]:
    errs: list[LintError] = []
    rel = str(task_dir.relative_to(layout.root))
    name = task_dir.name
    conv = layout.conventions

    def err(code: str, field: str, message: str, value: Any = None) -> None:
        errs.append(LintError(code, rel, field, message, value))

    try:
        cfg = tomllib.loads((task_dir / "task.toml").read_text())
    except tomllib.TOMLDecodeError as e:
        err("task.toml_invalid", "task.toml", f"task.toml does not parse: {e}")
        return errs

    if not layout.name_re.match(name):
        err("name_format", "dir", "task directory must be lowercase kebab-case", name)

    declared = cfg.get("task", {}).get("name")
    if declared != f"{layout.org}/{name}":
        err("name_mismatch", "task.name",
            f"[task].name must be '{layout.org}/{name}' to match the directory", declared)
    if not cfg.get("task", {}).get("version"):
        err("version_missing", "task.version",
            "[task].version is required. Bump it whenever a change alters what the task measures")

    meta = cfg.get("metadata", {})
    for key in conv["required_metadata"]:
        if not str(meta.get(key, "")).strip():
            err("metadata_missing", f"metadata.{key}", f"[metadata].{key} is required and must be non-empty")

    area = meta.get("area", "")
    if area and area not in conv["areas"]:
        err("area_unknown", "metadata.area",
            f"area must be one of {sorted(conv['areas'])}; add new areas to conventions.toml first", area)
    parts = ("area", "objective", "fixture", "subject")
    expected = "-".join(str(meta.get(k, "")) for k in parts)
    if all(meta.get(k) for k in parts) and name != expected:
        err("name_convention", "dir",
            "task name must be <area>-<objective>-<fixture>-<subject> built from [metadata]",
            {"name": name, "expected": expected})

    guid = conv["canary_guid"]
    for fname in ("instruction.md", "task.toml"):
        f = task_dir / fname
        if not f.is_file():
            err("file_missing", fname, f"{fname} is required")
        elif guid not in f.read_text():
            err("canary_missing", fname, f"{fname} must carry the canary GUID from conventions.toml", guid)

    verifier = cfg.get("verifier", {})
    separate = verifier.get("environment_mode") == "separate" or "environment" in verifier
    if not separate:
        err("verifier_shared", "verifier.environment_mode",
            "use a separate verifier so results can be regraded without rerunning the agent")
    if not cfg.get("artifacts"):
        err("artifacts_missing", "artifacts",
            "declare every file the separate verifier needs under top-level `artifacts`")

    if not (task_dir / "solution" / "solve.sh").is_file():
        err("oracle_missing", "solution/solve.sh",
            "every task needs an oracle solution so `evals oracle` can prove it is solvable")

    sol, truth = task_dir / "solution" / "answer.json", task_dir / "tests" / "expected.json"
    if sol.is_file() and truth.is_file() and sol.read_bytes() != truth.read_bytes():
        err("oracle_drift", "solution/answer.json",
            "the oracle answer differs from tests/expected.json; copy one over the other")
    return errs


# --- datasets ----------------------------------------------------------------


def _check_dataset(layout: Layout, cfg_path: Path, task_names: list[str]) -> list[LintError]:
    errs: list[LintError] = []
    rel = str(cfg_path.relative_to(layout.root))
    if not layout.name_re.match(cfg_path.stem):
        errs.append(LintError("name_format", rel, "file", "dataset name must be lowercase kebab-case", cfg_path.stem))
    try:
        data = yaml.safe_load(cfg_path.read_text()) or {}
    except yaml.YAMLError as e:
        errs.append(LintError("dataset_invalid", rel, "file", f"dataset YAML does not parse: {e}"))
        return errs
    if not isinstance(data, dict) or not isinstance(data.get("datasets", []), list):
        errs.append(LintError("dataset_invalid", rel, "datasets",
                              "a dataset file must be a mapping with a `datasets` list"))
        return errs
    if set(data) - {"datasets"}:
        errs.append(LintError("dataset_scope", rel, "keys",
                              "a dataset layer may only set `datasets`; move other settings to an arm",
                              sorted(set(data) - {"datasets"})))
    for i, ds in enumerate(data.get("datasets", [])):
        for pattern in ds.get("task_names") or ["*"]:
            if not fnmatch.filter(task_names, pattern):
                errs.append(LintError("dataset_empty_glob", rel, f"datasets[{i}].task_names",
                                      "pattern matches no task under tasks/", pattern))
    return errs


# --- experiments -------------------------------------------------------------


def _check_experiment(layout: Layout, exp_dir: Path, *, resolve_arms: bool) -> list[LintError]:
    errs: list[LintError] = []
    rel = str(exp_dir.relative_to(layout.root))
    name = exp_dir.name

    def err(code: str, field: str, message: str, value: Any = None) -> None:
        errs.append(LintError(code, rel, field, message, value))

    if not layout.name_re.match(name):
        err("name_format", "dir", "experiment name must be lowercase kebab-case", name)
    try:
        exp = load_experiment(layout, name)
    except (SystemExit, tomllib.TOMLDecodeError) as e:
        err("experiment_invalid", "experiment.toml", str(e))
        return errs

    for key in ("question", "hypothesis", "dataset", "primary_metric", "arms"):
        if not exp.get(key):
            err("experiment_field_missing", key, f"experiment.toml must set `{key}`")
    dataset = exp.get("dataset")
    if dataset and not layout.dataset_config(dataset).is_file():
        err("dataset_unknown", "dataset", f"no datasets/{dataset}.yaml", dataset)
    if dataset and not name.endswith(f"-on-{dataset}"):
        err("name_convention", "dir",
            "experiment name must be <subject>-on-<dataset>, matching experiment.toml `dataset`",
            {"name": name, "dataset": dataset})

    arms: dict = exp.get("arms", {})
    control = layout.control_arm
    if control not in arms:
        err("control_missing", "arms", f"every experiment needs an arm named '{control}'")
    for arm in arms:
        if not layout.name_re.match(arm):
            err("name_format", f"arms.{arm}", "arm name must be lowercase kebab-case", arm)
        if not layout.arm_config(name, arm).is_file():
            err("arm_file_missing", f"arms.{arm}", f"arm '{arm}' needs {arm}.yaml beside experiment.toml")
        if arm != control and not arms[arm].get("changes"):
            err("changes_missing", f"arms.{arm}.changes",
                "a treatment must declare the config paths it changes relative to control")
        if pattern := arms[arm].get("uptake"):
            try:
                re.compile(pattern)
            except re.error as e:
                err("uptake_regex", f"arms.{arm}.uptake", f"invalid regex: {e}", pattern)
    for yml in exp_dir.glob("*.yaml"):
        if yml.stem not in arms:
            err("arm_undeclared", yml.name, "arm file has no [arms.<name>] entry in experiment.toml", yml.stem)

    if errs or not resolve_arms or not dataset:
        return errs
    errs += _check_one_factor(layout, name, exp)
    return errs


def _check_one_factor(layout: Layout, name: str, exp: dict) -> list[LintError]:
    """Each treatment must differ from control exactly on its declared paths."""
    errs: list[LintError] = []
    rel = str(layout.experiment_dir(name).relative_to(layout.root))
    base = [layout.defaults_config, layout.dataset_config(exp["dataset"])]
    control = layout.control_arm
    try:
        resolved = {arm: harbor.resolve_config(layout, [*base, layout.arm_config(name, arm)])
                    for arm in exp["arms"]}
    except RuntimeError as e:
        return [LintError("arm_unresolvable", rel, "arms", str(e))]

    for arm, cfg in resolved.items():
        if len(cfg.get("agents", [])) != 1:
            errs.append(LintError("agents_count", rel, f"{arm}.yaml",
                                  "an arm must resolve to exactly one agent. Harbor appends `agents` across "
                                  "layers, so declare agents only in the arm file", len(cfg.get("agents", []))))

    for arm, cfg in resolved.items():
        if arm == control:
            continue
        declared = exp["arms"][arm].get("changes", [])
        actual = _diff_paths(resolved[control], cfg)
        undeclared = [p for p in actual if not any(p == d or p.startswith(d + ".") for d in declared)]
        unused = [d for d in declared if not any(p == d or p.startswith(d + ".") for p in actual)]
        if not actual:
            errs.append(LintError("arm_identical", rel, f"{arm}.yaml", "treatment resolves identical to control"))
        if undeclared:
            errs.append(LintError("arm_undeclared_change", rel, f"arms.{arm}.changes",
                                  "treatment differs from control on paths it does not declare", undeclared))
        if unused:
            errs.append(LintError("arm_unused_change", rel, f"arms.{arm}.changes",
                                  "declared change paths do not actually differ from control", unused))
    return errs


def _diff_paths(a: Any, b: Any, prefix: str = "") -> list[str]:
    # --print-config omits sections left at their defaults, so a missing mapping
    # on one side compares as empty rather than as a whole-section difference.
    if a is None and isinstance(b, dict):
        a = {}
    if b is None and isinstance(a, dict):
        b = {}
    if isinstance(a, dict) and isinstance(b, dict):
        out: list[str] = []
        for key in sorted(set(a) | set(b)):
            out += _diff_paths(a.get(key), b.get(key), f"{prefix}.{key}" if prefix else key)
        return out
    return [] if a == b else [prefix]
