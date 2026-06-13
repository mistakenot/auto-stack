---
hash: "8650545e"
id: "0ea257df"
read_when: "implementing the autograph context pack feature or understanding the codegraph/contextpack package layout"
summary: "Implementation plan for autograph code context: extracting reusable graph construction, adding context-pack model/builder/validator/token-estimator/renderer, and a new autograph code context command with markdown and JSON output."
title: "Plan: Task 004 — Context Pack"
---

# Plan: Task 004

## Summary

Extract reusable TypeScript graph construction, preserve import metadata, and add a markdown-first `autograph code context` command backed by deterministic context-pack selection, budgeting, rendering, fixtures, and docs.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | `auto-graph/internal/codegraph/build.go` | Reusable graph build flow currently embedded in `internal/cli/code_graph.go` |
| + | `auto-graph/internal/codegraph/build_test.go` | Unit coverage for reusable graph construction and merged import metadata |
| + | `auto-graph/internal/contextpack/model.go` | Pack, file, relationship, guidance, validation, and omitted-candidate types |
| + | `auto-graph/internal/contextpack/validate.go` | Seed file path normalization and structured validation |
| + | `auto-graph/internal/contextpack/validate_test.go` | Path validation and dedupe tests |
| + | `auto-graph/internal/contextpack/token.go` | Deterministic token estimator and rendered-payload budget helpers |
| + | `auto-graph/internal/contextpack/token_test.go` | Token estimator and budget edge case tests |
| + | `auto-graph/internal/contextpack/builder.go` | Candidate scoring, adjacency maps, risk flags, guidance, and budgeted pack construction |
| + | `auto-graph/internal/contextpack/builder_test.go` | Selection, risk flag, guidance, budget, and deterministic ordering tests |
| + | `auto-graph/internal/contextpack/markdown.go` | Compact markdown renderer |
| + | `auto-graph/internal/contextpack/json.go` | JSON renderer for the pack model |
| + | `auto-graph/internal/contextpack/format_test.go` | Markdown/JSON contract and golden tests |
| + | `auto-graph/internal/cli/code_context.go` | Cobra command for `autograph code context` |
| + | `auto-graph/internal/cli/code_context_test.go` | CLI input validation, default markdown, JSON mode, and error tests |
| + | `auto-graph/testdata/fixtures/context-pack/` | TypeScript fixture covering seeds, dependencies, dependents, risks, cycles, omitted files, oversized seeds |
| + | `auto-graph/testdata/golden/context-pack/` | Expected markdown and JSON pack outputs |
| ~ | `auto-graph/internal/scanner/typescript.go` | Preserve matches when the same source/import path has different import kinds |
| ~ | `auto-graph/internal/scanner/typescript_test.go` | Cover type+runtime imports to the same raw path |
| ~ | `auto-graph/internal/cli/code.go` | Register `newCodeContextCmd()` |
| ~ | `auto-graph/internal/cli/code_graph.go` | Delegate graph construction to `internal/codegraph` while preserving `code graph` behavior |
| ~ | `auto-graph/internal/cli/docs.go` | Document `code context`, markdown default, and JSON option |
| ~ | `auto-graph/internal/cli/quickstart.go` | Add one compact context-pack example |
| ~ | `auto-graph/CLAUDE.md` | Mention context-pack command and updated architecture |

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test

- [ ] `cd auto-graph && go build ./...` -- all packages compile after each Go-file step and at final verification
- [ ] `cd auto-graph && go test ./internal/codegraph ./internal/scanner` -- graph extraction and import metadata tests pass
- [ ] `cd auto-graph && go test ./internal/contextpack` -- validation, token, selection, guidance, markdown, JSON, and golden tests pass
- [ ] `cd auto-graph && go test ./internal/cli` -- `code graph` compatibility and `code context` CLI behavior pass
- [ ] `cd auto-graph && go test ./...` -- full non-e2e suite passes
- [ ] Manual smoke: `cd auto-graph && go run ./cmd/autograph code context testdata/fixtures/context-pack --token-limit 12000 --file src/App.tsx` starts with `# Context Pack`
- [ ] Manual smoke: `cd auto-graph && go run ./cmd/autograph code context testdata/fixtures/context-pack --token-limit 12000 --file src/App.tsx --format=json` emits parseable JSON on stdout

## Execution Sequence

```
Phase 1 (Reusable Graph + Metadata)
        \
         --> Phase 3 (Selection + Guidance) --> Phase 4 (CLI + Renderers) --> Phase 5 (Fixtures, Goldens, Final Verification)
        /
Phase 2 (Context Pack Core)
```

## Plan

### Phase 1: Reusable Graph Build And Import Metadata

- [ ] Step 1.1: Create `auto-graph/internal/codegraph/build.go` with `Build(projectRoot, lang string) (*graph.Graph, error)`, `DetectLanguage(projectRoot string) (string, error)`, and `DiscoverFiles(projectRoot, lang string) ([]string, error)` by moving logic from `internal/cli/code_graph.go`.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 1.2: Update `auto-graph/internal/cli/code_graph.go` so `runCodeGraph` validates the directory, resolves lang/default format as before, calls `codegraph.Build`, and keeps existing JSON/DOT/Mermaid output behavior.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes and no import cycles are introduced.
- [ ] Step 1.3: Update `auto-graph/internal/scanner/typescript.go` dedupe to preserve distinct `(sourceFile, importPath, kind)` matches while still suppressing exact duplicates from overlapping ast-grep patterns.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 1.4: In `internal/codegraph`, merge duplicate resolved `(source,target)` edges instead of dropping metadata. Keep `attrs.import_kind` as primary import kind, add stable comma-separated `attrs.import_kinds`, and add stable comma-separated `attrs.raws`.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 1.5: Add `auto-graph/internal/codegraph/build_test.go` covering language detection, file discovery, TypeScript graph parity, and merged metadata when runtime+type imports resolve to the same target.
  - Verify: run `cd auto-graph && go test ./internal/codegraph`; tests pass.
- [ ] Step 1.6: Update `auto-graph/internal/scanner/typescript_test.go` with a fixture/case proving different import kinds for the same raw path are both returned.
  - Verify: run `cd auto-graph && go test ./internal/scanner`; tests pass.
- [ ] Step 1.7: Run compatibility tests for the existing graph command.
  - Verify: `cd auto-graph && go test ./internal/cli ./internal/format` passes.
- [ ] Step 1.8: Commit: `feat(004): phase 1 - extract graph builder`
  - Verify: `git status --short` shows only unrelated pre-existing changes or a clean task diff after commit.

### Phase 2: Context Pack Core Types, Validation, And Token Budget Helpers

- [ ] Step 2.1: Create `auto-graph/internal/contextpack/model.go` with `Pack`, `ReadingOrderItem`, `FileEntry`, `Relationship`, `Guidance`, `OmittedCandidate`, and structured validation error types matching the solution schema.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 2.2: Create `auto-graph/internal/contextpack/validate.go` to normalize seed file paths: trim, clean, convert absolute paths inside project root, convert to slash paths, dedupe in input order, reject missing/out-of-project/not-in-node-set paths.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 2.3: Add `auto-graph/internal/contextpack/validate_test.go` for whitespace, duplicate seeds, safe absolute paths, missing files, outside-project paths, and not-in-graph paths.
  - Verify: run `cd auto-graph && go test ./internal/contextpack -run TestValidate`; validation tests pass.
- [ ] Step 2.4: Create `auto-graph/internal/contextpack/token.go` with `EstimateTokens(s string) = max(1, ceil(len([]rune(s))/4))` and helpers to estimate candidate content and final rendered payload strings.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 2.5: Add `auto-graph/internal/contextpack/token_test.go` for empty strings, ASCII, multibyte runes, seed-budget failure minimums, and selected-format budget accounting hooks.
  - Verify: run `cd auto-graph && go test ./internal/contextpack -run 'TestEstimate|TestBudget'`; tests pass.
- [ ] Step 2.6: Commit: `feat(004): phase 2 - add context pack core`
  - Verify: `git status --short` shows only unrelated pre-existing changes or a clean task diff after commit.

### Phase 3: Graph-Aware Candidate Selection And Guidance

- [ ] Step 3.1: Create `auto-graph/internal/contextpack/builder.go` with adjacency maps over `graph.Graph`, reverse adjacency, deterministic candidate collection, role assignment, and priority ordering from the solution.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 3.2: Implement import-kind helpers that read `attrs.import_kinds` first, fall back to `attrs.import_kind`, and classify runtime vs type-only relationships.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 3.3: Implement deterministic risk flags: side-effect/dynamic/reexport from import kinds, SCC-based cycles, `high_fan_in` >= 3 distinct sources, `high_fan_out` >= 5 distinct targets, fixed entrypoint-like path rules, fixed test-like path rules, and lexicographic flag ordering.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 3.4: Implement pack construction with mandatory seeds, file content reads, selected-format budget callback support, omitted-candidate recording, and `seed_budget_exceeded` errors when seeds cannot fit.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 3.5: Implement concise guidance generation from graph facts only: read order, dependents that may break, runtime-sensitive imports, cycles/re-exports/dynamic imports, and omitted files worth fetching with more budget.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 3.6: Add `auto-graph/internal/contextpack/builder_test.go` using synthetic graphs for seeds, dependencies, dependents, type-only ordering, merged import kinds, risk flags, cycle flags, omitted candidates, and deterministic sorting.
  - Verify: run `cd auto-graph && go test ./internal/contextpack -run 'TestBuild|TestCandidate|TestGuidance|TestRisk'`; tests pass.
- [ ] Step 3.7: Commit: `feat(004): phase 3 - build graph-aware context packs`
  - Verify: `git status --short` shows only unrelated pre-existing changes or a clean task diff after commit.

### Phase 4: Markdown/JSON Renderers And CLI Wiring

- [ ] Step 4.1: Create `auto-graph/internal/contextpack/markdown.go` with compact markdown rendering: budget line, omitted total, seeds, read-first list, watch guidance, files with fenced content, and omitted candidates.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 4.2: Create `auto-graph/internal/contextpack/json.go` using `json.Encoder` with indentation to emit the `Pack` model in stable struct field order.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 4.3: Add `auto-graph/internal/contextpack/format_test.go` for markdown shape, omitted token total, no generic command tutorial/API prose, valid JSON schema fields, and golden output comparison.
  - Verify: run `cd auto-graph && go test ./internal/contextpack -run 'TestMarkdown|TestJSON|TestGolden'`; tests pass.
- [ ] Step 4.4: Create `auto-graph/internal/cli/code_context.go` with `newCodeContextCmd()` and `runCodeContext`; support required `--token-limit`, repeatable `--file`, `--format=markdown|json`, and `--lang`, with markdown as command-local default.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 4.5: Update `auto-graph/internal/cli/code.go` to register both `newCodeGraphCmd()` and `newCodeContextCmd()`.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 4.6: Add `auto-graph/internal/cli/code_context_test.go` covering required token limit, required seed files, invalid format, default markdown output, JSON output parseability, validation errors to stderr through `ExitError`, and `--lang` override behavior.
  - Verify: run `cd auto-graph && go test ./internal/cli`; CLI tests pass.
- [ ] Step 4.7: Commit: `feat(004): phase 4 - add code context CLI`
  - Verify: `git status --short` shows only unrelated pre-existing changes or a clean task diff after commit.

### Phase 5: Fixtures, Goldens, Docs, And Final Verification

- [ ] Step 5.1: Add `auto-graph/testdata/fixtures/context-pack/` with `tsconfig.json` and TypeScript files covering seed files, direct runtime dependencies, direct dependents, type-only imports, side-effect imports, dynamic imports, re-exports, a two-file cycle, high fan-in, high fan-out, test-like paths, entrypoint-like paths, omitted candidates, and an oversized seed.
  - Verify: run `cd auto-graph && go test ./internal/codegraph ./internal/contextpack`; fixture-based tests pass or fail only on expected not-yet-updated goldens.
- [ ] Step 5.2: Add `auto-graph/testdata/golden/context-pack/` expected markdown and JSON outputs for at least one normal budget and one constrained budget.
  - Verify: run `cd auto-graph && go test ./internal/contextpack`; golden tests pass.
- [ ] Step 5.3: Update `auto-graph/internal/contextpack/format_test.go` and `builder_test.go` if needed to consume the new fixture/goldens and assert `estimated_tokens <= token_limit` for both markdown and JSON modes.
  - Verify: run `cd auto-graph && go test ./internal/contextpack`; tests pass.
- [ ] Step 5.4: Update `auto-graph/internal/cli/docs.go` to include `code context <dir>`, `--token-limit`, repeatable `--file`, markdown default, and `--format=json`.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 5.5: Update `auto-graph/internal/cli/quickstart.go` with one compact context-pack example and no long tutorial prose.
  - Verify: immediately run `cd auto-graph && go build ./...`; build passes.
- [ ] Step 5.6: Update `auto-graph/CLAUDE.md` to mention the context-pack command and `internal/codegraph` / `internal/contextpack` packages.
  - Verify: `rg -n "context|codegraph|contextpack" auto-graph/CLAUDE.md` shows the new entries.
- [ ] Step 5.7: Run final build and non-e2e test suite.
  - Verify: `cd auto-graph && go build ./...` passes; `cd auto-graph && go test ./...` passes.
- [ ] Step 5.8: Run manual smoke checks.
  - Verify: `cd auto-graph && go run ./cmd/autograph code context testdata/fixtures/context-pack --token-limit 12000 --file src/App.tsx` starts with `# Context Pack` and includes `Omitted:`.
  - Verify: `cd auto-graph && go run ./cmd/autograph code context testdata/fixtures/context-pack --token-limit 12000 --file src/App.tsx --format=json` emits JSON parseable by `jq` or `go test` helper and reports `estimated_tokens <= token_limit`.
- [ ] Step 5.9: Commit: `feat(004): phase 5 - add fixtures docs and final verification`
  - Verify: `git status --short` shows only unrelated pre-existing changes or a clean task diff after commit.

## Success Criteria

- [ ] AC-1: `autograph code context <dir> --token-limit N --file path` returns markdown by default for a TypeScript project.
- [ ] AC-2: seed path validation normalizes safe paths, dedupes repeated seeds, and rejects unsafe/missing/not-in-graph paths with structured remediation errors.
- [ ] AC-3: pack selection includes seeds first, prioritizes runtime dependencies and dependents, handles type-only and transitive neighbors according to budget, and records relationship reasons.
- [ ] AC-4: markdown and JSON modes both enforce the token limit against their final rendered payloads; seed overflow fails fast with minimum budget; output reports `token_limit`, `estimated_tokens`, and `omitted_tokens`.
- [ ] AC-5: default markdown contains budget, omitted total, reading order, files, relationships/watch guidance, omitted candidates, and no generic command tutorial or API boilerplate.
- [ ] AC-6: `--format=json` emits parseable JSON with all required top-level and file-entry fields.
- [ ] AC-7: guidance flags dependents, cycles, side-effect imports, dynamic imports, re-exports, high fan-in, and high fan-out from deterministic graph facts.
- [ ] AC-8: every non-content section is compact and justified by read/edit/test/avoid guidance.
- [ ] AC-9: repeated runs with the same fixture, seed files, and token limit produce stable markdown and JSON golden outputs.
- [ ] AC-10: checked-in fixtures and tests cover selection, budgeting, markdown, JSON, validation, and deterministic ordering.
- [ ] `cd auto-graph && go build ./...` passes.
- [ ] `cd auto-graph && go test ./...` passes.

## Open Questions

- (none, all resolved)
