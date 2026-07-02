#!/usr/bin/env python3
import sys
import os
from rdflib import Graph

if len(sys.argv) < 3:
    print("Usage: test_sparql.py <data.ttl> <tests_dir>", file=sys.stderr)
    sys.exit(1)

data_path = sys.argv[1]
tests_dir = sys.argv[2]

g = Graph()
g.parse(data_path, format="turtle")

rq_files = sorted(f for f in os.listdir(tests_dir) if f.endswith(".rq"))
if not rq_files:
    print("No .rq test files found", file=sys.stderr)
    sys.exit(1)

passed = 0
failed = 0

for rq_file in rq_files:
    rq_path = os.path.join(tests_dir, rq_file)
    with open(rq_path) as f:
        query = f.read()

    results = list(g.query(query))

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

total = passed + failed
print(f"\n{passed}/{total} tests passed")
sys.exit(0 if failed == 0 else 1)
