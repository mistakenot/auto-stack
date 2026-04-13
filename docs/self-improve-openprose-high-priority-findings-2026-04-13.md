# Self-Improve OpenProse Review: High-Priority Findings

**Date:** 2026-04-13  
**Scope:** `scripts/self-improve/*.md` reviewed as OpenProse v1 program/service contracts.

## P1. Missing Priority Items Can Break Parallel Implementation

The top-level program always calls three implementers (`item_1`, `item_2`, `item_3`) even when fewer than three priority items are viable.

- Evidence:
  - `index.md` only rejects `count < 1` before entering parallel implementation.
  - `index.md` unconditionally calls `priorities.item_2` and `priorities.item_3`.
  - `consolidator.md` explicitly allows fewer viable items.
- Why this is high priority:
  - This can trigger missing-input runtime failures in core execution, preventing completion of the loop.
- Fix direction:
  - Gate implementer calls by `priorities.count`, or have consolidator always emit placeholders with explicit skip semantics.

## P1. Delegation Schema Appears Misdeclared for `implementer`

`implementer.md` declares `delegates:` at top level, while OpenProse delegation constraints are defined via `shape.delegates`.

- Evidence:
  - `implementer.md` uses frontmatter `delegates:`.
  - OpenProse docs/examples consistently specify delegation under `shape.delegates` and describe runtime enforcement from manifest delegates.
- Why this is high priority:
  - Runtime delegation to `plan-reviewer` may fail validation or not be wired/enforced as intended.
- Fix direction:
  - Move delegation declaration into:
    - `shape.delegates.plan-reviewer: [review implementation plan]`
  - Keep all delegation behavior aligned with the same schema used by OpenProse examples.

## P1. Parallel Stage Is Fail-Fast But Summary Expects Partial Failures

The implementation phase runs as default `parallel:` (fail-fast), but the final summary contract expects to report failed PRs.

- Evidence:
  - `index.md` uses bare `parallel:` for `pr1/pr2/pr3`.
  - `implementer.md` defines failure errors (`tests-failed`, `build-failed`).
  - `consolidator.md` final-summary strategy expects to note failed PRs.
- Why this is high priority:
  - One implementer failure can abort the entire run before summary generation, contradicting declared behavior.
- Fix direction:
  - Use a non-fail-fast policy for the parallel block and always run final-summary with success/failure payloads from each item.

## Notes

- Findings are limited to high-priority execution/contract risks.
- This report is intentionally implementation-oriented so the next step can be direct script edits.
