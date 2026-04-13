---
name: consolidator
kind: service
---

requires:
- suggestions: the suggestions document from the analyst (optional, not present for final-summary task)
- review: the review document with feedback (optional, not present for final-summary task)
- focus: the tool or area being improved (optional)
- task: "consolidate" (default) or "final-summary"
- pr1: first PR result (only for final-summary)
- pr2: second PR result (only for final-summary)
- pr3: third PR result (only for final-summary)
- priorities: the priority items (only for final-summary)

ensures:
- output: either a priorities document (for consolidate task) or a final summary report (for final-summary task)

strategies:
- for consolidation:
  - merge the analyst's suggestions with the reviewer's feedback
  - where the reviewer disagreed, weigh the evidence from both sides
  - drop suggestions where the reviewer identified fatal flaws (wrong root cause, would break things)
  - upgrade suggestions where the reviewer added supporting evidence
  - select the top 3 items by impact-to-effort ratio, weighted by reviewer confidence
  - for each selected item, produce a clear scope statement: what changes, which files, what the acceptance criteria are
  - flag the top 3 prominently at the top of the document
  - output must include item_1, item_2, item_3 fields and a count field

- for final-summary:
  - summarize all 3 PR outcomes
  - list the branch names and PR URLs
  - note any PRs that failed (tests didn't pass, couldn't create PR)
  - provide a one-paragraph executive summary of what was improved

invariants:
- exactly 3 items are selected (or fewer if fewer are viable, with count reflecting actual number)
- each item has a clear, bounded scope — no open-ended improvements
- each item is independently implementable (no dependencies between the 3)
