"""Where things live under evals/, and the conventions that govern their names.

Every command resolves paths through this module, so the directory layout is
defined once.
"""

from __future__ import annotations

import re
import tomllib
from dataclasses import dataclass
from functools import cached_property
from pathlib import Path


@dataclass(frozen=True)
class Layout:
    root: Path

    @classmethod
    def discover(cls, start: Path | None = None) -> "Layout":
        """Walk up from `start` to the directory holding conventions.toml."""
        here = (start or Path.cwd()).resolve()
        for candidate in (here, *here.parents):
            if (candidate / "conventions.toml").is_file():
                return cls(candidate)
            if (candidate / "evals" / "conventions.toml").is_file():
                return cls(candidate / "evals")
        raise SystemExit(
            "error: could not find evals/conventions.toml above the current directory.\n"
            "hint: run from inside the evals/ directory of the repository."
        )

    @property
    def tasks_dir(self) -> Path:
        return self.root / "tasks"

    @property
    def datasets_dir(self) -> Path:
        return self.root / "datasets"

    @property
    def experiments_dir(self) -> Path:
        return self.root / "experiments"

    @property
    def jobs_dir(self) -> Path:
        return self.root / "jobs"

    @property
    def defaults_config(self) -> Path:
        return self.root / "defaults.yaml"

    @cached_property
    def conventions(self) -> dict:
        return tomllib.loads((self.root / "conventions.toml").read_text())

    @property
    def org(self) -> str:
        return self.conventions["org"]

    @property
    def control_arm(self) -> str:
        return self.conventions["control_arm"]

    @cached_property
    def name_re(self) -> re.Pattern[str]:
        return re.compile(self.conventions["name_regex"])

    def area_dir(self, area: str) -> Path:
        return self.tasks_dir / area

    def task_dir(self, name: str) -> Path:
        """Where task `name` lives: tasks/<area>/<name>, the area being its leading token."""
        return self.area_dir(name.split("-", 1)[0]) / name

    def task_dirs(self) -> list[Path]:
        """Every task directory, across all area folders."""
        if not self.tasks_dir.is_dir():
            return []
        return sorted((p for p in self.tasks_dir.glob("*/*") if (p / "task.toml").is_file()),
                      key=lambda p: p.name)

    def selection_layer(self, names: list[str], label: str) -> Path:
        """A job-config layer selecting exactly these tasks, one dataset entry per area.

        Harbor's `-p` takes a single directory, so a selection spanning areas, or
        narrowing a dataset by name, is expressed as generated `datasets` entries.
        """
        import json

        by_area: dict[str, list[str]] = {}
        for name in sorted(names):
            by_area.setdefault(name.split("-", 1)[0], []).append(name)
        layer = {"datasets": [{"path": str(self.area_dir(a).relative_to(self.root)), "task_names": ns}
                              for a, ns in sorted(by_area.items())]}
        path = self.root / ".cache" / "layers" / f"{label}.json"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(layer, indent=2))
        return path

    def dataset_config(self, name: str) -> Path:
        return self.datasets_dir / f"{name}.yaml"

    def experiment_dir(self, name: str) -> Path:
        return self.experiments_dir / name

    def arm_config(self, experiment: str, arm: str) -> Path:
        return self.experiment_dir(experiment) / f"{arm}.yaml"


def load_experiment(layout: Layout, name: str) -> dict:
    path = layout.experiment_dir(name) / "experiment.toml"
    if not path.is_file():
        raise SystemExit(
            f"error: experiment '{name}' has no {path.relative_to(layout.root)}.\n"
            f"hint: list experiments with `ls {layout.experiments_dir.relative_to(layout.root)}`."
        )
    return tomllib.loads(path.read_text())
