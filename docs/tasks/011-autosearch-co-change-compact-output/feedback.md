---
hash: "05b0cb27"
id: "3f56f4c4"
read_when: "reviewing lessons from the co-change compact output task or understanding approxTokens, treeDistance, or fixturegen package constraints"
summary: "Post-task feedback from the co-change compact output task: cross-phase compile coupling when deleting struct fields, tree-distance d-label arithmetic, empirical budget fixture tuning, and a multi-line subject bug caught by review."
title: "Feedback: Co-Change Compact Output (Task 011)"
---

# Feedback: Task 011

## Problems faced

1. **Phase boundaries leaked across the build gate.** Phase 1 removed `ParamsUsed.Limit` and `Options.Limit`, but the `--limit` flag registration and one `Options{Limit: ...}` literal lived in `cli/cochange.go` (a Phase 3 file) and in `conformance_test.go`. Deleting the field broke `go build ./...` immediately, so Phase 1 had to make a minimal one-line touch to a Phase-3-owned file just to keep the gate green. The plan's per-file phase assignment didn't account for compile-time coupling between the engine struct and its CLI consumer — when you delete a struct field, every reference must go in the *same* phase regardless of which phase "owns" the file.

2. **`treeDistance` d-labels are easy to get wrong by one.** The fixture authoring (Phase 5) initially used seed `src/hot.go` with `src/other/*` siblings expecting `d2`, but the real distance is `d1` (one up to `src`, one down to `other`). The seed had to be deepened to `src/a/hot.go` so the intended d0/d2/d4 labels were arithmetically correct. Distance is between *directories* (basename dropped), measured as up-segments-to-LCA + down-segments-to-seed.

3. **Token-budget fixtures need empirical tuning, not estimation.** The hot_file scenario had to be sized by running it: the first cut produced only 395 tokens under `--all` (so the "—all exceeds 500" assertion was vacuous). Sibling counts were bumped (40 d0 + 14 d2 + 4 d4) until the default budget trimmed to ≤500 (measured 496) while `--all` clearly exceeded it (657). You can't author a budget-bound fixture blind — generate, measure, adjust.

4. **PR-review bug the test suite structurally could not catch.** `sc.Subject` comes from the `message_truncated` column = full commit message (subject + body) + `MidTruncate`'s embedded `\n…[truncated]…\n` marker. Every test fixture and scenario used single-line subjects, so the multi-line-breaks-the-row bug never fired on CI but would fire on the first real commit with a body. Fixed by single-lining inside `truncSubject`.

## Reflections

- **What was tricky?** Keeping the JSON envelope byte-identical while changing the default output mode. The discipline that paid off: existing JSON tests were migrated to `--json` (not deleted), and a dedicated absence-assertion test (`*_NoDeletedParamsFields`) guards the two intentional field deletions so a future regression that re-adds them fails loudly.
- **What I'd tell myself at the start?** When a phase deletes a shared struct field, grep the whole module for references *first* and fold all of them into that phase — don't trust the plan's file-to-phase mapping to respect compile coupling.
- **What I almost did but didn't?** Almost put the newline sanitization at the `renderRow` call site (as the reviewer literally suggested). Putting it inside `truncSubject` instead makes it the single source of truth for display-safe subjects, so any future caller is protected.

## Useful context

- **`approxTokens` is `(utf8.RuneCountInString(s) + 3) / 4`** — rune count, never `len(s)`. The renderer deliberately emits multi-byte glyphs (`→ × — …`); byte length would over-trim. This is the project's budget contract (no external tokenizer, per the minimal-deps rule).
- **`fixturegen` is `package main` and unimportable.** Phase 5 had to create a sibling `scenariofixture` package re-hosting the four parquet struct shapes + parquet-go writer wiring. The generated layout must match the checked-in snapshot (`<root>/<dataset>/<dataset>.parquet`) so `etlscan.DiscoverDatasets` finds it. Reader names are `ReadCommitsSlim` / `ReadCommitFilesSlim` (not `ReadCommits`).
- **`ResolveRepo` relativises the seed arg against the git toplevel**, so CLI tests against a scenario tempdir must pass an absolute path (joined with the toplevel), not a bare `src/a/hot.go`.
- **The `--limit`-absence quickstart assertion must be section-scoped** — `--limit` is a legitimate flag for `search`/`stats`/`session`; only the co-change section must be free of it.
- Codex pre-review caught all four Phase 5 wiring hazards (unimportable `fixturegen`, wrong struct/reader names, wrong dataset layout) *in the plan* before execution — the embedded RESOLVED comments in plan.md were the authoritative corrections, more trustworthy than the prose steps.
