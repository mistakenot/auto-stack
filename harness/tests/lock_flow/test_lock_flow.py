"""lock-flow: two Workers on one host, the real binary, the real hook wiring.

This is the end-to-end re-proof of AC-1/2/3/6/7/9/11 (task 064). The Go tests
already pin every string and exit code at the package and command level; what
only this scenario proves is that the pieces are actually wired together in a
built binary — `auto hooks install` puts the fire command where the guard is
looked for, `auto hooks fire --agent claude` reaches `lock.Evaluate` with the
cwd and file path a real Claude Code payload carries, and the store that
`auto lock take` writes is the store the guard reads.

Every test starts from an empty store (see conftest), so the tests are
independent; the order below simply follows the user journey.
"""

from __future__ import annotations

import json

from harness.scenarios.lock_flow import (
    CLAUDE_FIRE_COMMAND,
    GH_STATE_FILE,
    GROUP,
    GROUP_DESCRIPTION,
    LOCKED_FILE,
    UNLOCKED_FILE,
    WORKER_A,
    WORKER_B,
    WORKSPACE,
)

# ── topology ─────────────────────────────────────────────────────────────────


def test_one_host_one_project_hook_on_pretooluse(lock_flow):
    """One host id, one registered project, the enforcing hook wired (D-11, D-13).

    Two containers would be two *hosts* (the harness seeds a distinct HOST_ID
    per container) and the lock store is host-global, so the topology itself
    is the thing to assert — and so is the PreToolUse entry, because without
    it the guard never runs and every deny below would fail for the wrong
    reason.
    """
    host = json.loads(lock_flow.run("host", 'cat "$HOME/.auto/host.json"').stdout)
    assert host["hostId"] == "lock-host"

    registry = json.loads(lock_flow.run("host", 'cat "$HOME/.auto/projects.json"').stdout)
    projects = [p for p in registry["projects"] if p["path"] == WORKSPACE]
    assert len(projects) == 1, f"{WORKSPACE} must be registered exactly once; registry: {registry}"

    # The store keys locks on the registered project id, so both Workers must
    # resolve the workspace to that same id — or they would never contend.
    for worker in (WORKER_A, WORKER_B):
        assert lock_flow.lock_status(worker)["project"] == projects[0]["id"], worker

    settings = json.loads(lock_flow.run("host", f"cat {WORKSPACE}/.claude/settings.json").stdout)
    commands = [
        handler.get("command")
        for group in settings["hooks"]["PreToolUse"]
        for handler in group.get("hooks", [])
    ]
    assert CLAUDE_FIRE_COMMAND in commands, settings
    assert lock_flow.claude_hook_installed()


def test_status_lists_the_configured_group_unheld(lock_flow):
    """A list command with no filters returns every configured Group, held or not."""
    st = lock_flow.lock_status(WORKER_A)
    assert [g["name"] for g in st["groups"]] == [GROUP], st
    group = st["groups"][0]
    assert group["description"] == GROUP_DESCRIPTION, group
    assert group["globs"] == ["db/schema/**", "db/migrations/**"], group
    assert group["held"] is False and group["held_by_you"] is False, group
    assert st["locks"] == [], st
    assert st["worker"] == {"kind": "override", "host": "lock-host", "branch": "main", "worker_id": WORKER_A}, st


# ── the guard ────────────────────────────────────────────────────────────────


def test_files_outside_every_glob_are_allowed(lock_flow):
    """AC-4 at the wire: an edit that matches no Group is silent, even bare.

    Silence is the allow verdict, so a hook that had anything to say here —
    a stray nudge, a stray hint — would be a real failure, not noise.
    """
    lock_flow.assert_allowed(WORKER_A, UNLOCKED_FILE)
    lock_flow.assert_allowed(WORKER_B, UNLOCKED_FILE)
    lock_flow.assert_allowed(None, UNLOCKED_FILE)

    # Not an edit tool: the guard has nothing to match (documented Bash gap).
    bash = {
        "hook_event_name": "PreToolUse",
        "tool_name": "Bash",
        "session_id": "harness-lock-flow",
        "cwd": WORKSPACE,
        "tool_input": {"command": f"sed -i s/a/b/ {LOCKED_FILE}"},
    }
    assert lock_flow.fire(WORKER_A, bash).stdout.strip() == ""


def test_first_edit_with_no_lock_is_hard_blocked(lock_flow):
    """AC-1: a lockable file with no holder is denied with why + the take command.

    The reason is what the agent reads and acts on, so it must carry the
    Group's description verbatim and the exact command to run next.
    """
    reason = lock_flow.assert_denied(WORKER_A, LOCKED_FILE)
    assert f'{LOCKED_FILE} is under a serial-update lock group "{GROUP}"' in reason, reason
    assert GROUP_DESCRIPTION in reason, reason
    assert f"auto lock take {GROUP}" in reason, reason
    assert "auto lock release" in reason, reason

    # Both globs of the Group are enforced, not just the first.
    migration = lock_flow.assert_denied(WORKER_B, "db/migrations/0002_orders.sql")
    assert f'db/migrations/0002_orders.sql is under a serial-update lock group "{GROUP}"' in migration

    # And the guard never touches the store on a deny: nothing was taken.
    assert lock_flow.read_store()["locks"] == []


def test_bare_worker_on_a_lockable_file_is_denied_with_remediation(lock_flow):
    """AC-6 / D-6: the main checkout with no pane and no override cannot lock.

    The container is exactly that checkout when AUTO_LOCK_WORKER is unset, so
    this is the real bare case, not a simulated one. The guard blocks rather
    than failing open — two indistinguishable agents must never both believe
    they hold the lock — and the CLI gives the same remediation.
    """
    reason = lock_flow.assert_denied(None, LOCKED_FILE)
    assert f'{LOCKED_FILE} is under a serial-update lock group "{GROUP}"' in reason, reason
    assert "cannot be identified" in reason, reason
    assert "linked git worktree" in reason, reason
    assert "AUTO_LOCK_WORKER=<name>" in reason, reason
    assert f"auto lock take {GROUP}" in reason, reason

    bare_take = lock_flow.lock(None, "take", GROUP)
    assert not bare_take.ok, f"a bare take succeeded:\n{bare_take.stdout}"
    assert bare_take.stdout.strip() == "", bare_take.stdout
    assert "AUTO_LOCK_WORKER" in bare_take.stderr, bare_take.stderr
    assert lock_flow.read_store()["locks"] == []


# ── take, hold, contend ──────────────────────────────────────────────────────


def test_holder_edits_and_the_other_worker_is_blocked(lock_flow):
    """AC-2, AC-3, AC-6 (override rung): take → holder allowed, other denied.

    Both Workers run in the same workspace on the same host; only
    AUTO_LOCK_WORKER tells them apart, which is the override rung of the
    identity chain (D-1 step 1) doing its job end to end.
    """
    taken = lock_flow.take(WORKER_A, GROUP, "--reason", "add orders table")
    assert taken["group"] == GROUP, taken
    assert taken["holder"]["kind"] == "override", taken
    assert taken["holder"]["worker_id"] == WORKER_A, taken
    assert taken["holder"]["host"] == "lock-host", taken
    assert taken["reason"] == "add orders table", taken
    assert taken["taken_at"], taken

    # Taking again as the holder is idempotent, not an error.
    again = lock_flow.take(WORKER_A, GROUP)
    assert again["holder"]["worker_id"] == WORKER_A, again

    # The holder edits freely; the other Worker is told who, why and what next.
    lock_flow.assert_allowed(WORKER_A, LOCKED_FILE)
    lock_flow.assert_allowed(WORKER_A, "db/migrations/0002_orders.sql")

    reason = lock_flow.assert_denied(WORKER_B, LOCKED_FILE)
    assert f'"{GROUP}" is locked by worker {WORKER_A} ("add orders table")' in reason, reason
    assert "on host lock-host" in reason, reason
    # An override holder maps to no PR, so the recovery path is its own
    # release or an explicitly confirmed --force — never a merge check.
    assert "no PR to verify" in reason, reason
    assert "auto lock release" in reason, reason
    assert f"auto lock clear {GROUP} --force" in reason, reason

    # take by the other Worker exits non-zero naming the holder, stdout empty.
    contended = lock_flow.lock(WORKER_B, "take", GROUP)
    assert not contended.ok, f"take by {WORKER_B} succeeded while {WORKER_A} holds:\n{contended.stdout}"
    assert contended.stdout.strip() == "", contended.stdout
    assert f"held by worker {WORKER_A}" in contended.stderr, contended.stderr
    assert "--force" in contended.stderr, contended.stderr

    # status is per Worker: held_by_you flips with AUTO_LOCK_WORKER.
    mine = lock_flow.group_status(WORKER_A)
    assert mine["held"] is True and mine["held_by_you"] is True, mine
    assert mine["holder"]["worker_id"] == WORKER_A and mine["reason"] == "add orders table", mine
    theirs = lock_flow.group_status(WORKER_B)
    assert theirs["held"] is True and theirs["held_by_you"] is False, theirs
    assert theirs["holder"]["worker_id"] == WORKER_A, theirs

    # The store holds exactly one lock for the pair, never two.
    assert [l["holder"]["worker_id"] for l in lock_flow.read_store()["locks"]] == [WORKER_A]


def test_release_hands_the_group_to_the_next_worker(lock_flow):
    """AC-7: take → release → the other Worker takes and edits; the first is blocked.

    release with no group frees everything the Worker holds on the project and
    nothing anyone else holds; releasing nothing is a success with 0.
    """
    lock_flow.take(WORKER_A, GROUP, "--reason", "add orders table")
    lock_flow.assert_denied(WORKER_B, LOCKED_FILE)

    released = lock_flow.release(WORKER_A)
    assert released["released"] == 1, released
    assert released["worker"]["worker_id"] == WORKER_A, released
    assert lock_flow.read_store()["locks"] == []

    # Nothing held: the file is lockable-but-unheld again for everyone.
    reason = lock_flow.assert_denied(WORKER_A, LOCKED_FILE)
    assert f"auto lock take {GROUP}" in reason, reason

    taken = lock_flow.take(WORKER_B, GROUP, "--reason", "rename users column")
    assert taken["holder"]["worker_id"] == WORKER_B, taken
    lock_flow.assert_allowed(WORKER_B, LOCKED_FILE)
    reason = lock_flow.assert_denied(WORKER_A, LOCKED_FILE)
    assert f'"{GROUP}" is locked by worker {WORKER_B} ("rename users column")' in reason, reason

    # A Worker can only release its own: worker-a releasing frees nothing.
    assert lock_flow.release(WORKER_A)["released"] == 0
    assert lock_flow.group_status(WORKER_B)["held_by_you"] is True

    # Releasing a named group works too, and a second release is a no-op.
    assert lock_flow.release(WORKER_B, GROUP)["released"] == 1
    assert lock_flow.release(WORKER_B, GROUP)["released"] == 0
    assert lock_flow.read_store()["locks"] == []


# ── clear ────────────────────────────────────────────────────────────────────


def test_clear_of_an_override_holder_needs_force_and_is_audited(lock_flow):
    """AC-9, the holder every Worker in this container is: no PR → --force only.

    An override holder has no branch and so no PR to verify, so a plain clear
    refuses without consulting gh and leaves the store untouched. --force
    clears, says so in the JSON, and writes a `cleared` audit entry naming both
    the holder and the Worker who forced it.
    """
    lock_flow.take(WORKER_B, GROUP, "--reason", "rename users column")

    refused = lock_flow.clear(WORKER_A, GROUP)
    assert not refused.ok, f"clear of an override holder succeeded without --force:\n{refused.stdout}"
    assert refused.stdout.strip() == "", refused.stdout
    assert "no PR to verify" in refused.stderr, refused.stderr
    assert "not cleared" in refused.stderr, refused.stderr
    assert "--force" in refused.stderr, refused.stderr
    assert lock_flow.gh_calls() == [], "gh was consulted for a holder that has no branch"

    # A refusal never writes the store: worker-b still holds and still edits.
    assert lock_flow.group_status(WORKER_B)["held_by_you"] is True
    lock_flow.assert_allowed(WORKER_B, LOCKED_FILE)
    assert lock_flow.read_store()["audit"] == []

    forced = lock_flow.clear(WORKER_A, GROUP, "--force")
    assert forced.ok, f"clear --force exited {forced.exit_code}: {forced.stderr}"
    payload = json.loads(forced.stdout)
    assert payload["cleared"] is True and payload["forced"] is True, payload
    assert payload["group"] == GROUP, payload
    assert payload["holder"]["kind"] == "override" and payload["holder"]["worker_id"] == WORKER_B, payload
    assert "pr" not in payload and "state" not in payload, payload

    store = lock_flow.read_store()
    assert store["locks"] == [], store
    cleared = [a for a in store["audit"] if a["action"] == "cleared"]
    assert len(cleared) == 1, store["audit"]
    assert cleared[0]["group"] == GROUP, cleared
    assert cleared[0]["forced"] is True, cleared
    assert cleared[0]["holder"]["worker_id"] == WORKER_B, cleared
    assert cleared[0]["by"]["worker_id"] == WORKER_A, cleared

    # And the Group is unheld again: the former holder is now blocked-as-unheld.
    reason = lock_flow.assert_denied(WORKER_B, LOCKED_FILE)
    assert f"auto lock take {GROUP}" in reason, reason
    assert WORKER_A not in reason, reason


def test_clear_is_merge_verified_through_gh(lock_flow):
    """AC-9 proper: a worktree holder's PR is looked up through gh.

    The container has no linked worktrees, so a worktree holder is seeded into
    the store the way the Go tests seed one (the path exists, so it is live).
    The gh on PATH is the scenario's stub, answering from a file: OPEN refuses
    and names the state, MERGED clears without --force and records PR + state
    in both the JSON and the audit.
    """
    lock_flow.seed_worktree_holder("feat/orders", reason="adding orders table", pr="42")

    # The other Worker is shown the branch, the PR and the merge-verified path.
    reason = lock_flow.assert_denied(WORKER_A, LOCKED_FILE)
    assert f'"{GROUP}" is locked by branch feat/orders (PR #42, "adding orders table")' in reason, reason
    assert "taken 2026-08-31 14:02 on host lock-host" in reason, reason
    assert f"Wait for that PR to merge, then:  auto lock clear {GROUP}" in reason, reason
    assert "clear refuses unless PR #42 is merged" in reason, reason

    contended = lock_flow.lock(WORKER_A, "take", GROUP)
    assert not contended.ok and "held by branch feat/orders" in contended.stderr, contended.stderr
    assert f"auto lock clear {GROUP}" in contended.stderr, contended.stderr

    # PR still open: refused, store untouched, and gh was asked about the branch.
    lock_flow.set_pr_state(42, "OPEN")
    refused = lock_flow.clear(WORKER_A, GROUP)
    assert not refused.ok, f"clear with an open PR succeeded:\n{refused.stdout}"
    assert refused.stdout.strip() == "", refused.stdout
    for want in ("feat/orders", "PR #42 is still OPEN", "not cleared", "--force"):
        assert want in refused.stderr, refused.stderr
    calls = lock_flow.gh_calls()
    assert len(calls) == 1 and "pr list --head feat/orders" in calls[0], calls
    assert [l["holder"]["branch"] for l in lock_flow.read_store()["locks"]] == ["feat/orders"]
    assert lock_flow.read_store()["audit"] == []

    # No PR at all is a refusal too — not a silent clear.
    lock_flow.run("host", f"rm -f {GH_STATE_FILE}")
    no_pr = lock_flow.clear(WORKER_A, GROUP)
    assert not no_pr.ok and "no PR found for holder branch feat/orders" in no_pr.stderr, no_pr.stderr
    assert "--force" in no_pr.stderr, no_pr.stderr

    # Merged: cleared without --force, PR and state recorded, audited.
    lock_flow.set_pr_state(42, "MERGED")
    cleared = lock_flow.clear(WORKER_A, GROUP)
    assert cleared.ok, f"clear of a merged PR exited {cleared.exit_code}: {cleared.stderr}"
    payload = json.loads(cleared.stdout)
    assert payload["cleared"] is True and payload["forced"] is False, payload
    assert payload["pr"] == "42" and payload["state"] == "MERGED", payload
    assert payload["holder"]["kind"] == "worktree" and payload["holder"]["branch"] == "feat/orders", payload

    store = lock_flow.read_store()
    assert store["locks"] == [], store
    audit = [a for a in store["audit"] if a["action"] == "cleared"]
    assert len(audit) == 1, store["audit"]
    assert audit[0]["holder"]["branch"] == "feat/orders", audit
    assert audit[0]["by"]["worker_id"] == WORKER_A, audit
    assert audit[0]["pr"] == "42" and audit[0]["state"] == "MERGED", audit
    assert audit[0].get("forced", False) is False, audit

    # The Group is free: the next take succeeds and its holder edits.
    assert lock_flow.take(WORKER_A, GROUP)["holder"]["worker_id"] == WORKER_A
    lock_flow.assert_allowed(WORKER_A, LOCKED_FILE)


# ── doctor ───────────────────────────────────────────────────────────────────


def test_doctor_reports_enforceable(lock_flow):
    """AC-11 / D-13: doctor passes only the Claude hook; Codex is warn, never pass.

    The setup the entrypoint built is exactly what doctor is for — and this is
    the same `.claude/settings.json` the guard tests above enforced through,
    so a pass here is backed by observed denies, not just by a string match.
    """
    checks = lock_flow.doctor_checks(WORKER_A)
    assert set(checks) == {"claude-hook", "codex-hook", "config", "identity", "store", "gh"}, checks

    assert checks["claude-hook"]["status"] == "pass", checks["claude-hook"]
    assert CLAUDE_FIRE_COMMAND in checks["claude-hook"]["message"], checks["claude-hook"]

    # `auto hooks install` wired Codex too, and doctor still cannot vouch for it.
    assert checks["codex-hook"]["status"] == "warn", checks["codex-hook"]
    assert "not verifiable" in checks["codex-hook"]["message"], checks["codex-hook"]

    assert checks["config"]["status"] == "pass", checks["config"]
    assert GROUP in checks["config"]["message"], checks["config"]

    assert checks["identity"]["status"] == "pass", checks["identity"]
    assert f"worker {WORKER_A}" in checks["identity"]["message"], checks["identity"]
    assert "kind override" in checks["identity"]["message"], checks["identity"]

    assert checks["store"]["status"] == "pass", checks["store"]
    assert checks["gh"]["status"] == "pass", checks["gh"]
    assert "/usr/local/bin/gh" in checks["gh"]["message"], checks["gh"]

    # Bare is a warning with the D-6 remediation, and doctor still exits 0.
    bare = lock_flow.doctor_checks(None)
    assert bare["identity"]["status"] == "warn", bare["identity"]
    assert "AUTO_LOCK_WORKER" in bare["identity"]["hint"], bare["identity"]
    assert bare["claude-hook"]["status"] == "pass", bare["claude-hook"]


def test_doctor_text_mode_is_readable_not_json(lock_flow):
    """--text: passes first, then warnings, each warning with its hint, no JSON."""
    r = lock_flow.doctor(WORKER_A, "--text")
    assert r.ok, f"doctor --text exited {r.exit_code}: {r.stderr}"
    lines = [line for line in r.stdout.splitlines() if line.strip()]
    assert lines and lines[0].startswith("✓ "), r.stdout
    assert "! codex-hook" in r.stdout, r.stdout
    assert "hint:" in r.stdout, r.stdout
    assert "{" not in r.stdout, r.stdout
    warned_at = r.stdout.index("!")
    assert "\n✓" not in r.stdout[warned_at:], f"a pass printed after a warning:\n{r.stdout}"


def test_hook_fire_never_breaks_the_agent(lock_flow):
    """The guard's fail-open contract at the wire: exit 0 in every state.

    `fire` already asserts the exit code on every call above; this pins the
    states explicitly — unheld deny, self-held allow, other-held deny — plus a
    payload that is not even JSON, which must be silent rather than fatal.
    """
    assert lock_flow.fire_edit(WORKER_A, LOCKED_FILE).exit_code == 0
    lock_flow.take(WORKER_A, GROUP)
    assert lock_flow.fire_edit(WORKER_A, LOCKED_FILE).exit_code == 0
    assert lock_flow.fire_edit(WORKER_B, LOCKED_FILE).exit_code == 0

    garbage = lock_flow._sh("printf '{not json' | auto hooks fire --agent claude", {"AUTO_LOCK_WORKER": WORKER_B})
    assert garbage.ok, f"a malformed payload broke the hook: {garbage.stderr}"
    assert garbage.stdout.strip() == "", garbage.stdout
