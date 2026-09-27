"""Validate candidate bug-fix commits for `fix-*` tasks, SWE-bench style.

For a commit C with parent P, C's test files are laid over P ("broken + new
tests") and over C ("fixed"). The affected packages' tests run in both trees,
offline, inside the pinned Go image. A commit qualifies when:

- the broken tree compiles, so new tests fail on behaviour, not on missing API
  the agent could only guess from the hidden tests;
- at least one test fails in the broken tree and passes in the fixed tree
  (FAIL_TO_PASS);
- every test that passes in the broken tree also passes in the fixed tree
  (PASS_TO_PASS is well defined).
"""

from __future__ import annotations

import json
import subprocess
import sys
from dataclasses import asdict, dataclass, field
from pathlib import Path

from .golist import GO_IMAGE, MODCACHE_VOLUME
from .layout import Layout


@dataclass
class Validation:
    sha: str
    parent: str
    ok: bool
    reason: str = ""
    src_files: list[str] = field(default_factory=list)
    test_files: list[str] = field(default_factory=list)
    packages: list[str] = field(default_factory=list)
    fail_to_pass: list[str] = field(default_factory=list)
    pass_to_pass: list[str] = field(default_factory=list)


def _git(args: list[str], cwd: Path, capture: bool = False) -> str:
    proc = subprocess.run(["git", *args], cwd=cwd, check=True, text=True,
                          stdout=subprocess.PIPE if capture else subprocess.DEVNULL, stderr=sys.stderr)
    return proc.stdout if capture else ""


def _checkout(dest: Path, repo: str, rev: str) -> None:
    if (dest / ".git").is_dir():
        return
    dest.mkdir(parents=True, exist_ok=True)
    _git(["init", "-q"], dest)
    _git(["fetch", "-q", "--depth", "1", repo, rev], dest)
    _git(["checkout", "-q", "FETCH_HEAD"], dest)


def _go(mount: Path, script: str, *, network: bool) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["docker", "run", "--rm", *([] if network else ["--network", "none"]),
         "-v", f"{mount}:/w", "-v", f"{MODCACHE_VOLUME}:/go/pkg/mod",
         "-e", "GOFLAGS=-mod=mod", "-e", "GOTOOLCHAIN=auto", "-e", "CGO_ENABLED=0",
         GO_IMAGE, "bash", "-c", script],
        capture_output=True, text=True,
    )


def _results(stream: str) -> tuple[dict[str, str], set[str]]:
    """Per-test outcome from `go test -json`, plus packages that failed to build."""
    outcome: dict[str, str] = {}
    build_failed: set[str] = set()
    for line in stream.splitlines():
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        if ev.get("Action") in ("pass", "fail", "skip") and ev.get("Test"):
            outcome[f"{ev['Package']}::{ev['Test']}"] = ev["Action"]
        if ev.get("Action") == "fail" and not ev.get("Test") and "FailedBuild" in ev:
            build_failed.add(ev["Package"])
        if ev.get("Action") == "build-fail" or (ev.get("ImportPath") and ev.get("Action") == "build-fail"):
            build_failed.add(ev.get("ImportPath", ""))
    return outcome, build_failed


def validate(layout: Layout, fixture: str, repo: str, history: Path, sha: str) -> Validation:
    parent = _git(["rev-parse", f"{sha}^"], history, capture=True).strip()
    files = _git(["diff", "--name-only", parent, sha], history, capture=True).split()
    go = [f for f in files if f.endswith(".go")]
    tests = [f for f in go if f.endswith("_test.go")]
    src = [f for f in go if not f.endswith("_test.go")]
    v = Validation(sha=sha, parent=parent, ok=False, src_files=src, test_files=tests)
    if not tests or not src:
        v.reason = "needs both source and test changes"
        return v

    work = layout.root / ".cache" / "fixwork" / f"{fixture}@{sha[:12]}"
    broken, fixed = work / "broken", work / "fixed"
    _checkout(fixed, repo, sha)
    _checkout(broken, repo, parent)
    for t in tests:  # lay the commit's tests over the parent
        (broken / t).parent.mkdir(parents=True, exist_ok=True)
        (broken / t).write_bytes((fixed / t).read_bytes())
    pkgs = sorted({"./" + str(Path(t).parent) for t in tests})
    v.packages = pkgs
    pkg_args = " ".join(pkgs)

    for tree in ("broken", "fixed"):  # deps and toolchain online, tests offline
        warm = _go(work, f"cd /w/{tree} && go mod download && go test -count=1 -run '^$' {pkg_args} >/dev/null 2>&1; true",
                   network=True)
        if warm.returncode != 0:
            v.reason = f"dependency download failed in {tree}: {warm.stderr[-300:]}"
            return v
    runs = {}
    for tree in ("broken", "fixed"):
        proc = _go(work, f"cd /w/{tree} && go test -json -count=1 {pkg_args}", network=False)
        runs[tree] = _results(proc.stdout)
    (b_out, b_fail), (f_out, f_fail) = runs["broken"], runs["fixed"]
    if f_fail:
        v.reason = f"fixed tree fails to build: {sorted(f_fail)}"
        return v
    if b_fail:
        v.reason = f"broken tree fails to build with new tests (tests need new API): {sorted(b_fail)}"
        return v
    v.fail_to_pass = sorted(t for t, a in f_out.items() if a == "pass" and b_out.get(t) == "fail")
    v.pass_to_pass = sorted(t for t, a in b_out.items() if a == "pass" and f_out.get(t) == "pass")
    regressions = sorted(t for t, a in b_out.items() if a == "pass" and f_out.get(t) == "fail")
    if regressions:
        v.reason = f"fix breaks tests that passed before: {regressions[:3]}"
    elif not v.fail_to_pass:
        v.reason = "no test fails before the fix and passes after"
    else:
        v.ok = True
    return v


def to_json(v: Validation) -> str:
    return json.dumps(asdict(v), indent=2)
