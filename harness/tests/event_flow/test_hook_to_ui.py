"""End-to-end: a hook fired inside an agent container is received by auto-ui.

Rides the real production seams with no product change: `auto hooks fire` →
co-located autowatch (ingest + derive doc.changed) → relay → auto-ui debug ring.
Asserts on the observable outcome — the derived event present in
/api/debug/recent keyed by (data.path, host) — not on elapsed time (AC-4).
"""

from __future__ import annotations

from harness.scenarios.event_flow import EventFlowScenario


def test_hook_from_agent_received_by_ui(event_flow: EventFlowScenario) -> None:
    s = event_flow
    s.edit_doc("agent-1", "docs/plan.md", "# hello")
    s.fire_hook("agent-1", "/workspace/docs/plan.md")
    ev = s.assert_doc_changed(path="docs/plan.md", host="agent-1")
    assert ev["type"] == "doc.changed"
    assert (ev.get("data") or {}).get("path") == "docs/plan.md"
    assert ev["host"] == "agent-1"
