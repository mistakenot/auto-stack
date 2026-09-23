"""Render `impact-*` tasks from generators/impact.toml with go list ground truth.

Semantics, which the instruction templates state to the agent verbatim:

- package level: P is a dependent of T when T is in P's non-test transitive
  Deps. Test files never form a link, because Go cannot import a test file.
- file level: a file F depends on T when one of F's own imports is T, or is a
  package whose non-test Deps contain T. Test files count as dependents. The
  target package's own non-test and in-package test files are excluded; its
  external `_test` package files count.
"""

from __future__ import annotations

import json
import shutil
import tomllib
from collections import defaultdict, deque
from dataclasses import dataclass
from pathlib import Path
from string import Template

from .golist import GO_IMAGE, import_graph
from .layout import Layout

OBJECTIVES = {"pkg": "pkg-dependents", "file": "file-dependents"}


@dataclass
class Rendered:
    name: str
    files: dict[str, str]  # relative path -> content
    stats: dict


def _spec(layout: Layout) -> dict:
    return tomllib.loads((layout.root / "generators" / "impact.toml").read_text())


def _index(graph: dict) -> tuple[dict[str, dict], dict[str, dict]]:
    by_import = {p["import_path"]: p for p in graph["packages"]}
    by_dir = {p["dir"]: p for p in graph["packages"]}
    return by_import, by_dir


def compute(graph: dict, target_dir: str, granularity: str) -> dict:
    by_import, by_dir = _index(graph)
    if target_dir not in by_dir:
        raise SystemExit(f"error: no package at '{target_dir}'. Known dirs include: {sorted(by_dir)[:15]}")
    target = by_dir[target_dir]["import_path"]
    dependents = [p for p in graph["packages"] if target in p["deps"]]

    reverse: dict[str, set[str]] = defaultdict(set)
    for p in graph["packages"]:
        for imp in p["imports"]:
            if imp in by_import:
                reverse[imp].add(p["import_path"])
    dist = {target: 0}
    queue = deque([target])
    while queue:
        node = queue.popleft()
        for src in reverse[node]:
            if src not in dist:
                dist[src] = dist[node] + 1
                queue.append(src)
    depth = max((dist[p["import_path"]] for p in dependents), default=0)
    direct = sum(1 for p in dependents if dist[p["import_path"]] == 1)

    if granularity == "pkg":
        answer = sorted(p["dir"] for p in dependents)
        pool = sorted(d for d in by_dir if d not in answer and d != target_dir)
    else:
        def reaches(imp: str) -> bool:
            return imp == target or (imp in by_import and target in by_import[imp]["deps"])

        answer, pool = [], []
        for p in graph["packages"]:
            for f in p["files"]:
                own = p["import_path"] == target and f["kind"] in ("go", "test")
                (answer if not own and any(reaches(i) for i in f["imports"]) else pool).append(f["path"])
        answer.sort()
        pool = sorted(x for x in pool if not x.endswith("_test.go"))
    if not answer:
        raise SystemExit(f"error: '{target_dir}' has no dependents at {granularity} level; pick another target")
    example = pool[len(pool) // 2] if pool else "."
    difficulty = "easy" if depth <= 2 else "medium" if depth <= 4 else "hard"
    return {"answer": answer, "target_import": target, "depth": depth, "direct": direct,
            "difficulty": difficulty, "example": example}


def render_all(layout: Layout, only: list[str] | None = None) -> list[Rendered]:
    spec = _spec(layout)
    tmpl_dir = layout.root / "generators" / "impact"
    out: list[Rendered] = []
    for t in spec["tasks"]:
        fx = spec["fixtures"][t["fixture"]]
        module_dir = t.get("module_dir", ".")
        objective = OBJECTIVES[t["granularity"]]
        name = f"impact-{objective}-{t['fixture']}-{t['subject']}"
        if only and name not in only:
            continue
        graph = import_graph(layout, t["fixture"], fx["repo"], fx["rev"], module_dir)
        s = compute(graph, t["target"], t["granularity"])

        module_abs = f"/app/{fx['repo_dir']}" + ("" if module_dir == "." else f"/{module_dir}")
        if module_dir == ".":
            location = f"A Go module is checked out at `{module_abs}`. All paths below are relative to it."
            module_note = ""
        else:
            location = (f"A repository is checked out at `/app/{fx['repo_dir']}`. The module in scope is the one "
                        f"rooted at `{module_abs}`. All paths below are relative to that module root.")
            module_note = f", module `{module_dir}/`"
        unit = "package" if t["granularity"] == "pkg" else "file"
        values = {
            "canary": layout.conventions["canary_guid"], "org": layout.org, "name": name,
            "version": t.get("version", "1.0.0"), "objective": objective, "fixture": t["fixture"],
            "subject": t["subject"], "repo": fx["repo"], "rev": fx["rev"], "repo_dir": fx["repo_dir"],
            "module_dir": module_dir, "module_label": module_abs, "module_note": module_note,
            "target_dir": t["target"], "target_import": s["target_import"], "granularity": t["granularity"],
            "location": location, "example": s["example"], "unit": unit,
            "answer_size": len(s["answer"]), "direct": s["direct"], "depth": s["depth"],
            "difficulty": s["difficulty"], "go_image": GO_IMAGE,
            "description": (f"{unit.capitalize()}-level reverse dependencies of {t['target']} in "
                            f"{fx['repo'].removeprefix('https://github.com/')}{module_note}. "
                            "Reward is F1 against go list ground truth."),
        }
        sub = lambda text: Template(text).substitute(values)
        answer = json.dumps(s["answer"], indent=2) + "\n"
        files = {
            "instruction.md": sub((tmpl_dir / f"instruction-{t['granularity']}.md").read_text()),
            "task.toml": sub((tmpl_dir / "task.toml").read_text()),
            "README.md": sub((tmpl_dir / "README.md").read_text()),
            "environment/Dockerfile": sub((tmpl_dir / "Dockerfile").read_text()),
            "solution/solve.sh": sub((tmpl_dir / "solve.sh").read_text()),
            "solution/answer.json": answer,
            "tests/expected.json": answer,
        }
        for f in sorted((tmpl_dir / "tests").rglob("*")):
            if f.is_file():
                files[f"tests/{f.relative_to(tmpl_dir / 'tests')}"] = f.read_text()
        out.append(Rendered(name, files, {k: values[k] for k in ("answer_size", "direct", "depth", "difficulty")}))
    return out


def write(layout: Layout, rendered: Rendered) -> None:
    task_dir = layout.tasks_dir / rendered.name
    if task_dir.exists():
        meta = tomllib.loads((task_dir / "task.toml").read_text()).get("metadata", {})
        if meta.get("generator") != "impact":
            raise SystemExit(f"error: {task_dir.name} exists and was not generated; refusing to overwrite it.")
        shutil.rmtree(task_dir)
    for rel, content in rendered.files.items():
        path = task_dir / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        if rel.endswith(".sh"):
            path.chmod(0o755)


def drift(layout: Layout, rendered: Rendered) -> list[str]:
    """Paths whose committed content differs from a fresh render."""
    task_dir = layout.tasks_dir / rendered.name
    if not task_dir.is_dir():
        return ["<task missing>"]
    on_disk = {str(p.relative_to(task_dir)) for p in task_dir.rglob("*") if p.is_file()}
    diffs = sorted(on_disk ^ set(rendered.files))
    diffs += sorted(rel for rel, content in rendered.files.items()
                    if rel in on_disk and (task_dir / rel).read_text() != content)
    return diffs


def stale_generated(layout: Layout, rendered: list[Rendered]) -> list[str]:
    """Generated task dirs that no longer appear in the spec."""
    names = {r.name for r in rendered}
    stale = []
    for task_dir in layout.task_dirs():
        meta = tomllib.loads((task_dir / "task.toml").read_text()).get("metadata", {})
        if meta.get("generator") == "impact" and task_dir.name not in names:
            stale.append(task_dir.name)
    return stale


def candidates(layout: Layout, fixture: str, module_dir: str = ".", limit: int = 15) -> list[dict]:
    spec = _spec(layout)
    fx = spec["fixtures"][fixture]
    graph = import_graph(layout, fixture, fx["repo"], fx["rev"], module_dir)
    rows = []
    for p in graph["packages"]:
        try:
            s = compute(graph, p["dir"], "pkg")
        except SystemExit:
            continue
        rows.append({"target": p["dir"], "dependents": len(s["answer"]), "direct": s["direct"],
                     "max_depth": s["depth"], "difficulty": s["difficulty"]})
    rows.sort(key=lambda r: (-r["max_depth"], -r["dependents"]))
    return rows[:limit]
