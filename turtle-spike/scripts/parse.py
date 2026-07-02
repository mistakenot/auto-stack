#!/usr/bin/env python3
import sys
from rdflib import Graph

if len(sys.argv) < 2:
    print("Usage: parse.py <file.ttl>", file=sys.stderr)
    sys.exit(1)

path = sys.argv[1]
try:
    g = Graph()
    g.parse(path, format="turtle")
    print(f"OK: parsed {len(g)} triples")
except Exception as e:
    print(f"ERROR: Failed to parse {path}", file=sys.stderr)
    print(f"  {e}", file=sys.stderr)
    sys.exit(1)
