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

    def task_dirs(self) -> list[Path]:
        if not self.tasks_dir.is_dir():
            return []
        return sorted(p for p in self.tasks_dir.iterdir() if (p / "task.toml").is_file())

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
