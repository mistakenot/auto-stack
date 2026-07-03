"""E2E regressions for the fuzz-campaign fixes (H1, H2, H3).

Each test drives the real Docker Compose harness (HTTPS transport, blobless
cache, git-archive extraction) exactly as a user would, and asserts the
post-fix behavior for the confirmed high-severity findings.

These tests use the `code-review` fixture skill (and `--as` clones of it) rather
than `deploy-checklist`, because another test in the session renames
`deploy-checklist` upstream in the shared fixture repo. `code-review` is never
mutated, so these tests are order-independent.
"""

from __future__ import annotations

from harness.scenarios.skill_remote import GIT_REMOTE_URL, SkillRemoteScenario as Harness

SKILL = "code-review"


def _init_workspace(harness: Harness, name: str) -> str:
    harness.fresh_workspace(name)
    ws = f"/workspace/{name}"
    r = harness.run("sut", f"cd {ws} && auto skill init --project -y")
    assert r.ok, f"init failed: {r.stderr}"
    r = harness.trust_source(ws)
    assert r.ok, f"trust failed: {r.stderr}"
    return ws


# ── H3: path traversal via targets in skills.yaml ────────────────────────────


def test_h3_target_traversal_is_refused(harness: Harness) -> None:
    """A `targets:` entry with `..` must NOT write outside the project root."""
    ws = _init_workspace(harness, "test-h3")

    r = harness.run(
        "sut", f"cd {ws} && auto skill add '{GIT_REMOTE_URL}' --skill {SKILL}"
    )
    assert r.ok, f"add failed: {r.stderr}"

    # Ensure no stale escape dir exists from a previous run.
    harness.run("sut", "rm -rf /tmp/h3-escape")

    # Rewrite skills.yaml with a traversal target. From
    # /workspace/test-h3/.auto/skills/, "../../../../tmp/h3-escape" escapes to
    # /tmp/h3-escape.
    skills_yaml = (
        "auto_update: true\\n"
        "targets:\\n"
        "  - ../../../../tmp/h3-escape\\n"
        "skills:\\n"
        f"  {SKILL}:\\n"
        "    version: latest\\n"
    )
    r = harness.run(
        "sut", f"printf '{skills_yaml}' > {ws}/.auto/skills/skills.yaml"
    )
    assert r.ok, f"could not write skills.yaml: {r.stderr}"

    r = harness.run("sut", f"cd {ws} && auto skill sync --text", timeout=120)
    assert not r.ok, (
        "sync must refuse a traversal target\n"
        f"stdout:\n{r.stdout}\nstderr:\n{r.stderr}"
    )

    # The critical assertion: nothing was written outside the project root.
    escaped = harness.run("sut", "ls /tmp/h3-escape 2>/dev/null; echo done")
    assert SKILL not in escaped.stdout, (
        f"files escaped the project root into /tmp/h3-escape: {escaped.stdout}"
    )


def test_h3_sync_reports_traversal_target(harness: Harness) -> None:
    """The refusal explains why instead of silently rendering."""
    ws = _init_workspace(harness, "test-h3-report")
    skills_yaml = (
        "auto_update: true\\n"
        "targets:\\n"
        "  - ../../../../tmp/h3-escape\\n"
    )
    harness.run("sut", f"printf '{skills_yaml}' > {ws}/.auto/skills/skills.yaml")
    r = harness.run("sut", f"cd {ws} && auto skill sync --text", timeout=120)
    combined = r.stdout + r.stderr
    assert "outside the project root" in combined or "invalid_target" in combined, (
        f"expected a traversal-target diagnostic, got:\n{combined}"
    )


# ── H1: add exits non-zero when its post-add sync fails ───────────────────────


def test_h1_add_exits_nonzero_on_sync_failure(harness: Harness) -> None:
    """When the post-add render of the just-added skill collides with a foreign
    target dir, the sync fails — and `add` must propagate a non-zero exit
    instead of reporting a clean success that masks the diverged state."""
    ws = _init_workspace(harness, "test-h1")

    # Plant a foreign (un-managed) dir where the skill will render.
    harness.run(
        "sut",
        f"mkdir -p {ws}/.claude/skills/{SKILL} && "
        f"echo 'foreign content' > {ws}/.claude/skills/{SKILL}/SKILL.md",
    )

    r = harness.run(
        "sut",
        f"cd {ws} && auto skill add '{GIT_REMOTE_URL}' --skill {SKILL}",
        timeout=120,
    )
    assert not r.ok, (
        "add must exit non-zero when its post-add sync fails\n"
        f"stdout:\n{r.stdout}\nstderr:\n{r.stderr}"
    )
    # The sync failure is reported on stderr; stdout stays the parseable payload.
    assert "sync error" in r.stderr, f"expected a sync error on stderr, got: {r.stderr}"


def test_h1_add_succeeds_when_unrelated_skill_is_broken(harness: Harness) -> None:
    """A scoped post-add render means adding a NEW skill is not failed by an
    UNRELATED pre-existing skill whose target dir is foreign."""
    ws = _init_workspace(harness, "test-h1-scope")

    # A foreign dir for a different name must not fail this add.
    harness.run(
        "sut",
        f"mkdir -p {ws}/.claude/skills/some-other && "
        f"echo foreign > {ws}/.claude/skills/some-other/SKILL.md",
    )
    r = harness.run(
        "sut",
        f"cd {ws} && auto skill add '{GIT_REMOTE_URL}' --skill {SKILL}",
        timeout=120,
    )
    assert r.ok, f"add of {SKILL} should succeed despite an unrelated foreign dir: {r.stderr}"


# ── H2: concurrent add operations must not lose an entry ──────────────────────


def test_h2_concurrent_add_keeps_both(harness: Harness) -> None:
    """Two concurrent `add` runs in one workspace must both persist in
    lock.json — the file lock serializes the read-modify-write, so neither
    writer's entry is lost to a TOCTOU race. Uses two `--as` clones of the same
    rename-safe fixture skill so the test does not depend on which other skills
    exist in the (possibly-mutated) shared fixture."""
    ws = _init_workspace(harness, "test-h2")

    harness.run(
        "sut",
        f"cd {ws} && ( "
        f"auto skill add '{GIT_REMOTE_URL}' --skill {SKILL} --as cr-one & "
        f"auto skill add '{GIT_REMOTE_URL}' --skill {SKILL} --as cr-two & "
        f"wait )",
        timeout=180,
    )
    # Exit codes of the backgrounded adds are not asserted (concurrent post-add
    # syncs may race on target writes); the invariant is the lock.
    r = harness.run("sut", f"cat {ws}/.auto/skills/lock.json")
    assert r.ok, f"lock.json unreadable: {r.stderr}"
    lock = r.json()
    names = set(lock.get("skills", {}).keys())
    assert {"cr-one", "cr-two"} <= names, (
        f"concurrent add lost an entry; lock has: {names}"
    )
