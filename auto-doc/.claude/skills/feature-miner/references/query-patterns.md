# Query Patterns for Feature Mining

Detailed search queries organized by signal category. Each query targets a specific type of agent-documentation interaction.

## Category: Doc Discovery

Agents trying to find, locate, or navigate documentation.

| Query | Rationale |
|-------|-----------|
| `where is the documentation how to find docs` | Agents explicitly looking for docs |
| `let me check the docs readme documentation` | Proactive doc-reading behavior |
| `looking for documentation about` | Targeted doc search |
| `read the project documentation` | Agent following doc-first approach |

**What to look for in results:**
- Is the agent finding docs via CLAUDE.md index? Or grep/glob?
- How many hops does it take to find the right doc?
- Does the agent give up and ask the user?

## Category: Doc Frustration

Agents failing to find docs, encountering outdated content, or missing information.

| Query | Rationale |
|-------|-----------|
| `can't find documentation missing docs no documentation` | Explicit failure signals |
| `couldn't find any docs about` | Search miss |
| `documentation doesn't mention doesn't cover` | Gap identification |
| `outdated instructions wrong documentation` | Staleness signals |
| `no README no docs undocumented` | Missing doc signals |

**What to look for in results:**
- What topics are undocumented that agents need?
- Are agents finding stale docs and following wrong instructions?
- How does the agent recover from missing docs?

## Category: Doc Usage

Agents actively reading and using documentation files.

| Query | Rationale |
|-------|-----------|
| `reading CLAUDE.md AGENTS.md documentation index` | Agent file consumption |
| `according to the documentation docs say` | Agent citing docs |
| `based on the docs documentation shows` | Doc-informed decisions |
| `documentation index section` | Index navigation |

**What to look for in results:**
- Which sections of CLAUDE.md/AGENTS.md are most referenced?
- Is the doc index being used for navigation?
- Are agents following doc instructions correctly?

## Category: Search Behavior

How agents search for information within documentation.

| Query | Rationale |
|-------|-----------|
| `search keyword grep docs find documentation file` | Search tool usage |
| `searching for documentation about` | Explicit search intent |
| `autodoc search keyword` | Autodoc search CLI usage |
| `grep -r docs README` | Manual doc search patterns |

**What to look for in results:**
- Are agents using autodoc search or falling back to grep?
- What queries do agents construct?
- Are search results relevant?

## Category: Doc Staleness

Signals about documentation being out of date.

| Query | Rationale |
|-------|-----------|
| `stale outdated documentation wrong instructions broken` | Explicit staleness |
| `documentation says but actually` | Doc/reality mismatch |
| `docs need updating documentation is wrong` | User-flagged staleness |
| `hash mismatch frontmatter stale` | Autodoc staleness detection |

**What to look for in results:**
- How often do agents encounter stale docs?
- What's the impact of following stale instructions?
- Does `autodoc stale` catch these cases?

## Category: Autodoc CLI Usage

Direct usage of autodoc (or predecessor docm) commands.

| Query | Rationale |
|-------|-----------|
| `autodoc docm tree stale fix agents search` | All CLI commands |
| `autodoc init setup documentation` | Init workflow |
| `autodoc fix frontmatter` | Fix workflow |
| `autodoc search keyword` | Search usage |
| `autodoc agents CLAUDE.md index` | Agent file generation |

**What to look for in results:**
- Which commands are used most?
- Are there error patterns or UX friction?
- What commands do users wish existed?

## Category: Frontmatter Usage

How agents interact with doc frontmatter.

| Query | Rationale |
|-------|-----------|
| `documentation frontmatter title summary hash` | Frontmatter fields |
| `yaml frontmatter markdown` | Format interactions |
| `missing frontmatter no title no summary` | Incomplete frontmatter |

**What to look for in results:**
- Do agents understand frontmatter format?
- Are agents correctly parsing title/summary?
- Is the hash field causing confusion?

## Category: Doc Maintenance

Patterns around keeping documentation current.

| Query | Rationale |
|-------|-----------|
| `need to update docs documentation out of date` | Maintenance triggers |
| `add documentation for create docs` | Doc creation patterns |
| `document this feature write docs` | Doc generation requests |

**What to look for in results:**
- What triggers doc updates?
- Are agents helping maintain docs or making them stale?
- Is there a pattern of docs being created then abandoned?

## Advanced Queries

### Chained searches (narrow results)

Find sessions where agents searched for docs AND encountered frustration:
```bash
cass search "searching for documentation" --robot-format sessions | \
  cass search "couldn't find" --sessions-from -
```

### Workspace-specific deep dive

Focus on a single project to see the full doc lifecycle:
```bash
cass search "documentation" --workspace /path/to/project --limit 50 --json
```

### Agent comparison

Compare how different agents handle docs:
```bash
cass search "documentation" --agent claude_code --limit 20 --json
cass search "documentation" --agent cursor --limit 20 --json
cass search "documentation" --agent codex --limit 20 --json
```
