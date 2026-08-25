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
import time

from harness.core import Result
from harness.scenarios.base import SCENARIOS_ROOT, Scenario

#: The two registered workspaces, one per agent.
WORKSPACE_A = "/workspace/project-a"
WORKSPACE_B = "/workspace/project-b"
#: Ready-file the entrypoint writes once both workspaces are registered.
READY_FILE = "/tmp/mail-ready.json"
#: The alpha store path; `alpha` is in the filename, not only in the docs (G10).
STORE_PATH = "$HOME/.auto/mail/alpha-store.db"
#: Bounded-retry budget for observing an outcome. Mail is durable and local, so
#: the loop normally succeeds on its first pass; the budget exists to absorb a
#: slow `docker compose exec`, not to wait for eventual consistency.
DEFAULT_POLL_TIMEOUT = 10.0
DEFAULT_POLL_INTERVAL = 0.25
#: The command the in-band nudge tells a working agent to run. The nudge is a
#: fixed constant carrying no mailbox content (G14), so this substring is the
#: whole of what a test can and should assert on.
NUDGE_COMMAND = "auto mail list"


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

    def subscribe(self, agent: str, address: str, *flags: str) -> dict:
        """`auto mail subscribe <address>` in a workspace; the parsed payload.

        Subscribing is what binds an agent: the CLI derives the opaque
        (manager, target) pair from its own process, and in a container with no
        tmux that is the cwd rung — which is exactly why two workspaces are two
        independently addressable agents here.
        """
        payload = self.mail_json(agent, "subscribe", address, *flags)
        if not isinstance(payload, dict) or "subscription" not in payload:
            raise AssertionError(f"subscribe returned an unexpected payload: {payload!r}")
        return payload

    def send(self, agent: str, to: str, text: str, *flags: str) -> dict:
        """`auto mail send --to <to> --message <text>`; the parsed payload."""
        payload = self.mail_json(agent, "send", "--to", to, "--message", text, *flags)
        if not isinstance(payload, dict) or "id" not in payload:
            raise AssertionError(f"send returned an unexpected payload: {payload!r}")
        return payload

    def ack(self, agent: str, mail_id: str) -> dict:
        """`auto mail ack <id>`; the parsed payload.

        Losing an ack race is a reported outcome and still exits 0 (D-062-7), so
        a non-zero exit here is a genuine failure and `mail_json` raising on it
        is the right behaviour.
        """
        payload = self.mail_json(agent, "ack", mail_id)
        if not isinstance(payload, dict) or "wonTransition" not in payload:
            raise AssertionError(f"ack returned an unexpected payload: {payload!r}")
        return payload

    # ── the in-band nudge ────────────────────────────────────────────────────

    @property
    def _hooks_dir(self):
        return SCENARIOS_ROOT / self.name / "fixtures" / "hooks"

    def fire_hook(self, agent: str, agent_kind: str = "claude") -> Result:
        """Pipe a PostToolUse payload into `auto hooks fire` for an agent.

        This is the real notification path, not a simulation of one: the same
        binary, the same entry point and the same payload shape an agent's hook
        fires with. `cwd` is patched to the agent's workspace because that is
        what the binding is derived from on the cwd rung — in a container with
        no tmux, the workspace *is* the agent's identity (D-062-2).

        A hook must never break the agent, so a non-zero exit here is a genuine
        failure of the property under test rather than a flake to retry.
        """
        payload = json.loads((self._hooks_dir / "post-tool-use.json").read_text())
        workspace = self.workspace(agent)
        payload["cwd"] = workspace
        payload.setdefault("tool_input", {})["file_path"] = f"{workspace}/README.md"
        b64 = base64.b64encode(json.dumps(payload).encode()).decode()
        r = self.run("host", f"echo {b64} | base64 -d | auto hooks fire --agent {agent_kind}")
        if not r.ok:
            raise AssertionError(
                f"auto hooks fire exited {r.exit_code} in {workspace} — a hook must never "
                f"break the agent: {r.stderr or r.stdout}"
            )
        return r

    def nudge_context(self, agent: str) -> str:
        """Fire a hook and return `hookSpecificOutput.additionalContext`, or "".

        Stdout is either empty (nothing to say) or exactly one JSON object —
        two objects on one hook's stdout is undefined behaviour in both agents'
        hook contracts (D-062-9), so a second line is a finding, not something
        to parse around.
        """
        out = self.fire_hook(agent).stdout.strip()
        if not out:
            return ""
        lines = [line for line in out.splitlines() if line.strip()]
        if len(lines) != 1:
            raise AssertionError(
                f"the hook wrote {len(lines)} lines to stdout for {self.workspace(agent)}; "
                f"exactly one hookSpecificOutput object is allowed:\n{out}"
            )
        try:
            payload = json.loads(lines[0])
        except ValueError as exc:
            raise AssertionError(
                f"the hook wrote non-JSON to stdout for {self.workspace(agent)}: {exc}\n{out}"
            ) from exc
        return payload.get("hookSpecificOutput", {}).get("additionalContext", "")

    def assert_nudge(
        self,
        agent: str,
        *,
        timeout: float = DEFAULT_POLL_TIMEOUT,
        interval: float = DEFAULT_POLL_INTERVAL,
    ) -> str:
        """Wait until a hook fire nudges this agent about waiting mail.

        Bounded retry on the observable outcome. Each pass is a fresh hook fire,
        which is safe to repeat: the check is one stat and it neither reads nor
        retires anything.
        """
        deadline = time.monotonic() + timeout
        seen = ""
        while True:
            seen = self.nudge_context(agent)
            if NUDGE_COMMAND in seen:
                return seen
            if time.monotonic() >= deadline:
                raise AssertionError(
                    f"no hook fire in {self.workspace(agent)} mentioned {NUDGE_COMMAND!r} "
                    f"within {timeout}s; last additionalContext: {seen!r}"
                )
            time.sleep(interval)

    def assert_no_nudge(
        self,
        agent: str,
        *,
        timeout: float = DEFAULT_POLL_TIMEOUT,
        interval: float = DEFAULT_POLL_INTERVAL,
    ) -> None:
        """Wait until a hook fire is silent for this agent.

        The complement of `assert_nudge`, and the assertion an ack is judged by:
        once the mailbox is empty the flag falls and the hook goes quiet again.
        """
        deadline = time.monotonic() + timeout
        seen = ""
        while True:
            seen = self.nudge_context(agent)
            if not seen:
                return
            if time.monotonic() >= deadline:
                raise AssertionError(
                    f"hook fires in {self.workspace(agent)} were still emitting context "
                    f"{timeout}s after the mailbox was emptied: {seen!r}"
                )
            time.sleep(interval)

    # ── observe the outcome ──────────────────────────────────────────────────

    def await_mail(
        self,
        agent: str,
        mail_id: str,
        *args: str,
        timeout: float = DEFAULT_POLL_TIMEOUT,
        interval: float = DEFAULT_POLL_INTERVAL,
    ) -> dict:
        """Wait until `auto mail list` shows a mail id; return that delivery.

        Bounded retry on the observable outcome, never a sleep-to-settle. The
        match is on **presence of the id**: mail is at-least-once and unordered
        (G4), so a duplicate must not fail an assertion that a count would.
        """
        deadline = time.monotonic() + timeout
        seen: list[str] = []
        while True:
            listed = self.list_mail(agent, *args)
            seen = [d.get("id") for d in listed]
            for delivery in listed:
                if delivery.get("id") == mail_id:
                    return delivery
            if time.monotonic() >= deadline:
                raise AssertionError(
                    f"mail {mail_id} never appeared in `auto mail list` for "
                    f"{self.workspace(agent)} within {timeout}s; observed ids: {seen}"
                )
            time.sleep(interval)

    def await_no_mail(
        self,
        agent: str,
        mail_id: str,
        *args: str,
        timeout: float = DEFAULT_POLL_TIMEOUT,
        interval: float = DEFAULT_POLL_INTERVAL,
    ) -> None:
        """Wait until a mail id is absent from `auto mail list`.

        The complement of `await_mail`, and the assertion an ack is judged by:
        acked mail leaves the default view. Absence is again a presence test on
        the id, so an unrelated delivery arriving cannot fail it.
        """
        deadline = time.monotonic() + timeout
        seen: list[str] = []
        while True:
            seen = [d.get("id") for d in self.list_mail(agent, *args)]
            if mail_id not in seen:
                return
            if time.monotonic() >= deadline:
                raise AssertionError(
                    f"mail {mail_id} is still listed for {self.workspace(agent)} "
                    f"{timeout}s after it was acked; observed ids: {seen}"
                )
            time.sleep(interval)
