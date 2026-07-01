# auto-skill

Agent skill management: install, sync, lint, and customize reusable agent skills from remote repos.

## Quickstart

```bash
auto skill quickstart    # full happy-path walkthrough
auto skill docs          # complete command reference
```

## Skill Customization (Templating Engine)

Skills are not static files — they are **templates with declared extension points**. A skill author declares `customize:` variables in SKILL.md frontmatter; a project fills in values via `skills.yaml`. On `auto skill sync`, the render engine substitutes values into `{{ .var }}` placeholders and emits the final skill files. The skill controls the shape; the project controls the content.

### End-to-end example

**1. The upstream skill** (maintained by the skill author in a git repo):

```markdown
# skills/deploy-checklist/SKILL.md

---
name: deploy-checklist
description: Pre-deploy verification checklist
customize:
  team_name:
    required: true
    description: "Your team's name for the checklist header"
  staging_url:
    default: "https://staging.example.com"
    description: "Staging environment URL to verify"
  extra_checks:
    default: ""
    description: "Additional checklist items (markdown)"
---

# {{ .team_name }} Deploy Checklist

Before deploying, verify:

1. All tests pass on the target branch
2. Staging at {{ .staging_url }} matches expected behavior
3. No unresolved P1 issues in the tracker
{{ .extra_checks }}
```

The `customize:` block declares three variables: `team_name` (required — sync fails without it), `staging_url` (optional, defaults to the example URL), and `extra_checks` (optional, defaults to empty).

**2. Install the skill** in your project:

```bash
auto skill add https://github.com/org/skills --skill deploy-checklist
```

This writes a lock entry to `.auto/skills/lock.json` pinning the exact commit.

**3. Configure replacements** in `.auto/skills/skills.yaml`:

```yaml
skills:
  deploy-checklist:
    replacements:
      team_name: "Platform Team"
      staging_url: "https://staging.platform.internal"
      extra_checks: |
        4. Database migrations reviewed by DBA
        5. Feature flags verified in LaunchDarkly
```

**4. Sync** to render the skill into your agent directories:

```bash
auto skill sync
```

This renders the template with your values and writes the result to `.claude/skills/deploy-checklist/SKILL.md` (and `.agents/skills/` for other agents):

```markdown
# Platform Team Deploy Checklist

Before deploying, verify:

1. All tests pass on the target branch
2. Staging at https://staging.platform.internal matches expected behavior
3. No unresolved P1 issues in the tracker
4. Database migrations reviewed by DBA
5. Feature flags verified in LaunchDarkly
```

The `customize:` block is stripped from the output. The agent sees only the rendered skill.

### File-ref replacements

Instead of inline literal values, replacements can pull content from files in the skill's repo:

```yaml
# skills.yaml
skills:
  deploy-checklist:
    replacements:
      extra_checks:
        file: "docs/deploy-checks.md"
        section: ["Additional Checks"]
```

This inlines the "Additional Checks" section from `docs/deploy-checks.md` into the `{{ .extra_checks }}` placeholder at sync time. The content hash is tracked in the manifest for change detection.

### Shared replacements

Values that apply across all skills go under `shared.replacements`:

```yaml
shared:
  replacements:
    team_name: "Platform Team"

skills:
  deploy-checklist: {}
  oncall-runbook:
    replacements:
      team_name: "SRE Team"  # per-skill override wins
```

### Template grammar

The template engine is deliberately restricted — no control flow, no function calls, no code execution. The only allowed constructs are:

- `{{ .var }}` — field access, replaced by the configured value
- `{{ "{{" }}` — literal brace escape, emits `{{` in the output

Anything else (`{{ if }}`, `{{ range }}`, `{{ printf }}`, pipelines) is rejected at parse time with a typed error naming the offending construct.

### How it works

```
SKILL.md template          skills.yaml values
       │                         │
       ▼                         ▼
  ParseCustomize()         replacementValues()
       │                         │
       ▼                         ▼
  ParseTemplate()          ResolveValues()
       │                         │
       └──────────┬──────────────┘
                  ▼
            Template.Render()
                  │
                  ▼
         Tree (emitted files + skill_version digest)
```

The render is pure and deterministic: same inputs always produce the same `skill_version` hash. The digest is computed before the provenance stamp is added, so metadata changes don't trigger unnecessary re-renders.
