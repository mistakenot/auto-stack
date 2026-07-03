"""Session-scoped fixture for the event-flow scenario.

Brings up the agent container(s) + auto-ui once per session and runs the
scenario readiness gates (each daemon bound, each backend subscribed) before any
test observes an event.
"""

from __future__ import annotations

import pytest

from harness.scenarios.event_flow import EventFlowScenario


@pytest.fixture(scope="session")
def event_flow() -> EventFlowScenario:
    s = EventFlowScenario()
    s.up(build=True, timeout=600)
    yield s
    s.down()
