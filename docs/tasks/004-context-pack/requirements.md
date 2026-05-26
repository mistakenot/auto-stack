# Task 004: Context Pack

## Problem

`autograph code graph` can show how TypeScript files depend on each other, but a coding agent still has to decide which files to read, in what order, and what risks to watch for. Given seed files and a token budget, autograph should produce a bounded context bundle that explains the relevant dependency neighborhood and includes the file content most likely to matter.

## Assumptions

- Initial implementation targets the existing TypeScript import graph foundation.
- The command accepts a project directory, a required token limit, and one or more file paths relative to the project root.
- Markdown is the default output for this command because the primary consumer is an LLM context window and markdown avoids JSON key/escaping overhead.
- JSON remains available via `--format=json` for scripts, tests, and downstream tools.
- Callers are expected to understand the full autograph API from `autograph docs` / `autograph quickstart`; the context pack should not spend tokens explaining general tool usage.

## Goals

- Add a context-pack command that accepts seed file paths and a token limit, for example `autograph code context ./project --token-limit 12000 --file src/App.tsx --file src/hooks/useAuth.ts`.
- Build the current import graph, derive reverse dependencies, and select a prioritized neighborhood around the seed files.
- Include seed files first, then direct dependencies, direct dependents, nearby transitive files, and relevant high-risk edges as the token budget allows.
- Attach reasons for every included file so an agent knows whether it is a seed, dependency, dependent, entrypoint-like file, test-like file, cycle member, or high fan-in/fan-out file.
- Estimate token cost deterministically from file contents, keep the returned pack within the requested budget, and report omitted candidates with reasons.
- Output a compact markdown bundle by default, containing reading order, included file contents, relationship summaries, omitted candidates, and guidance on what to do, what to look for, and what to expect.
- Support `--format=json` for callers that need a parseable payload.
- Keep the bundle context-efficient: prioritize project-specific evidence, omit generic instructions, avoid repeating information, and prefer compact summaries when full text would not change the caller's next action.

## Acceptance Criteria

**AC-1**: Context command accepts seed files and token limit
- Given: a TypeScript project with `tsconfig.json`
- When: `autograph code context ./project --token-limit 12000 --file src/App.tsx --file src/hooks/useAuth.ts` runs
- Then: autograph builds a context pack rooted at those seed files and exits successfully with markdown on stdout

**AC-2**: Path validation and normalization
- Given: duplicate, whitespace-padded, absolute, missing, or out-of-project file paths
- When: the context command validates inputs
- Then: it normalizes safe project-relative paths, dedupes repeated seed files, rejects invalid paths, and reports concrete remediation hints

**AC-3**: Graph-aware file selection
- Given: seed files that import dependencies and are imported by other files
- When: the context pack is built
- Then: the pack includes the seed files, prioritizes direct runtime dependencies and direct dependents, includes type-only and transitive neighbors only as budget allows, and records the graph relationship that justified each candidate

**AC-4**: Token budget enforcement
- Given: a token limit smaller than the full dependency neighborhood
- When: the context pack is generated
- Then: included file contents stay within the estimated token limit, lower-priority candidates are omitted with reasons, and the output reports `token_limit`, `estimated_tokens`, and `omitted_tokens`
- Given: seed file contents alone exceed the token limit
- When: the command runs
- Then: it fails fast with a structured error explaining the minimum estimated budget needed

**AC-5**: Default markdown output contract
- Given: a successful context pack
- When: the command runs without `--format`
- Then: stdout contains a compact LLM-friendly markdown bundle with sections for budget, reading order, included files, relationships, omitted candidates, and guidance
- And: the markdown avoids command tutorials, generic API explanations, repeated path lists, and boilerplate prose

**AC-6**: JSON output contract
- Given: a successful context pack
- When: the command runs with `--format=json` and output is parsed as JSON
- Then: it contains `project_root`, `token_limit`, `estimated_tokens`, `seed_files`, `reading_order`, `files`, `relationships`, `guidance`, and `omitted_candidates`
- And: each included file entry contains `path`, `role`, `reason`, `estimated_tokens`, and `content`

**AC-7**: Guidance for agents
- Given: the graph contains dependents, cycles, side-effect imports, dynamic imports, re-exports, or high fan-in/fan-out files
- When: the context pack is generated
- Then: the `guidance` section highlights what to do, what to inspect, and what behavioral risks to expect from those relationships

**AC-8**: Context efficiency and signal discipline
- Given: a successful context pack
- When: the output is inspected
- Then: it avoids generic autograph API explanations, command tutorials, repeated path lists, and boilerplate prose
- And: it spends the token budget on seed file content, selected neighboring file content, relationship facts, omitted-candidate facts, and concise task-relevant guidance
- And: every non-content section is short enough to justify its token cost by changing what an agent should read, edit, test, or avoid

**AC-9**: Deterministic ordering
- Given: the same project, seed files, and token limit
- When: the context command is run repeatedly
- Then: included files, omitted candidates, guidance, and JSON field ordering are stable enough for fixture/golden tests

**AC-10**: Fixture-based tests
- Given: checked-in TypeScript fixtures with dependencies, dependents, type-only imports, side-effect imports, dynamic imports, cycles, and oversized files
- When: `go test ./...` runs in `auto-graph`
- Then: tests validate candidate selection, token budgeting, default markdown output, JSON output, input validation, and deterministic ordering

## Out of Scope

- AI-generated summaries or semantic interpretation of file contents
- Repeating command reference, flag documentation, or generic autograph usage guidance already available from docs/quickstart
- Symbol-level dependency analysis
- Persistent graph caching or watch mode
- Pulling in documentation, git history, session history, or package-manager metadata
- Multi-tsconfig monorepo context planning
- Go context packs until the Go graph task is implemented and ready to compose with this command

## Open Questions

- (none, all resolved)
