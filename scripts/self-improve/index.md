---
name: self-improve
kind: program
services: [preflight, explorer, analyst, reviewer, consolidator, implementer]
---

requires:
- focus: tool name or area to focus on (e.g. "autosearch", "autoetl", "auto-doc stats command")
- codebase-context: brief description of the codebase and conventions (optional)

ensures:
- summary: an insights report describing structural gaps and patterns discovered, plus up to 3 tactical PRs for specific fixes. The insights report is the primary deliverable — PRs are secondary. Includes status for any PRs that failed or were skipped.

errors:
- no-problems-found: the explorer found no problems or patterns worth addressing
- no-actionable-items: analysis produced no insights and no tactical suggestions

### Execution

# ==========================================================================
# Phase 0: Preflight — refresh data so exploration uses current state
# ==========================================================================

let preflight-result = call preflight
  focus: focus

# ==========================================================================
# Phase 1: Exploration — mine session history for structural patterns,
#           then explore the tool hands-on for tactical issues
# ==========================================================================

let problems = call explorer
  focus: focus
  codebase-context: codebase-context
  preflight: preflight-result

if no problems found:
  throw no-problems-found

# ==========================================================================
# Phase 2: Analysis — separate structural insights from tactical fixes
# ==========================================================================

let analysis = call analyst
  problems: problems
  focus: focus
  codebase-context: codebase-context

# ==========================================================================
# Phase 3: Independent review — a fresh reviewer critiques both insights
#           and tactical suggestions
# ==========================================================================

let review = call reviewer
  analysis: analysis
  focus: focus

# ==========================================================================
# Phase 4: Consolidation — produce insights report (primary), pick top 3
#           tactical items (secondary)
# ==========================================================================

let priorities = call consolidator
  analysis: analysis
  review: review
  focus: focus

if no insights and priorities.count < 1:
  throw no-actionable-items

# ==========================================================================
# Phase 5: Implementation — up to 3 parallel agents for tactical PRs.
#           Skipped entirely if no tactical items survived review.
#           One implementer failing should not abort the others.
# ==========================================================================

if priorities.count >= 1:
  parallel (on-fail: "continue"):
    let pr1 = call implementer
      priority-item: priorities.item_1
      item-number: "1"
      focus: focus
      codebase-context: codebase-context

    if priorities.count >= 2:
      let pr2 = call implementer
        priority-item: priorities.item_2
        item-number: "2"
        focus: focus
        codebase-context: codebase-context

    if priorities.count >= 3:
      let pr3 = call implementer
        priority-item: priorities.item_3
        item-number: "3"
        focus: focus
        codebase-context: codebase-context

# ==========================================================================
# Phase 6: Summary — always runs. Leads with insights, then PR status.
# ==========================================================================

let summary = call consolidator
  task: "final-summary"
  insights-report: priorities.insights-report
  pr1: pr1
  pr2: pr2
  pr3: pr3
  priorities: priorities

return summary
