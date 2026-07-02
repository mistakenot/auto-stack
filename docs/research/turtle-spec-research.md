---
hash: "8ba00c34"
id: "02167643"
read_when: "designing a verification pipeline for RDF/Turtle system specs, evaluating SHACL vs OWL reasoning, or setting up CI for ontology validation"
summary: "Design and spike plan for a fail-fast RDF/Turtle verification pipeline (format → parse → lint → SHACL → reason → SPARQL tests) to validate a machine-readable system spec in CI."
title: "Tech Spike: RDF/Turtle Verification Toolchain"
---

# Tech Spike: A Verification Toolchain for an RDF/Turtle System Spec

**Purpose.** You have (or will have) a machine-readable description of a software
system written in Turtle (`.ttl`). This document describes a toolchain to build
around that spec so you can be confident it is *well-formed, internally
consistent, and compliant with your own rules* — and so that CI enforces all of
that on every change. It is scoped as a spike: the goal is to stand up a thin
end-to-end version of the pipeline on a real repo and learn where the value and
the friction actually are.

**Who this is for.** An engineer comfortable with the command line, a JVM, and
Python, who has not necessarily worked with RDF before. No prior semantic-web
experience is assumed; the background section covers what you need.

---

## 1. The mental model

The spec is data (a set of RDF triples). Verifying it is a **fail-fast
pipeline** of independent CLI stages, run cheapest-first so a syntax typo never
waits on a reasoner:

```
format  →  parse  →  lint  →  validate  →  reason  →  test
(Stage0)  (Stage1)  (Stage2)  (Stage3)    (Stage4)   (Stage5)
 cheap  ───────────────────────────────────────────►  expensive
```

Every stage is a standalone CLI. Each fails loudly with a machine-readable
report. A `Makefile` chains them; pre-commit runs the cheap ones locally; CI
runs the whole thing. That is the entire idea — the rest is detail.

The payoff of doing it this way: an agent or a CI job gets the fastest possible
feedback, and you only pay for heavy reasoning once the basics hold.

---

## 2. Background you'll want before starting

**RDF, in one paragraph.** RDF models everything as *triples*:
`subject — predicate — object`. A set of triples is a graph. Turtle is a
human-readable text serialization of that graph (there are others — JSON-LD,
N-Triples, RDF/XML — all interchangeable, same underlying triples). Turtle is
the right choice here because it diffs cleanly in git and both humans and LLMs
read and write it reliably.

**The vocabulary stack.** RDF is the base data model. RDFS adds lightweight
schema vocabulary (classes, subclass relations). OWL sits on top and adds real
logical expressiveness (cardinality, disjointness, property characteristics like
transitivity) with description-logic semantics that a *reasoner* can act on.
SHACL is a separate standard for validating a graph against constraints. SPARQL
is the query language. You will touch all of these; you do not need to master
any of them to run the spike.

**The one conceptual trap: open vs closed world.** OWL reasoning is
*open-world* and makes *no unique-name assumption*: "not stated" does not mean
"false," and two differently-named things might secretly be the same unless you
say otherwise. This surprises everyone at first. SHACL, by contrast, is
effectively *closed-world* — it checks "does the data as given satisfy these
constraints." This is why the toolchain uses **both**: OWL reasoning for
logical consistency, SHACL for "check my design against my rules." They answer
different questions and neither replaces the other.

**Three kinds of "is my spec broken":**

1. *Syntactic / hygiene* — does it parse, are prefixes and terms used correctly.
   Caught by Stages 1–2.
2. *Constraint violations* — does the data break the structural rules of your
   domain (cardinalities, required fields, layering). Caught by Stage 3 (SHACL).
3. *Logical contradictions* — do the axioms conflict such that no consistent
   model exists, or is some class provably impossible to instantiate. Caught by
   Stage 4 (reasoner).

Plus a fourth, *custom invariants* ("no dependency cycles," "every module
reachable from root"), which are Stage 5 (SPARQL-as-tests).

**What this chain deliberately does NOT do.** It verifies the spec is
internally well-formed and rule-compliant. It cannot tell you the spec
faithfully describes the *actual software* — that gap is closed by review, or
by generating the spec from code. And it cannot check *behavioral* properties
("can this deadlock," "is this state reachable over time"); those are not
ontology questions and belong to a model checker like TLA+. If your most
valuable checks turn out to be behavioral, that is a signal you need a different
tool, not a bigger ontology.

---

## 3. The toolchain, stage by stage

Each stage below lists the primary CLI, an alternative in the other runtime
(JVM vs Python), and what it catches.

### Stage 0 — Canonical formatting

Turtle lets you write the same triples many ways; without normalization your
diffs are noise. Pick one canonicalizing serializer and commit only its output.
This is `gofmt`/`prettier` for the spec — run it on save and in pre-commit.

- **Primary:** `rdf-toolkit` serializer — produces stable, sorted Turtle
  designed specifically to diff cleanly.
- **Alternative:** `riot --formatted=TURTLE in.ttl` (Apache Jena) — good enough,
  already present if you use Jena elsewhere.

Acceptance: running the formatter twice is a no-op (idempotent), and a
semantically-null edit produces an empty git diff.

### Stage 1 — Syntax validation

The cheapest gate. Fails in milliseconds on unclosed IRIs, missing periods, bad
prefixes. Nothing heavier runs until this is green.

- **Primary:** `riot --validate file.ttl` (Jena) or `rapper -c file.ttl`
  (raptor).
- **JS option:** an N3.js parse wrapped in try/catch does the same in-browser or
  in Node.

### Stage 2 — Lint / hygiene

Well-formed, not merely valid. Flags: prefixes declared-but-unused or
used-but-undeclared; terms a vocabulary doesn't actually define (catches typos
like `foaf:naem`); IRIs that violate your naming convention; orphan nodes. Best
done as two complementary things:

- **A dedicated linter:** `rdflint` (SPARQL-based rules, undefined-subject
  detection, datatype checks).
- **Your own SPARQL-as-lint rules:** any query that returns rows = a lint
  failure. This is where project-specific conventions live (e.g. "every
  component IRI must sit under `http://…/component/`"). Run via `robot verify`
  or a short script.

### Stage 3 — Constraint validation (SHACL)

Check the model against the structural rules of *your* domain — the closed-world
workhorse. Cardinalities ("exactly one owner"), required properties, value
ranges, layering rules. The validation report is itself RDF, so an agent can
parse failures and act on them.

- **Primary (Python):** `pyshacl data.ttl -s shapes.ttl`
- **Alternative (JVM):** `shacl validate --data data.ttl --shapes shapes.ttl`
  (Jena)

Keep shapes in a separate versioned file. This is likely where you'll spend the
most spike time, because writing good shapes *is* the act of pinning down what
"correct" means for your system.

### Stage 4 — Logical consistency + classification (OWL reasoner)

Only meaningful if your spec uses real OWL semantics (class hierarchies,
disjointness, property characteristics). Gives you consistency checking,
unsatisfiable-class detection, and the full inferred hierarchy.

- **Primary:** `robot reason --reasoner hermit` plus `robot report` (a battery
  of built-in quality checks). Use the ELK reasoner instead if your ontology is
  in the OWL 2 EL profile — dramatically faster.
- **The key output:** `robot explain` returns the *justification* — the minimal
  set of conflicting axioms behind an inconsistency. This is the single most
  useful thing for fixing problems: it doesn't just say "broken," it hands you
  the three axioms that conflict.
- **Python option:** `owlready2` driving HermiT or Pellet.

If your spec is purely structural (no OWL axioms), you can skip this stage
entirely for the spike and add it later — note that in your findings.

### Stage 5 — Custom invariants / spec tests (SPARQL-as-tests)

Your regression suite. Each test is a named SPARQL query with an expected
result, run pass/fail. This is where reachability and cycle detection live,
using property paths: `dependsOn+` finds dependency cycles; a query can assert
every module is reachable from a root. Treat these exactly like unit tests —
add one every time you find a spec bug so it can't regress.

- **Runner:** `robot verify` (fails the build if any query returns rows), or a
  small Python/`rdflib` harness if you're staying single-runtime.

### Stage 6 (optional) — Byproducts: docs + diagrams

Once green, generate artifacts so the spec stays the source of truth:
`pyLODE` or `WIDOCO` produce HTML docs from the ontology; an N3.js + Cytoscape.js
viewer renders an interactive graph. Wire these to publish only on green builds.
Out of scope for the initial spike but worth noting as the natural next step.

---

## 4. Orchestration

**Task runner.** A plain `Makefile` is ideal — agent-friendly, no framework.
Targets map one-to-one onto stages; `all` runs them fail-fast.

```makefile
SPEC   := spec/system.ttl
SHAPES := shapes/shapes.ttl
TESTS  := tests/

.PHONY: all format lint validate reason test

format:
	rdf-toolkit serialize --source $(SPEC) --target $(SPEC) --format turtle

lint:
	riot --validate $(SPEC)
	rdflint $(SPEC)

validate:
	pyshacl $(SPEC) -s $(SHAPES) -f human

reason:
	robot reason --input $(SPEC) --reasoner hermit --output /dev/null
	robot report --input $(SPEC) --output report.tsv

test:
	robot verify --input $(SPEC) --queries $(TESTS)/*.rq

all: lint validate reason test
```

**pre-commit.** Run the fast stages (format, syntax, lint) locally so nothing
malformed is ever committed. Use the `pre-commit` framework pointed at your
format and `riot --validate` steps.

**CI.** Run the *full* pipeline (including reason + test) on every push via
GitHub Actions or equivalent. One wrinkle to plan for: **this stack is
polyglot.** ROBOT and Jena are JVM tools; pyshacl/rdflib/owlready2 are Python.
Your CI image needs both a JDK and Python. That is normal for this space. If you
want single-runtime, you can go all-JVM (Jena + ROBOT + Jena SHACL) or
mostly-Python (rdflib + owlready2 + pyshacl) at the cost of some best-of-breed
tools — worth explicitly deciding during the spike.

---

## 5. Suggested repo layout

```
spec/          canonical .ttl — the source of truth
shapes/        SHACL shapes
ontology/      OWL axioms, if kept separate from instance data
tests/         SPARQL pass/fail checks (*.rq)
Makefile       orchestration
.pre-commit-config.yaml
.github/workflows/verify.yml
report.tsv     (generated, gitignored)
```

Keeping instance data (`spec/`), constraints (`shapes/`), and axioms
(`ontology/`) in separate files pays off quickly: you can reason over axioms +
data together while validating shapes independently, and reviewers can see at a
glance whether a change touched the rules or the description.

---

## 6. The spike plan

Time-boxed to roughly 2–3 focused days. Each step has an acceptance criterion so
you know when to stop.

**Day 1 — Skeleton end-to-end.**
1. Pick or write a *small* slice of the system spec — a handful of components and
   their relationships is plenty. Don't model everything; model enough to
   exercise every stage.
2. Install the tools (see §7) and get each CLI running once by hand on the
   sample.
3. Write the `Makefile`. **Acceptance:** `make all` runs all stages on the
   sample and exits 0.
4. Deliberately break the file three ways — a syntax error, a constraint
   violation, a logical contradiction — and confirm the *right stage* catches
   each with a comprehensible message. **Acceptance:** each break fails exactly
   the stage you expect, and `robot explain` gives a usable justification for
   the contradiction.

**Day 2 — Make the checks real.**
5. Write 3–5 SHACL shapes encoding actual rules of your system. **Acceptance:**
   they pass on a correct spec and fail on a plausibly-wrong one.
6. Write 2–3 SPARQL tests including one graph-traversal check (e.g. no
   dependency cycles via `dependsOn+`). **Acceptance:** the cycle test catches a
   cycle you introduce on purpose.
7. Decide the runtime question (polyglot vs single-runtime) based on what
   friction you actually hit. Record the decision.

**Day 3 — Automate and write up.**
8. Wire pre-commit (fast stages) and a CI workflow (full pipeline).
   **Acceptance:** a PR with a broken spec fails CI; a clean one passes.
9. Write findings: which stages earned their keep on *your* spec, which felt
   like overhead, whether OWL reasoning added value or whether SHACL + SPARQL
   covered everything (common outcome for purely structural specs), and whether
   any check you wanted was actually behavioral (→ TLA+ signal).

**Overall success criterion for the spike:** a teammate can clone the repo, run
`make all`, and a deliberately-planted spec bug is caught by CI with a message
that points at the fix.

---

## 7. Tool reference

| Tool | Runtime | Install | Used in |
|------|---------|---------|---------|
| Apache Jena (`riot`, `shacl`, `arq`) | JVM | download dist / `brew install jena` | Stages 0,1,3 |
| ROBOT | JVM | download jar from ROBOT site | Stages 2,4,5 |
| rdf-toolkit | JVM | download jar | Stage 0 |
| rdflint | JVM | download jar | Stage 2 |
| pyshacl | Python | `pip install pyshacl` | Stage 3 |
| rdflib | Python | `pip install rdflib` | Stage 5 (custom harness) |
| owlready2 | Python | `pip install owlready2` | Stage 4 (alt) |
| raptor (`rapper`) | native | `apt/brew install raptor2-utils` | Stage 1 (alt) |
| N3.js | JS/Node | `npm install n3` | Stage 1 (JS), viewer |
| Cytoscape.js | JS | `npm install cytoscape` | Stage 6 (viewer) |
| pyLODE / WIDOCO | Python / JVM | `pip install pylode` / jar | Stage 6 (docs) |

Verify exact current versions and install commands when you start — several of
these are JVM jars whose download locations and version numbers change.

---

## 8. Honest caveats to keep in view

- **Internal, not faithful.** Green means the spec is self-consistent and
  rule-compliant, not that it matches reality. Close that gap with review or by
  deriving the spec from code.
- **Reasoning may be overkill.** If your spec is purely structural, SHACL +
  SPARQL likely cover everything and the OWL reasoner adds cost without value.
  The spike should tell you which camp you're in.
- **Behavioral properties are out of scope.** "Can it deadlock / reach a bad
  state" is a model-checking question (TLA+), not an ontology one. No amount of
  this chain reaches it.
- **Polyglot by default.** Best-of-breed means a JDK *and* Python in CI. Decide
  early whether that's acceptable or whether you consolidate on one runtime.

---

*End of document.*
