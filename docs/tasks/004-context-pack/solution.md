---
hash: "420f3aae"
id: "81b586d8"
read_when: "implementing or reviewing the autograph context-pack solution design"
summary: "Design for the autograph code context command using a token-budgeted dependency neighborhood, a reusable internal/codegraph package, and merged import metadata."
title: "Solution: Task 004 — Context Pack"
---

# Solution: Task 004

## Approach

1. **Move graph construction out of the CLI** into a small reusable `internal/codegraph` package. The existing `autograph code graph` command already knows how to detect TypeScript, discover files, scan imports, resolve aliases, and build `graph.Graph`; `autograph code context` should call the same path instead of duplicating it. The CLI keeps argument parsing and output selection, while `codegraph.Build` owns the graph-building workflow. During this refactor, preserve multiple import relationships between the same source and target by merging import metadata instead of dropping duplicates: `import_kind` stores the primary kind, `import_kinds` stores a stable comma-separated set, and `raws` stores stable comma-separated raw import strings.
2. **Add `autograph code context <dir>`** with flags `--token-limit`, repeatable `--file`, `--format` (`markdown` default, `json` optional), and `--lang`. Markdown is the documented default because the output is meant for LLM context windows and avoids JSON key/escaping overhead. JSON remains available for scripts and tests.
3. **Define a context-pack model** in `internal/contextpack` that is compact but still structured: project root, token budget, normalized seed files, reading order, included file entries, relationship facts, concise guidance, and omitted candidates. The JSON formatter emits this model directly; the markdown formatter renders the same model without generic command help or repeated boilerplate.
4. **Normalize and validate seed paths** before graph selection. Trim whitespace, clean paths, convert absolute paths inside the project to project-relative paths, dedupe in input order, reject paths outside the project, reject missing files, and reject files not present in the graph node set. Validation returns structured errors with `code`, `path`, `field`, `message`, and optional `value`, then the CLI prints those errors to stderr.
5. **Build dependency neighborhoods from the graph**. Create forward and reverse adjacency maps from import edges. Candidate priority is deterministic: seeds first; direct runtime dependencies; direct runtime dependents; direct high-risk neighbors; direct type-only neighbors; cycle members touching seeds; then second-hop neighbors as budget allows. Runtime imports outrank type-only imports using the preserved `import_kinds` metadata; side-effect, dynamic, re-export, cycle, and high fan-in/fan-out facts are retained as risk flags.

<!-- RESOLVED(P2): Import-kind metadata can be lost before context selection
REVIEW: I checked `auto-graph/internal/scanner/typescript.go` and `auto-graph/internal/cli/code_graph.go`: `TypeScriptScanner.Scan` dedupes matches by `(sourceFile, importPath)`, and `buildGraph` dedupes edges by `(source, target)`. If a source has both type-only and runtime imports to the same target, or otherwise multiple relationships to the same resolved file, the current graph path keeps only one `import_kind`. The context-pack selection relies on runtime imports outranking type-only imports and retaining side-effect/dynamic/re-export risk flags, so the solution needs an explicit change to preserve or merge import kinds during the `internal/codegraph` refactor before `contextpack` consumes the graph.
AUTHOR: Added an explicit codegraph refactor requirement to merge duplicate source/target import metadata instead of dropping it. The graph keeps a primary `import_kind`, a stable `import_kinds` set, and stable `raws`, and context selection is now defined to consume `import_kinds`.
-->

6. **Enforce the token budget with a deterministic estimator over the selected output format**. Use one shared estimator, `EstimateTokens(s string) = max(1, ceil(len([]rune(s)) / 4))`, against the final rendered payload for the requested format (`markdown` by default, `json` for `--format=json`). Seed files are mandatory; if seed content plus required pack metadata exceeds the selected format's limit, fail fast with a structured `seed_budget_exceeded` error that reports the minimum estimated budget for that format. Non-seed candidates are included only while the tentative rendered payload for the selected format remains within budget; `estimated_tokens` is the final selected-format payload estimate, while `omitted_tokens` is the sum of omitted candidate content estimates.

<!-- RESOLVED(P2): Budget rule is markdown-specific but JSON is also a returned pack
REVIEW: AC-4 and the Goals say the returned pack must stay within the requested token budget, and AC-6 requires `--format=json`. This step only gates non-seed inclusion on the "estimated markdown payload"; JSON adds keys, quotes, escaped newlines, and array/object syntax. Define whether the budget is computed over the canonical model independent of renderer, over each rendered format, or markdown-only with a documented exception for JSON; otherwise `--format=json` can satisfy selection tests while returning a payload that exceeds the requested budget.
AUTHOR: Changed the budget rule to estimate the final rendered payload for the requested format. Markdown and JSON now each gate candidate inclusion against their own rendered output, and `estimated_tokens` reports that selected-format estimate.
-->

7. **Keep guidance algorithmic and context-efficient**. Do not summarize file semantics or repeat autograph API usage. Generate short guidance only from graph facts: what to read first, which dependents may break if a seed changes, which imports are runtime-sensitive, where cycles/re-exports/dynamic imports exist, and which omitted files may be worth fetching next if more budget is available.
8. **Add focused fixtures and golden assertions**. Reuse existing TypeScript fixtures where possible, then add a context-specific fixture with enough graph shape to exercise direct dependencies, dependents, type-only imports, side effects, dynamic imports, cycles, omitted candidates, and oversized seeds.

### CLI Contract

```bash
autograph code context <dir> \
  --token-limit 12000 \
  --file src/App.tsx \
  --file src/hooks/useAuth.ts

autograph code context <dir> --token-limit 12000 --file src/App.tsx --format=json
```

Supported formats:
- `markdown` (default): compact LLM-ready bundle.
- `json`: parseable payload with stable struct field ordering.

The command still writes successful payloads to stdout and diagnostics/errors to stderr.

### Context Pack Shape

```go
type Pack struct {
    ProjectRoot       string             `json:"project_root"`
    TokenLimit        int                `json:"token_limit"`
    EstimatedTokens   int                `json:"estimated_tokens"`
    OmittedTokens     int                `json:"omitted_tokens"`
    SeedFiles         []string           `json:"seed_files"`
    ReadingOrder      []ReadingOrderItem `json:"reading_order"`
    Files             []FileEntry        `json:"files"`
    Relationships     []Relationship     `json:"relationships"`
    Guidance          Guidance           `json:"guidance"`
    OmittedCandidates []OmittedCandidate `json:"omitted_candidates"`
}

type FileEntry struct {
    Path            string   `json:"path"`
    Role            string   `json:"role"`
    Reason          string   `json:"reason"`
    EstimatedTokens int      `json:"estimated_tokens"`
    Flags           []string `json:"flags,omitempty"`
    Content         string   `json:"content"`
}

type Relationship struct {
    Source            string   `json:"source"`
    Target            string   `json:"target"`
    Direction         string   `json:"direction"`
    PrimaryImportKind string   `json:"primary_import_kind"`
    ImportKinds       []string `json:"import_kinds"`
    Distance          int      `json:"distance"`
    Reason            string   `json:"reason"`
}
```

Roles stay intentionally small: `seed`, `dependency`, `dependent`, `type_dependency`, `type_dependent`, `cycle_neighbor`, and `transitive_neighbor`. Flags capture facts such as `side_effect_import`, `dynamic_import`, `reexport`, `cycle`, `high_fan_in`, `high_fan_out`, `entrypoint_like`, and `test_like`.

Risk flag derivation is fixed and testable:
- `side_effect_import`, `dynamic_import`, and `reexport`: any relationship touching the file has that import kind in `import_kinds`.
- `cycle`: file belongs to a strongly connected component with more than one file, or has a self-edge.
- `high_fan_in`: file has incoming edges from at least 3 distinct source files.
- `high_fan_out`: file has outgoing edges to at least 5 distinct target files.
- `entrypoint_like`: path base is one of `index.ts`, `index.tsx`, `main.ts`, `main.tsx`, `app.ts`, `app.tsx`, or path contains `/pages/`, `/routes/`, or `/app/`.
- `test_like`: path base ends with `.test.ts`, `.test.tsx`, `.spec.ts`, or `.spec.tsx`, or path contains `/__tests__/`, `/test/`, or `/tests/`.

Flags are deduped and sorted lexicographically before rendering.

<!-- RESOLVED(P2): Risk flag heuristics are not deterministic enough for golden tests
REVIEW: The existing `graph.Graph` only gives paths and import edges, so flags like `high_fan_in`, `high_fan_out`, `entrypoint_like`, and `test_like` need exact derivation rules. AC-7 requires guidance for high fan-in/fan-out files, and AC-9 requires stable fixture/golden assertions, but the solution does not define fan-in/fan-out thresholds or path/name heuristics for entrypoint/test detection. Add those rules so implementers and tests do not choose incompatible thresholds.
AUTHOR: Added exact deterministic flag rules for import-kind flags, cycles, fan-in/fan-out thresholds, entrypoint-like paths, test-like paths, and flag ordering.
-->

### Candidate Selection

Candidate scoring is deterministic and intentionally simple:

```go
type Candidate struct {
    Path            string
    Role            string
    Priority        int
    Distance        int
    Reason          string
    Relationship    Relationship
    Flags           []string
    EstimatedTokens int
}
```

Priority order:
1. Seed files (`priority=0`)
2. Direct runtime dependencies (`priority=10`)
3. Direct runtime dependents (`priority=20`)
4. Direct neighbors with risk flags (`priority=25`)
5. Direct type-only dependencies/dependents (`priority=30`)
6. Cycle members touching seeds (`priority=35`)
7. Second-hop runtime dependencies/dependents (`priority=40`)
8. Other second-hop type-only neighbors (`priority=50`)

Ties sort by distance, then path. Duplicate candidates merge reasons and flags rather than producing duplicate output.

### Markdown Shape

The default markdown output should be terse:

````markdown
# Context Pack

Budget: 8420/12000 tokens
Omitted: 1920 tokens
Seeds: src/App.tsx, src/hooks/useAuth.ts

## Read First
1. src/App.tsx - seed
2. src/hooks/useAuth.ts - seed
3. src/services/userService.ts - direct runtime dependency of src/hooks/useAuth.ts

## Watch
- Changing src/hooks/useAuth.ts may affect src/components/App.tsx.
- src/config.ts has a side-effect import of src/utils/validate.ts.

## Files
### src/App.tsx
Role: seed. Tokens: 430.

```tsx
...
```

## Omitted
- src/services/analyticsService.ts - second-hop dynamic dependency, 620 tokens
````

<!-- RESOLVED(P2): Markdown contract omits the required omitted token total
REVIEW: AC-4 says successful output reports `token_limit`, `estimated_tokens`, and `omitted_tokens`. The model includes `OmittedTokens`, but the default markdown shape only shows `Budget: 8420/12000 tokens` and per-candidate omitted costs. Since markdown is the default output, add the omitted token total to the markdown contract and golden tests so AC-4 is covered outside JSON mode.
AUTHOR: Added the `Omitted: 1920 tokens` line to the markdown contract so the default output reports the required omitted token total.
-->

No command tutorial, API reference, or generic explanation appears in the pack.

## Files

```
+ auto-graph/internal/codegraph/build.go             # reusable Build/DetectLanguage/DiscoverFiles graph construction
+ auto-graph/internal/codegraph/build_test.go        # graph construction tests, including merged import-kind metadata
+ auto-graph/internal/contextpack/model.go           # Pack, FileEntry, Relationship, Guidance, OmittedCandidate types
+ auto-graph/internal/contextpack/validate.go        # seed path normalization and structured validation errors
+ auto-graph/internal/contextpack/validate_test.go   # path normalization and validation tests
+ auto-graph/internal/contextpack/builder.go         # graph neighborhood selection, candidate scoring, budget enforcement
+ auto-graph/internal/contextpack/token.go           # deterministic token estimator and pack cost helpers
+ auto-graph/internal/contextpack/token_test.go      # token estimator and budget edge case tests
+ auto-graph/internal/contextpack/markdown.go        # compact markdown renderer
+ auto-graph/internal/contextpack/json.go            # JSON renderer using stable struct order
+ auto-graph/internal/contextpack/builder_test.go    # selection, budget, guidance, omitted-candidate tests
+ auto-graph/internal/contextpack/format_test.go     # markdown/JSON contract and signal-discipline tests
+ auto-graph/testdata/fixtures/context-pack/         # TypeScript fixture covering deps, dependents, risks, cycles, oversized files
+ auto-graph/testdata/golden/context-pack/           # expected markdown and JSON context-pack outputs
+ auto-graph/internal/cli/code_context.go            # Cobra command and CLI wiring for `autograph code context`
+ auto-graph/internal/cli/code_context_test.go       # CLI validation, format, and error behavior tests
~ auto-graph/internal/scanner/typescript.go          # preserve duplicate path matches when import kinds differ
~ auto-graph/internal/scanner/typescript_test.go     # cover type+runtime imports to the same raw path
~ auto-graph/internal/cli/code.go                    # register newCodeContextCmd()
~ auto-graph/internal/cli/code_graph.go              # delegate graph construction to internal/codegraph
~ auto-graph/internal/cli/docs.go                    # document `code context` with markdown default and JSON option
~ auto-graph/internal/cli/quickstart.go              # add one compact context-pack example
~ auto-graph/CLAUDE.md                               # mention context-pack command in architecture/usage notes
```

## Test Coverage

| AC  | Test Type   | File |
|-----|-------------|------|
| AC-1 | CLI/unit | `auto-graph/internal/cli/code_context_test.go` |
| AC-2 | unit | `auto-graph/internal/contextpack/validate_test.go`, `auto-graph/internal/contextpack/builder_test.go` |
| AC-3 | unit/fixture | `auto-graph/internal/contextpack/builder_test.go`, `auto-graph/internal/codegraph/build_test.go`, `auto-graph/testdata/fixtures/context-pack/` |
| AC-4 | unit | `auto-graph/internal/contextpack/builder_test.go`, `auto-graph/internal/contextpack/token_test.go` |
| AC-5 | unit/golden | `auto-graph/internal/contextpack/format_test.go`, `auto-graph/testdata/golden/context-pack/` |
| AC-6 | unit/golden | `auto-graph/internal/contextpack/format_test.go`, `auto-graph/testdata/golden/context-pack/` |
| AC-7 | unit/fixture | `auto-graph/internal/contextpack/builder_test.go` |
| AC-8 | unit/golden | `auto-graph/internal/contextpack/format_test.go` |
| AC-9 | unit/golden | `auto-graph/internal/contextpack/builder_test.go`, `auto-graph/internal/contextpack/format_test.go` |
| AC-10 | fixture/integration | `auto-graph/testdata/fixtures/context-pack/`, `auto-graph/internal/cli/code_context_test.go` |

## Out of Scope

- AI-generated summaries or semantic interpretation of file contents
- Repeating command reference, flag documentation, or generic autograph usage guidance already available from docs/quickstart
- Symbol-level dependency analysis
- Persistent graph caching or watch mode
- Pulling in documentation, git history, session history, or package-manager metadata
- Multi-tsconfig monorepo context planning
- Go context packs until the Go graph task is implemented and ready to compose with this command
- Exact model-token counting with tokenizer-specific libraries; deterministic approximation is sufficient for this task
- Truncating seed file contents to fit budget; seed files either fit or the command fails with the required minimum budget

## Rejected Alternatives

- **JSON default**: Better for machines but worse for the primary LLM-context use case. JSON repeats keys, quotes, braces, and escaped newlines; markdown carries the same high-signal content with less serialization overhead. Keep JSON behind `--format=json`.
- **Graph dump plus file contents**: Simple to implement but token-inefficient. The pack should include selected relationship facts and reasons, not the full node/edge graph.
- **AI-generated file summaries**: Could be compact, but it adds model dependency, nondeterminism, and semantic risk. This task should remain deterministic and source-grounded.
- **Truncate files to fit budget**: Avoids hard failures for large seeds, but partial source can mislead agents. Non-seed files may be omitted; seed files must fit in full.
- **Separate `context` top-level command**: Shorter CLI, but code context belongs under the existing `code` command group next to `code graph`.
- **Persistent graph cache before selection**: Useful later, but unnecessary for this feature and explicitly out of scope.
