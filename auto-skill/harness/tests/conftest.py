"""Shared pytest fixtures for harness tests."""

from __future__ import annotations

import pytest

from harness.core import Harness


@pytest.fixture(scope="session")
def harness() -> Harness:
    """Session-scoped harness: starts on first use, tears down at session end."""
    h = Harness()
    h.up(build=True, timeout=600)
    yield h
    h.down()
