#!/usr/bin/env python3
import sys
from pyshacl import validate
from rdflib import Graph

if len(sys.argv) < 3:
    print("Usage: validate.py <data.ttl> <shapes.ttl> [extra_data.ttl ...] [extra_shapes.ttl ...]", file=sys.stderr)
    print("  Extra files are merged into the data or shapes graph by filename convention:", file=sys.stderr)
    print("  files in shapes/ are treated as shapes, all others as data.", file=sys.stderr)
    sys.exit(1)

data_graph = Graph()
shapes_graph = Graph()

data_graph.parse(sys.argv[1], format="turtle")
shapes_graph.parse(sys.argv[2], format="turtle")

for extra in sys.argv[3:]:
    if "/shapes/" in extra or extra.startswith("shapes/"):
        shapes_graph.parse(extra, format="turtle")
    else:
        data_graph.parse(extra, format="turtle")

conforms, results_graph, results_text = validate(
    data_graph,
    shacl_graph=shapes_graph,
    inference="none",
)

print(results_text)
sys.exit(0 if conforms else 1)
