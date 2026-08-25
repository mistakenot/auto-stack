"""Scenario base class: a self-contained Compose stack over the shared core.

A `Scenario` binds a name to its Compose stack (found at
`harness/scenarios/<name>/docker-compose.yaml`) and a shared `Harness` core.
Subclasses declare the expected `services` and override `check_ready()` with the
scenario's fail-fast readiness gates — the checks that must hold *before* the
tests assert anything, so a broken stand-up aborts with an obvious error rather
than a downstream flake.
"""

from __future__ import annotations

import os
from pathlib import Path

from harness.core import Harness, Result

# harness/src/harness/scenarios/base.py -> parents[3] == harness/ (project root)
SCENARIOS_ROOT = Path(__file__).resolve().parents[3] / "scenarios"

#: Values that count as "set" for HARNESS_KEEP_IMAGES. An unset or empty
#: variable, or a literal falsey word, means images are still removed — so
#: `HARNESS_KEEP_IMAGES=0` reads the way it looks.
_TRUTHY = {"1", "true", "yes", "on"}


class Scenario:
    """One Compose stack plus its readiness gates and command DSL."""

    #: Scenario name; also the subdir under `scenarios/` and the Compose project id.
    name: str = ""
    #: Service names the scenario's Compose stack is expected to expose.
    services: list[str] = []

    def __init__(self, project_name: str | None = None) -> None:
        if not self.name:
            raise ValueError(f"{type(self).__name__} must set a class-level `name`")
        self.harness = Harness(
            compose_file=self.compose_path,
            project_name=project_name or f"harness-{self.name}",
        )

    @property
    def compose_path(self) -> Path:
        """Absolute path to this scenario's docker-compose.yaml."""
        return SCENARIOS_ROOT / self.name / "docker-compose.yaml"

    # ── lifecycle ────────────────────────────────────────────────────────────

    def up(self, build: bool = True, timeout: int = 600) -> None:
        """Bring the stack up (`--wait`), then run scenario readiness gates."""
        if not self.compose_path.exists():
            raise FileNotFoundError(f"no compose file for scenario {self.name!r}: {self.compose_path}")
        self.harness.up(build=build, timeout=timeout)
        self.check_ready()

    def down(self, remove_images: bool | None = None) -> None:
        """Tear the stack down, remove volumes, and delete the images it built.

        Removing the images is the default because a scenario image is a full
        `auto` build and the stacks are cheap to rebuild but expensive to hoard:
        left behind, every scenario across every branch accumulates until the
        host runs out of disk, which takes the agents down with it.

        Pass `remove_images=False` — or set `HARNESS_KEEP_IMAGES=1` — while
        iterating on a scenario, so repeated `up`/`down` cycles reuse the layers
        instead of rebuilding from source each time.

        Note this reclaims images only. The builder cache is the larger consumer
        and is shared across every project on the host, so it is never pruned
        from here; `docker builder prune` is the (manual, global) lever for it.
        """
        if remove_images is None:
            remove_images = os.environ.get("HARNESS_KEEP_IMAGES", "").strip().lower() not in _TRUTHY
        self.harness.down(rmi="local" if remove_images else None)

    def status(self) -> dict:
        """Health of every service in the stack."""
        return self.harness.status()

    def check_ready(self) -> None:
        """Fail-fast readiness gates beyond `compose --wait`.

        Override in subclasses to assert scenario-specific invariants (a daemon
        bound, a backend subscribed, a project registered) before any test runs.
        The default is a no-op: `--wait` already gates on per-service health.
        """

    # ── command DSL ──────────────────────────────────────────────────────────

    def run(self, service: str, cmd: str, timeout: int = 60) -> Result:
        """Run a shell command inside a named service; returns a `Result`."""
        return self.harness.run(service, cmd, timeout=timeout)
