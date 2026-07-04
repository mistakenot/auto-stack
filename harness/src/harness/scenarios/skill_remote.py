"""Scenario 1: skill-remote — auto-skill add/sync/rename over a real HTTPS git server.

Two services: a `git-server` (nginx + fcgiwrap + git-http-backend serving a bare
skills repo over self-signed HTTPS) and a `sut` (the `auto` binary compiled from
monorepo source). Tests drive the SUT through the shared `Harness` exec path,
exercising the full remote code path: canonicalize URL -> blobless clone ->
realize -> git-archive extraction -> render to targets. No mocks, real transport.
"""

from __future__ import annotations

from harness.core import Result
from harness.scenarios.base import Scenario

GIT_REMOTE_URL = "https://git-server/repos/skills.git"


class SkillRemoteScenario(Scenario):
    """The promoted auto-skill e2e stack (formerly `auto-skill/harness/`)."""

    name = "skill-remote"
    services = ["git-server", "sut"]

    def check_ready(self) -> None:
        """`--wait` already gates on both healthchecks; confirm the SUT binary runs."""
        r = self.run("sut", "auto skill --help")
        if not r.ok:
            raise RuntimeError(f"sut `auto` binary not runnable: {r.stderr or r.stdout}")

    # ── skill-remote helpers (formerly on Harness) ───────────────────────────

    def run_auto(self, *args: str, timeout: int = 60) -> Result:
        """Run `auto <args...>` inside the SUT container."""
        cmd = " ".join(["auto", *args])
        return self.run("sut", cmd, timeout=timeout)

    def run_skill(self, subcmd: str, *args: str, timeout: int = 60) -> Result:
        """Run `auto skill <subcmd> [args...]` inside the SUT container."""
        return self.run_auto("skill", subcmd, *args, timeout=timeout)

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
