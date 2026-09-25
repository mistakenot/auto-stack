"""Fetch a pinned fixture and extract its Go import graph inside a pinned Go container.

Ground truth for generated impact tasks comes from `go list`, the Go toolchain's
own view of the import graph, never from `auto graph`, which is a tool under test.
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

from .layout import Layout

GO_IMAGE = "golang:1.26@sha256:6c2a5538f964f1c82f97ad14988bf05de100d922d159d0e398b54c7b0ca0c6c9"
MODCACHE_VOLUME = "auto-evals-gomodcache"


def _git(args: list[str], cwd: Path) -> None:
    subprocess.run(["git", *args], cwd=cwd, check=True, stdout=subprocess.DEVNULL, stderr=sys.stderr)


def fixture_checkout(layout: Layout, fixture: str, repo: str, rev: str) -> Path:
    """Shallow checkout of `repo` at exactly `rev`, cached under .cache/fixtures/."""
    dest = layout.root / ".cache" / "fixtures" / f"{fixture}@{rev[:12]}"
    if (dest / ".git").is_dir():
        return dest
    dest.mkdir(parents=True, exist_ok=True)
    print(f"fetching {repo}@{rev[:12]}", file=sys.stderr)
    _git(["init", "-q"], dest)
    _git(["fetch", "-q", "--depth", "1", repo, rev], dest)
    _git(["checkout", "-q", "FETCH_HEAD"], dest)
    return dest


def import_graph(layout: Layout, fixture: str, repo: str, rev: str, module_dir: str = ".") -> dict:
    """Import graph of the module at `module_dir`, cached per (fixture, rev, module_dir)."""
    stem = "root" if module_dir in (".", "") else module_dir.replace("/", "_")
    cache = layout.root / ".cache" / "graphs" / f"{fixture}@{rev[:12]}" / f"{stem}.json"
    if cache.is_file():
        return json.loads(cache.read_text())
    src = fixture_checkout(layout, fixture, repo, rev)
    helper = layout.root / "generators" / "golist"
    workdir = f"/src/{module_dir}".rstrip("/.") if module_dir not in (".", "") else "/src"
    script = "cd /helper && GOWORK=off go build -o /tmp/golist main.go && cd " + workdir + " && GOWORK=off /tmp/golist"
    print(f"extracting import graph for {fixture}:{module_dir}", file=sys.stderr)
    proc = subprocess.run(
        ["docker", "run", "--rm",
         "-v", f"{helper}:/helper:ro",
         "-v", f"{src}:/src:ro",
         "-v", f"{MODCACHE_VOLUME}:/go/pkg/mod",
         "-e", "GOFLAGS=-mod=mod", "-e", "GOTOOLCHAIN=auto", "-e", "GOOS=linux", "-e", "GOARCH=amd64",
         GO_IMAGE, "bash", "-c", script],
        capture_output=True, text=True,
    )
    if proc.returncode != 0:
        raise SystemExit(f"error: import graph extraction failed for {fixture}:{module_dir}\n{proc.stderr[-2000:]}")
    graph = json.loads(proc.stdout)
    cache.parent.mkdir(parents=True, exist_ok=True)
    cache.write_text(json.dumps(graph))
    return graph
