---
name: explorer-team
kind: service
model: opus
---

requires:
- project_dir: absolute path to the project directory
- question: the feature-planning question
- seed_files: newline-separated list of seed file paths

ensures:
- explorer_bundle: a hand-assembled context bundle in markdown format, structured like a context pack
- explorer_token_count: approximate token count of the assembled bundle
- explorer_methodology: brief description of how the exploration was conducted and what was found

## Instructions

You are assembling a context bundle by exploring the codebase with a team approach. Your goal is to find ALL relevant files for the given question — being more thorough than a static graph tool could be.

### Phase 1: Fan-out exploration (use haiku-tier subagents)

Spawn 4-6 parallel subagents (use the Agent tool with model: haiku), each exploring a different aspect of the codebase. Give each agent a SPECIFIC search mission. They should NOT see each other's results.

Suggested exploration missions (adapt based on the question):

1. **Import tracer**: Starting from the seed files, trace all imports/dependencies and report which files they reference and why they matter.
2. **Reverse dependency finder**: Find all files that import or reference the seed files' packages. Report what depends on them.
3. **Test & fixture scanner**: Find all test files, test fixtures, and testdata related to the seed files' packages. Report what they test.
4. **Config & CLI surface scanner**: Find configuration files, CLI command definitions, and flag handling that relates to the feature area.
5. **Pattern matcher**: Search for similar patterns elsewhere in the codebase (grep for function names, types, interfaces used by the seed files).
6. **Doc & comment scanner**: Find documentation, comments, and README content related to the packages involved.

Each haiku agent should:
- Read actual files (not just list them)
- Report: file path, relevance score (1-5), and a one-line reason why it matters
- Stay focused on their specific mission
- NOT read the question in full — give them just enough context to search effectively

### Phase 2: Assembly (you, the opus agent)

After all haiku agents return:

1. Deduplicate the file lists across all agents.
2. Score each file by how many agents flagged it and their relevance ratings.
3. Read the top files yourself to verify they're actually relevant.
4. Assemble a context bundle in this format:

```markdown
# Context Bundle (Agent-Assembled)

Budget: {token_count}/8000 tokens
Seeds: {seed_files}

## Read First
1. {file_path} - {role/reason}
2. {file_path} - {role/reason}
...

## Watch
- Changing {file} may affect {other_file}.
- {other observations about ripple effects}

## Files
### {file_path}
Role: {seed|dependency|dependent|test|config|related}. Tokens: {count}.

\```{language}
{file contents}
\```

## Omitted
- {file_path} - {reason}, {token_count} tokens
```

5. Stay within an 8000 token budget for the assembled bundle. Prioritize seed files, then direct dependencies, then the most-flagged files from the exploration.

### Important

- The haiku agents explore independently — they must not share context with each other.
- YOU assemble the final result — the haiku agents just provide raw findings.
- Include file contents in the bundle, not just file paths.
- The bundle should be directly usable by an LLM to understand the code and start working.
