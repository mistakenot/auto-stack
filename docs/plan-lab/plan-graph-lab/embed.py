#!/usr/bin/env python3
"""Paste a plan's graph.json into plan-graph-lab pages.

Each page holds the graph verbatim in
    <script type="application/json" id="plan-graph"> … </script>
and reads it with JSON.parse(document.getElementById('plan-graph').textContent).
Rerun this after the plan changes to refresh every page.

Usage: embed.py [page.html …]            (default: every *.html next to this file)
       PLAN_GRAPH=path/to/graph.json embed.py …
"""
import os
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve().parent
REPO = HERE.parent.parent
GRAPH = pathlib.Path(os.environ.get("PLAN_GRAPH", REPO / ".auto/plan/plans/001-auto-plan-graph/graph.json"))
BLOCK = re.compile(r'(<script type="application/json" id="plan-graph">)(.*?)(</script>)', re.S)


def main():
    data = GRAPH.read_text().replace("</", "<\\/")  # never close the script tag early
    pages = [pathlib.Path(p) for p in sys.argv[1:]] or sorted(HERE.glob("*.html"))
    for page in pages:
        html = page.read_text()
        if not BLOCK.search(html):
            print(f"skip {page.name}: no plan-graph block", file=sys.stderr)
            continue
        page.write_text(BLOCK.sub(lambda m: m.group(1) + "\n" + data + m.group(3), html, count=1))
        print(f"embedded {GRAPH.name} → {page.name}")


if __name__ == "__main__":
    main()
