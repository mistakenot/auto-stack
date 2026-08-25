"""Core harness: wraps a Docker Compose lifecycle and command execution.

This layer is scenario-agnostic. It knows how to bring a Compose stack up and
down, exec commands inside its services, and report health. Scenario-specific
helpers (which services exist, how to seed a workspace, readiness gates) live in
`harness.scenarios`.
"""

from __future__ import annotations

import json
import subprocess
import time
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class Result:
    """Outcome of a command run inside a container."""

    stdout: str
    stderr: str
    exit_code: int
    command: str = ""

    def json(self) -> dict:
        """Parse stdout as JSON. Raises ValueError on bad JSON."""
        return json.loads(self.stdout)

    @property
    def ok(self) -> bool:
        return self.exit_code == 0

    def __repr__(self) -> str:
        status = "ok" if self.ok else f"exit={self.exit_code}"
        preview = self.stdout[:80].replace("\n", "\\n")
        return f"Result({status}, {preview!r})"


@dataclass
class Harness:
    """Manages one scenario's Docker Compose stack and provides a command DSL.

    `compose_file` is the absolute path to the scenario's `docker-compose.yaml`;
    `project_name` isolates this scenario's Compose project from every other one,
    so scenarios can run side by side without colliding.
    """

    compose_file: Path
    project_name: str = "auto-harness"
    _up: bool = field(default=False, repr=False)

    def _compose(self, *args: str, check: bool = True, timeout: int = 300) -> subprocess.CompletedProcess:
        cmd = [
            "docker", "compose",
            "-f", str(self.compose_file),
            "-p", self.project_name,
            *args,
        ]
        return subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=timeout,
            check=check,
        )

    def up(self, build: bool = True, timeout: int = 300) -> None:
        """Start the stack. Blocks until all services are healthy (`--wait`)."""
        args = ["up", "-d", "--wait"]
        if build:
            args.append("--build")
        proc = self._compose(*args, timeout=timeout, check=False)
        if proc.returncode != 0:
            raise RuntimeError(
                f"docker compose up failed (exit {proc.returncode}):\n"
                f"stdout: {proc.stdout}\nstderr: {proc.stderr}"
            )
        self._up = True

    def down(self, rmi: str | None = None) -> None:
        """Tear down the stack and remove volumes.

        `rmi` is passed straight through to `docker compose down --rmi`: pass
        `"local"` to also delete the images Compose built for this stack (the
        scenario Dockerfiles set no `image:` key, so every one of them is
        untagged and therefore `local`), or `"all"` to additionally drop pulled
        base images shared with other stacks. `None` keeps every image, which is
        what you want while iterating — the next `up` then reuses the layers.
        """
        args = ["down", "-v", "--remove-orphans"]
        if rmi:
            args += ["--rmi", rmi]
        self._compose(*args, check=False)
        self._up = False

    def status(self) -> dict:
        """Return service health as a dict of {service: status}."""
        proc = self._compose("ps", "--format", "json", check=False)
        if proc.returncode != 0:
            return {"error": proc.stderr}
        result = {}
        for line in proc.stdout.strip().splitlines():
            try:
                svc = json.loads(line)
                result[svc.get("Service", svc.get("Name", "?"))] = svc.get("Health", svc.get("State", "unknown"))
            except json.JSONDecodeError:
                continue
        return result

    def run(self, container: str, cmd: str, timeout: int = 60) -> Result:
        """Run a shell command inside a container and return the Result."""
        proc = self._compose(
            "exec", "-T", container, "sh", "-c", cmd,
            check=False,
            timeout=timeout,
        )
        return Result(
            stdout=proc.stdout,
            stderr=proc.stderr,
            exit_code=proc.returncode,
            command=cmd,
        )

    def wait_healthy(self, timeout: int = 60) -> None:
        """Poll until all services report healthy."""
        deadline = time.time() + timeout
        status: dict = {}
        while time.time() < deadline:
            status = self.status()
            if status and all(v == "healthy" for v in status.values()):
                return
            time.sleep(1)
        raise TimeoutError(f"Services not healthy after {timeout}s: {status}")
