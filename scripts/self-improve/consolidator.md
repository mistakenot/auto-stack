---
name: consolidator
kind: service
---

requires:
- analysis: the analysis document with insights and suggestions (optional, not present for final-summary task)
- review: the review document with feedback (optional, not present for final-summary task)
- focus: the tool or area being improved (optional)
- task: "consolidate" (default) or "final-summary"
- pr1: first PR confirmation — branch name and PR URL (only for final-summary)
- pr2: second PR confirmation — branch name and PR URL (only for final-summary)
- pr3: third PR confirmation — branch name and PR URL (only for final-summary)
- priorities: the priority items (only for final-summary)
- insights-report: the insights report from consolidation (only for final-summary)

ensures:
- output: either a consolidated priorities document with insights report (for consolidate task) or a final summary (for final-summary task)

strategies:
- for consolidation:
  - FIRST, produce the findings report — this is the primary deliverable:
    - merge the analyst's structural findings with the reviewer's feedback
    - where the reviewer added supporting evidence, strengthen the finding
    - where the reviewer disagreed, weigh evidence from both sides
    - for each validated finding, write a clear summary: what the tool does wrong, where in the code, and what direction a fix should take
    - the findings report should be understandable by someone who hasn't used the tool — include reproduction steps and key evidence inline
    - write the findings report to the workspace as a standalone document

  - SECOND, select up to 3 tactical items for implementation:
    - merge the analyst's tactical suggestions with the reviewer's feedback
    - drop suggestions where the reviewer identified fatal flaws
    - select the top 3 items by impact-to-effort ratio, weighted by reviewer confidence
    - for each selected item, produce a clear scope statement: what changes, which files, what the acceptance criteria are
    - note which structural insight each tactical item partially addresses (if any)
    - output must include item_1, item_2, item_3 fields and a count field
    - if no tactical items survive review, set count to 0 — the insights report is still valuable on its own

- for final-summary:
  - lead with the insights report — this is what the user cares about most
  - for each PR URL, fetch the PR body using `gh pr view <url> --json body` to get the full workflow record
  - summarize PR outcomes (branch names, URLs, pass/fail)
  - frame PRs as tactical steps toward the structural insights, not as the main achievement
  - provide a one-paragraph executive summary connecting insights to actions taken

invariants:
- the findings report is always produced, even if no tactical items are viable
- findings are concise and evidence-based — reproduction steps and code locations, not essay-length narratives
- each finding connects observed tool behavior to root cause in the code
- tactical items include concrete code changes, not just descriptions of what to fix
- the final summary leads with findings, not PRs
