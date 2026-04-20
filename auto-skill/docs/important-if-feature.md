---
hash: "2c2d03b8"
id: "d30bbd13"
read_when: "implementing skill-to-agent-file injection or important_if metadata"
summary: "Design for skills to declare trigger conditions that get auto-injected as important-if blocks in CLAUDE.md"
title: "important_if Skill Metadata for Agent File Injection"
---

# Feature: `important_if` skill metadata for agent file injection

## Problem

Installed skills with clear trigger conditions (contextual-commit, release, recall) have 0% adoption because nothing in CLAUDE.md tells the agent to use them. The agent follows CLAUDE.md instructions over skill descriptions — if CLAUDE.md doesn't reference a skill, it doesn't get invoked.

Manual `<important if>` blocks in CLAUDE.md work but don't scale: they drift, they're not tied to skill install/uninstall, and they require the user to know the syntax.

## Solution

Skills can opt in to agent file injection via `meta.important_if`. When set, `autoskill agent` (and `autoskill sync`) writes `<important if>` blocks into CLAUDE.md inside a fenced section it owns.

## Metadata fields

Two new optional fields in SKILL.md frontmatter under `metadata:`:

```yaml
metadata:
  important_if: "you are committing code or the user asks you to commit"
  important_if_body: "Use the **contextual-commit** skill. Never use bare `git commit -m`."
```

- `important_if` (string, optional): The trigger condition. Goes into `<important if="...">`. If not set, the skill is not injected.
- `important_if_body` (string, optional): Overrides the default body text. If not set, defaults to `Use the **{name}** skill.`

## Agent file output

`autoskill agent` produces a fenced section in CLAUDE.md (and AGENTS.md, GEMINI.md):

```markdown
<!-- autoskill: start -->
**autoskill** — Author and lint reusable agent skills. Run `autoskill quickstart` to learn more.

<important if="you are committing code or the user asks you to commit">
Use the **contextual-commit** skill.
</important>

<important if="the user asks to release, tag, or create a new version">
Use the **release** skill. Do not manually run `git tag` or `gh release create`.
</important>
<!-- autoskill: end -->
```

On re-run, the entire section between markers is replaced. Skills that were uninstalled have their blocks removed. Idempotent.

## Commands affected

### `autoskill agent`

Current behavior: appends a one-line snippet if not present.

New behavior:
1. Scan `skills/*/SKILL.md` for all installed skills
2. Collect those with `meta.important_if` set
3. Find or create `<!-- autoskill: start -->` / `<!-- autoskill: end -->` markers
4. Replace everything between markers with: tool snippet + `<important if>` blocks
5. If markers don't exist but the old un-fenced snippet does, replace the snippet with the fenced section (migration)
6. If neither exists, append the fenced section

### `autoskill sync`

Should also run the agent file update. `sync` becomes the single "make everything consistent" command — skill files, agent files, all in one pass.

### `autoskill create`

Include `important_if` in the scaffold template as an empty string (commented or blank):

```yaml
metadata:
  short-description: ""
  # important_if: ""
```

### `autoskill quickstart` / `docs`

Document the feature. Emphasize the criteria for when to use it:
- The skill is almost always agent-invoked (not user-invoked via `/slash-command`)
- The skill will be heavily used (e.g. every commit, every release)
- The user explicitly designates it as important

Otherwise don't set it — filling CLAUDE.md with `<important if>` blocks for rarely-used skills wastes context.

### `autoskill lint`

- Warn if `important_if` is set but empty
- Warn if `important_if` exceeds 200 chars (keep conditions short)
- Warn if `important_if_body` is set but `important_if` is not (body without trigger is useless)

## Files to change

| File | Change |
|------|--------|
| `auto-skill/internal/skill/skill.go` | Parse `important_if` and `important_if_body` from frontmatter. Add to `parsedSkill` struct. Add lint rules. Update scaffold template. |
| `auto-skill/internal/cli/agents.go` | Rewrite `ensureSkillSnippet` to use fenced sections. Scan skills for `important_if`. Generate blocks. Handle migration from un-fenced snippet. |
| `auto-skill/internal/cli/root.go` | Add agent update call to `sync` command. Update quickstart/docs text. |
| `auto-skill/internal/cli/cli_integration_test.go` | Test cases for fenced section creation, replacement, migration, and skill uninstall cleanup. |

## Migration

Existing CLAUDE.md files with the old un-fenced snippet:
```
**autoskill** — Author and lint reusable agent skills. Run `autoskill quickstart` to learn more.
```

On first `autoskill agent` run after this change, detect the old snippet (no markers), remove it, and write the new fenced section in its place.

## Criteria for setting `important_if`

Not all skills should use this. Guidance for skill authors:

- **Set it** for skills that are agent-invoked and high-frequency (contextual-commit, release, recall)
- **Don't set it** for skills that are user-invoked via slash commands (self-improve, open-prose)
- **Don't set it** for skills that are rarely used (one-off utilities)
- **Don't set it** just because a skill exists — context is expensive

## Example skills that would use this

| Skill | `important_if` | `important_if_body` |
|-------|----------------|---------------------|
| contextual-commit | `you are committing code or the user asks you to commit` | (default) |
| release | `the user asks to release, tag, or create a new version` | `Use the **release** skill. Do not manually run git tag or gh release create.` |
| recall | `you are starting a new session or resuming work on a branch` | (default) |

## Out of scope

- No changes to `.claude/` directory (that's Claude Code's space)
- No automatic setting of `important_if` on existing skills — skill authors opt in
- PR #11 (manual CLAUDE.md skill section) becomes unnecessary once this ships
