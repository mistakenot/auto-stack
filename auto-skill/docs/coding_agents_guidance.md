
## Claude Code Skills reference

```markdown
# How Skills Work

## TL;DR For Skill Authors

- Skills are loaded into a registry early.
- Full `SKILL.md` bodies are not injected into every prompt.
- The model usually sees skills first through:
  - generic system-prompt guidance.
  - the Skill tool prompt/schema.
  - `skill_listing` and `skill_discovery` system-reminder attachments.
- Static skill listings are checked on every attachment pass:
  - once before the first model call of a user turn.
  - again after each tool batch before recursive follow-up calls.
- Static `skill_listing` is effectively injected once per agent, then only for new skills.
- Full skill content is injected only when:
  - the skill is invoked.
  - an agent preloads it.
  - compaction or resume restores previously invoked skills.
- Dynamic subtree skills and `paths:`-activated skills expand availability during the session.
- Dynamic and conditional activation do not inject the full body by themselves.

### What This Means For Writing Good Skills

- Optimize for two separate moments:
  - discovery.
  - execution.
- Treat frontmatter as selection metadata.
- Treat the top of the body as the high-value execution payload.

### Discovery Guidance

- Make `name` specific.
- Make `description` specific.
- Make `when_to_use` specific.
- Assume the model may decide whether to invoke the skill from only those fields.
- Use distinctive wording.
- Prefer concrete task signals over generic wording.
- Keep `when_to_use` compact.
- Avoid vague descriptions like:
  - "helps with coding".
  - "general development workflow".

### Execution Guidance

- Put the most important rules at the top of the skill body.
- Start with:
  - when to use.
  - hard constraints.
  - exact workflow.
  - required tools.
  - required files.
- Keep intros short.
- Avoid long explanatory or motivational opening sections.
- Assume the head of the skill is the most valuable part:
  - it is read first.
  - it is the part most worth preserving under compaction pressure.

### Structure Guidance

- Keep bulky reference content out of the main body when possible.
- Put examples, templates, scripts, and extended references next to the skill in its directory.
- Let the skill body point to those files explicitly.
- This works well with the architecture because the skill base directory is injected and the model can read supporting files on demand.

### Permission and Execution Model Guidance

- Use `allowed-tools` narrowly.
- Those permissions are granted at invocation time, not globally.
- Use `context: fork` for:
  - heavy workflows.
  - self-contained workflows.
  - workflows that benefit from a separate token budget.
- Keep inline skills for:
  - short workflows.
  - steering-heavy workflows.
  - workflows where the user is likely to interrupt or redirect mid-process.
- Use `agent:` when the skill clearly belongs to a specialized agent.
- Use `paths:` when the skill should activate only around specific files.
- Use nested `.claude/skills` directories when a workflow is local to a subtree.
- Use `user-invocable: false` for model-only skills.
- Use `disable-model-invocation` only when the model should never call the skill.

### Shell and Runtime Guidance

- Be careful with shell interpolation in skill markdown.
- Embedded shell executes only on invocation.
- Embedded shell adds latency.
- Embedded shell should earn its cost.
- Prefer deterministic, high-signal shell snippets over convenience snippets.

### Writing Style Guidance

- Write steps as crisp imperatives.
- Prefer concrete checks over vague advice.
- Prefer:
  - "run X"
  - "inspect Y"
  - "then do Z"
- Avoid broad prose where a step list is clearer.

### Best Heuristic

- Short frontmatter for selection.
- Dense top-of-file instructions for execution.
- Side files for everything bulky.

## Purpose

- Goal: document how skills are loaded, surfaced, selected, expanded, cached, preserved, and refreshed.
- Scope: bundled skills, disk-backed skills, plugin skills, MCP skills, dynamic skills, conditional skills, and experimental discovery paths.
- Focus: model context flow, not just filesystem loading.

## Core Mental Model

- The system splits skills into three layers.
- Layer 1: registry metadata.
- Layer 2: model-visible skill advertisement.
- Layer 3: full skill-body expansion.
- The system does not inject every `SKILL.md` body into every prompt.
- The system loads metadata early.
- The system advertises availability separately.
- The system expands full skill content only on invocation, preload, or restoration.

## Main Skill Sources

- Bundled skills.
- Disk-backed skills from `.claude/skills`.
- Legacy commands-as-skills from `.claude/commands`.
- Plugin skills.
- Built-in plugin skills.
- MCP skills.
- Dynamic project skills discovered below `cwd`.
- Conditional skills activated by touched file paths.
- Experimental remote canonical skills.

## High-Level Pipeline

- Startup registration:
  - Bundled skills are registered into an in-memory registry at startup.
  - Source: `src/main.tsx:1919-1928`.
  - Source: `src/skills/bundled/index.ts:24-70`.
- Registry construction:
  - `getCommands(cwd)` builds the complete command registry.
  - Source: `src/commands.ts:449-517`.
- Skill filtering:
  - `getSkillToolCommands(cwd)` builds the list of model-invocable prompt skills.
  - `getSlashCommandToolSkills(cwd)` builds the lighter "skills" view used by SDK/UI and some prompt paths.
  - Source: `src/commands.ts:561-603`.
- Tool schema render:
  - Tool prompts are rendered into the API tool schema before the system prompt.
  - The Skill tool prompt explains how to invoke skills.
  - Source: `src/utils/api.ts:136-178`.
  - Source: `src/utils/toolSchemaCache.ts:3-8`.
  - Source: `src/tools/SkillTool/prompt.ts:173-194`.
- System prompt guidance:
  - The main system prompt adds generic guidance about `/<skill-name>` and `Skill`.
  - The system prompt does not inline all skill bodies.
  - Source: `src/constants/prompts.ts:352-389`.
  - Source: `src/constants/prompts.ts:444-461`.
- Attachment pass:
  - Each user turn and each recursive tool-follow-up turn runs attachment collection.
  - `skill_listing` and `skill_discovery` attach here.
  - Source: `src/utils/processUserInput/processUserInput.ts:495-510`.
  - Source: `src/query.ts:1580-1628`.
  - Source: `src/utils/attachments.ts:874-875`.
- Invocation:
  - Full skill content is expanded by `command.getPromptForCommand(...)`.
  - Inline skills inject new hidden user messages into the conversation.
  - Forked skills pass the rendered skill body as the first prompt message to a sub-agent.
  - Agent frontmatter preloads can inject skill bodies before agent execution starts.
```

## Codes skills reference


```markdown
# Skills Internals

Repo-specific reference for how skills are discovered, cached, exposed to the model, and turned into prompt input in Codex.

## Quick start for users

1. Use explicit mentions when you care about reliability.
   - Write `use $skill-name` or select the skill in the UI.
   - That is the path that injects the full `SKILL.md` into the turn.
2. Treat skills as local playbooks, not magic plugins.
   - A skill is instructions plus optional scripts, references, and assets.
   - Codex still decides how to execute the work.
3. If you want Codex to discover a skill implicitly, optimize the skill description.
   - If you want Codex to definitely use it, mention it explicitly.
4. Put repeatable logic in `scripts/`, not in long prose.
5. Keep `SKILL.md` short and procedural.
   - Put heavy docs in `references/`.
   - Put templates and output inputs in `assets/`.
6. Declare dependencies in `agents/openai.yaml` instead of burying them in prose.
7. Do not expect skills to bypass sandbox or approval rules.
8. For plugin-provided skills, use the full namespaced name like `plugin-name:skill-name`.
9. The best way to improve a skill is to run real prompts with and without `use $skill-name` and tighten the instructions until behavior is repeatable.

## Quick start for skill authors

1. Make the `SKILL.md` frontmatter clear.
   - `name` and especially `description` are the trigger surface.
   - Write the description as “Use when... Prefer for... Do not use for...”
2. Keep the body short and procedural.
3. Move detailed reference material into `references/`.
4. Put deterministic or repeated transformations in `scripts/`.
5. Add `agents/openai.yaml` for UI metadata, default prompt, dependencies, and policy.
6. If you want reliable usage, teach users to say `use $skill-name`.

## Starter template

Use this as the default starting point for a new skill.

```md
---
name: my-skill
description: Use when the user needs X. Prefer this skill for A and B. Do not use it for C.
metadata:
  short-description: One-line UI summary
---

# My Skill

## When to use
- Use for X
- Use for Y when Z is true
- Skip if C

## Workflow
1. Confirm the goal and constraints.
2. Read only the relevant file from `references/` if needed.
3. Prefer `scripts/` for deterministic or repeated work.
4. Produce the output in the expected format.
5. Validate the result before finishing.

## Load on demand
- Read `references/domain.md` only for domain rules.
- Run `scripts/do_task.py` for the core transformation.
- Use files in `assets/` only as output inputs/templates.

## Output requirements
- Return concise results first.
- Include citations, paths, or diffs when relevant.

## Avoid
- Do not load all references by default.
- Do not improvise when a bundled script already exists.
```

Optional `agents/openai.yaml`:

```yaml
interface:
  display_name: "My Skill"
  short_description: "One-line UI summary"
  default_prompt: "Use $my-skill to do X."

dependencies:
  tools:
    - type: "env_var"
      value: "MY_API_KEY"

policy:
  allow_implicit_invocation: true
```

## Most important rules

1. Write the `description` as the trigger surface.
2. Keep `SKILL.md` short and procedural.
3. Move details into `references/`.
4. Put repeatable logic in `scripts/`.
5. Use explicit mentions for reliable invocation.
6. Read the rest of this document for the runtime and prompt-level internals.

## Accepted frontmatter metadata

Codex currently reads a very small frontmatter surface from `SKILL.md`.

### Accepted keys

1. `name`
   - string
   - optional in practice
   - if omitted, Codex falls back to the skill directory name
2. `description`
   - string
   - effectively required
   - this is the most important trigger field
3. `metadata.short-description`
   - string
   - optional
   - this is the only Codex-specific nested frontmatter metadata currently accepted
   - use the hyphenated key `short-description`, not `short_description`

### Example

```md
---
name: my-skill
description: Use when the user needs X. Prefer this skill for A and B. Do not use it for C.
metadata:
  short-description: One-line UI summary
---
```

### Constraints and behavior

1. Frontmatter must be valid YAML between opening and closing `---` lines.
2. Values are normalized to a single line.
   - repeated whitespace is collapsed
3. Length limits:
   - `name`: 64 chars
   - `description`: 1024 chars
   - `metadata.short-description`: 1024 chars
4. If `metadata.short-description` is too long, the skill fails to load.

### What does not belong in frontmatter

Put these in `agents/openai.yaml`, not in `SKILL.md` frontmatter:

1. `interface.display_name`
2. `interface.short_description`
3. `interface.default_prompt`
4. `interface.icon_small`
5. `interface.icon_large`
6. `interface.brand_color`
7. `dependencies`
8. `policy.allow_implicit_invocation`
9. `policy.products`

### Unknown frontmatter keys

1. The current loader only deserializes `name`, `description`, and `metadata.short-description`.
2. Other frontmatter keys are not part of the supported Codex frontmatter contract.
3. In practice, extra keys are ignored by the current loader, but you should not rely on that behavior.
4. If you need Codex-specific behavior beyond the three accepted keys above, use `agents/openai.yaml`.

## Mental model

1. A skill is a directory with a required `SKILL.md` file and optional `agents/openai.yaml`, `scripts/`, `references/`, and `assets/`.
2. The loader reads metadata first.
   - Required prompt-facing metadata comes from `SKILL.md` frontmatter.
   - Optional UI/dependency/policy metadata comes from `agents/openai.yaml`.
3. Every turn gets a snapshot of loaded skills in `TurnSkillsContext`.
4. The model usually sees skills in two different ways:
   - Developer-side skill catalog: metadata only, wrapped in `<skills_instructions>`.
   - User-side explicit skill injection: full `SKILL.md` body, wrapped in `<skill>`.
5. Those two paths are different:
   - Explicit selection: the harness injects full skill contents.
   - Implicit/model-driven use: the model starts from the catalog, then reads `SKILL.md` or runs skill scripts itself if needed.

## Key distinction

1. `allow_implicit_invocation = true` means:
   - the skill can appear in the developer-side skills catalog
   - the skill can be recognized for implicit invocation telemetry
2. It does **not** mean:
   - the harness automatically injects the full `SKILL.md` body on every turn
3. Full skill-body injection happens only for explicit skill mentions/selections.
```

## Opencode skills info

```markdown
# How Skills Work in OpenCode

## Guidance for Pro Engineers

### Skill selection is description-driven

- The model chooses skills based **solely on the `description` field** in frontmatter and the task at hand. The full SKILL.md body is never seen until after selection. Treat description as a search index entry — it must clearly signal when the skill applies.
- Vague descriptions like "helps with deployment" lose to specific ones like "Generates and validates Kubernetes manifests for GKE clusters using Helm". Front-load the distinguishing keywords.
- Descriptions can be up to 1024 chars. Use that budget if disambiguation requires it — a longer, precise description beats a short, ambiguous one.

### With many skills, curate what's visible

- Every discovered skill lands in the system prompt as XML and in the tool description as markdown — **on every turn**. 50 skills means 50 entries the model must scan each time it decides whether to invoke one.
- Use **per-agent permission rules** to scope down visibility. If your `code-review` agent never needs the `deploy-k8s` skill, deny it:
  ```json
  {
    "permission": {
      "skill": {
        "*": "allow",
        "deploy-*": "deny"
      }
    }
  }
  ```
  Denied skills are completely invisible to the model, not just blocked — this reduces noise and improves selection accuracy.
- Prefer granular agents with scoped skill sets over one omniscient agent that sees everything.

### Names matter more than you think

- The model must emit the exact skill name as a tool parameter. Names like `sdk` or `utils` are ambiguous and collide easily. Use `cloudflare-workers-sdk` or `python-test-utils`.
- Duplicate names across discovery locations are silently dropped (first-found wins, global before project before config). Run `opencode debug skill` to verify what's actually registered and catch shadowed skills.
- Names are the primary dedup key — if two teams ship a skill named `deploy`, only one survives.

### Discovery order determines precedence

- Skills are scanned in this order: global (`~/.claude/skills/`, `~/.agents/skills/`) -> project-local (walking up to worktree) -> config directories -> explicit `skills.paths` -> remote `skills.urls`.
- First match for a given name wins. If you need a project-local override of a global skill, you can't — the global one shadows it. Rename or remove the global one.
- Use `opencode debug skill` liberally to audit what's loaded and from where.

### Design skills for lazy loading

- The full skill body is only injected when the model calls the `skill` tool. This means the upfront cost of having many skills is proportional to the number of **descriptions**, not content. Keep descriptions tight; put the heavy instructions in the body.
- The model also gets a sample of up to 10 sibling files from the skill directory. Bundle reference docs, templates, and scripts next to SKILL.md — the model will see their paths and can `Read` them without being told to glob.

### Trigger phrases in descriptions improve recall

- If your skill should fire for specific user intents (e.g., "set up CI", "review this PR"), include those phrases in the description. The model pattern-matches against user input, and explicit trigger phrases reduce missed activations.
- Example: `"description": "Create and configure GitHub Actions workflows. Use when the user asks to set up CI, add a workflow, or automate tests with GitHub Actions."`

### Test skill selection, not just skill content

- The most common failure mode is the right skill existing but not getting selected. Test by giving the model ambiguous prompts and checking which skill (if any) it invokes.
- If two skills compete for the same task, the model will sometimes pick the wrong one or skip both. Resolve by making descriptions mutually exclusive or merging the skills.

### Remote skills are cached aggressively

- Skills fetched from `skills.urls` are downloaded once to `~/.cache/opencode/skills/` and never re-fetched if the file exists. To force an update, delete the cache directory. There is no TTL or cache-busting mechanism.

---

## What is a Skill?

A skill is a self-contained markdown file (`SKILL.md`) with YAML frontmatter that injects domain-specific instructions, workflows, and bundled resources into the agent's context on demand. Skills are **lazily loaded** — their existence is advertised to the model, but their full content is only injected when the model explicitly invokes the `skill` tool.

## Skill File Format

```yaml
---
name: my-skill          # required, 1-64 chars, lowercase alphanumeric + hyphens
description: One-liner  # required, 1-1024 chars
license: MIT            # optional
compatibility: ...      # optional
metadata: ...           # optional key-value pairs
---

Markdown body with instructions, workflows, etc.
```

- Name must match `^[a-z0-9]+(-[a-z0-9]+)*$` and match the containing directory name
- The markdown body is what gets injected into context when the skill is loaded
- Skills can include sibling files (scripts, references, templates) in their directory

## Discovery (`skill/index.ts:106-188`)

Skills are discovered at startup from multiple locations, scanned in this order:

1. **Global external directories** (unless `OPENCODE_DISABLE_EXTERNAL_SKILLS` is set):
   - `~/.claude/skills/**/SKILL.md`
   - `~/.agents/skills/**/SKILL.md`

2. **Project-local external directories** — walks parent directories from CWD up to the worktree root, looking for `.claude/skills/` and `.agents/skills/`

3. **Config directories** — scans for `{skill,skills}/**/SKILL.md` in directories returned by `config.directories()` (e.g. `.opencode/`)

4. **Explicit paths** — from `opencode.json` -> `skills.paths[]`, supports `~/` and relative paths

5. **Remote URLs** — from `opencode.json` -> `skills.urls[]`, fetched via the Discovery service

Each discovered `SKILL.md` is parsed with `ConfigMarkdown.parse()`, which extracts frontmatter and body. The result is stored in an in-memory `Record<string, Skill.Info>` keyed by name. Duplicates log a warning; first-found wins.
```
