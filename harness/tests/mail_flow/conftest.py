"""Session-scoped fixture for the mail-flow scenario.

Brings up the single host container once per session and runs the scenario
readiness gates (host id seeded, both workspaces registered, the alpha store
initialised and empty) before any test asserts anything.
"""

from __future__ import annotations

import pytest

from harness.scenarios.mail_flow import MailFlowScenario


@pytest.fixture(scope="session")
def mail_flow() -> MailFlowScenario:
    s = MailFlowScenario()
    s.up(build=True, timeout=600)
    yield s
    s.down()
