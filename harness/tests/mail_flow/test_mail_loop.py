"""mail-flow: two agents on one host (AC-13, walking-skeleton baseline).

This phase stands the oracle up. It proves the topology D-062-1 specifies —
ONE container, one host id, two registered workspaces — and that `auto mail` is
mounted and usable from each workspace against an initialised alpha store. The
C1 subscribe/send/list/ack loop lands here as the following phases implement it,
so every one of them is verified inside a scenario that already runs.
"""

from __future__ import annotations

import json

from harness.scenarios.mail_flow import STORE_PATH, WORKSPACE_A, WORKSPACE_B


def test_single_host_two_registered_workspaces(mail_flow):
    """One host id, two projects — that is what "two agents on one host" means.

    Two containers would be two *hosts* (the harness seeds a distinct HOST_ID
    per container, which is exactly what makes event-flow multi-host), and the
    mail store is host-global, so the topology itself is the thing to assert.
    """
    host = json.loads(mail_flow.run("host", 'cat "$HOME/.auto/host.json"').stdout)
    assert host["hostId"] == "mail-host"

    registry = json.loads(mail_flow.run("host", 'cat "$HOME/.auto/projects.json"').stdout)
    paths = {p["path"] for p in registry["projects"]}
    assert WORKSPACE_A in paths, f"{WORKSPACE_A} not registered; registry: {registry}"
    assert WORKSPACE_B in paths, f"{WORKSPACE_B} not registered; registry: {registry}"

    ids = {p["id"] for p in registry["projects"]}
    assert len(ids) >= 2, f"the two workspaces must register as distinct projects: {registry}"


def test_store_carries_the_alpha_marker_in_its_filename(mail_flow):
    """G10 / D-2: the marker is in the artifact, not only in the prose."""
    listing = mail_flow.run("host", 'ls "$HOME/.auto/mail"')
    assert listing.ok, listing.stderr
    assert "alpha-store.db" in listing.stdout, listing.stdout

    present = mail_flow.run("host", f"test -f {STORE_PATH} && echo present")
    assert "present" in present.stdout


def test_list_is_empty_json_from_both_workspaces(mail_flow):
    """Both agents can read their (empty) mail with no daemon running (D-11).

    `auto mail list` with no filters returns everything the caller is bound to,
    per the project convention that a list command with no filters returns all
    items — which on a fresh store is `[]`.
    """
    for agent in ("a", "b"):
        result = mail_flow.mail(agent, "list")
        assert result.ok, f"agent {agent}: {result.stderr or result.stdout}"
        assert result.stdout.strip() == "[]", f"agent {agent}: {result.stdout!r}"
        # Pure JSON on stdout, nothing mixed in — parsing it is the assertion.
        assert mail_flow.list_mail(agent) == []


def test_docs_are_discoverable_from_a_workspace(mail_flow):
    """An agent must be able to discover the surface it is asked to use."""
    result = mail_flow.mail("a", "docs")
    assert result.ok, result.stderr
    assert "auto mail" in result.stdout
    assert "alpha-store.db" in result.stdout


def test_init_is_idempotent(mail_flow):
    """Re-running init against an initialised store is safe and reports so."""
    payload = mail_flow.mail_json("b", "init")
    assert payload["alpha"] is True
    assert payload["created"] is False, f"store reported as freshly created: {payload}"
    assert payload["store"].endswith("/.auto/mail/alpha-store.db"), payload
