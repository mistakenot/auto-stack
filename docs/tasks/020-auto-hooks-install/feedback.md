---
hash: "de36a9ee"
id: "6bd4937a"
read_when: "implementing hooks installation or debugging .codex/.claude directory creation issues"
summary: "Post-implementation reflections on the auto hooks install task: stray .codex file shadowing, pre-existing gofmt drift on main, and the generic map merge design for lossless field preservation."
title: "Feedback: Task 020 — Auto Hooks Install"
---

# Feedback: Task 020

## Problems faced
1. **Stray `.codex` *file* shadowed the `.codex/` directory** — a 0-byte `.codex`
   file had been accidentally committed to `main` (commit b63d4e8), so the
   installer's `os.ReadFile(".codex/hooks.json")` failed with "not a directory".
   The installer was correct; the repo state was wrong. Resolved by `git rm`-ing
   the stray file as part of the dogfood commit. Context to understand it:
   `git log -- .codex` shows it arrived in a "add pending docs" chore commit and
   was always empty.
2. **Pre-existing `gofmt` drift on `main` blocked CI** — `make check`'s
   `fmt-check` flags `auto-watch/internal/config/paths.go`, which is unformatted
   on `main` itself (not in this task's diff). Every branch off main fails this
   check. Per user direction, merged PR #74 past the unrelated red check (it was
   MERGEABLE) and left the drift to be fixed separately.

## Reflections
- **What was tricky?** Nothing in the implementation — the generic
  `map[string]any` merge was the right call and made AC-2 (lossless field
  preservation) fall out naturally. The friction was entirely environmental
  (stray `.codex` file, main formatting drift), not in the code under test.
- **What would you tell yourself at the start?** Before dogfooding a command
  that creates `.codex/` or `.claude/`, check whether those paths already exist
  as *files* in the repo — a single `ls -la .codex .claude` up front would have
  predicted the failure.
- **What did you almost do but didn't?** Almost suppressed the Codex trust hint
  on no-op re-runs (the PR reviewer suggested it), but AC-6 requires the hint on
  *any* successful install — suppressing it would have violated the AC. Kept it
  unconditional and documented why on the PR.

## Useful context
- The generic-tree merge design in `solution.md` (driven by a P1 review comment
  rejecting typed structs) was the single most valuable decision — it preserves
  unknown handler fields (`timeout`/`statusMessage`/`args`/`if`) byte-for-byte
  and made the AC-2 test trivial to satisfy.
- AC-5's PATH-resolution verification (`command -v auto` must resolve to the
  built binary, then pipe a payload through `sh -c 'auto hooks fire …'`) was the
  right way to prove the *bare* installed command string works — calling
  `./bin/auto` directly would have passed even if `auto` weren't on PATH.
- `sharedconfig.WriteJSONFileAtomic` + `encoding/json`'s sorted map keys gave
  deterministic output for free; no custom ordering needed.
