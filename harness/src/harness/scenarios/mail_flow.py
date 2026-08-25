"""Scenario 3: mail-flow — two agents on one host trading mail.

One container, one seeded host id, one ``~/.auto``, and **two registered
project workspaces** (``/workspace/project-a``, ``/workspace/project-b``). Each
"agent" is a separate ``auto mail …`` invocation with its cwd in one of those
workspaces. That is what "two agents on one host" means here: the mail store is
host-global, and the harness's own convention gives each container a distinct
host id, so two containers would be two *hosts* (D-062-1).

The DSL is deliberately thin — ``mail(workspace, args)`` runs a mail verb inside
a workspace and returns a ``Result``; the typed helpers on top of it parse the
JSON payload. Mail promises at-least-once rather than the bus's at-most-once, so
assertions here can be stronger than event-flow's — but they must still tolerate
duplicates (G4), which is why every observation matches on **presence of a mail
id**, never on a count.
"""

from __future__ import annotations

import base64
import json
import shlex

from harness.core import Result
from harness.scenarios.base import Scenario

#: The two registered workspaces, one per agent.
WORKSPACE_A = "/workspace/project-a"
WORKSPACE_B = "/workspace/project-b"
#: Ready-file the entrypoint writes once both workspaces are registered.
READY_FILE = "/tmp/mail-ready.json"
#: The alpha store path; `alpha` is in the filename, not only in the docs (G10).
STORE_PATH = "$HOME/.auto/mail/alpha-store.db"


class MailFlowScenario(Scenario):
    """One host, two registered workspaces, `auto mail` driven in each."""

    name = "mail-flow"
    services = ["host"]

    #: Workspace paths, addressable by the tests as "agent a" / "agent b".
    workspaces = {"a": WORKSPACE_A, "b": WORKSPACE_B}

    # ── readiness gates (fail-fast, before any assertion) ────────────────────

    def check_ready(self) -> None:
        """Assert the host is seeded, both workspaces registered, store empty.

        `compose --wait` gates on the ready-file healthcheck only; these gates
        make the scenario's real invariants explicit so a mis-wired stand-up
        aborts with an obvious diagnostic rather than a downstream flake.
        """
        ready = self.run("host", f"cat {READY_FILE}")
        if not ready.ok or '"hostId"' not in ready.stdout:
            raise RuntimeError(
                f"host: mail-flow ready-file missing or incomplete ({READY_FILE}): "
                f"{ready.stderr or ready.stdout}"
            )

        registry = self.run("host", 'cat "$HOME/.auto/projects.json"')
        if not registry.ok:
            raise RuntimeError(f"host: project registry unreadable: {registry.stderr or registry.stdout}")
        for workspace in self.workspaces.values():
            if workspace not in registry.stdout:
                raise RuntimeError(
                    f"host: {workspace} is not registered in the project registry; "
                    f"registry: {registry.stdout}"
                )

        store = self.run("host", f"test -f {STORE_PATH} && echo present")
        if "present" not in store.stdout:
            raise RuntimeError(
                f"host: the alpha mail store was not initialised at {STORE_PATH} — "
                "did `auto mail init` run in the entrypoint?"
            )

        for key, workspace in self.workspaces.items():
            listed = self.mail_json(key, "list")
            if listed != []:
                raise RuntimeError(
                    f"{workspace}: `auto mail list` is not empty at stand-up: {listed!r}"
                )

    # ── command DSL ──────────────────────────────────────────────────────────

    def workspace(self, agent: str) -> str:
        """Resolve an agent key ("a"/"b") or a literal workspace path."""
        return self.workspaces.get(agent, agent)

    def mail(self, agent: str, *args: str) -> Result:
        """Run `auto mail <args>` with cwd set to an agent's workspace.

        The command is base64-piped into `sh` so no argument can be mangled by
        the outer shell quoting (the event-flow convention).
        """
        argv = " ".join(shlex.quote(a) for a in args)
        script = f"cd {shlex.quote(self.workspace(agent))} && auto mail {argv}"
        b64 = base64.b64encode(script.encode()).decode()
        return self.run("host", f"echo {b64} | base64 -d | sh")

    def mail_json(self, agent: str, *args: str) -> object:
        """Run a mail verb and parse its stdout as JSON, raising on failure.

        Stdout is required to be pure JSON — diagnostics belong on stderr — so a
        parse failure is itself a finding, not something to work around.
        """
        r = self.mail(agent, *args)
        if not r.ok:
            raise AssertionError(
                f"auto mail {' '.join(args)} failed in {self.workspace(agent)} "
                f"(exit {r.exit_code}): {r.stderr or r.stdout}"
            )
        try:
            return json.loads(r.stdout)
        except ValueError as exc:
            raise AssertionError(
                f"auto mail {' '.join(args)} did not print JSON on stdout in "
                f"{self.workspace(agent)}: {exc}\nstdout: {r.stdout!r}\nstderr: {r.stderr!r}"
            ) from exc

    def list_mail(self, agent: str, *args: str) -> list[dict]:
        """`auto mail list` in a workspace, as a list of delivery dicts."""
        listed = self.mail_json(agent, "list", *args)
        if not isinstance(listed, list):
            raise AssertionError(f"auto mail list returned {type(listed).__name__}, want a JSON array")
        return listed
