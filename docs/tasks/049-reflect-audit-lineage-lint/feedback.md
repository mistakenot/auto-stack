# Feedback: Task 049

## Problems faced

1. **Validation gates have blind spots between layers** — the `split` consolidate op
   ran every child through the fuzzy live-rule similarity gate, but that gate
   excludes drafts and only compares against *already-written* rules. Two children
   in the same `into` batch with identical `use_when` + `domain` slipped through
   because neither existed yet and both are drafts. The fix had to be a separate
   explicit pairwise pass over the batch, not a tweak of the existing gate.

2. **Adding a lifecycle value to the global valid-set has reach** — putting
   `enforced` in the shared `validLifecycles` set silently authorized
   `rule create/edit --lifecycle enforced`, bypassing the `rule graduate`
   provenance path. The valid-set membership and the per-command capability were
   coupled in a way that wasn't obvious until review. Solution was a cross-field
   invariant (`enforced` ⇒ `lint_ref` present) in `ValidateRule`, not a per-command
   guard.

3. **Best-effort provenance still needs format discipline** — evidence commits and
   line ranges were stored verbatim, so `not-a-hash` could become persisted audit
   provenance. The subtlety: empty entries must stay valid (capture is best-effort
   and positional), so only *present* values get format-checked.

## Reflections

- **What was tricky?** The three review findings were all "this validation looks
  complete but has a gap one layer over." None were visible from a single function;
  each needed reasoning about what an adjacent layer does and doesn't cover.
- **What would you tell yourself at the start?** When you add a member to a shared
  valid-set (lifecycles, kinds, tags), immediately ask which command surfaces that
  member now becomes reachable from — the blast radius is wider than the diff.
- **What did you almost do but didn't?** Almost tightened the existing fuzzy
  live-rule gate to catch sibling collisions; rejected because siblings are drafts
  the matcher deliberately excludes — a separate exact-key pass is the right shape.

## Useful context

- `auto-reflect/internal/observations/model.go` — `validateEvidence` is the single
  choke point for all provenance format rules; the `validate()`-returns-structured-
  errors convention (code/path/field/message/value) made adding checks mechanical.
- `auto-reflect/internal/rules/validate.go` — `ValidateRule` is the shared validator
  reused across `create`/`edit`/`graduate`; cross-field invariants belong here, not
  in command handlers.
- The CLAUDE.md guidance on enforcing exact regex formats for constrained
  identifiers/hashes (`^[0-9a-f]{8}$` family) directly informed the commit
  (`7-40` hex) and line-range regexes.
