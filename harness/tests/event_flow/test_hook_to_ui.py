"""End-to-end: hooks fired inside agent containers are received by auto-ui.

Rides the real production seams with no product change: `auto hooks fire` →
co-located autowatch (ingest + derive doc.changed) → relay → auto-ui debug ring.
Assertions match on the observable outcome — the derived event present in
/api/debug/recent keyed by (data.path, host) — not on elapsed time or counts.
"""

from __future__ import annotations

from harness.scenarios.event_flow import EventFlowScenario


def test_hook_from_agent_received_by_ui(event_flow: EventFlowScenario) -> None:
    """AC-4: a hook fired in agent-1 surfaces as doc.changed in auto-ui."""
    s = event_flow
    s.edit_doc("agent-1", "docs/plan.md", "# hello")
    s.fire_hook("agent-1", "/workspace/docs/plan.md")
    ev = s.assert_doc_changed(path="docs/plan.md", host="agent-1")
    assert ev["type"] == "doc.changed"
    assert (ev.get("data") or {}).get("path") == "docs/plan.md"
    assert ev["host"] == "agent-1"


def test_multi_host_attribution(event_flow: EventFlowScenario) -> None:
    """AC-5: events from distinct agents keep distinct host attribution.

    The co-located-daemon topology (one daemon per agent, each stamping its own
    seeded host id) is what yields real multi-host aggregation rather than a
    single-daemon host-id collapse.
    """
    s = event_flow
    for a in s.agents:
        s.edit_doc(a, "docs/plan.md", f"# from {a}")
        s.fire_hook(a, "/workspace/docs/plan.md")

    # Each event must arrive attributed to its own originating host.
    for a in s.agents:
        s.assert_doc_changed(path="docs/plan.md", host=a)

    hosts = {e.get("host") for e in s.recent_events(match={"type": "doc.changed"})}
    assert {"agent-1", "agent-2"} <= hosts, f"expected both hosts attributed, saw {hosts}"


def test_probe_run_and_recent_events(event_flow: EventFlowScenario) -> None:
    """AC-6: arbitrary in-agent command returns a Result; the ring is pollable.

    Demonstrates the shared command+assert surface a probe or scripted test uses:
    `run(agent, cmd)` for an in-container command (with .ok/.json()) and
    `recent_events()` to observe auto-ui.
    """
    s = event_flow
    ready = s.run("agent-1", "cat /tmp/watch-ready.json")
    assert ready.ok, f"probe command failed: {ready.stderr}"
    assert ready.json()["hookAddr"] == "127.0.0.1:7787"

    events = s.recent_events(timeout=2.0)
    assert isinstance(events, list)
