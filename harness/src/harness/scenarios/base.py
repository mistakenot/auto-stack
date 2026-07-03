"""Scenario base class: a self-contained Compose stack over the shared core.

A `Scenario` binds a name to its Compose stack (found at
`harness/scenarios/<name>/docker-compose.yaml`) and a shared `Harness` core.
Subclasses declare the expected `services` and override `check_ready()` with the
scenario's fail-fast readiness gates — the checks that must hold *before* the
tests assert anything, so a broken stand-up aborts with an obvious error rather
than a downstream flake.
"""

from __future__ import annotations

from pathlib import Path

from harness.core import Harness, Result

# harness/src/harness/scenarios/base.py -> parents[3] == harness/ (project root)
SCENARIOS_ROOT = Path(__file__).resolve().parents[3] / "scenarios"


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

    def down(self) -> None:
        """Tear the stack down and remove volumes."""
        self.harness.down()

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
