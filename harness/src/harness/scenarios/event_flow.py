"""Scenario 2: event-flow — agent containers → autowatch → auto-ui.

Each agent container runs a co-located ``auto watch start`` daemon (loopback
hook-ingest + TCP RPC) seeded with a distinct host id. One ``auto-ui`` container
subscribes to every agent's RPC backend and records relayed events in its debug
ring. The DSL fires ``auto hooks fire`` inside an agent and asserts the derived
``doc.changed`` event lands in auto-ui's ``/api/debug/recent`` (D-3).

The command surface is deliberately thin (reusing ``Harness``/``Result``): a
probe or a scripted test share ``run`` (arbitrary in-agent command), ``edit_doc``
+ ``fire_hook`` (produce an event), and ``recent_events`` / ``assert_doc_changed``
(observe it on auto-ui).
"""

from __future__ import annotations

import base64
import json
import time

from harness.core import Result
from harness.scenarios.base import SCENARIOS_ROOT, Scenario

RPC_PORT = 7788
UI_PORT = 8080
# Delivery is at-most-once / lossy under backpressure, so every observation polls
# with a bounded retry and matches on presence — never exact counts (bus-spec §5).
DEFAULT_POLL_TIMEOUT = 10.0
DEFAULT_POLL_INTERVAL = 0.25


class EventFlowScenario(Scenario):
    """Multi-agent hook → autowatch → auto-ui event-flow stack."""

    name = "event-flow"
    #: Agent service names, each with a distinct seeded host id. Phase 3 adds more.
    agents = ["agent-1"]

    # `services` is a read-only view; the auto-ui service plus every agent.
    @property
    def services(self) -> list[str]:  # type: ignore[override]
        return [*self.agents, "auto-ui"]

    @property
    def _hooks_dir(self):
        return SCENARIOS_ROOT / self.name / "fixtures" / "hooks"

    # ── readiness gates (fail-fast, before any assertion) ────────────────────

    def check_ready(self) -> None:
        """Assert each daemon is bound + registered and auto-ui subscribed to all.

        `compose --wait` already gates on per-service health; these gates make the
        multi-container wiring explicit so a mis-wired stand-up aborts with an
        obvious error rather than a downstream event-assertion flake (AC-3).
        """
        for a in self.agents:
            ready = self.run(a, "cat /tmp/watch-ready.json")
            if not ready.ok or '"addr"' not in ready.stdout:
                raise RuntimeError(f"{a}: autowatch daemon not ready (ready-file missing/incomplete): {ready.stderr or ready.stdout}")
            reg = self.run(a, 'cat "$HOME/.auto/projects.json"')
            if not reg.ok or '"projects"' not in reg.stdout:
                raise RuntimeError(f"{a}: /workspace not registered in the project registry: {reg.stderr or reg.stdout}")

        listed = self.run("auto-ui", "auto ui backends list")
        if not listed.ok:
            raise RuntimeError(f"auto-ui backends list failed: {listed.stderr}")
        for a in self.agents:
            uri = f"tcp://{a}:{RPC_PORT}"
            if uri not in listed.stdout:
                raise RuntimeError(f"auto-ui is not subscribed to {uri}; backends: {listed.stdout}")

    # ── produce events ───────────────────────────────────────────────────────

    def edit_doc(self, agent: str, relpath: str, text: str) -> Result:
        """Write a docs file in the agent's registered /workspace repo.

        `relpath` is repo-relative (e.g. ``docs/plan.md``); this is the change the
        subsequent ``fire_hook`` references. Content is piped via base64 to avoid
        any shell-quoting hazard.
        """
        abspath = f"/workspace/{relpath}"
        b64 = base64.b64encode(text.encode()).decode()
        cmd = f'mkdir -p "$(dirname {abspath})" && echo {b64} | base64 -d > {abspath}'
        r = self.run(agent, cmd)
        if not r.ok:
            raise RuntimeError(f"edit_doc failed on {agent}: {r.stderr or r.stdout}")
        return r

    def fire_hook(self, agent: str, file_path: str, agent_kind: str = "claude") -> Result:
        """Pipe a PostToolUse hook payload into ``auto hooks fire`` inside an agent.

        `file_path` is the absolute in-container path the tool edited (e.g.
        ``/workspace/docs/plan.md``). The daemon derives ``doc.changed`` for it and
        relays it to auto-ui. Asserts the fire itself exits 0.
        """
        payload = json.loads((self._hooks_dir / "doc-edit.json").read_text())
        payload["cwd"] = "/workspace"
        payload.setdefault("tool_input", {})["file_path"] = file_path
        b64 = base64.b64encode(json.dumps(payload).encode()).decode()
        cmd = f"echo {b64} | base64 -d | auto hooks fire --agent {agent_kind}"
        r = self.run(agent, cmd)
        if not r.ok:
            raise RuntimeError(f"fire_hook failed on {agent} (exit {r.exit_code}): {r.stderr or r.stdout}")
        return r

    # ── observe events on auto-ui ────────────────────────────────────────────

    def _debug_recent(self) -> list[dict]:
        """Fetch auto-ui's /api/debug/recent ring (via exec); [] on any hiccup."""
        r = self.run("auto-ui", f"wget -qO- http://127.0.0.1:{UI_PORT}/api/debug/recent")
        if not r.ok or not r.stdout.strip():
            return []
        try:
            events = json.loads(r.stdout)
        except ValueError:
            return []
        return events if isinstance(events, list) else []

    def recent_events(
        self,
        match: dict | None = None,
        timeout: float = DEFAULT_POLL_TIMEOUT,
        interval: float = DEFAULT_POLL_INTERVAL,
    ) -> list[dict]:
        """Poll the debug ring with bounded retry; return matching events.

        `match` filters on top-level event fields (e.g. ``{"type": "doc.changed"}``).
        Returns as soon as at least one match appears, or the last snapshot at
        timeout (possibly empty) — callers assert on the returned list.
        """
        deadline = time.monotonic() + timeout
        snapshot: list[dict] = []
        while True:
            events = self._debug_recent()
            snapshot = [e for e in events if _matches(e, match)]
            if snapshot or time.monotonic() >= deadline:
                return snapshot
            time.sleep(interval)

    def assert_doc_changed(self, path: str, host: str, timeout: float = DEFAULT_POLL_TIMEOUT) -> dict:
        """Wait until a ``doc.changed`` with matching ``data.path`` and ``host`` appears.

        Matches on presence by (data.path, host) — the observable outcome — not on
        counts or elapsed time. Raises AssertionError on timeout with diagnostics.
        """
        deadline = time.monotonic() + timeout
        seen: list[tuple] = []
        while True:
            for e in self.recent_events(match={"type": "doc.changed"}, timeout=0.0):
                data = e.get("data") or {}
                seen.append((e.get("host"), data.get("path")))
                if data.get("path") == path and e.get("host") == host:
                    return e
            if time.monotonic() >= deadline:
                raise AssertionError(
                    f"no doc.changed with path={path!r} host={host!r} in /api/debug/recent "
                    f"within {timeout}s; observed doc.changed (host, path): {seen}"
                )
            time.sleep(DEFAULT_POLL_INTERVAL)


def _matches(event: dict, match: dict | None) -> bool:
    """True when every key in `match` equals the event's top-level field."""
    if not match:
        return True
    return all(event.get(k) == v for k, v in match.items())
