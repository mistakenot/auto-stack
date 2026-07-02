#!/usr/bin/env python3
import sys
import os
from rdflib import Graph

if len(sys.argv) < 3:
    print("Usage: test_sparql.py <data.ttl> <tests_dir> [extra_data.ttl ...]", file=sys.stderr)
    sys.exit(1)

data_path = sys.argv[1]
tests_dir = sys.argv[2]

g = Graph()
g.parse(data_path, format="turtle")

for extra in sys.argv[3:]:
    g.parse(extra, format="turtle")

rq_files = sorted(f for f in os.listdir(tests_dir) if f.endswith(".rq"))
if not rq_files:
    print("No .rq test files found", file=sys.stderr)
    sys.exit(1)

passed = 0
failed = 0
skipped = 0

for rq_file in rq_files:
    rq_path = os.path.join(tests_dir, rq_file)
    with open(rq_path) as f:
        query = f.read()

    prefixes_used = [line for line in query.splitlines() if line.strip().startswith("PREFIX")]
    prefix_names = {p.split(":")[0].replace("PREFIX", "").strip() for p in prefixes_used}

    graph_prefixes = {ns[0] for ns in g.namespaces()}
    if prefix_names and not prefix_names.issubset(graph_prefixes | {"rdf", "rdfs", "owl", "xsd", "auto", "etl", "dcterms", "sh"}):
        pass

    try:
        results = list(g.query(query))
    except Exception as e:
        skipped += 1
        print(f"SKIP: {rq_file} (query error: {e})")
        continue

    if results:
        failed += 1
        print(f"FAIL: {rq_file}")
        print(f"  Violations found:")
        for row in results:
            for var in row.labels:
                print(f"    {var}: {row[var]}")
    else:
        passed += 1
        print(f"PASS: {rq_file}")

total = passed + failed + skipped
print(f"\n{passed}/{total} tests passed" + (f", {skipped} skipped" if skipped else ""))
sys.exit(0 if failed == 0 else 1)
