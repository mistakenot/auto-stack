"""End-to-end tests: init → add from HTTPS git server → sync → verify."""

from __future__ import annotations

from harness.scenarios.skill_remote import GIT_REMOTE_URL, SkillRemoteScenario as Harness


def _init_workspace(harness: Harness, name: str) -> str:
    """Create a fresh workspace and initialize auto-skill in it."""
    harness.fresh_workspace(name)
    ws = f"/workspace/{name}"
    r = harness.run("sut", f"cd {ws} && auto skill init --project -y")
    assert r.ok, f"init failed: {r.stderr}"
    r = harness.trust_source(ws)
    assert r.ok, f"trust failed: {r.stderr}"
    return ws


def test_clone_from_git_server(harness: Harness) -> None:
    """Verify the SUT can clone from the HTTPS git server."""
    r = harness.run("sut", f"git clone {GIT_REMOTE_URL} /tmp/clone-test")
    assert r.ok, f"git clone failed: {r.stderr}"

    r = harness.run("sut", "ls /tmp/clone-test/skills/")
    assert r.ok
    assert "deploy-checklist" in r.stdout


def test_init_project(harness: Harness) -> None:
    """Verify `auto skill init --project -y` sets up the project structure."""
    harness.fresh_workspace("test-init")
    r = harness.run("sut", "cd /workspace/test-init && auto skill init --project -y")
    assert r.ok, f"init --project failed: {r.stderr}"

    data = r.json()
    assert data["mode"] == "project"
    assert data["skills_yaml"]["created"] is True
    assert data["lock"]["created"] is True


def test_add_skill_from_remote(harness: Harness) -> None:
    """Verify `auto skill add` clones via HTTPS into the blobless cache."""
    ws = _init_workspace(harness, "test-add")

    r = harness.run(
        "sut",
        f"cd {ws} && auto skill add '{GIT_REMOTE_URL}' --skill deploy-checklist",
    )
    assert r.ok, f"add failed: {r.stderr}"

    data = r.json()
    assert len(data.get("added", [])) == 1
    assert data["added"][0]["name"] == "deploy-checklist"
    assert len(data["added"][0]["commit"]) == 40


def test_full_add_sync_renders_to_targets(harness: Harness) -> None:
    """Full flow: init → add → verify skill rendered into target dirs."""
    ws = _init_workspace(harness, "test-full-flow")

    r = harness.run(
        "sut",
        f"cd {ws} && auto skill add '{GIT_REMOTE_URL}' --skill deploy-checklist",
    )
    assert r.ok, f"add failed: {r.stderr}"

    # Verify lock.json has the skill with a real commit
    r = harness.run("sut", f"cat {ws}/.auto/skills/lock.json")
    assert r.ok
    lock = r.json()
    assert "deploy-checklist" in lock["skills"]
    entry = lock["skills"]["deploy-checklist"]
    assert entry["url"] == "https://git-server/repos/skills"
    assert len(entry["commit"]) == 40

    # Verify the skill was rendered into target dirs
    r = harness.run("sut", f"cat {ws}/.claude/skills/deploy-checklist/SKILL.md")
    assert r.ok, f"SKILL.md not rendered to .claude target: {r.stderr}"
    assert "Deploy Checklist" in r.stdout

    r = harness.run("sut", f"cat {ws}/.agents/skills/deploy-checklist/SKILL.md")
    assert r.ok, f"SKILL.md not rendered to .agents target: {r.stderr}"
    assert "Deploy Checklist" in r.stdout


def test_sync_check_reports_clean(harness: Harness) -> None:
    """After add+sync, `sync --check` should report everything in sync."""
    ws = _init_workspace(harness, "test-sync-check")

    harness.run(
        "sut",
        f"cd {ws} && auto skill add '{GIT_REMOTE_URL}' --skill deploy-checklist",
    )

    r = harness.run("sut", f"cd {ws} && auto skill sync --check")
    assert r.ok, f"sync --check failed (stale targets?): {r.stderr}"


def test_add_multiple_skills(harness: Harness) -> None:
    """Adding multiple skills from the same HTTPS source works."""
    ws = _init_workspace(harness, "test-multi")

    r = harness.run(
        "sut",
        f"cd {ws} && auto skill add '{GIT_REMOTE_URL}' "
        f"--skill deploy-checklist --skill code-review",
    )
    assert r.ok, f"add failed: {r.stderr}"

    data = r.json()
    names = {a["name"] for a in data.get("added", [])}
    assert "deploy-checklist" in names
    assert "code-review" in names


def _rename_skill_upstream(harness: Harness, old: str, new: str) -> None:
    """Push a commit to the git server that renames a skill directory
    and updates the frontmatter name to match."""
    script = (
        "cd /tmp && rm -rf rename-work && "
        "git clone /repos/skills.git rename-work && "
        "cd rename-work && "
        f"git mv skills/{old} skills/{new} && "
        f"sed -i 's/^name: {old}/name: {new}/' skills/{new}/SKILL.md && "
        f"git add skills/{new}/SKILL.md && "
        f"git commit -m 'rename {old} -> {new}' && "
        "git push origin main && "
        "cd /repos/skills.git && git update-server-info && "
        "rm -rf /tmp/rename-work"
    )
    r = harness.git_server_cmd(script)
    assert r.ok, f"upstream rename failed: {r.stderr}"


def test_upstream_rename_detected_and_remediated(harness: Harness) -> None:
    """Full rename scenario: add skill, rename upstream, sync detects it,
    then remediate with add-new + remove-old."""
    ws = _init_workspace(harness, "test-rename")

    # 1. Add deploy-checklist at the current commit
    r = harness.run(
        "sut",
        f"cd {ws} && auto skill add '{GIT_REMOTE_URL}' --skill deploy-checklist",
    )
    assert r.ok, f"initial add failed: {r.stderr}"

    r = harness.run("sut", f"cat {ws}/.claude/skills/deploy-checklist/SKILL.md")
    assert r.ok, "skill not rendered after add"

    # 2. Rename the skill directory upstream
    _rename_skill_upstream(harness, "deploy-checklist", "deployment-guide")

    # 3. Sync should fail — the old subpath no longer exists at the new commit
    r = harness.run("sut", f"cd {ws} && auto skill sync", timeout=120)
    assert not r.ok, "sync should have failed after upstream rename"
    combined = r.stdout + r.stderr
    assert "renamed or removed upstream" in combined, (
        f"expected RenamedUpstreamError message, got:\nstdout: {r.stdout}\nstderr: {r.stderr}"
    )

    # 4. Remediate: add the skill under its new name
    r = harness.run(
        "sut",
        f"cd {ws} && auto skill add '{GIT_REMOTE_URL}' --skill deployment-guide",
        timeout=120,
    )
    assert r.ok, f"add new name failed: {r.stderr}"

    # 5. Remove the stale old entry
    r = harness.run(
        "sut",
        f"cd {ws} && auto skill remove deploy-checklist --vendored",
    )
    assert r.ok, f"remove old name failed: {r.stderr}"
    remove_result = r.json()
    assert "vendored" in remove_result["removed"]

    # With the transactional-manifest carry-forward, deploy-checklist stays in
    # the manifest through step 3's failing sync (it is still declared in the
    # lock), so removing it now reconciles its stale target copies under the
    # receipt-gated prune path — previously they leaked as "foreign" and had to
    # be deleted by hand. Accept either outcome (pruned or reported) so the test
    # is robust, but require the remove to have handled them.
    handled = remove_result.get("pruned", []) + remove_result.get("reported", [])
    assert len(handled) > 0, (
        f"expected the old targets to be pruned or reported, got: {remove_result}"
    )

    # 6. Verify the new skill is rendered and functional
    r = harness.run("sut", f"cat {ws}/.claude/skills/deployment-guide/SKILL.md")
    assert r.ok, f"new skill not rendered: {r.stderr}"
    assert "Deploy Checklist" in r.stdout

    # 7. Verify deploy-checklist is gone from the lock
    r = harness.run("sut", f"cat {ws}/.auto/skills/lock.json")
    assert r.ok
    lock = r.json()
    assert "deploy-checklist" not in lock["skills"], "old skill still in lock"
    assert "deployment-guide" in lock["skills"], "new skill missing from lock"

    # 8. sync --check should be clean (only managed skills matter)
    r = harness.run("sut", f"cd {ws} && auto skill sync --check")
    assert r.ok, f"sync --check not clean after remediation: {r.stderr}"
