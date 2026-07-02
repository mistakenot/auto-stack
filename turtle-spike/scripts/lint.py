#!/usr/bin/env python3
import sys
from rdflib import Graph, Namespace, URIRef, RDF, RDFS, OWL

if len(sys.argv) < 2:
    print("Usage: lint.py <file.ttl>", file=sys.stderr)
    sys.exit(1)

KNOWN_PREFIXES = {
    "http://auto.dev/ontology",
    "http://auto.dev/ontology/etl",
    "http://www.w3.org/1999/02/22-rdf-syntax-ns",
    "http://www.w3.org/2000/01/rdf-schema",
    "http://www.w3.org/2002/07/owl",
    "http://www.w3.org/2001/XMLSchema",
    "http://purl.org/dc/terms",
    "http://www.w3.org/ns/shacl",
}

STANDARD_PREDICATES = {
    RDF.type, RDFS.label, RDFS.comment, RDFS.domain, RDFS.range,
    RDFS.subClassOf, OWL.Class, OWL.ObjectProperty, OWL.DatatypeProperty,
}

path = sys.argv[1]
g = Graph()
g.parse(path, format="turtle")

errors = []
warnings = []

for s, p, o in g:
    if isinstance(s, URIRef):
        s_str = str(s)
        ns = s_str.rsplit("#", 1)[0] if "#" in s_str else s_str.rsplit("/", 1)[0]
        if s_str == ns:
            continue
        if not any(s_str.startswith(kp) for kp in KNOWN_PREFIXES):
            warnings.append(f"Subject from unknown namespace: {s}")

    if isinstance(p, URIRef) and p not in STANDARD_PREDICATES:
        p_str = str(p)
        is_project_predicate = any(p_str.startswith(kp) for kp in KNOWN_PREFIXES
                                   if "auto.dev" in kp)
        if not is_project_predicate and not any(p_str.startswith(kp) for kp in KNOWN_PREFIXES):
            errors.append(f"Predicate from unknown namespace: {p}")

if warnings:
    for w in warnings:
        print(f"WARN: {w}", file=sys.stderr)
if errors:
    for e in errors:
        print(f"ERROR: {e}", file=sys.stderr)
    sys.exit(1)

print(f"OK: lint passed ({len(g)} triples, {len(warnings)} warnings)")
