"""Fixtures for the lock-flow scenario.

The single host container comes up once per session and runs the scenario
readiness gates (host id seeded, project registered and opted in, store empty,
Claude PreToolUse hook wired) before any test asserts anything. The store is
host-global and shared by every test in the module, so each test starts from
an empty store and a "no PR" gh stub rather than relying on the one before it
having cleaned up.
"""

from __future__ import annotations

import pytest

from harness.scenarios.lock_flow import LockFlowScenario


@pytest.fixture(scope="session")
def lock_flow() -> LockFlowScenario:
    s = LockFlowScenario()
    s.up(build=True, timeout=600)
    yield s
    s.down()


@pytest.fixture(autouse=True)
def clean_host_state(lock_flow: LockFlowScenario) -> None:
    """Every test starts with no locks, no audit and a gh that reports no PR."""
    lock_flow.reset_store()
    lock_flow.reset_gh()
