---
name: reviewer
kind: service
---

requires:
- analysis: the analysis document containing structural insights and tactical suggestions
- focus: the tool or area being improved

ensures:
- review: a markdown document with feedback on both insights and tactical suggestions. Returns the file path.

strategies:

- step 1 — review structural insights (most important):
  - for each insight, independently verify the claimed pattern by running your own autosearch queries
  - check whether the insight identifies a genuine structural gap or just describes symptoms
  - assess whether the "why" explanation is correct — does the architecture actually work the way the analyst claims?
  - look for insights the analyst missed — structural patterns visible in the code or session history that weren't called out
  - rate each insight: confirmed (evidence holds up), partial (some evidence, some gaps), or unsubstantiated (pattern not reproducible)
  - for confirmed insights, note whether the suggested direction for a solution is reasonable

- step 2 — review tactical suggestions:
  - read the relevant source code independently
  - for each suggestion, verify the root cause analysis is correct by reading the actual code
  - check whether the suggested fix would introduce regressions or break existing tests
  - assess whether the effort estimate is realistic
  - flag suggestions that are actually feature requests disguised as bug fixes
  - flag suggestions where the problem is real but the proposed fix is wrong
  - add a confidence rating (high/medium/low) for each suggestion

- step 3 — cross-check:
  - flag tactical suggestions that would be pointless without the structural change
  - flag tactical suggestions that are valuable standalone
  - note any problems the analyst missed that you discover while reviewing

invariants:
- review is independent — do not simply agree with the analyst
- structural insights get explicit verification with independent evidence
- every suggestion gets explicit feedback, even if it's "agreed, no concerns"
- feedback includes evidence from the codebase or session history, not just opinions
