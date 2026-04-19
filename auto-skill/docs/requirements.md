---
hash: "2d076b8b"
id: "8c156daa"
read_when: "when implementing autoskill create, lint, and skill validation commands"
summary: "Requirements for autoskill: agent skill management, linting, and scaffolding"
title: "autoskill — Requirements"
---

# autoskill — Requirements

## Overview

`autoskill` manages agent skills stored in a project's `./skills/` directory. Skills are SKILL.md files with YAML frontmatter, designed to be consumed by coding agents (Claude Code, Codex, OpenCode). Docs are authoritative — skills encode distilled versions and link back to their source docs via autodoc.

`./skills/` is the autoskill-managed canonical store. Syncing or exporting to agent-specific discovery paths (`.claude/skills/`, `.agents/skills/`) is a future concern, not v1 scope.

## init

Set up autoskill configuration.

### Behavior

- `autoskill init` creates `~/.auto/skill/settings.json` for global defaults.
- `autoskill init --project` creates the `./skills/` directory and a `.auto/skill/settings.json` project config.
- Idempotent — safe to run multiple times, does not overwrite existing config.
- Reports what was created vs what already existed.

### Rationale

Separates one-time setup from repeated skill creation. Follows the auto-stack `init` / `init --project` convention.

## create

Create a new skill from a best-practices template.

### Behavior

- `autoskill create <name> --description "..."` creates `./skills/<name>/SKILL.md`.
- `--description` is required. Enforced at the CLI level.
- Name must match `^[a-z0-9]+(-[a-z0-9]+)*$` and be ≤64 chars. Reject otherwise.
- Template includes:
  - Frontmatter with `name`, `description`, and optional `metadata.short-description`.
  - Body sections: "When to use", "Workflow", "Load on demand", "Output requirements", "Avoid".
- `--with-dirs` optionally creates `references/`, `scripts/`, and `assets/` subdirectories.
- Fails if the skill directory already exists (no silent overwrite).
- Runs `autoskill lint` on the created skill and prints any warnings to stderr as human-readable text. JSON lint output is not mixed into stdout.

### Rationale

All three major coding agents expect skills as directories with a SKILL.md plus optional side files. Scaffolding enforces the right structure from the start and reduces boilerplate errors. Requiring `--description` upfront prevents the most common skill authoring mistake — a missing or placeholder description.

## lint

Validate skills against a cross-agent best-practices schema. This is the core feature — agent-portable skill validation that no other tool provides.

### Behavior

- `autoskill lint` validates all skills under `./skills/`.
- `autoskill lint <path>` validates a specific skill or directory.
- Returns all diagnostics as structured JSON to stdout — array of objects with `severity` (`error`, `warning`, `info`), `code`, `path`, `field`, `message`, and optional `value`.
- Exits non-zero if any errors found. Warnings and info do not cause non-zero exit.

### Checks

#### Frontmatter (discovery phase)

| Check | Severity | Code |
|-------|----------|------|
| Valid YAML between `---` lines | error | `invalid_frontmatter` |
| `name` present | error | `missing_name` |
| `name` matches `^[a-z0-9]+(-[a-z0-9]+)*$` | error | `invalid_name` |
| `name` ≤64 chars | error | `name_too_long` |
| `name` matches containing directory name | error | `name_dir_mismatch` |
| `description` present | error | `missing_description` |
| `description` ≤1024 chars | error | `description_too_long` |
| `description` contains trigger phrases (regex match for patterns like "Use when", "Prefer for", "Do not use", "Trigger when") | warning | `missing_trigger_phrase` |
| `metadata.short-description` ≤1024 chars if present | error | `short_description_too_long` |

#### Body (execution phase)

| Check | Severity | Code |
|-------|----------|------|
| Body is non-empty | error | `empty_body` |
| Body starts with actionable content (rules, constraints, workflow), not prose preamble | warning | `weak_opening` |
| Token estimate (chars/4) reported | info | `token_estimate` |

#### Links and references

| Check | Severity | Code |
|-------|----------|------|
| All `[autodoc()]` links resolve to valid doc IDs | error | `broken_autodoc_link` |
| Doc hashes in `[autodoc()]` links match current doc hashes (freshness) | warning | `stale_autodoc_link` |
| All markdown links to local files resolve (relative paths exist) | error | `broken_local_link` |
| All `references/`, `scripts/`, `assets/` paths mentioned in body exist | error | `broken_side_file` |

Skills use the same `[autodoc(<docId>@<docHash>, <scopeHash>)]` comment syntax as code files. In skill markdown, place it as an HTML comment (`<!-- [autodoc(...)] -->`) or a standalone line above the section that derives from the referenced doc.

#### Token budgets

| Check | Severity | Code |
|-------|----------|------|
| Aggregate skill listing (all names + descriptions, ls-style) exceeds token threshold | warning at >2000 tokens, error at >4000 tokens | `listing_too_large` |
| Individual skill body exceeds token threshold | warning at >4000 tokens, error at >8000 tokens | `body_too_large` |

Token estimation uses chars/4 (conservative). The aggregate check reuses the same `ls`-style output format (name + description per skill) to measure what the agent actually sees during discovery.

#### Structure

| Check | Severity | Code |
|-------|----------|------|
| Skill is a directory containing SKILL.md (not a standalone .md file) | error | `not_a_directory` |
| No secrets or API keys detected in skill content | error | `secret_detected` |

## ls

List all skills in the project.

### Behavior

- `autoskill ls` scans `./skills/` and lists each skill's name and description.
- Default output is a compact listing matching Claude Code's skill_listing style:
  ```
  - my-skill: Use when the user needs X. Prefer this skill for A and B.
  - deploy-k8s: Generate and validate Kubernetes manifests for GKE clusters.
  ```
- `--json` outputs a JSON array with `name`, `description`, `path` fields.
- Reports skills with missing or unparseable frontmatter as errors to stderr, still lists valid skills.
- Exits non-zero if any skills failed to parse.

### Rationale

Mirrors how coding agents present skills to the model — a flat list of name + description. Useful for authors to preview exactly what the agent will see during discovery. JSON mode supports piping into other auto-stack tools.

## Drift Detection

- Detect drift between skill content and documentation content.
- Skills often encode rules/patterns that originate from docs — when the doc changes, the skill may become stale.
- Use autodoc-style hash-based freshness links between skills and their source docs.
- Flag skills whose referenced docs have changed since the skill was last updated.
- Surface in `autoskill lint` output via the `broken_autodoc_link` and `stale_autodoc_link` checks.
- Workflow: doc changes → `autodoc fix` flags stale hashes → author updates the skill → `autoskill lint` confirms consistency.

## Test Strategy

### Approach

Each test gets a fresh directory under `.tmp/`. The CLI accepts a `--root <path>` flag that overrides where autoskill looks for `./skills/` and `.auto/`. Tests seed the `skills/` folder with fixture data, run autoskill with `--root .tmp/test-<name>/`, and assert on stdout/stderr/exit code. Cleanup is `os.RemoveAll`.

No external dependencies — no network, no `~/.auto/`, no real repos.

### init fixtures

| Test | Seed | Expected |
|------|------|----------|
| `init-empty` | Empty dir | `skills/` and `.auto/skill/settings.json` created |
| `init-idempotent` | Dir with existing config | Nothing overwritten, reports already exists |

### create fixtures

| Test | Seed | Expected |
|------|------|----------|
| `create-happy` | Empty `skills/` | `skills/<name>/SKILL.md` created from template |
| `create-conflict` | Existing `skills/my-skill/` | Error, refuses to overwrite |
| `create-bad-name` | Empty `skills/` | Rejected for `"My Skill"` |
| `create-long-name` | Empty `skills/` | Rejected for name >64 chars |
| `create-with-dirs` | Empty `skills/` | `references/`, `scripts/`, `assets/` created |

### lint — frontmatter fixtures

| Test | Seed SKILL.md | Expected |
|------|---------------|----------|
| `lint-valid` | Correct name, description with "Use when...", body with workflow | Clean, exit 0 |
| `lint-missing-name` | No `name` field | error `missing_name` |
| `lint-bad-name` | `name: "My Skill"` | error `invalid_name` |
| `lint-name-mismatch` | Dir is `foo/`, name field is `bar` | error `name_dir_mismatch` |
| `lint-no-description` | Missing `description` | error `missing_description` |
| `lint-no-trigger` | `description: "Helps with deployment"` | warning `missing_trigger_phrase` |
| `lint-no-frontmatter` | Body only, no `---` block | error `invalid_frontmatter` |

### lint — body fixtures

| Test | Seed SKILL.md | Expected |
|------|---------------|----------|
| `lint-empty-body` | Frontmatter only, no body | error `empty_body` |
| `lint-prose-opening` | Body starts with "This skill is designed to..." | warning `weak_opening` |
| `lint-large-body` | >16k chars body (~4000+ tokens) | warning `body_too_large` |
| `lint-huge-body` | >32k chars body (~8000+ tokens) | error `body_too_large` |

### lint — link fixtures

| Test | Seed | Expected |
|------|------|----------|
| `lint-broken-link` | Body contains `[see docs](../docs/nonexistent.md)` | error `broken_local_link` |
| `lint-broken-side-file` | Body references `scripts/run.sh` that doesn't exist | error `broken_side_file` |
| `lint-valid-links` | All referenced files present | Clean |

### lint — token budget fixtures (aggregate)

| Test | Seed | Expected |
|------|------|----------|
| `lint-listing-warning` | 20+ skills with long descriptions (~2000+ tokens aggregate) | warning `listing_too_large` |
| `lint-listing-error` | 40+ skills with long descriptions (~4000+ tokens aggregate) | error `listing_too_large` |

### lint — structure fixtures

| Test | Seed | Expected |
|------|------|----------|
| `lint-standalone-file` | `skills/standalone.md` (file, not directory) | error `not_a_directory` |
| `lint-has-secret` | Body contains `AKIAIOSFODNN7EXAMPLE` | error `secret_detected` |

### ls fixtures

| Test | Seed | Expected |
|------|------|----------|
| `ls-mixed` | 3 valid skills + 1 with broken frontmatter | Valid skills listed on stdout, error on stderr, exit non-zero |
| `ls-empty` | Empty `skills/` | No output, exit 0 |
| `ls-json` | 2 valid skills | JSON array with `name`, `description`, `path` fields |
