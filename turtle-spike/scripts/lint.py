#!/usr/bin/env python3
import sys
from rdflib import Graph, Namespace, URIRef

if len(sys.argv) < 2:
    print("Usage: lint.py <file.ttl>", file=sys.stderr)
    sys.exit(1)

AUTO = Namespace("http://auto.dev/ontology#")
KNOWN_PREFIXES = {
    "http://auto.dev/ontology",
    "http://www.w3.org/1999/02/22-rdf-syntax-ns",
    "http://www.w3.org/2000/01/rdf-schema",
    "http://www.w3.org/2002/07/owl",
    "http://www.w3.org/2001/XMLSchema",
    "http://purl.org/dc/terms",
    "http://www.w3.org/ns/shacl",
}
KNOWN_AUTO_PREDICATES = {
    AUTO.componentName, AUTO.hasOwner, AUTO.hasLayer,
    AUTO.hasStatus, AUTO.dependsOn, AUTO.hasComponent,
}
KNOWN_AUTO_CLASSES = {
    AUTO.Component, AUTO.System, AUTO.Layer, AUTO.Status,
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
        if not any(str(s).startswith(kp) for kp in KNOWN_PREFIXES):
            warnings.append(f"Subject from unknown namespace: {s}")

    if isinstance(p, URIRef) and str(p).startswith(str(AUTO)):
        if p not in KNOWN_AUTO_PREDICATES:
            from rdflib import RDF, RDFS, OWL
            standard = {RDF.type, RDFS.label, RDFS.comment, RDFS.domain, RDFS.range,
                        RDFS.subClassOf, OWL.Class, OWL.ObjectProperty, OWL.DatatypeProperty}
            if p not in standard:
                errors.append(f"Unknown auto: predicate: {p}")

if warnings:
    for w in warnings:
        print(f"WARN: {w}", file=sys.stderr)
if errors:
    for e in errors:
        print(f"ERROR: {e}", file=sys.stderr)
    sys.exit(1)

print(f"OK: lint passed ({len(g)} triples, {len(warnings)} warnings)")
