---
hash: ""
id: "8c156daa"
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
