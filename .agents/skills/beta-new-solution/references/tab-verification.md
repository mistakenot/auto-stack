# Verification Tab Guidelines

The Verification tab defines how the task will be tested and what "done" means. It is the second tab in `plan.html`, created by `beta-new-solution` (stage 2).

## Sections (in order)

1. **Test Strategy** (id: `test-strategy`) — Testing approach, tooling, and coverage philosophy.
2. **Coverage Map** (optional) — A mermaid diagram showing the testing architecture (e.g. Unit → Integration → E2E). Include when it clarifies the testing layers.
3. **Acceptance Criteria** — One `pd-ac` card per criterion, using id format `AC-1`, `AC-2`, etc. Write each in Given/When/Then format. Leave `phases` and `tests` attributes empty — the Plan stage backfills these with traceability data.
4. **Known Gaps & Risks** (id: `verification-gaps`) — What isn't covered and why.

## Rules

- Scan available project skills for testing, verification, and assurance strategies before writing this tab.
- Acceptance criteria must be concrete and testable — avoid vague "should work" language.
