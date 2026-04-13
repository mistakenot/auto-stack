---
name: self-improve
kind: program
services: [preflight, explorer, analyst, reviewer, consolidator, implementer, plan-reviewer]
---

requires:
- focus: tool name or area to focus on (e.g. "autosearch", "autoetl", "auto-doc stats command")
- codebase-context: brief description of the codebase and conventions (optional)

ensures:
- summary: a report with up to 3 pull requests ready for human review, each addressing a top-priority improvement. Includes status for any that failed or were skipped.

errors:
- no-problems-found: the explorer found no problems or setbacks worth addressing
- no-actionable-items: analysis produced suggestions but none were actionable enough to implement

### Execution

# ==========================================================================
# Phase 0: Preflight — refresh data so exploration uses current state
# ==========================================================================

let preflight-result = call preflight
  focus: focus

# ==========================================================================
# Phase 1: Exploration — discover problems by using the tool as a real user
# ==========================================================================

let problems = call explorer
  focus: focus
  codebase-context: codebase-context
  preflight: preflight-result

if no problems found:
  throw no-problems-found

# ==========================================================================
# Phase 2: Analysis — compare problems to codebase, produce suggestions doc
# ==========================================================================

let suggestions = call analyst
  problems: problems
  focus: focus
  codebase-context: codebase-context

# ==========================================================================
# Phase 3: Independent review — a fresh reviewer critiques the suggestions
# ==========================================================================

let review = call reviewer
  suggestions: suggestions
  focus: focus

# ==========================================================================
# Phase 4: Consolidation — incorporate feedback, pick top 3
# ==========================================================================

let priorities = call consolidator
  suggestions: suggestions
  review: review
  focus: focus

if priorities.count < 1:
  throw no-actionable-items

# ==========================================================================
# Phase 5: Implementation — up to 3 parallel agents, each on its own worktree
# One implementer failing should not abort the others.
# ==========================================================================

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
# Phase 6: Summary — always runs, even if some implementers failed
# ==========================================================================

let summary = call consolidator
  task: "final-summary"
  pr1: pr1
  pr2: pr2
  pr3: pr3
  priorities: priorities

return summary
