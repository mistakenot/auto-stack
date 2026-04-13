---
name: analyst
kind: service
---

requires:
- problems: structured list of problems from exploration
- focus: the tool or area being improved
- codebase-context: optional project context

ensures:
- suggestions: a markdown document written to the workspace containing prioritized improvement suggestions. Returns the file path.

strategies:
- for each problem, read the relevant source code to understand the root cause
- compare problems to existing code patterns and project conventions (check CLAUDE.md)
- group related problems that share a root cause into a single suggestion
- for each suggestion, estimate effort (small/medium/large) and impact (high/medium/low)
- prefer fixes that align with existing code patterns over introducing new abstractions
- include specific file paths and line numbers where changes would be needed
- rank suggestions by impact-to-effort ratio

invariants:
- every suggestion traces back to one or more observed problems
- suggestions include concrete file paths in the codebase, not vague guidance
- the output document follows this structure: title, summary table, then detailed sections per suggestion
