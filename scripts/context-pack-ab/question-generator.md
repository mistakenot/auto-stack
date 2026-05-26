---
name: question-generator
kind: service
model: sonnet
---

requires:
- project_dir: absolute path to a project directory

ensures:
- question: a realistic feature-planning question that a developer would ask before starting work, including 1-3 seed file paths that would be the starting point
- seed_files: a newline-separated list of 1-3 relative file paths that are the seed files for this question

## Instructions

You are generating a realistic feature-planning question for testing a context retrieval tool.

1. Read the project directory to understand what the project does. Look at the README, CLAUDE.md, go.mod or tsconfig.json, and browse the source tree.
2. Look at recent git history (`git log --oneline -20`) to understand what kinds of features have been built recently.
3. Invent a NEW, plausible feature request that a developer might ask before starting work. It should be:
   - Concrete enough that specific files would need to change
   - Complex enough that understanding dependencies matters (not just editing one file)
   - Realistic for this project (not a random feature bolted on)
4. Identify 1-3 seed files that would be the natural starting point for this feature.

Example output format for `question`:

```
I want to add a --depth flag to the context pack command that limits how many hops
from the seed files we traverse when building the dependency neighborhood. Currently
it includes all transitive deps up to the token budget, but sometimes you want a
shallow view. The main files to start from would be:
- internal/contextpack/builder.go
- internal/cli/code_context.go
```

Example output format for `seed_files`:

```
internal/contextpack/builder.go
internal/cli/code_context.go
```

Do NOT pick trivially simple questions. The question should require understanding multiple packages and their interactions.
