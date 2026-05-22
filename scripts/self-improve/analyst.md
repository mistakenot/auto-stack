---
name: analyst
kind: service
---

requires:
- problems: structured list of findings (structural and tactical) from exploration
- focus: the tool or area being improved
- codebase-context: optional project context

ensures:
- analysis: a markdown document written to the workspace containing two sections: (1) structural findings about systemic tool issues traced to root causes in the code, and (2) tactical suggestions for specific code fixes. Returns the file path.

strategies:

- step 1 — analyze structural findings first (this is the primary output):
  - for each structural finding, read the relevant source code to understand the full picture
  - trace each finding to its root cause in the code — which function, which data path, which assumption is wrong?
  - connect structural findings to each other — problems that seem separate often share a code-level root cause
  - frame each insight as: what the tool does wrong, where in the code it happens, why the code works this way, and what kind of fix it needs
  - rate each insight by: breadth (how many commands/workflows affected), severity (how broken the behavior is), and addressability (how feasible a fix would be)

- step 2 — analyze tactical findings (secondary output):
  - for each tactical problem, read the relevant source code to understand the root cause
  - compare problems to existing code patterns and project conventions (check CLAUDE.md)
  - group related problems that share a root cause into a single suggestion
  - for each suggestion, estimate effort (small/medium/large) and impact (high/medium/low)
  - prefer fixes that align with existing code patterns over introducing new abstractions
  - include specific file paths and line numbers where changes would be needed
  - rank suggestions by impact-to-effort ratio

- step 3 — connect the two levels:
  - for each tactical suggestion, note which structural insight it partially addresses (if any)
  - flag tactical fixes that would be pointless without the structural change
  - flag tactical fixes that are valuable standalone regardless of structural work

invariants:
- structural findings come first in the document, tactical suggestions second
- every finding traces to specific code (file paths, function names, line numbers)
- findings describe what the tool does wrong and where in the code the problem lives
- tactical suggestions include concrete file paths and proposed changes
- the document follows this structure: title, findings summary, detailed finding sections, then tactical summary table, then detailed tactical sections
