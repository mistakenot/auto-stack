"""Shared pytest configuration for harness tests.

Each scenario owns a session-scoped up/down fixture in its own
`tests/<scenario>/conftest.py`, so bringing up one scenario's stack never starts
another's. This top-level conftest holds only cross-scenario configuration.
"""

from __future__ import annotations
