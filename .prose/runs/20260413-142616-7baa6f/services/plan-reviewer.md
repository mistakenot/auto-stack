---
name: plan-reviewer
kind: service
---

requires:
- plan-doc: the implementation plan document to review
- priority-item: the priority item this plan addresses
- focus: the tool or area being improved

ensures:
- feedback: structured feedback on the plan including approval status, concerns, and required changes

strategies:
- verify the plan addresses the original problem completely
- check that the plan follows existing project conventions (check CLAUDE.md)
- verify test coverage is included in the plan
- flag any scope creep beyond the priority item's boundaries
- check that the plan won't break existing functionality
- keep feedback actionable and specific

invariants:
- feedback includes an explicit approve/revise verdict
- if revise, feedback lists specific required changes
- feedback never expands scope beyond the original priority item
