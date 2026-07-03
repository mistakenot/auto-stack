"""Session-scoped fixture for the skill-remote scenario.

Kept under the fixture name `harness` so the carried-over auto-skill e2e tests
run unchanged: they receive a `SkillRemoteScenario`, which exposes the same
`run`/`fresh_workspace`/`trust_source`/`git_server_cmd` surface the tests used
when these helpers lived on `Harness`.
"""

from __future__ import annotations

import pytest

from harness.scenarios.skill_remote import SkillRemoteScenario


@pytest.fixture(scope="session")
def harness() -> SkillRemoteScenario:
    """Bring up the skill-remote stack once per session; tear down at the end."""
    s = SkillRemoteScenario()
    s.up(build=True, timeout=600)
    yield s
    s.down()
