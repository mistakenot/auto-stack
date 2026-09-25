"""Render `fix-*` tasks from generators/fix.toml.

Each entry names an upstream bug-fix commit and a problem statement curated
from its linked issue or pull request. The commit is validated with
`fixmine.validate` (cached), then rendered into a Harbor task whose verifier
runs the commit's own tests against the agent's patch.
"""

from __future__ import annotations

import json
import subprocess
import tomllib
from pathlib import Path
from string import Template

from .fixmine import Validation, validate
from .generate import Rendered
from .golist import GO_IMAGE
from .layout import Layout


def _spec(layout: Layout) -> dict:
    path = layout.root / "generators" / "fix.toml"
    return tomllib.loads(path.read_text()) if path.is_file() else {"fixtures": {}, "tasks": []}


def _history(layout: Layout, fixture: str, repo: str) -> Path:
    path = layout.root / ".cache" / "history" / fixture
    if not (path / ".git").is_dir() and not (path / "HEAD").is_file():
        path.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(["git", "clone", "-q", "--filter=blob:none", "--no-checkout", repo, str(path)], check=True)
    return path


def _validated(layout: Layout, fixture: str, repo: str, history: Path, sha: str) -> Validation:
    cache = layout.root / ".cache" / "fixval" / f"{fixture}@{sha}.json"
    if cache.is_file():
        return Validation(**json.loads(cache.read_text()))
    v = validate(layout, fixture, repo, history, sha)
    cache.parent.mkdir(parents=True, exist_ok=True)
    cache.write_text(json.dumps(v.__dict__, indent=2))
    return v


def _statement(layout: Layout, t: dict) -> str:
    path = layout.root / "generators" / "fix" / "statements" / f"{t['fixture']}-{t['subject']}.md"
    if not path.is_file():
        raise SystemExit(f"error: missing problem statement {path.relative_to(layout.root)}")
    return path.read_text().strip()


def _git(history: Path, *args: str) -> str:
    return subprocess.run(["git", *args], cwd=history, check=True, capture_output=True, text=True).stdout


def render_all(layout: Layout, only: list[str] | None = None) -> list[Rendered]:
    spec = _spec(layout)
    tmpl = layout.root / "generators" / "fix"
    out: list[Rendered] = []
    for t in spec["tasks"]:
        fx = spec["fixtures"][t["fixture"]]
        name = f"fix-issue-{t['fixture']}-{t['subject']}"
        if only and name not in only:
            continue
        history = _history(layout, t["fixture"], fx["repo"])
        v = _validated(layout, t["fixture"], fx["repo"], history, t["sha"])
        if not v.ok:
            raise SystemExit(f"error: {name}: commit {t['sha'][:12]} does not qualify: {v.reason}")

        patch = _git(history, "diff", "--binary", v.parent, v.sha, "--", *v.src_files)
        fix_lines = sum(1 for line in patch.splitlines()
                        if line[:1] in "+-" and not line.startswith(("+++", "---")))
        fixed_tree = layout.root / ".cache" / "fixwork" / f"{t['fixture']}@{v.sha[:12]}" / "fixed"
        values = {
            "canary": layout.conventions["canary_guid"], "org": layout.org, "name": name,
            "version": t.get("version", "1.0.0"), "fixture": t["fixture"], "subject": t["subject"],
            "repo": fx["repo"], "repo_dir": fx["repo_dir"], "repo_name": fx["repo"].removeprefix("https://github.com/"),
            "parent": v.parent, "sha": v.sha, "go_image": GO_IMAGE, "packages": " ".join(v.packages),
            "statement": _statement(layout, t), "statement_source": t["statement_source"],
            "n_f2p": len(v.fail_to_pass), "n_p2p": len(v.pass_to_pass), "n_src": len(v.src_files),
            "fix_lines": fix_lines,
            "difficulty": "easy" if fix_lines <= 10 else "medium" if fix_lines <= 40 else "hard",
            "description": (f"Resolve {t['statement_source']} in "
                            f"{fx['repo'].removeprefix('https://github.com/')}; graded by the fix commit's tests."),
        }
        sub = lambda text: Template(text).substitute(values)
        files = {
            "instruction.md": sub((tmpl / "instruction.md").read_text()),
            "task.toml": sub((tmpl / "task.toml").read_text()),
            "README.md": sub((tmpl / "README.md").read_text()),
            "environment/Dockerfile": sub((tmpl / "Dockerfile").read_text()),
            "solution/solve.sh": sub((tmpl / "solve.sh").read_text()),
            "solution/fix.patch": patch,
            "tests/Dockerfile": sub((tmpl / "tests.Dockerfile").read_text()),
            "tests/test.sh": sub((tmpl / "test.sh").read_text()),
            "tests/fail_to_pass.json": json.dumps(v.fail_to_pass, indent=2) + "\n",
            "tests/pass_to_pass.json": json.dumps(v.pass_to_pass, indent=2) + "\n",
        }
        for f in sorted((tmpl / "grading").rglob("*")):
            if f.is_file():
                files[f"tests/grading/{f.relative_to(tmpl / 'grading')}"] = f.read_text()
        for test_file in v.test_files:
            files[f"tests/hidden/{test_file}"] = (fixed_tree / test_file).read_text()
        out.append(Rendered(name, files, {k: values[k] for k in ("n_f2p", "n_p2p", "fix_lines", "difficulty")}))
    return out
