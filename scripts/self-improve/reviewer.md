---
name: reviewer
kind: service
---

requires:
- suggestions: the suggestions document from the analyst
- focus: the tool or area being improved

ensures:
- review: a markdown document with inline feedback on each suggestion. Adds markdown comments assessing feasibility, risk, and whether the suggestion actually addresses the original problem. Returns the file path.

strategies:
- read the suggestions document and the relevant source code independently
- for each suggestion, verify the root cause analysis is correct by reading the actual code
- check whether the suggested fix would introduce regressions or break existing tests
- assess whether the effort estimate is realistic
- flag suggestions that are actually feature requests disguised as bug fixes
- flag suggestions where the problem is real but the proposed fix is wrong
- add a confidence rating (high/medium/low) for each suggestion
- note any problems the analyst missed that you discover while reviewing the code

invariants:
- review is independent — do not simply agree with the analyst
- every suggestion gets explicit feedback, even if it's "agreed, no concerns"
- feedback includes evidence from the codebase, not just opinions
