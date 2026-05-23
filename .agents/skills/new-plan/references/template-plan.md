# Plan Template

```markdown
# Plan: Task $ID

## Summary
One sentence describing the implementation approach.

## Changes
| Symbol | File | Description |
|--------|------|-------------|
| + | path/to/new.ts | New service for X |
| ~ | path/to/existing.ts | Add Y method |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [ ] `path/to/feature.test.ts` -- unit tests for service
- [ ] `e2e/tests/feature.spec.ts` -- e2e happy path

## Execution Sequence
```
Phase 1 (DB) --> Phase 2 (Backend) --> Phase 4 (E2E)
                                   \-> Phase 3 (Frontend) -/
```

## Plan

### Phase 1: Database Schema
- [ ] Step 1.1: Create migration for new table
- [ ] Step 1.2: Run migration, verify schema
- [ ] Step 1.3: Commit: `feat($ID): phase 1 - database schema`

### Phase 2: Backend Service
- [ ] Step 2.1: Create repository
- [ ] Step 2.2: Create service with business logic
- [ ] Step 2.3: Create server function
- [ ] Step 2.4: Run typecheck
- [ ] Step 2.5: Write and run unit tests
- [ ] Step 2.6: Commit: `feat($ID): phase 2 - backend service`

### Phase 3: Frontend
- [ ] Step 3.1: Create route
- [ ] Step 3.2: Create components
- [ ] Step 3.3: Wire to server function
- [ ] Step 3.4: Run typecheck
- [ ] Step 3.5: Commit: `feat($ID): phase 3 - frontend`

### Phase 4: E2E Tests
- [ ] Step 4.1: Write e2e test covering AC-1 through AC-3
- [ ] Step 4.2: Run e2e tests
- [ ] Step 4.3: Commit: `feat($ID): phase 4 - e2e tests`

## Success Criteria
- [ ] `typecheck` passes
- [ ] All unit tests pass
- [ ] All e2e tests pass
- [ ] Manual verification: $description

## Open Questions
- (empty if all resolved)
```

## Rules

- Each phase is atomic -- one subagent handles it end-to-end
- Every phase ends with a commit step
- Every phase includes verification (typecheck, tests, lint)
- Execution Sequence shows the dependency DAG for parallel dispatch
- Checkboxes track progress and enable session resumption
