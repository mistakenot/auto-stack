---
hash: "a42d3d65"
id: "9a31f8c7"
read_when: "adding symbol-aware code context and navigation features to autoskill"
summary: "Technical notes on SCIP (scip-code.org) with practical adoption guidance for auto-skill."
title: "SCIP Notes for auto-skill"
---

# SCIP Notes for auto-skill

## Why SCIP is relevant to autoskill

SCIP gives us a language-agnostic, standardized way to represent symbol-level code intelligence (definitions, references, implementations, type relationships, diagnostics, syntax kinds). For `autoskill`, this can upgrade skill authoring and linting from path-level matching to symbol-level context selection.

## What SCIP is (and is not)

From SCIP’s design docs:
- It is a transmission format for code intelligence data.
- It is not intended to be the final query/storage model.
- It is optimized for indexer producers (ease of emission, streaming, debuggability), not for direct consumer querying.

Implication for us:
- Ingest `.scip` as an interchange artifact.
- Build `autoskill`-specific derived indexes/tables for fast lookups and skill workflows.

## Core protocol details that matter for implementation

Top-level structure:
- `Index.metadata` (must appear first in the stream, once).
- `Index.documents[]` (repo-local files).
- `Index.external_symbols[]` (optional symbols from external packages, for hover/docs when upstream index is absent).

Path + root constraints:
- `Metadata.project_root` is a URI-encoded absolute path.
- `Document.relative_path` must be canonical, relative, slash-separated, no leading slash, no `.` or `..`.

Ranges and encodings:
- `Occurrence.range` is compact int array encoding:
- 3 ints: `[startLine, startChar, endChar]` (same line)
- 4 ints: `[startLine, startChar, endLine, endChar]`
- Values are 0-based.
- `Document.position_encoding` can be UTF-8, UTF-16, or UTF-32 code-unit offset from line start.
- `Metadata.text_document_encoding` is separate and refers to file text on disk.

Symbol model:
- `Occurrence.symbol` points to `SymbolInformation.symbol`.
- Symbol string grammar is standardized (scheme, package tuple, descriptor chain) and includes local symbols (`local <id>`).
- `SymbolInformation.kind` is the preferred semantic category (better than relying on descriptor suffix only).
- `Relationship` encodes cross-symbol semantics for find-references, find-implementations, and go-to-type-definition behavior.

Occurrence semantics:
- `symbol_roles` is a bitset (`Definition`, `Import`, `ReadAccess`, `WriteAccess`, `Generated`, `Test`, `ForwardDefinition`).
- Optional `override_documentation` allows occurrence-specific docs (helpful for typed/generic specializations).
- Optional `enclosing_range` supports higher-level structure use cases (outline/call hierarchy-like grouping).

## Operational tooling to reuse immediately

The `scip` CLI already provides key developer workflows:
- `scip lint` for index well-formedness checks.
- `scip print` for inspection/debugging.
- `scip stats` for quick index profiling.
- `scip snapshot` + `scip test` for golden-style correctness tests against human-readable test files.

This is enough to build a reliable `autoskill` integration test harness without inventing custom validators first.

## Governance and roadmap signal

As of March 2026, SCIP moved to community-driven governance with:
- A Core Steering Committee.
- A public SEP (SCIP Enhancement Proposal) process for major schema/architecture changes.

Implication for us:
- Track SEPs and schema changes proactively.
- Keep parser/ingestor behind capability flags and strict compatibility tests.

## Concrete adoption plan for auto-skill

### Phase 1: ingestion + normalization

Add an importer:
- `autoskill scip import --from <index.scip> --root <repo>`

Normalize into internal tables:
- `documents(path, language, position_encoding)`
- `symbols(symbol, kind, display_name, docs, signature, enclosing_symbol)`
- `occurrences(path, range, symbol, roles, syntax_kind, enclosing_range)`
- `relationships(src_symbol, dst_symbol, ref, impl, type_def, is_def)`

Validation gates:
- Verify metadata-first stream contract.
- Verify path canonicality and root containment.
- Verify range shape (`len=3|4`) and encoding compatibility.

### Phase 2: skill-aware features

Use SCIP-derived context in authoring/lint:
- Suggest `important_if` conditions from observed symbol activity.
- Validate that code snippets in `SKILL.md` reference existing symbols.
- Warn when a skill’s stated scope doesn’t match symbol distribution.

Add symbol-scoped context export:
- `autoskill context --skill <name> --symbols <sym...>`
- Emits compact symbol neighborhoods: defs, refs, impl/type links, signatures.

### Phase 3: drift + impact analysis

Link skill files to symbol sets:
- Track how symbol graphs changed between revisions.
- Flag when high-impact symbols for a skill moved/renamed/disappeared.

Add skill impact reports:
- “This skill touched X files / Y symbols / Z impl links”.
- Useful for prioritizing skill revalidation.

### Phase 4: ecosystem alignment

- Subscribe to SEP changes and test against newest `scip.proto`.
- Keep compatibility matrix by indexer (`tool_info.name/version`).
- Add fallback behavior for partial indexes with heavy `external_symbols`.

## Key engineering cautions

- Do not assume one position encoding across all documents.
- Do not treat local symbols as globally stable IDs.
- Do not query directly over raw SCIP stream at runtime; precompute indexes.
- Do not rely on non-JSON TTY output from `scip print` in scripts.
- Do not collapse `SymbolInformation.kind` into a language-agnostic “class/function” too early; preserve raw kind.

## Suggested autoskill command surface (draft)

- `autoskill scip import --from index.scip`
- `autoskill scip lint --from index.scip`
- `autoskill scip stats --from index.scip`
- `autoskill scip symbol find --name <pattern>`
- `autoskill scip context --symbol <symbol-id> --depth <n>`
- `autoskill skill impact --name <skill>`

## Sources

- SCIP homepage: https://scip-code.org/
- SCIP protocol docs: https://scip-code.org/docs.html
- SCIP governance: https://scip-code.org/governance.html
- SCIP repo: https://github.com/scip-code/scip
- `scip.proto`: https://github.com/scip-code/scip/blob/main/scip.proto
- Design rationale: https://github.com/scip-code/scip/blob/main/docs/DESIGN.md
- CLI reference: https://github.com/scip-code/scip/blob/main/docs/CLI.md
- Development/debugging: https://github.com/scip-code/scip/blob/main/docs/Development.md
- `scip test` file format: https://github.com/scip-code/scip/blob/main/docs/test_file_format.md
- “The future of SCIP” announcement (governance transition): https://sourcegraph.com/blog/the-future-of-scip
