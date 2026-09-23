A tool for managing agent skills.

## Migrating from vercel (`auto skill migrate vercel`)

One-shot, additive translator from vercel's checked-in `skills-lock.json` into the
native model. It **never** touches the source `skills-lock.json` — it only
creates/extends `.auto/skills/lock.json` and `.auto/skills/skills.yaml`, and
copies authored skills under `./skills/`. Existing native entries are preserved
(a name collision is left untouched and reported, never overwritten).

Two-step flow (migrate is offline — it resolves nothing):

1. `auto skill migrate vercel` — writes each migrated dep as a lock entry with
   `state: "unresolved"` (no commit) plus a `skills.yaml` entry seeding the
   **version intent** from the vercel `ref`.
2. `auto skill sync` — resolves commits for the `unresolved` entries and renders
   into each target.

Flags:

- `--from <path>` — vercel lock to read (default `./skills-lock.json`, resolved
  against the project root when relative).
- `--dry-run` — compute and print the full plan/result without writing anything.
- `--format json|text` — JSON (default) prints `{migrated, skipped, imported,
  failed, dry_run, counts}` to stdout with per-entry warnings on stderr; text
  prints the one-line summary `migrated N deps, skipped M (unsupported); run
  auto skill sync to resolve commits and render.`

Per-source handling:

- **github / gitlab** → lock entry with a credential-free `https://…` URL and a
  `subpath` derived from the vercel `skillPath`.
- **local** → inspected on this machine: a git repo becomes a non-portable
  `local: true` lock entry; a non-git directory is imported into
  `./skills/<name>/` as an authored skill (no lock entry); a missing path is
  reported and skipped.
- **node_modules / well-known / huggingface / mintlify** → unsupported: warned,
  skipped, and listed.

Version intent (vercel `ref` → `skills.yaml` `version`): absent ref → `latest`;
`branch:<name>` → `branch:<name>`; a 7–40 char sha → that sha (commit pin); any
other tag/name → the bare value (tag pin). Migrated `skills.yaml` entries are
written as `version: <spec>`; `replacements` is a named map (var → value) and is
omitted entirely when empty.

Migration returns valid results first, then exits non-zero when any entry was
skipped (so a `--dry-run` plan and a real run both surface unsupported deps).

## Agent Plugins (`auto skill add <source> --plugin <path|name>`)

`--plugin` installs a bundle of skills that follows the
[Agent Plugins](https://agent-plugins.org/specification) standard (v1.0.0). A
plugin is a directory in the source with a root `plugin.json` (`$schema` +
`name` required) and its skills as **immediate** children of `skills/`
(`skills/<dir>/SKILL.md`, no recursion). The argument is either the repo-relative
path of that directory (`--plugin plugins/planning-workflow`) or the manifest
`name` (`--plugin planning-workflow`, matched against every `plugin.json` in the
source; an ambiguous name asks for the path). The spec has no index file, so
Claude Code's `.claude-plugin/marketplace.json` is not consulted.

The plugin, not each skill, is the unit of intent and identity:

- `skills.yaml` gains `plugins.<name>.version`; member skills get **no**
  `skills.<name>` stub (an existing one is kept only for its `replacements`).
- `lock.json` gains `plugins.<name>` (same shape as a skill entry; `subpath` is
  the plugin root) and each member skill entry is stamped `plugin: <name>`.
- `sync` renders members like any vendored skill. `update <plugin>` (or naming any
  member) floats the plugin whole and re-reads the manifest at the new commit, so
  skills added upstream are inserted and skills dropped upstream are removed from
  the lock and pruned from targets. A manifest that disappears or changes its
  `name` fails the plugin as unavailable (lock and targets untouched) with a
  remove-and-re-add hint.
- `remove <plugin> --plugin` (inferred when the name is only a plugin) drops the
  plugin, every member, and both skills.yaml entries, then prunes. A member cannot
  be removed on its own.
- `list` / `describe` carry a `plugin` field on member rows.

Failure boundaries follow the spec: an invalid `plugin.json` rejects the whole
plugin; a `skills/` child without a regular `SKILL.md`, or whose declared name is
not lowercase kebab-case, is skipped and reported; unknown manifest fields are
ignored. Only the skills component is consumed — `mcp.json` and client extension
directories are ignored. `--plugin` cannot be combined with `--skill`, `--path`,
`--as` or `--full-depth`, and needs a git source (remote or local checkout).

## Git hooks (`make install-hooks`)

The checked-in `hooks/*` shims delegate to Makefile targets. All skill stanzas
are guarded by `.auto/skills/lock.json` presence (and `command -v auto`), so they
**no-op cleanly in repos without native skills**:

- **pre-commit → `skills-check`** (check-only, replaces the old npx `skills-sync`):
  runs `auto skill sync --check` then `auto skill lint` (both JSON by default);
  **fails the commit** if any target is stale or any skill fails lint. It never
  mutates the tree (no render, no `git add`).
- **post-merge / post-checkout → `skills-sync-locked`**: runs `auto skill sync
  --locked` to re-materialize the locked commit into each target. Non-blocking —
  never fails the hook.
- **pre-push → `skills-update-check`**: **opt-in, off by default.** Enable with
  `SKILLS_UPDATE_CHECK=1` (per-invocation or exported) to run `auto skill update
  --check`. Warn-only — never blocks the push.

<!-- autodoc: start -->
## Documentation Index

*Auto-generated by `autodoc`. Do not edit manually.*

- Run `auto doc quickstart` before first use to learn the workflow.
- Search docs with `auto doc search keyword <query>`.
- Check doc freshness with `auto doc stale`, fix issues with `auto doc fix`.
- Link code to docs with `[autodoc()]` tags — run `auto doc fix` for details.

**auto-skill/docs**

- [auto-skill Assurance Strategy](auto-skill/docs/assurance-strategy.md): Four-axis assurance diagnosis and prescribed testing techniques (model-based, property-based, edge-case pinning) for auto-skill's sync pipeline edge cases. Read when: designing verification strategy for auto-skill, adding new test techniques, or diagnosing silent edge-case bug classes
- [Coding Agents Skill Guidance](auto-skill/docs/coding_agents_guidance.md): Comprehensive reference for Claude Code, Codex, and OpenCode skill architecture, discovery, loading, and best practices for skill authors. Read when: authoring agent skills or designing skill discovery systems
- [important_if Skill Metadata for Agent File Injection](auto-skill/docs/important-if-feature.md): Design for skills to declare trigger conditions that get auto-injected as important-if blocks in CLAUDE.md. Read when: implementing skill-to-agent-file injection or important_if metadata
- [Meta Skill (ms) — Reference](auto-skill/docs/meta_skill.md): Technical reference for the meta_skill Rust CLI: architecture, data model, mining pipeline, search, security, and distribution. Read when: implementing autoskill mining, search, and skill distribution
- [autoskill — Requirements](auto-skill/docs/requirements.md): Requirements for autoskill: agent skill management, linting, and scaffolding. Read when: implementing autoskill create, lint, and skill validation commands
- [SCIP Notes for auto-skill](auto-skill/docs/scip-code-notes.md): Technical notes on SCIP (scip-code.org) with practical adoption guidance for auto-skill. Read when: adding symbol-aware code context and navigation features to autoskill
- [SkillOpt Paper Notes for auto-skill](auto-skill/docs/skillopt-paper-notes.md): Technical implementation notes from SkillOpt (arXiv:2605.23904), focused on what to adopt in auto-skill. Read when: designing automated skill optimization loops for autoskill
- [auto-skill vs. vercel-labs/skills — Feature Gap & Path to Parity](auto-skill/docs/vercel-skills-gap-analysis.md): What the vercel skills CLI does that auto-skill doesn't, and the concrete pieces needed to make auto-skill a native tool for installing, updating, and managing skills from remote repos. Read when: scoping auto-skill toward remote skill install/update/distribution, or deciding what to build to replace the npx skills shell-out
<!-- autodoc: end -->









