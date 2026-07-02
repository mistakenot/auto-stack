#!/usr/bin/env python3
import sys
from rdflib import Graph, Namespace

if len(sys.argv) < 2:
    print("Usage: format.py <file.ttl>", file=sys.stderr)
    sys.exit(1)

path = sys.argv[1]
try:
    g = Graph()
    g.parse(path, format="turtle")
    g.bind("auto", Namespace("http://auto.dev/ontology#"))
    g.bind("dcterms", Namespace("http://purl.org/dc/terms/"))
    g.serialize(destination=path, format="turtle")
    print(f"OK: formatted {path}")
except Exception as e:
    print(f"ERROR: Failed to format {path}", file=sys.stderr)
    print(f"  {e}", file=sys.stderr)
    sys.exit(1)
