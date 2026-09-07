"""mail-flow: `#parent`, the handle a Subagent has instead of an address (J1).

Phase 1's walking skeleton, end to end against the real binary: the hook writes
down which Subagent is acting, `auto mail send --to '#parent'` reads that back
and resolves the supervisor's absolute address, and the supervisor is nudged,
lists and acks. The container cannot run Claude Code, so the *provenance* of the
hook payload is simulated — every other part of the path is the product's own.

Each test runs in a **directory of its own** rather than in one of the two
registered workspaces, and that is load-bearing rather than tidy. `#parent`
resolves through the same lookup as the from-ladder's rung 2: the address of the
*first* subscription created under the caller's binding. On-disk state is
session-scoped and shared across this module, so a test sharing a workspace with
an earlier one would resolve to that test's address and fail for a reason that
has nothing to do with handles. With no tmux in the container the binding falls
to its cwd rung, so a fresh directory is a fresh agent — which is also, exactly,
what makes the supervisor and its Subagent one agent here.
"""

from __future__ import annotations

from harness.scenarios.mail_flow import NUDGE_COMMAND, WORKSPACE_A

#: The supervisor's own directory, and therefore its own binding.
SUPERVISOR = f"{WORKSPACE_A}/j1-supervisor"
#: A directory deliberately left unmarked, so the refusal is about the absence
#: of a Subagent rather than about the order the tests happened to run in.
NO_SUBAGENT = f"{WORKSPACE_A}/j1-no-subagent"
#: A supervisor whose only Subagent stops mid-test, so the refusal is about the
#: marker's retirement rather than about it never having been written.
STOPPED = f"{WORKSPACE_A}/j1-stopped"
#: A supervisor running two Subagents, one of which stops.
SIBLINGS = f"{WORKSPACE_A}/j1-siblings"

SUPERVISOR_ADDRESS = "auto-stack/supervisor"
SUBAGENT_ID = "harness-subagent-parent-handle"


def _agent_dir(mail_flow, path: str) -> str:
    """Make a directory and return it, for use as an agent key.

    `Scenario.workspace` passes a literal path straight through, so any
    directory is addressable as an agent — which is the only seam needed to give
    a test its own binding. No product change and no scenario change: the cwd
    rung already means what this relies on (D-062-2).
    """
    made = mail_flow.run("host", f"mkdir -p {path} && echo ok")
    assert made.ok, f"could not create {path}: {made.stderr or made.stdout}"
    return path


def test_parent_is_refused_when_no_subagent_is_acting(mail_flow):
    """AC-3: the guard, at the real command surface.

    The refusal is what makes the handle safe to offer. A process that cannot
    tell whether it is a Subagent must not be allowed to act as one — and this
    caller holds a live subscription of its own, which is precisely the state
    that must *not* be enough to make `#parent` resolve.
    """
    agent = _agent_dir(mail_flow, NO_SUBAGENT)
    mail_flow.subscribe(agent, "auto-web/not-a-supervisor")

    result = mail_flow.mail(agent, "send", "--to", "#parent", "--message", "hello?")

    assert result.exit_code == 1, (
        f"send --to '#parent' from a non-Subagent exited {result.exit_code}, want 1; "
        f"stdout: {result.stdout!r} stderr: {result.stderr!r}"
    )
    assert result.stdout.strip() == "", (
        "stdout must be empty on a hard error — in JSON mode it carries parseable "
        f"payload data only, and a caller must never be handed half of one: {result.stdout!r}"
    )
    # The three things the message owes a reader: the constraint, an absolute
    # alternative, and where to read more.
    for fragment in ("#parent", "Subagent", "auto mail docs"):
        assert fragment in result.stderr, (
            f"the refusal does not mention {fragment!r}: {result.stderr!r}"
        )


def test_j1_a_subagent_mails_its_supervisor_by_handle(mail_flow):
    """J1 end to end (AC-1, AC-2, AC-5).

    The supervisor subscribes; the hook records a Subagent acting under the same
    binding; the Subagent sends to `#parent` knowing no address at all; the
    supervisor is nudged in-band, lists the mail with the resolved absolute
    `from`, and acks it. Observation is on **presence of the mail id** with
    bounded retry — mail is at-least-once and unordered (G4), so a duplicate
    must not fail an assertion a count would.
    """
    agent = _agent_dir(mail_flow, SUPERVISOR)
    subscription = mail_flow.subscribe(agent, SUPERVISOR_ADDRESS)
    assert subscription["address"] == SUPERVISOR_ADDRESS
    mail_flow.assert_no_nudge(agent)

    # The hook is the whole of the Subagent's self-identification: it carries an
    # agent_id, and nothing else about the call changes.
    mail_flow.mark_subagent(agent, SUBAGENT_ID, agent_type="phase3")

    text = "phase 3 blocked: the fixture has no agent_id"
    sent = mail_flow.send(agent, "#parent", text)
    assert sent["to"] == SUPERVISOR_ADDRESS, (
        f"'#parent' resolved to {sent['to']!r}, want the supervisor's absolute address"
    )
    assert sent["resolvedFrom"] == "#parent", sent
    assert sent["subscriptions"] == 1, sent
    assert sent["bound"] == 1, sent

    # The supervisor learns about it the way any agent does: in-band, on its
    # next tool call, from a hook that opened no store.
    assert NUDGE_COMMAND in mail_flow.assert_nudge(agent)

    delivered = mail_flow.await_mail(agent, sent["id"])
    assert delivered["body"]["message"] == text, delivered
    assert not delivered["from"].startswith("#"), (
        f"a handle reached the stored envelope: {delivered!r}"
    )

    # Reading never retires (G3): the same mail is still there.
    assert sent["id"] in {d["id"] for d in mail_flow.list_mail(agent)}

    acked = mail_flow.ack(agent, sent["id"])
    assert acked["wonTransition"] is True, acked
    mail_flow.await_no_mail(agent, sent["id"])


def test_parent_is_refused_again_once_the_subagent_has_stopped(mail_flow):
    """AC-9's lifecycle, end to end: the handle is a statement about *now*.

    `#parent` resolving is the whole risk surface of this feature, so what makes
    it safe is not that it resolves for a Subagent but that it stops resolving
    the moment one is no longer acting. `SubagentStop` is the event that says
    so, and it is installed for both agents precisely so this holds.

    The resolve is asserted before the stop rather than only the refusal after
    it. A refusal on its own is what a marker that was never written looks like
    too, and that version of this test would pass against a completely broken
    bridge.
    """
    agent = _agent_dir(mail_flow, STOPPED)
    mail_flow.subscribe(agent, "auto-stack/supervisor-of-a-stopped-child")

    agent_id = "harness-subagent-that-stops"
    mail_flow.mark_subagent(agent, agent_id, agent_type="phase3")

    sent = mail_flow.send(agent, "#parent", "still working")
    assert sent["resolvedFrom"] == "#parent", sent

    mail_flow.stop_subagent(agent, agent_id, agent_type="phase3")

    result = mail_flow.mail(agent, "send", "--to", "#parent", "--message", "and now?")
    assert result.exit_code == 1, (
        f"send --to '#parent' after SubagentStop exited {result.exit_code}, want 1 — "
        "the marker is retired, so the caller can no longer be shown to be a Subagent; "
        f"stdout: {result.stdout!r} stderr: {result.stderr!r}"
    )
    assert result.stdout.strip() == "", (
        f"stdout must be empty on a hard error: {result.stdout!r}"
    )
    for fragment in ("#parent", "Subagent", "auto mail docs"):
        assert fragment in result.stderr, (
            f"the post-stop refusal does not mention {fragment!r}: {result.stderr!r}"
        )

    # And the marker is gone from disk, not merely disbelieved. A file that is
    # ignored but never unlinked is unbounded state on a long-lived host, and it
    # is the state the scenario's stand-up gate exists to catch.
    markers = mail_flow.run(
        "host",
        'if [ -d "$HOME/.auto/mail/alpha-agents" ]; then '
        'grep -rl "harness-subagent-that-stops" "$HOME/.auto/mail/alpha-agents" || true; '
        "else echo __absent__; fi",
    )
    assert agent_id not in markers.stdout, (
        f"the marker for {agent_id} survived SubagentStop: {markers.stdout!r}"
    )


def test_a_sibling_keeps_working_when_one_subagent_stops(mail_flow):
    """AC-9's per-agent clause, at the real command surface (D-063-9).

    One marker file per `agent_id` exists so that a sibling's `SubagentStop`
    cannot retire a Subagent that is still working. A single shared slot would
    pass every test above and still make `#parent` fail intermittently in the
    one situation this feature is for — a supervisor running several Subagents
    at once.
    """
    agent = _agent_dir(mail_flow, SIBLINGS)
    mail_flow.subscribe(agent, "auto-stack/supervisor-of-two")

    mail_flow.mark_subagent(agent, "harness-sibling-one", agent_type="phase3")
    mail_flow.mark_subagent(agent, "harness-sibling-two", agent_type="phase3")

    mail_flow.stop_subagent(agent, "harness-sibling-two", agent_type="phase3")

    # The survivor can still reach its supervisor. The recipient is resolved
    # from the Binding, which both siblings share, so stopping one must not
    # change the answer for the other.
    sent = mail_flow.send(agent, "#parent", "sibling two stopped; I am still here")
    assert sent["resolvedFrom"] == "#parent", sent
    assert sent["to"] == "auto-stack/supervisor-of-two", sent

    mail_flow.stop_subagent(agent, "harness-sibling-one", agent_type="phase3")
    result = mail_flow.mail(agent, "send", "--to", "#parent", "--message", "both gone")
    assert result.exit_code == 1, (
        f"with both siblings stopped, '#parent' still resolved (exit {result.exit_code}); "
        f"stdout: {result.stdout!r}"
    )
