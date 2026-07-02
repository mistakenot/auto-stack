#!/usr/bin/env python3
import sys
from pyshacl import validate
from rdflib import Graph

if len(sys.argv) < 3:
    print("Usage: validate.py <data.ttl> <shapes.ttl>", file=sys.stderr)
    sys.exit(1)

data_path = sys.argv[1]
shapes_path = sys.argv[2]

data_graph = Graph()
data_graph.parse(data_path, format="turtle")

shapes_graph = Graph()
shapes_graph.parse(shapes_path, format="turtle")

conforms, results_graph, results_text = validate(
    data_graph,
    shacl_graph=shapes_graph,
    inference="none",
)

print(results_text)
sys.exit(0 if conforms else 1)
