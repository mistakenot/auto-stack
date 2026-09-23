"""Thin wrapper over the pinned `harbor` CLI installed in this uv project."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

from .layout import Layout


def harbor_bin() -> str:
    candidate = Path(sys.executable).parent / "harbor"
    if candidate.is_file():
        return str(candidate)
    raise SystemExit(
        "error: the harbor CLI is not installed in this environment.\n"
        "hint: run `uv sync` in evals/ and invoke commands with `uv run evals ...`."
    )


def harbor_env(layout: Layout, extra: dict[str, str] | None = None) -> dict[str, str]:
    """Process env for harbor. EVALS_ROOT lets arm configs reference absolute paths,
    because Harbor writes mounts into a compose file in a temp directory where
    relative paths would resolve against the wrong place."""
    env = dict(os.environ)
    env["EVALS_ROOT"] = str(layout.root)
    env.update(extra or {})
    return env


def layer_args(configs: list[Path]) -> list[str]:
    args: list[str] = []
    for cfg in configs:
        args += ["-c", str(cfg)]
    return args


def resolve_config(layout: Layout, configs: list[Path]) -> dict:
    """Return the JobConfig Harbor would run for these layered config files."""
    proc = subprocess.run(
        [harbor_bin(), "run", *layer_args(configs), "--print-config"],
        cwd=layout.root,
        env=harbor_env(layout),
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"harbor could not resolve {', '.join(map(str, configs))}:\n{proc.stderr}")
    # --print-config omits fields left at their defaults. Round-trip through
    # Harbor's own model so two configs compare field by field.
    from harbor.models.job.config import JobConfig

    full = JobConfig.model_validate(json.loads(proc.stdout)).model_dump(mode="json")
    full.pop("job_name", None)  # defaults to a timestamp, never an experimental factor
    return full


def run(layout: Layout, args: list[str], extra_env: dict[str, str] | None = None) -> int:
    """Run harbor with inherited stdio, from evals/, and return its exit code."""
    cmd = [harbor_bin(), "run", *args]
    print("+ " + " ".join(cmd), file=sys.stderr)
    return subprocess.run(cmd, cwd=layout.root, env=harbor_env(layout, extra_env)).returncode
