"""mail-flow: two agents on one host (AC-13, walking-skeleton baseline).

This phase stands the oracle up. It proves the topology D-062-1 specifies —
ONE container, one host id, two registered workspaces — and that `auto mail` is
mounted and usable from each workspace against an initialised alpha store. The
C1 subscribe/send/list/ack loop lands here as the following phases implement it,
so every one of them is verified inside a scenario that already runs.
"""

from __future__ import annotations

import json

from harness.scenarios.mail_flow import NUDGE_COMMAND, STORE_PATH, WORKSPACE_A, WORKSPACE_B


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


# ── the C1 loop ──────────────────────────────────────────────────────────────
#
# These run after the baseline tests above, which assert that `auto mail list` is
# empty from both workspaces. They leave the store with acked mail only, so the
# baseline still holds on a second run against a live stack.


def test_c1_loop_one_message_travels_and_is_acked(mail_flow):
    """The epic's transcript, end to end, against the real binary (AC-2/AC-13).

    project-a subscribes to its own reply address **first**: that is what makes
    `from` resolve by rung 2 of the ladder with no `--from` flag, which is the
    shape C1 shows — and it is also what makes the sender reachable for a reply.
    """
    reply_to = mail_flow.subscribe("a", "auto-stack/reviewer")
    assert reply_to["address"] == "auto-stack/reviewer"
    assert reply_to["subscription"].startswith("sub_"), reply_to
    assert reply_to["backfilled"] == 0, reply_to

    inbox = mail_flow.subscribe("b", "auto-web/bugs")
    assert inbox["address"] == "auto-web/bugs"
    assert inbox["subscription"] != reply_to["subscription"], (
        "two agents on one host must get two subscriptions, not one shared row"
    )

    text = "normalizeRemote drops the port on ssh:// URLs"
    sent = mail_flow.send("a", "auto-web/bugs", text)
    assert sent["to"] == "auto-web/bugs"
    assert sent["id"], sent
    # The two counts mean different things to a sender and are asserted apart.
    assert sent["subscriptions"] == 1, sent
    assert sent["bound"] == 1, sent

    # Presence of the id, never a count: G4 permits duplicates.
    delivered = mail_flow.await_mail("b", sent["id"])
    assert delivered["from"] == "auto-stack/reviewer", delivered
    assert delivered["body"]["message"] == text, delivered
    assert delivered["sentAt"], delivered

    # Reading does not retire (G3): the same mail is still there.
    assert sent["id"] in {d["id"] for d in mail_flow.list_mail("b")}

    acked = mail_flow.ack("b", sent["id"])
    assert acked["id"] == sent["id"]
    assert acked["acked"] is True, acked
    assert acked["wonTransition"] is True, acked

    # And acked mail is gone from the default view.
    mail_flow.await_no_mail("b", sent["id"])

    # The sender never sees the mail it sent — a subscription is a reader of one
    # address, not a copy of the whole store.
    assert sent["id"] not in {d["id"] for d in mail_flow.list_mail("a")}


def test_ack_is_explicit_and_transitions_exactly_once(mail_flow):
    """G3 and G13 at the real command surface.

    Listing twice returns the same mail twice, and a second ack still exits 0
    while reporting that it did not win the transition (D-062-7): losing a race
    is a correct outcome, not invalid usage.
    """
    address = "auto-web/acks"
    mail_flow.subscribe("b", address)
    sent = mail_flow.send("a", address, "ack me twice")

    mail_flow.await_mail("b", sent["id"])
    assert sent["id"] in {d["id"] for d in mail_flow.list_mail("b")}, (
        "the second list dropped the mail; reading must never retire it"
    )

    won = mail_flow.ack("b", sent["id"])
    assert won["wonTransition"] is True, won

    lost = mail_flow.mail("b", "ack", sent["id"])
    assert lost.ok, f"a losing ack exited {lost.exit_code}: {lost.stderr}"
    payload = json.loads(lost.stdout)
    assert payload["acked"] is True, payload
    assert payload["wonTransition"] is False, payload
    assert "already acked" in lost.stderr, lost.stderr

    mail_flow.await_no_mail("b", sent["id"])


def test_send_with_no_subscription_persists_for_a_later_subscriber(mail_flow):
    """G6 and D-10's J2 half: mail outlives the absence of a reader.

    The send exits 0 and warns on stderr rather than failing — free-form
    addresses (D-9) make a typo possible, and a note is the mitigation the
    design chose over narrowing the namespace.
    """
    address = "auto-web/nobody-yet"
    result = mail_flow.mail("a", "send", "--to", address, "--message", "hello?")
    assert result.ok, result.stderr
    sent = json.loads(result.stdout)
    assert sent["subscriptions"] == 0, sent
    assert sent["bound"] == 0, sent
    assert "typo" in result.stderr, result.stderr

    late = mail_flow.subscribe("b", address)
    assert late["backfilled"] >= 1, late
    delivered = mail_flow.await_mail("b", sent["id"], "--address", address)
    assert delivered["body"]["message"] == "hello?", delivered

    mail_flow.ack("b", sent["id"])
    mail_flow.await_no_mail("b", sent["id"])


def test_from_now_opts_out_of_the_backlog(mail_flow):
    """D-10's opt-out: --from-now starts the cursor at the high-water mark."""
    address = "auto-web/from-now"
    sent = mail_flow.send("a", address, "before anyone was listening")

    late = mail_flow.subscribe("b", address, "--from-now")
    assert late["backfilled"] == 0, late
    assert sent["id"] not in {d["id"] for d in mail_flow.list_mail("b", "--address", address)}

    # But mail sent afterwards does arrive on that same subscription.
    after = mail_flow.send("a", address, "after subscribing")
    delivered = mail_flow.await_mail("b", after["id"], "--address", address)
    assert delivered["body"]["message"] == "after subscribing", delivered

    mail_flow.ack("b", after["id"])
    mail_flow.await_no_mail("b", after["id"], "--address", address)


def test_addresses_carry_no_physical_identity(mail_flow):
    """G5: an address names a channel, never a machine, a pane or a session.

    The container has one host id and no tmux, so the assertion here is that
    neither appears in what a reader is handed — the store-level version of this
    lives in the package tests.
    """
    address = "auto-web/virtual"
    mail_flow.subscribe("b", address)
    sent = mail_flow.send("a", address, "no physical identity here")
    delivered = mail_flow.await_mail("b", sent["id"], "--address", address)

    rendered = json.dumps(delivered)
    for physical in ("mail-host", "/workspace/project-a", "tmux", "%0"):
        assert physical not in rendered, (
            f"the delivered mail carries the physical identity {physical!r}: {rendered}"
        )

    mail_flow.ack("b", sent["id"])
    mail_flow.await_no_mail("b", sent["id"], "--address", address)


# ── the in-band nudge ────────────────────────────────────────────────────────
#
# The riskiest integration in the walking skeleton, and the reason it is inside
# it rather than deferred: hook → notification, in-band, at stat cost, without
# breaking the agent's turn. These run against the real `auto hooks fire`, so
# they exercise the same entry point an agent's hook does.


def test_hook_nudges_a_working_agent_that_has_mail(mail_flow):
    """AC-10: after a send, project-b's next hook fire tells it to read its mail.

    The nudge is what closes the loop between "mail exists" and "an agent knows
    it does" with no daemon anywhere in the path — and it is silent again once
    the mail is acked, which is what makes it a notification rather than a
    permanent banner.
    """
    address = "auto-web/nudge"
    mail_flow.subscribe("b", address)

    # Nothing waiting yet, so the hook has nothing to say.
    mail_flow.assert_no_nudge("b")

    sent = mail_flow.send("a", address, "you have mail")

    context = mail_flow.assert_nudge("b")
    assert NUDGE_COMMAND in context, context
    assert "auto mail ack" in context, context

    # The nudge carries no mailbox content (G14): it says only "go and read".
    for content in ("you have mail", address, sent["id"], "auto-stack/reviewer"):
        assert content not in context, (
            f"the nudge interpolated mailbox content {content!r}: {context!r}"
        )

    # The sender is not nudged about the mail it sent — the flag is per binding.
    assert mail_flow.nudge_context("a") == ""

    # Reading does not retire mail (G3), so the nudge survives a list.
    mail_flow.await_mail("b", sent["id"], "--address", address)
    assert NUDGE_COMMAND in mail_flow.nudge_context("b")

    mail_flow.ack("b", sent["id"])
    mail_flow.await_no_mail("b", sent["id"], "--address", address)

    # And once the mailbox is empty the hook goes quiet again.
    mail_flow.assert_no_nudge("b")


def test_hook_fire_never_breaks_the_agent(mail_flow):
    """AC-10's other half: the mail check can never stall or break the hook.

    Both states are fired — mail waiting and none — and both must exit 0 with
    stdout that is either empty or exactly one JSON object. `fire_hook` asserts
    the exit code; `nudge_context` asserts the shape.
    """
    address = "auto-web/never-breaks"
    mail_flow.subscribe("b", address)

    assert mail_flow.fire_hook("b").exit_code == 0
    assert mail_flow.nudge_context("b") == ""

    sent = mail_flow.send("a", address, "still exits zero")
    assert mail_flow.fire_hook("b").exit_code == 0
    assert NUDGE_COMMAND in mail_flow.assert_nudge("b")

    mail_flow.ack("b", sent["id"])
    mail_flow.await_no_mail("b", sent["id"], "--address", address)
    mail_flow.assert_no_nudge("b")
