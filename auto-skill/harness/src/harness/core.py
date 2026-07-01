"""Core harness: wraps Docker Compose lifecycle and command execution."""

from __future__ import annotations

import json
import subprocess
import time
from dataclasses import dataclass, field
from pathlib import Path

HARNESS_DIR = Path(__file__).resolve().parent.parent.parent
COMPOSE_FILE = HARNESS_DIR / "docker-compose.yaml"
GIT_REMOTE_URL = "https://git-server/repos/skills.git"


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
    """Manages the Docker Compose stack and provides a command DSL."""

    compose_file: Path = field(default=COMPOSE_FILE)
    project_name: str = "autoskill-harness"
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
        """Start the harness stack. Blocks until all services are healthy."""
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

    def down(self) -> None:
        """Tear down the stack and remove volumes."""
        self._compose("down", "-v", "--remove-orphans", check=False)
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

    def run_skill(self, subcmd: str, *args: str, timeout: int = 60) -> Result:
        """Run `auto skill <subcmd> [args...]` inside the SUT container."""
        parts = ["auto", "skill", subcmd] + list(args)
        cmd = " ".join(parts)
        return self.run("sut", cmd, timeout=timeout)

    def run_auto(self, *args: str, timeout: int = 60) -> Result:
        """Run `auto <args...>` inside the SUT container."""
        cmd = " ".join(["auto"] + list(args))
        return self.run("sut", cmd, timeout=timeout)

    def fresh_workspace(self, name: str = "test") -> Result:
        """Create an isolated git-initialized workspace in the SUT container."""
        cmd = (
            f"rm -rf /workspace/{name} && "
            f"mkdir -p /workspace/{name} && "
            f"cd /workspace/{name} && "
            f"git init && "
            f"git commit --allow-empty -m 'init'"
        )
        return self.run("sut", cmd)

    def trust_source(self, workspace: str = "/workspace") -> Result:
        """Pre-approve the HTTPS endpoint so add+sync work without interaction."""
        return self.run("sut", f"cd {workspace} && auto skill trust add 'https://git-server'")

    def git_server_cmd(self, cmd: str, timeout: int = 30) -> Result:
        """Run a command on the git-server container."""
        return self.run("git-server", cmd, timeout=timeout)

    def wait_healthy(self, timeout: int = 60) -> None:
        """Poll until all services report healthy."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            status = self.status()
            if all(v == "healthy" for v in status.values()):
                return
            time.sleep(1)
        raise TimeoutError(f"Services not healthy after {timeout}s: {status}")
