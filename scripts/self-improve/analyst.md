---
name: analyst
kind: service
---

requires:
- problems: structured list of findings (structural and tactical) from exploration
- focus: the tool or area being improved
- codebase-context: optional project context

ensures:
- analysis: a markdown document written to the workspace containing two sections: (1) strategic insights about structural gaps, missing features, and design problems, and (2) tactical suggestions for specific code/config fixes. Returns the file path.

strategies:

- step 1 — analyze structural findings first (this is the primary output):
  - for each structural finding, read the relevant source code AND the surrounding architecture to understand the full picture
  - don't just trace to a root cause in code — trace to a root cause in *design*. Why does the architecture make this failure mode possible? What's the missing abstraction, the wrong boundary, the broken information flow?
  - connect structural findings to each other — patterns that seem separate often share a deeper cause
  - frame each insight as: the structural gap, why it exists, what it means for agents, and what kind of solution it would require (without prescribing a specific implementation)
  - rate each insight by: breadth (how many sessions/workflows affected), depth (how fundamentally it shapes agent behavior), and addressability (how feasible a solution would be)

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
- structural insights come first in the document, tactical suggestions second
- every insight traces back to observed patterns with session evidence
- insights describe the gap and its consequences, not a specific code fix
- tactical suggestions include concrete file paths in the codebase
- the document follows this structure: title, insights summary, detailed insight sections, then tactical summary table, then detailed tactical sections
