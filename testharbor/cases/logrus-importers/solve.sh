#!/bin/bash
# Oracle (treatment arm): auto graph + reverse BFS from logrus.go over import edges.
set -euo pipefail

auto graph code graph /app/logrus | python3 -c '
import json, sys
from collections import defaultdict
g = json.load(sys.stdin)
rev = defaultdict(set)
for e in g["edges"]:
    rev[e["target"]].add(e["source"])
seen, stack = set(), ["logrus.go"]
while stack:
    for s in rev.get(stack.pop(), ()):
        if s not in seen:
            seen.add(s); stack.append(s)
print(json.dumps(sorted(seen)))
' > /app/answer.json
