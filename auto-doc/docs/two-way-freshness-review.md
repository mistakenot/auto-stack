---
hash: "c60b31ec"
id: "b48925a1"
read_when: "when reviewing edge cases and user experience in two-way freshness implementation"
summary: "Edge case analysis and user-perspective review of the two-way freshness design covering tag parsing, duplicate IDs, and scope hashing"
title: "Two-Way Freshness Review"
---

# Two-Way Freshness Review (Edge Cases + User POV)

This review covers:

- `auto-doc/docs/two-way-freshness.md`
- `auto-doc/docs/two-way-freshness-guide.md`

## Findings (Ordered by Severity)

### 1) High: Tag parsing can trigger on non-comments and corrupt scope hashing

References:

- `two-way-freshness.md:65`
- `two-way-freshness.md:79`
- `two-way-freshness.md:127`

Risk:

- Any `[autodoc(...)]`-looking text inside strings/docs/code could be treated as metadata and stripped.

User POV:

- "Why is this unrelated file suddenly stale/orphaned?"

### 2) High: Duplicate doc IDs are not handled

Reference:

- `two-way-freshness.md:11`

Risk:

- ID is described as "random unique," but no conflict detection/resolution path is documented.

User POV:

- One code tag may resolve to the wrong doc with no obvious failure.

### 3) High: 8-char hashes are collision-prone at scale

References:

- `two-way-freshness.md:37`
- `two-way-freshness.md:38`
- `two-way-freshness.md:66`
- `two-way-freshness-guide.md:13`

Risk:

- False "in sync" states can occur silently.

User POV:

- Trust in the system drops after one unexplained miss.

### 4) High: Indentation-only scoping is brittle across languages and formatting changes

References:

- `two-way-freshness.md:60`
- `two-way-freshness.md:63`
- `two-way-freshness.md:78`

Risk:

- Tabs/spaces, heredocs, Python/YAML-like structures, and dedent patterns can change scope unexpectedly. 
<!-- lets not overengineer, most of our projects are either typescript or golang, well see what happens with real usage -->

User POV:

- "I only reformatted and now hashes changed everywhere."
<!-- if we stay on top of lint / formatting, this shouldnt' happen often where lots of files need changing. let it come out in the wash -->
### 5) Medium: `git ls-files` misses untracked/ignored files with tags

References:

- `two-way-freshness.md:83`
- `two-way-freshness-guide.md:212`

Risk:

- False clean runs in active branches before files are added.

User POV:

- Stale links slip through until later CI/merge points.

### 6) Medium: The UX accepts high noise from non-semantic changes

References:

- `two-way-freshness-guide.md:184`
- `two-way-freshness-guide.md:235`
- `two-way-freshness.md:129`

Risk:

- Routine formatting/renames create repeated AI review loops.

User POV:

- Alert fatigue and "tool churn" rather than value.

### 7) Medium: Guide demonstrates a file-level tag that creates broad invalidation

References:

- `two-way-freshness-guide.md:195`
- `two-way-freshness-guide.md:208`

Risk:

- One local edit can invalidate top-level linkage every time.

User POV:

- Noisy stale reports become background noise.

### 8) Medium: Command model is easy to misread (`fix` vs `fixed`, plus "no rehash command")

References:

- `two-way-freshness.md:83`
- `two-way-freshness.md:108`
- `two-way-freshness.md:119`
- `two-way-freshness-guide.md:148`

Risk:

- Users run the wrong command during remediation.

User POV:

- Unnecessary trial/error during already noisy fix flows.

### 9) Medium: "Manual resolution" and "merge conflicts acceptable" under-specify recovery workflow

References:

- `two-way-freshness.md:116`
- `two-way-freshness.md:133`
- `two-way-freshness-guide.md:234`

Risk:

- Teams get stuck on orphaned tags/rebases without deterministic steps.

User POV:

- "I know it's broken, but I don't know the safest next action."

## Missing Test Cases (Important)

1. Duplicate `id` across docs should hard-fail with deterministic error output. 
<!-- yes keep this, we should have a general health / check function that asserts this, and is run in `docs fix` -->

2. Parser should ignore `[autodoc(...)]` inside string literals/non-comment contexts.
<!-- unlikely, it over complicates parsing -->

3. Mixed tabs/spaces and CRLF/LF normalization should hash consistently.
4. Untracked files containing tags should be optionally included (or clearly warned).
5. Large refactor/reformat-only PR should have a low-noise remediation path.

## Open Questions / Assumptions

1. Is the parser intentionally allowed to match tag-shaped text outside comments?
2. Should `id` uniqueness be repo-wide enforced in `autodoc fix`?
3. Is 8-char hash length a hard requirement, or can it be extended for collision safety?
4. Should there be an explicit "bulk refresh hashes without semantic doc changes" mode for user ergonomics?
