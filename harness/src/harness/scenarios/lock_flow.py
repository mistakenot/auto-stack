"""Scenario 4: lock-flow — two Workers on one host contending for a lock Group.

One container, one seeded host id, one ``~/.auto``, and **one registered
project workspace** (``/workspace/project``) that both Workers share. Each
"Worker" is a separate ``auto`` invocation with ``AUTO_LOCK_WORKER`` set to
``worker-a`` or ``worker-b`` — the override rung of the identity chain, which
makes identity deterministic in a container with no tmux and no linked
worktrees (D-11). The lock store is host-global, and the harness gives each
container a distinct host id, so two containers would be two *hosts*; two
Workers in one container is the case the store exists for.

The DSL is thin: ``lock(worker, *args)`` runs a lock verb as a Worker and
returns a ``Result``; ``fire_edit`` pipes a real Claude ``PreToolUse`` Edit
payload into ``auto hooks fire --agent claude`` as that Worker, and
``assert_denied`` / ``assert_allowed`` read the verdict off stdout — exactly one
deny object, or nothing. Everything here is synchronous and local, so there is
no bounded retry: an observation either holds on the first pass or it is a
finding.
"""

from __future__ import annotations

import base64
import json
import shlex

from harness.core import Result
from harness.scenarios.base import SCENARIOS_ROOT, Scenario

#: The one registered workspace both Workers edit in.
WORKSPACE = "/workspace/project"
#: Ready-file the entrypoint writes once the project is registered, the hooks
#: are installed and the lock config is in place.
READY_FILE = "/tmp/lock-ready.json"
#: The host-global store, one per ~/.auto (D-8).
STORE_PATH = "$HOME/.auto/lock/locks.json"
#: The project-local opt-in the entrypoint writes.
CONFIG_PATH = f"{WORKSPACE}/.auto/lock/settings.json"
#: Where `auto hooks install` wires the Claude hook that enforces locks (D-13).
CLAUDE_SETTINGS = f"{WORKSPACE}/.claude/settings.json"
#: The exact command doctor and the guard look for on PreToolUse.
CLAUDE_FIRE_COMMAND = "auto hooks fire --agent claude"
#: The env override that names a Worker (D-1 step 1).
WORKER_ENV = "AUTO_LOCK_WORKER"
#: The two contending Workers.
WORKER_A = "worker-a"
WORKER_B = "worker-b"
#: The one Group the entrypoint configures, and the description a blocked
#: agent is shown for it.
GROUP = "drizzle-schema"
GROUP_DESCRIPTION = "Ordered migrations clash if two branches change the schema in parallel."
#: A file under the Group's globs and one outside every glob.
LOCKED_FILE = "db/schema/users.ts"
UNLOCKED_FILE = "src/app.ts"
#: The gh stub's answer file and call log (see scenarios/lock-flow/scripts/gh).
GH_STATE_FILE = "/tmp/gh-pr-state.json"
GH_CALLS_LOG = "/tmp/gh-calls.log"


class LockFlowScenario(Scenario):
    """One host, one project, two Workers, `auto lock` + the PreToolUse guard."""

    name = "lock-flow"
    services = ["host"]

    # ── readiness gates (fail-fast, before any assertion) ────────────────────

    def check_ready(self) -> None:
        """Assert the host is seeded, the project registered and opted in, the
        store empty and the enforcing hook wired.

        `compose --wait` gates on the ready-file healthcheck only; these gates
        make the scenario's real invariants explicit so a mis-wired stand-up
        aborts with an obvious diagnostic rather than a downstream flake.
        """
        ready = self.run("host", f"cat {READY_FILE}")
        if not ready.ok or '"hostId"' not in ready.stdout:
            raise RuntimeError(
                f"host: lock-flow ready-file missing or incomplete ({READY_FILE}): "
                f"{ready.stderr or ready.stdout}"
            )

        registry = self.run("host", 'cat "$HOME/.auto/projects.json"')
        if not registry.ok:
            raise RuntimeError(f"host: project registry unreadable: {registry.stderr or registry.stdout}")
        if WORKSPACE not in registry.stdout:
            raise RuntimeError(
                f"host: {WORKSPACE} is not registered in the project registry; registry: {registry.stdout}"
            )

        config = self.run("host", f"cat {CONFIG_PATH}")
        if not config.ok or GROUP not in config.stdout:
            raise RuntimeError(
                f"host: lock config at {CONFIG_PATH} is missing or lacks group {GROUP!r}: "
                f"{config.stderr or config.stdout}"
            )

        store = self.read_store()
        if store["locks"]:
            raise RuntimeError(
                f"host: the lock store is not empty at stand-up: {store['locks']!r} — tear the stack down"
            )

        if not self.claude_hook_installed():
            raise RuntimeError(
                f"host: {CLAUDE_FIRE_COMMAND!r} is not wired onto PreToolUse in {CLAUDE_SETTINGS}; "
                "the guard would never run — did `auto hooks install` run in the entrypoint?"
            )

    # ── command DSL ──────────────────────────────────────────────────────────

    def _sh(self, script: str, env: dict[str, str] | None = None) -> Result:
        """Run a shell script in the workspace, base64-piped into `sh`.

        The base64 pipe means no argument can be mangled by the outer shell
        quoting (the event-flow convention). `env` is exported for the script
        only — which is how one container is two Workers.
        """
        exports = "".join(f"export {k}={shlex.quote(v)}; " for k, v in (env or {}).items())
        b64 = base64.b64encode(f"{exports}cd {shlex.quote(WORKSPACE)} && {script}".encode()).decode()
        return self.run("host", f"echo {b64} | base64 -d | sh")

    @staticmethod
    def _worker_env(worker: str | None) -> dict[str, str]:
        """The env that makes an invocation act as `worker`; `None` is bare.

        Bare is the D-6 case: the main checkout with no pane and no override,
        where co-located agents cannot be told apart.
        """
        return {WORKER_ENV: worker} if worker else {}

    def lock(self, worker: str | None, *args: str) -> Result:
        """Run `auto lock <args>` as a Worker (`None` = bare) in the workspace."""
        argv = " ".join(shlex.quote(a) for a in args)
        return self._sh(f"auto lock {argv}", self._worker_env(worker))

    def lock_json(self, worker: str | None, *args: str) -> object:
        """Run a lock verb and parse its stdout as JSON, raising on failure.

        Stdout is required to be pure JSON — diagnostics belong on stderr — so a
        parse failure is itself a finding, not something to work around.
        """
        r = self.lock(worker, *args)
        if not r.ok:
            raise AssertionError(
                f"auto lock {' '.join(args)} as {worker or 'bare'} failed (exit {r.exit_code}): "
                f"{r.stderr or r.stdout}"
            )
        try:
            return json.loads(r.stdout)
        except ValueError as exc:
            raise AssertionError(
                f"auto lock {' '.join(args)} as {worker or 'bare'} did not print JSON on stdout: "
                f"{exc}\nstdout: {r.stdout!r}\nstderr: {r.stderr!r}"
            ) from exc

    def take(self, worker: str | None, group: str = GROUP, *flags: str) -> dict:
        """`auto lock take <group>`; the parsed Lock. A held Group is a failure here."""
        payload = self.lock_json(worker, "take", group, *flags)
        if not isinstance(payload, dict) or "holder" not in payload:
            raise AssertionError(f"take returned an unexpected payload: {payload!r}")
        return payload

    def release(self, worker: str | None, *args: str) -> dict:
        """`auto lock release [group]`; the parsed payload with its `released` count."""
        payload = self.lock_json(worker, "release", *args)
        if not isinstance(payload, dict) or "released" not in payload:
            raise AssertionError(f"release returned an unexpected payload: {payload!r}")
        return payload

    def lock_status(self, worker: str | None) -> dict:
        """`auto lock status`; the parsed payload (groups + locks, held_by_you per Worker).

        Named `lock_status` because `Scenario.status()` is the stack's health.
        """
        payload = self.lock_json(worker, "status")
        if not isinstance(payload, dict) or "groups" not in payload:
            raise AssertionError(f"status returned an unexpected payload: {payload!r}")
        return payload

    def group_status(self, worker: str | None, group: str = GROUP) -> dict:
        """The one configured Group's row from `auto lock status`, as this Worker sees it."""
        for g in self.lock_status(worker)["groups"]:
            if g.get("name") == group:
                return g
        raise AssertionError(f"group {group!r} is not in `auto lock status` for {worker or 'bare'}")

    def clear(self, worker: str | None, group: str = GROUP, *flags: str) -> Result:
        """`auto lock clear <group> [--force]`, unparsed: a refusal is a reported outcome."""
        return self.lock(worker, "clear", group, *flags)

    def doctor(self, worker: str | None, *flags: str) -> Result:
        """`auto lock doctor`, unparsed: exit code is part of what it reports."""
        return self.lock(worker, "doctor", *flags)

    def doctor_checks(self, worker: str | None) -> dict[str, dict]:
        """`auto lock doctor` as {check: row}; raises on a non-zero exit."""
        r = self.doctor(worker)
        if not r.ok:
            raise AssertionError(f"auto lock doctor exited {r.exit_code}:\nstdout: {r.stdout}\nstderr: {r.stderr}")
        rows = json.loads(r.stdout)
        if not isinstance(rows, list):
            raise AssertionError(f"doctor printed {type(rows).__name__}, want a JSON array: {r.stdout!r}")
        return {row["check"]: row for row in rows}

    # ── the guard ────────────────────────────────────────────────────────────

    @property
    def _hooks_dir(self):
        return SCENARIOS_ROOT / self.name / "fixtures" / "hooks"

    def fire(self, worker: str | None, payload: dict) -> Result:
        """Pipe a hook payload into `auto hooks fire --agent claude` as a Worker.

        This is the real enforcement path, not a simulation of one: the same
        binary, the same entry point and the same payload shape Claude Code
        fires with. A hook must never break the agent, so a non-zero exit here
        is a genuine failure of the property under test rather than a flake.
        """
        b64 = base64.b64encode(json.dumps(payload).encode()).decode()
        r = self._sh(f"echo {b64} | base64 -d | auto hooks fire --agent claude", self._worker_env(worker))
        if not r.ok:
            raise AssertionError(
                f"auto hooks fire exited {r.exit_code} as {worker or 'bare'} — a hook must never "
                f"break the agent: {r.stderr or r.stdout}"
            )
        return r

    def edit_payload(self, rel_path: str) -> dict:
        """The fixture PreToolUse Edit payload, re-pointed at `rel_path` in the workspace."""
        payload = json.loads((self._hooks_dir / "pre-tool-use-edit.json").read_text())
        payload["cwd"] = WORKSPACE
        payload.setdefault("tool_input", {})["file_path"] = f"{WORKSPACE}/{rel_path}"
        return payload

    def fire_edit(self, worker: str | None, rel_path: str) -> Result:
        """Fire a PreToolUse Edit of `rel_path` as a Worker."""
        return self.fire(worker, self.edit_payload(rel_path))

    def decision(self, worker: str | None, rel_path: str) -> dict | None:
        """Fire an edit and return the `hookSpecificOutput` verdict, or None.

        Stdout is either empty (allow) or exactly one JSON object (deny) —
        two objects on one hook's stdout is undefined behaviour in the hook
        contract, so a second line is a finding, not something to parse around.
        """
        out = self.fire_edit(worker, rel_path).stdout.strip()
        if not out:
            return None
        lines = [line for line in out.splitlines() if line.strip()]
        if len(lines) != 1:
            raise AssertionError(
                f"the hook wrote {len(lines)} lines to stdout for {rel_path} as {worker or 'bare'}; "
                f"exactly one hookSpecificOutput object is allowed:\n{out}"
            )
        try:
            payload = json.loads(lines[0])
        except ValueError as exc:
            raise AssertionError(
                f"the hook wrote non-JSON to stdout for {rel_path} as {worker or 'bare'}: {exc}\n{out}"
            ) from exc
        verdict = payload.get("hookSpecificOutput")
        if not isinstance(verdict, dict):
            raise AssertionError(f"the hook's stdout object carries no hookSpecificOutput: {payload!r}")
        return verdict

    def assert_denied(self, worker: str | None, rel_path: str) -> str:
        """Assert an edit of `rel_path` by a Worker is denied; return the reason."""
        verdict = self.decision(worker, rel_path)
        if verdict is None:
            raise AssertionError(f"edit of {rel_path} as {worker or 'bare'} was allowed (empty stdout), want deny")
        if verdict.get("hookEventName") != "PreToolUse":
            raise AssertionError(f"deny hookEventName = {verdict.get('hookEventName')!r}, want PreToolUse: {verdict!r}")
        if verdict.get("permissionDecision") != "deny":
            raise AssertionError(
                f"permissionDecision = {verdict.get('permissionDecision')!r}, want deny: {verdict!r}"
            )
        reason = verdict.get("permissionDecisionReason", "")
        if not reason:
            raise AssertionError(f"deny carries no permissionDecisionReason: {verdict!r}")
        return reason

    def assert_allowed(self, worker: str | None, rel_path: str) -> None:
        """Assert an edit of `rel_path` by a Worker is allowed: empty stdout."""
        r = self.fire_edit(worker, rel_path)
        if r.stdout.strip():
            raise AssertionError(
                f"edit of {rel_path} as {worker or 'bare'} was not allowed; the hook wrote:\n{r.stdout}"
            )

    # ── the store and the gh stub ────────────────────────────────────────────

    def read_store(self) -> dict:
        """The host-global store as a dict; an absent file is an empty store."""
        r = self.run("host", f'if [ -f {STORE_PATH} ]; then cat {STORE_PATH}; else echo __absent__; fi')
        if not r.ok:
            raise AssertionError(f"cannot read {STORE_PATH}: {r.stderr or r.stdout}")
        if r.stdout.strip() == "__absent__":
            return {"version": 1, "locks": [], "audit": []}
        store = json.loads(r.stdout)
        store.setdefault("locks", [])
        store.setdefault("audit", [])
        return store

    def reset_store(self) -> None:
        """Forget every lock and audit entry on the host, so a test starts clean."""
        r = self.run("host", f"rm -f {STORE_PATH}")
        if not r.ok:
            raise AssertionError(f"cannot remove {STORE_PATH}: {r.stderr or r.stdout}")

    def seed_worktree_holder(
        self,
        branch: str,
        *,
        reason: str = "",
        pr: str = "",
        taken_at: str = "2026-08-31T14:02:11Z",
        group: str = GROUP,
    ) -> dict:
        """Write a lock held by a *worktree* Worker straight into the store.

        `auto lock take` records the kind of the Worker running it, and inside
        this container every Worker is an override (no linked worktrees), so a
        worktree holder — the only kind `clear` verifies through gh — cannot be
        produced through the CLI. It is seeded the way the Go tests seed it,
        under a worktree path that exists, because a holder whose path is gone
        is dead and is reclaimed on the next read (D-3).
        """
        project = self.lock_status(WORKER_A)["project"]
        host = json.loads(self.run("host", 'cat "$HOME/.auto/host.json"').stdout)["hostId"]
        worktree = f"/tmp/holder-{branch.replace('/', '-')}"
        holder = {"kind": "worktree", "host": host, "branch": branch, "worktree_path": worktree}
        lock = {"project": project, "group": group, "holder": holder, "taken_at": taken_at}
        if reason:
            lock["reason"] = reason
        if pr:
            lock["pr"] = pr
        store = self.read_store()
        store["locks"] = [l for l in store["locks"] if not (l["project"] == project and l["group"] == group)]
        store["locks"].append(lock)
        b64 = base64.b64encode(json.dumps(store).encode()).decode()
        r = self.run(
            "host",
            f'mkdir -p {worktree} "$(dirname {STORE_PATH})" && echo {b64} | base64 -d > {STORE_PATH}',
        )
        if not r.ok:
            raise AssertionError(f"cannot seed the store: {r.stderr or r.stdout}")
        return lock

    def set_pr_state(self, number: int, state: str) -> None:
        """Make the gh stub report one PR in `state` (OPEN, MERGED, CLOSED) for any branch."""
        body = json.dumps([{"number": number, "state": state}])
        r = self.run("host", f"printf %s {shlex.quote(body)} > {GH_STATE_FILE}")
        if not r.ok:
            raise AssertionError(f"cannot write {GH_STATE_FILE}: {r.stderr or r.stdout}")

    def reset_gh(self) -> None:
        """Back to "no PR" and an empty call log."""
        r = self.run("host", f"rm -f {GH_STATE_FILE} {GH_CALLS_LOG}")
        if not r.ok:
            raise AssertionError(f"cannot reset the gh stub: {r.stderr or r.stdout}")

    def gh_calls(self) -> list[str]:
        """Every gh invocation since the last reset, one argument line each."""
        r = self.run("host", f"if [ -f {GH_CALLS_LOG} ]; then cat {GH_CALLS_LOG}; fi")
        return [line for line in r.stdout.splitlines() if line.strip()]

    def claude_hook_installed(self) -> bool:
        """Whether the Claude PreToolUse fire command is wired in the project settings.

        Mirrors the test `auto hooks install` and `auto lock doctor` apply: a
        `{"type":"command","command":…}` handler in any PreToolUse group.
        """
        r = self.run("host", f"cat {CLAUDE_SETTINGS}")
        if not r.ok:
            return False
        try:
            doc = json.loads(r.stdout)
        except ValueError:
            return False
        groups = (doc.get("hooks") or {}).get("PreToolUse") or []
        for group in groups:
            for handler in (group.get("hooks") or []) if isinstance(group, dict) else []:
                if isinstance(handler, dict) and handler.get("type") == "command" and handler.get("command") == CLAUDE_FIRE_COMMAND:
                    return True
        return False
