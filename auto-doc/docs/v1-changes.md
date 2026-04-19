---
hash: "1a6a3af6"
id: "b034fd7c"
read_when: "when planning autodoc v1 release and implementation phases"
summary: "Gap analysis between user journey vision and current autodoc, with changes needed for v1"
title: "Autodoc v1 Changes"
---

# Autodoc v1 Changes

Comparison of the [user journey](../../docs/user-journey.md) vision against current autodoc, identifying what needs to change to bring autodoc to v1.

## What already works

These features from the user journey are fully implemented:

- **`autodoc fix`** — validates frontmatter (`id`, `hash`, `title`, `summary`), flags missing/empty summaries, outputs LLM-friendly markdown instructions grouped by parallelism config
- **`autodoc fixed <path>`** — recalculates hash, updates file in-place, re-indexes in search
- **`[autodoc(docId@docHash, scopeHash)]` code tags** — two-way freshness checking: detects doc drift, code drift, orphaned tags, malformed tags
- **Doc ID auto-assignment** — `fix` assigns random 8-char hex IDs to docs missing them
- **BM25 keyword search** — `search reindex` + `search keyword` with JSON output
- **`quickstart`** — LLM-friendly happy-path guide
- **Agent memory file updates** — `autodoc agents` inserts doc index into AGENTS.md/CLAUDE.md

## Changes needed for v1

### 1. Config path alignment

**Current:** `.autodoc/docs.json` in repo root, no global config.
**Target:** `~/.auto/doc/settings.json` (global), `.auto/doc/settings.json` (project-local).

Changes:
- Move project config from `.autodoc/docs.json` to `.auto/doc/settings.json`
- Add global config at `~/.auto/doc/settings.json`
- No automatic migration — old `.autodoc/` is simply no longer read; manual cleanup by user
- Update `.gitignore` handling for the new `.auto/doc/` directory
- Index/search data stays under `.auto/doc/` too (replaces `.autodoc/index/`)

Global config supports fields where machine-level defaults make sense:
- `ignores` — global ignore patterns, unioned with project-local `ignores`
- Other fields use sensible defaults; project-local config overrides where applicable

### 2. `~/.auto/host.json` creation

**Current:** Not created.
**CLAUDE.md says:** "Any tool init should create this, use the current hostname as default host id."

Changes:
- On `autodoc init`, if `~/.auto/host.json` doesn't exist, create it with `{ "host": "<hostname>" }`
- This is a shared convention across all auto-stack tools

### 3. Global vs project init

**Current:** `autodoc init` only does project setup.
**CLAUDE.md convention:** `init` = global setup, `init --project` = project-local setup.

Changes:
- `autodoc init` (no flags): create `~/.auto/doc/settings.json` + `~/.auto/host.json`
- `autodoc init --project`: current behavior — create `.auto/doc/settings.json`, `./docs/` dir, run tree, advise fix
- When `--project` is run and global config doesn't exist, create it automatically

### 4. `--json` flag

**Current:** Some commands output JSON (search keyword), others output text only.
**CLAUDE.md says:** "Most of these commands default to outputting text. Add `--json` to any command to get json output."

Commands that need `--json` support:
- `autodoc tree` — output doc list as JSON array
- `autodoc stale` — output stale files as JSON array with issue details
- `autodoc fix` — output issues as JSON array instead of markdown instructions
- `autodoc agents` — output updated files as JSON
- `autodoc fixed` — output result as JSON (path, old hash, new hash)

In `--json` mode, diagnostics/errors go to stderr, stdout is strictly parseable.

### 5. `doctor` command

**Current:** Not implemented.
**CLAUDE.md convention:** `doctor` checks configuration health, reports problems as JSON with detailed explanations.

Changes:
- Add `autodoc doctor` command
- Checks: global config exists, project config exists and is valid, docs dir exists, search index exists and is not stale, agent files have autodoc markers
- Returns structured errors with remediation hints (e.g. "run `autodoc init`")

### 6. `docs` command

**Current:** Not implemented.
**CLAUDE.md convention:** `docs` outputs full docstring, all functions the tool can do in one output.

Changes:
- Add `autodoc docs` — comprehensive reference of every command, flag, and behavior
- Differs from `quickstart` (which is a happy-path tutorial) by being exhaustive

### 7. Recursive docs discovery

**Current:** Listed as TODO in todo.md, partially designed in `recursive-docs-discovery-tech-design.md`.
**CLAUDE.md says:** Already specified as the intended behavior — "find directories named `docs`, include `.md` files recursively under each."

Changes:
- Implement the recursive discovery described in the tech design doc
- The discovery code in `doctree/` needs to walk the full repo tree, not just the configured `docsDir`

### 8. Text mode output conventions

**Current:** `fix` outputs markdown instructions. Other commands output formatted text.
**CLAUDE.md says:** "In text mode, print successful results first, then append readable errors and remediation instructions."

Changes:
- Ensure `stale`, `tree`, and other commands follow the pattern: results first, errors appended
- Every hard error should include a concrete remediation hint

## Acceptance criteria

### 1. Config path alignment

- [ ] `autodoc` reads project config from `.auto/doc/settings.json` when present
- [ ] `.auto/doc/` directory contains a `.gitignore` that tracks `settings.json` and excludes index/search data
- [ ] Search index is stored under `.auto/doc/index/` instead of `.autodoc/index/`
- [ ] Old `.autodoc/` path is no longer read or written to by any command (manual cleanup by user)
- [ ] Global config at `~/.auto/doc/settings.json` supports `ignores` (unioned with project-local `ignores`)
- [ ] When both global and project-local `ignores` are set, the effective ignore list is the union of both

### 2. `~/.auto/host.json` creation

- [ ] `autodoc init` creates `~/.auto/host.json` with `{ "host": "<current hostname>" }` if the file does not exist
- [ ] If `~/.auto/host.json` already exists, `autodoc init` leaves it untouched
- [ ] `~/.auto/` directory is created if it doesn't exist

### 3. Global vs project init

- [ ] `autodoc init` (no flags) creates `~/.auto/doc/settings.json` with sensible defaults and `~/.auto/host.json`
- [ ] `autodoc init --project` creates `.auto/doc/settings.json`, `./docs/` directory, runs tree, advises fix if stale docs found
- [ ] `autodoc init --project` also runs global init if `~/.auto/doc/settings.json` doesn't exist
- [ ] Running `autodoc init` twice is idempotent — existing configs are not overwritten
- [ ] Running `autodoc init --project` twice is idempotent

### 4. `--json` flag

- [ ] `autodoc tree --json` outputs a JSON array of doc objects (`path`, `title`, `summary`, `hash`, `id`)
- [ ] `autodoc stale --json` outputs a JSON array of stale doc objects with issue type (`missing_frontmatter`, `stale_hash`, `default_title`)
- [ ] `autodoc fix --json` outputs a JSON array of all issues (doc issues + code tag issues) with `type`, `path`, `details`
- [ ] `autodoc fixed <path> --json` outputs `{ "path": "...", "oldHash": "...", "newHash": "..." }`
- [ ] `autodoc agents --json` outputs a JSON array of files that were updated
- [ ] In `--json` mode, stderr receives any diagnostic/warning messages; stdout contains only valid JSON
- [ ] All JSON output is parseable by `jq` without errors
- [ ] Exit codes remain the same regardless of `--json` flag (e.g. `stale` still exits 1 when stale files found)

### 5. `doctor` command

- [ ] `autodoc doctor` checks: global config exists, project config exists and is valid JSON, docs dir exists, search index exists
- [ ] Each check is reported as pass/fail with a remediation hint on failure (e.g. "run `autodoc init`", "run `autodoc init --project`", "run `autodoc search reindex`")
- [ ] Exit code 0 when all checks pass, exit code 1 when any check fails
- [ ] `autodoc doctor --json` outputs structured results as a JSON array of `{ "check": "...", "status": "pass|fail", "message": "..." }`
- [ ] Text mode prints checks in order with pass/fail indicators, errors appended with remediation

### 6. `docs` command

- [ ] `autodoc docs` outputs a complete reference covering every command, subcommand, and flag
- [ ] Output includes: `init`, `init --project`, `tree`, `stale`, `fix`, `fixed`, `agents`, `search reindex`, `search keyword`, `doctor`, `quickstart`, `docs`
- [ ] Each command section includes: description, all flags with types/defaults, exit codes, example usage
- [ ] Output is valid markdown suitable for piping into an LLM context
- [ ] `quickstart` remains a concise end-to-end tutorial; `docs` is the exhaustive reference with full flag details that `quickstart` may omit

### 7. Recursive docs discovery

- [ ] `autodoc tree` discovers and displays docs from all directories named `docs/` anywhere in the repo, not just the configured `docsDir`
- [ ] `docsDir` is still included as a compatibility root even if not named `docs`
- [ ] Git submodule directories are excluded from discovery
- [ ] Paths in `ignores` config are respected across all discovered docs directories
- [ ] `stale`, `fix`, `agents`, and `search reindex` all use the same recursive discovery
- [ ] Discovery results are consistent across all commands (same set of files)

### 8. Text mode output conventions

- [ ] `autodoc stale` prints clean docs first (if any shown), then stale docs with errors
- [ ] `autodoc tree` prints the full tree, then appends a count summary (e.g. "12 docs, 3 stale")
- [ ] `autodoc fix` prints "all clean" message when no issues, or grouped instructions when issues exist
- [ ] Every error message from any command includes a concrete remediation hint
- [ ] No command prints errors to stdout in text mode — errors and diagnostics go to stderr

### End-to-end

- [ ] The full user journey flow works: `autodoc init` → `autodoc init --project` → create docs → `autodoc fix` → follow instructions → `autodoc fixed <path>` → `autodoc fix` returns clean → `autodoc search reindex` → `autodoc search keyword "query"` returns results
- [ ] All existing e2e tests updated to use new config paths and pass
- [ ] New e2e tests cover global init, `--json` output, `doctor`, and global+local ignores union

## Out of scope for v1

These are mentioned in the user journey or docs but are stretch goals, not v1 blockers:

- **Semantic/embedding search** (`autodoc search semantic`) — researched in `semantic-search.md`, not needed for v1
- **Feedback loops** — designed in `feedback.md`, not part of the core user journey
- **`--since`/`--after`/`--before` date filters** — autodoc doesn't deal with time-series data, N/A
- **Multi-format support** — markdown only is fine for v1
- **Cross-host sync** — handled by other tools (autoetl, autowatch)

## Suggested implementation order

1. **Config path migration** (#1, #2, #3) — foundational, everything else builds on it
2. **Recursive docs discovery** (#7) — already designed, unblocks real-world usage
3. **`--json` flag** (#4) — aligns with cross-project conventions
4. **`doctor` command** (#5) — validates the new config setup works
5. **`docs` command** (#6) — reference docs for the complete v1 surface
6. **Text output conventions** (#8) — polish pass
