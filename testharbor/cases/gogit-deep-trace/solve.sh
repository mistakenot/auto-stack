#!/bin/bash
# Oracle (treatment arm): auto graph reverse-BFS from the utils/trace package,
# mapped up to package (directory) granularity. auto graph nodes already exclude
# Go-ignored dirs (_examples, testdata), so no extra filtering is needed.
set -euo pipefail

auto graph code graph /app/go-git | python3 -c '
import json, sys, os
from collections import defaultdict
g = json.load(sys.stdin)
rev = defaultdict(set)
for e in g["edges"]:
    rev[e["target"]].add(e["source"])
pkgof = lambda p: os.path.dirname(p)
TARGET = "utils/trace"
seeds = [n["path"] for n in g["nodes"] if pkgof(n["path"]) == TARGET]
seen, stack = set(), list(seeds)
while stack:
    for s in rev.get(stack.pop(), ()):
        if s not in seen:
            seen.add(s); stack.append(s)
pkgs = sorted({(pkgof(p) or ".") for p in seen} - {TARGET})
print(json.dumps(pkgs))
' > /app/answer.json
