---
hash: "e5f34098"
id: "ecafb95b"
read_when: "when implementing two-way freshness links between documentation files"
summary: "Technical design for dropping [autodoc()] links inside any markdown file as HTML comments, with header-depth-based scope selection for freshness checks across doc-to-doc dependencies."
title: "Markdown-Embedded Autodoc Tags Technical Design"
---

# Markdown-Embedded Autodoc Tags Technical Design

## Goal

Allow `[autodoc(docId@docHash, scopeHash)]` tags to be placed inside any markdown file as an HTML comment, so that freshness checks extend to **doc-to-doc** dependencies — not just code-to-doc.

Example use case: a skill file (`SKILL.md`) or a requirements doc that depends on a canonical reference doc should flag as stale when either side drifts. Today the freshness mechanism only fires on tags inside source code (`.md` files are explicitly excluded in `internal/linkscan/linkscan.go:39-49`).

This feature is **not** skill-specific. It works for any markdown file — skills, requirements docs, playbooks, CLAUDE.md, agent memory files, README sections.

## Scope

Included:

- Accept `[autodoc(...)]` tags inside markdown files, delimited as HTML comments.
- Select scope by nearest enclosing markdown heading depth.
- Freshness reporting integrated with `autodoc fix`, same output shape as code tags.
- Hash rewriting via `autodoc fixed` applies to doc-embedded tag `scopeHash` values too.
- E2E test covering the full lifecycle across doc-to-doc dependencies.

Not included:

- AST-aware markdown parsing beyond what is required to locate headings and strip embedded tags.
- New commands; feature extends existing `fix` / `fixed` / `stale`.
- Transitive drift propagation (if doc A depends on B, and B depends on C, we do not infer a C→A link).
- Rewriting docs' semantic content automatically — fix output remains instruction-driven.

## Motivation

Two observed patterns today cannot be expressed:

1. A skill's instructions depend on a requirements or reference doc. When the reference changes, the skill quietly drifts.
2. A high-level doc (e.g. `user-journey.md`) includes a section that restates constraints from a lower-level spec. The restatement rots when the spec changes.

Both are doc-to-doc freshness problems with the same shape as code-to-doc freshness. The existing `[autodoc()]` machinery (doc `id`, doc `hash`, `scopeHash`, `fix` reporting) is already the right primitive — only the **location** of the tag and the **scope delimiter** differ.

## Tag Syntax in Markdown

Tags in markdown MUST be wrapped in an HTML comment so they do not render:

```markdown
<!-- [autodoc(a1b2c3d4@deadbeef, 00000000)] -->
```

Rules:

- Exactly one tag per HTML comment. Multiple tags in a single comment block are a parse error (`MalformedTag`).
- The comment may contain surrounding whitespace inside `<!-- ... -->` but no other text on the same "line" inside the comment markers — this keeps the regex strict and prevents false positives in prose.
- The comment must be on its own line. An inline comment on a heading line (e.g. `## Foo <!-- [autodoc(...)] -->`) is a parse error.
- The tag body itself (`[autodoc(docId@docHash, scopeHash)]`) obeys the existing strict regex defined in `linkscan.go:16`.

Rationale for HTML comment wrapping:

- HTML comments are universally supported markdown syntax for invisible metadata.
- They are clearly distinct from prose mentions of `[autodoc(...)]` inside code fences or backtick spans, which can then be safely ignored.
- They render to nothing in GitHub, VS Code preview, and any standard markdown renderer.

## Scope Selection by Header Depth

Code tags today use **indentation depth**. Markdown tags instead use **markdown heading depth**. The rule set:

1. Find the tag's position in the file.
2. Walk **upward** through preceding lines to find the nearest ATX heading (`#`, `##`, …, `######`) at any depth. Call this the **anchor heading**. Let `anchorDepth` be the number of `#` characters.
3. If no anchor heading is found (the tag appears before the first heading — typical placement: immediately after the frontmatter), the scope is **the entire doc body**, defined as every line after the closing `---` of the frontmatter block through end of file.
4. Otherwise, the scope is every line from the anchor heading (inclusive) forward, up to but not including the first subsequent heading with depth `<= anchorDepth`. End of file terminates scope.
5. Content inside deeper nested headings (depth `> anchorDepth`) is included in the scope.
6. The tag's own line is always stripped from the hash input (per the Scope Hash Algorithm below), so tag placement within the scope does not affect the computed hash.

### Worked examples

**Example A — top-of-doc tag (whole-doc scope):**

```markdown
---
id: "..."
hash: "..."
---
<!-- [autodoc(otherid@hashx, scope00)] -->

# Title

## Section 1
...
## Section 2
...
```

Scope covers everything after the frontmatter through end of file. The tag's own line is stripped from the hash input, so the scope hash reflects `# Title` onward without including the `<!-- -->` wrapper.

**Example B — section-scoped tag under `##`:**

```markdown
## Section 1
<!-- [autodoc(otherid@hashx, scope00)] -->

Content of section 1.

### Subsection 1.1
More content.

## Section 2      <-- scope ends here (next `##`)
```

Anchor heading is `## Section 1` (depth 2). Scope runs from `## Section 1` inclusive up to but not including `## Section 2`. The `### Subsection 1.1` heading and its content are included because depth 3 > 2.

**Example C — subsection-scoped tag under `###`:**

```markdown
## Section 1
### Subsection 1.1
<!-- [autodoc(otherid@hashx, scope00)] -->
Content.

### Subsection 1.2  <-- scope ends here (next `###`, same depth)
```

Anchor heading is `### Subsection 1.1` (depth 3). Scope runs from `### Subsection 1.1` up to but not including `### Subsection 1.2`. A following `## Section 2` would also terminate (depth 2 < 3).

**Example D — two tags in one doc:**

Each tag resolves its own anchor independently. No coupling. The scopes may overlap: a top-of-doc tag's scope contains every section-scoped tag's scope in the same file.

**Implication of overlap**: editing content inside a section that is also covered by a top-of-doc tag invalidates **both** scope hashes, so `autodoc fix` will emit two `LINK STALE` reports for that single edit. This is correct and expected — the two tags assert consistency against two different target docs. The test plan below covers this explicitly.

### Heading style

- Only ATX headings (`#` prefix) are supported. Setext headings (underline with `===` / `---`) are out of scope for v1; they are uncommon in the repo and add parse complexity.
- Headings inside fenced code blocks (```` ``` ````) are ignored when resolving anchors and terminators. This requires a minimal scanner that tracks fence state.

## Scope Hash Algorithm

For each doc-embedded tag:

1. Determine the scope range per the rules above.
2. Collect the raw lines in that range.
3. Normalize line endings (`\r\n` → `\n`).
4. **Remove any line whose sole content (ignoring leading/trailing whitespace) is an autodoc-tag HTML comment** matching `^\s*<!--\s*\[autodoc\([^)]*\)\]\s*-->\s*$`. This removes the full line — including the `<!-- -->` wrapper — so whitespace variations in the comment shell cannot cause spurious hash changes. This also covers the tag whose scope we are computing *and* any other markdown tags in the same scope, preventing cascade invalidation.
5. For non-markdown cases (defensive, in case a code-style bare `[autodoc(...)]` appears inside a fenced code block within the scope), also strip `[autodoc(...)]` substrings matching the loose regex in `linkscan.go:17`. In practice this is a no-op for markdown docs under our parsing rules.
6. MD5-hash the result; take the first 8 hex chars.

The algorithm intentionally hashes **raw markdown source** (including heading markers, list syntax, code fences). This catches formatting changes, which for docs are semantically meaningful.

## Tag Discovery in Markdown Files

Extend `internal/linkscan` without broadening its default file set:

- **Keep** `.md` and `.markdown` in `ignoredExtensions` for the existing bare-tag path (`linkscan.go:40-41`). This preserves current behavior for non-doc markdown files (READMEs, CLAUDE.md, MEMORY.md, arbitrary prose) and prevents false positives from HTML comments anywhere in the repo.
- Add a **separate markdown scan** invoked after the doc index is built. Input: the set of markdown files already discovered as docs (files with valid frontmatter and a registered `id`). Output: additional `Tag` entries with `ScopeKind = ScopeKindMarkdown`.
- For each indexed doc, recognize tags only inside HTML-comment wrappers (`<!-- [autodoc(...)] -->`). Bare `[autodoc(...)]` in doc prose is ignored (no malformed-tag warning), because that form appears legitimately in documentation about the tool itself (this design, `two-way-freshness.md`, `two-way-freshness-guide.md`).
- Non-markdown files keep today's behavior: bare `[autodoc(...)]` inside source comments.

Blast-radius rationale: markdown tag scanning is a doc-index operation, not a repo-walk operation. Files that are not in the doc index (no frontmatter id) are not scanned, regardless of extension. This keeps the feature's file set bounded and predictable.

**`ignores` config honored**: the markdown scan operates on the doc index already produced by existing code paths. That index already applies the `ignores` glob list from `.auto/doc/settings.json`, so excluded docs are never presented to the tag scanner. No second exclusion path exists.

Detection heuristic for the HTML-comment wrapper:

```text
^\s*<!--\s*\[autodoc\([^)]*\)\]\s*-->\s*$
```

The line must match this end-to-end. Anything else is not a markdown-embedded tag and is not considered.

**Fenced-code-block exclusion (symmetric with heading parsing)**: tag detection tracks fenced-code-block state in the same single pass that indexes headings. A line matching the detection heuristic is a tag **only when outside** a fenced block. Inside a fence it is illustrative prose. This prevents the scanner from flagging examples in this design doc, `two-way-freshness.md`, `two-way-freshness-guide.md`, and any future tool-documentation doc that shows the syntax inside triple-backtick blocks.

**Strict malformed detection**: when a line contains an HTML comment wrapper but its inner tag body fails the strict regex AND the body contains `@` (the signal that a real tag was attempted), emit `MalformedMarkdownTag` with the file path and line. This mirrors the code-side `malformedCandidateRegex` (`linkscan.go:22`) so users get feedback on typos like `<!-- [autodoc(abc@def) -->` (missing closing bracket) or `<!-- [autodoc(abcdefgh@deadbeef, 123)] -->` (short hash). Bodies without `@` remain silent to preserve prose-safety.

## Data Model Changes

`internal/linkscan.Tag` is already language-agnostic and carries `FilePath`, `Line`, `DocId`, `DocHash`, `ScopeHash`, `RawTag`. No struct changes.

Add a `ScopeKind` field to distinguish:

```go
type ScopeKind int

const (
    ScopeKindIndent   ScopeKind = iota // existing code-scope behavior
    ScopeKindMarkdown                  // new: header-depth-based
)

type Tag struct {
    // existing fields...
    ScopeKind ScopeKind
}
```

This lets the scope-hash function dispatch on `ScopeKind` without inferring from file extension downstream.

**API change**: `ComputeScopeHash(filePath string, tagLine int)` becomes `ComputeScopeHash(tag Tag)`. Dispatch happens inside: `ScopeKindIndent` runs the existing indentation algorithm; `ScopeKindMarkdown` runs the heading-depth algorithm. Call sites in `fix.go` and `fixed.go` pass the already-parsed `Tag` struct rather than re-deriving fields. This is a minor signature change confined to one function.

## Freshness Evaluation Changes

`internal/linkscan` checker logic: no semantic change. Each tag is evaluated the same way regardless of `ScopeKind`:

- Resolve `docId` against doc index; emit `OrphanedTag` if missing.
- Compare tag `docHash` to current doc hash.
- Compute current `scopeHash` (dispatching on `ScopeKind`) and compare.

Output from `autodoc fix` reuses the existing templates. The `code file:` label becomes `location:` in output and prints `file:line` unchanged. An existing vs new field distinction is not required — users can tell from the file extension whether the source is code or doc.

### New edge cases surfaced by doc-to-doc links

- **Self-reference**: a doc with tag pointing to itself. Flag as a validation error (`SelfReferencingTag`), not useful.
- **Cycles**: doc A links to B links back to A. Allowed (each tag is evaluated independently), but reported once per tag. No transitive staleness propagation.
- **Tag in doc matching its own frontmatter id**: same as self-reference, rejected.
- **Freshly-created target**: a tag references a doc whose frontmatter `id` has not yet been generated (e.g., newly added file with `id: ""`). The doc index skips entries without a valid id, so the tag surfaces as `OrphanedTag`. Remediation: run `autodoc fix` first to populate ids across the repo, then re-run; the tag will now resolve.
- **Bidirectional staleness**: when two docs form a cycle (A has a tag to B and B has a tag to A) and both drift, `autodoc fix` emits two `LINK STALE` reports. Remediation ordering the output should recommend: **update content from the leaf outward** — identify which doc carries the canonical change, run `autodoc fixed` on it first to update its frontmatter hash, then update the other doc's `docHash` to point at the new value, then run `autodoc fixed` on the second doc. Document this sequence in the `fix` output for cycle cases.


## `autodoc fix` Integration

`internal/commands/fix.go` orchestrates the combined pipeline. The **execution order in a single `fix` run** is:

1. **Doc discovery and id assignment** — walk the doc index; for any doc missing `id`, generate and write an 8-char hex id (existing behavior).
2. **Doc index rebuild** — reload the doc index so newly-generated ids are visible.
3. **Code tag scan** — `ScanFiles` over non-markdown source files (existing behavior).
4. **Markdown tag scan** — `ScanMarkdownDocs` over the indexed doc set produced in step 2.
5. **Freshness evaluation** — run the checker over code tags + markdown tags with the fresh doc index.

Rationale: doing id-assignment first means a single `fix` run can report accurately on markdown tags that reference docs whose ids were generated in the same run. Without this ordering, a user adding a new target doc and a tag referencing it in the same commit would see a spurious `OrphanedTag` report and need a second `fix` run.

Output verbiage remains identical. Examples:

- `LINK STALE: both code and doc changed since last sync` → still accurate when the "code" is itself a markdown file
- Consider renaming the label to `LINK STALE: both source and doc changed since last sync` to avoid misleading wording. Non-breaking label-only change.

The remediation action text for markdown-embedded tags points to the same flow: update `scopeHash` in place, optionally update `docHash` if the target doc changed, run `autodoc fixed <path>` on the source doc to update **its own** frontmatter hash after editing it.

## `autodoc fixed` Integration

`autodoc fixed <path>` currently updates only the frontmatter `hash`. With this feature, it additionally rewrites `scopeHash` values in markdown-embedded tags — but only when the tag's `docHash` is already consistent with its target doc.

### Rewrite algorithm

For a markdown file `<path>`:

1. Read the file into memory. Detect the dominant line-ending style by inspecting the first several lines: if any `\r\n` is found, remember the file is CRLF; otherwise assume LF. Split into lines on `\n` after normalizing CRLF → LF for internal processing.
2. Load the doc index (same index used by `fix`) so target doc hashes are available.
3. Scan lines for HTML-comment-wrapped tags (per the detection heuristic in "Tag Discovery").
4. For each tag found:
   a. Resolve `docId` → target doc. If missing, skip with a warning to stderr (orphaned tag; will be surfaced next time `fix` runs).
   b. If `tag.docHash != target.currentHash`, **skip rewrite** and emit a stderr warning: `skipping scope hash rewrite for <path>:<line> — docHash stale, run 'autodoc fix' first`. Rationale: rewriting `scopeHash` under a stale `docHash` would lock in local content against an out-of-date assertion of consistency with the target doc.
   c. Otherwise, compute the new `scopeHash` for this tag using the heading-depth algorithm on the **pre-rewrite** line buffer. (Because the rewrite only edits the `scopeHash` substring of the tag line, and the tag line is itself stripped by step 4 of the scope hash algorithm, the computed hash is identical whether computed before or after rewrites. Using pre-rewrite is simpler and avoids sequencing issues when a file contains multiple tags.)
   d. Replace the `scopeHash` substring inside the tag, preserving the `<!-- -->` wrapper and any leading whitespace exactly.
5. Re-join the updated line buffer using the **original line-ending style** detected in step 1, then write back to the file.
6. Call `frontmatter.UpdateHash(<path>)` so the frontmatter `hash` reflects the post-rewrite content.
7. If the search index is enabled, re-index the file (existing behavior).

Notes:

- `docHash` is **never** updated by `autodoc fixed`. Updating it is an explicit assertion "I reviewed the target doc and this doc is still consistent"; this remains a user or agent decision driven by `autodoc fix` output.
- Rewrites are deterministic and minimal — only the `scopeHash` field inside each eligible tag changes.
- Step 4b provides the answer to prior Open Question 1: skip rewrite when docHash is stale. This is now part of the contract.

### Rationale

The `scopeHash` is a local content hash the user does not need to reason about. The `docHash` is an assertion of consistency with an external doc and must remain an explicit decision.

## `autodoc stale` Integration

`autodoc stale` currently only reports frontmatter-hash staleness. It does not need changes — link staleness remains a `fix`-level concern. Document this explicitly in the `stale` help text so users do not expect markdown link staleness to appear there.

## Validation in `fix`

New validation rules, emitted alongside existing doc-metadata issues:

1. `MalformedMarkdownTag` — HTML comment wrapper present AND the inner body contains `@` AND strict regex fails. Silent for wrapper-only comments without `@` (preserves prose-safety for docs that reference the syntax shape but not a real target).
2. `SelfReferencingTag` — tag in doc `X` references `docId == X.id`.
3. `BareTagInMarkdown` — **not** emitted. Bare `[autodoc(...)]` in markdown is ignored, matching how documentation examples in the repo are written today.
4. `TagInsideFrontmatter` — tag detected within `---` / `---` frontmatter block. Rejected as invalid placement.

## Performance

- **Doc file set is bounded by the doc index**, not by a repo walk. Non-indexed markdown files (READMEs, CLAUDE.md, etc.) are not scanned. The indexed set is already enumerated for frontmatter work, so marginal cost is per-tag work on those files only.
- **Single-pass heading index per file**: for each markdown file with at least one tag, build a `[]HeadingEntry{Line, Depth}` index in one O(lines) pass that also tracks fenced-code-block state. Tag scope lookups then become O(log n) binary search over the heading index. Avoids the naive O(tags × lines) worst case for docs with many section-scoped tags.
- **Scope hash compute**: linear in scope size. The line-removal step (stripping whole-line tag comments) is a single regex per line.
- No new git operations; the markdown scan reuses the doc index already produced by existing code paths.

## Test Design

### Unit tests (`internal/linkscan`)

- Wrapped-tag regex accepts `<!-- [autodoc(...)] -->`, rejects inline variants, rejects multiple tags per comment.
- Bare `[autodoc(...)]` in `.md` does not produce `Tag` or `MalformedTag`.
- Anchor heading resolution:
  - Tag before any heading → whole-body scope.
  - Tag under H1/H2/H3 → correct scope terminators.
  - Nested deeper headings included in scope.
  - Same-depth heading terminates scope.
  - Shallower heading terminates scope.
  - EOF terminates scope.
- Fenced code blocks: headings inside fences do not act as anchors or terminators.
- Fenced code blocks: tag lines inside fences are **not** detected (covers self-documentation cases).
- Setext headings ignored (do not act as anchors); produce the next ATX heading as anchor.
- Strip behavior: tag rewrites inside a scope do not cascade-invalidate hashes of other tags in the same scope.
- `SelfReferencingTag` validation fires when applicable.
- `MalformedMarkdownTag` fires for wrapper + `@` + strict-regex fail (e.g., short hash, missing `]`); does not fire for wrapper-only examples without `@`.
- Line-ending preservation: file originally CRLF stays CRLF after `autodoc fixed` rewrite; file originally LF stays LF.
- Single `fix` run can assign an id to a new target doc and resolve a tag pointing at it (no second run required).

### E2E test (`e2e/markdown_embedded_tags_test.go`)

New test `TestE2EMarkdownEmbeddedTagsLifecycle`, modeled on `TestE2ETwoWayFreshnessLifecycle`.

Fixture tree `e2e/testdata/markdown_embedded_tags/`:

```
docs/
  reference.md       # target of the link, has id + hash
  consumer.md        # has <!-- [autodoc(...)] --> tag at top-of-doc scope
  section-scoped.md  # tag under ## with content above and below
```

Steps the test exercises end-to-end:

1. `autodoc init` → populates frontmatter ids.
2. `autodoc fix` → generates and writes missing ids, reports initial staleness for the fixture tags (which start with `00000000` placeholders).
3. `autodoc fixed docs/reference.md` → stabilizes the reference doc hash.
4. Edit fixture `consumer.md` to include the reference doc's actual id and hash (still placeholder `00000000` for `scopeHash`).
5. Run `autodoc fix` → expect `LINK STALE: ... scope` with a current `scopeHash`.
6. Capture the suggested hash, rewrite the tag, run `autodoc fix` → clean.
7. Modify `reference.md` content, run `autodoc fixed docs/reference.md`, run `autodoc fix` → expect `LINK STALE: doc updated, source tag stale` (doc hash mismatch).
8. Rewrite tag's `docHash`, rerun `autodoc fix` → clean.
9. **Whole-doc scope variants**: (a) edit `consumer.md` below `# Title` → expect scope mismatch; (b) edit `consumer.md` in the region between frontmatter and the tag line → expect clean (this content is before the tag, but since the tag precedes any heading it captures the whole body *after* the frontmatter, so pre-tag content above is outside the frontmatter and must not exist — enforce in fixture layout by keeping the tag as the first non-frontmatter line).
10. Run `autodoc fixed docs/consumer.md` → scope hash rewritten in place, frontmatter hash updated; rerun `autodoc fix` → clean.
11. **Section-scoped bounded-edit tests on `section-scoped.md`**: (a) edit content *outside* the tag's section → `fix` remains clean; (b) edit content *inside* the section → expect scope mismatch; (c) add a new `###` subheading inside the section → expect scope mismatch (content structure changed); (d) change a same-depth heading *after* the section (shifts no boundary) → remains clean; (e) insert a new same-depth heading that now terminates the section earlier → expect scope mismatch (scope boundary moved).
12. **Overlapping scopes**: add a second tag at top-of-doc in `section-scoped.md` so it has both a whole-doc and a section-scoped tag. Edit content inside the section → expect *two* `LINK STALE` reports (one per tag). Run `autodoc fixed` on the file → both `scopeHash` values update in one pass.
13. **OrphanedTag for freshly-created target**: add a tag to `consumer.md` pointing at a doc with `id: ""`. Expect `autodoc fix` to report `OrphanedTag`. Run `autodoc fix` to populate the id. Rerun `autodoc fix` → tag now resolves (becomes a docHash mismatch instead).
14. **Self-reference**: add a tag to a doc referencing its own id; expect `autodoc fix` to emit `SelfReferencingTag` and non-zero exit.
15. **Skip-rewrite-when-docHash-stale**: edit `reference.md` content without running `autodoc fixed` on it; then run `autodoc fixed docs/consumer.md`. Expect stderr warning and `scopeHash` unchanged for tags whose `docHash` is now stale. Expect frontmatter hash of `consumer.md` still updated (it reflects the un-rewritten content).

Helpers reused from `e2e/helpers_test.go`: `copyFixtureTree`, `initGitRepo`, `runCLI`, `readDoc`, `rewriteText`, `extractCurrentHashes`. Add `rewriteMarkdownTag(t, path, docId, docHash, scopeHash)` mirroring the existing `rewriteAutodocTag`.

### Regression coverage

Ensure prose examples in `auto-doc/docs/*.md` (this design, `two-way-freshness.md`, `two-way-freshness-guide.md`) are not misidentified. Add an assertion in the scanner unit tests using those real files as input and asserting zero tags found.

## File-Level Implementation Targets

Modify:

- `internal/linkscan/linkscan.go` — keep `.md` / `.markdown` in `ignoredExtensions` (preserves current bare-tag behavior); add `ScopeKind` field to `Tag`; add a new exported `ScanMarkdownDocs(index)` entry point that is called separately from `ScanFiles`.
- `internal/linkscan/linkscan_test.go` — new unit cases.
- `internal/commands/fix.go` — label adjustments (`code` → `source`), new validation kinds.
- `internal/commands/fixed.go` — scope-hash rewrite for markdown source files.
- `internal/commands/fix_test.go` — snapshot updates for new validation kinds.
- `internal/commands/stale.go` — help text only.

Add:

- `internal/linkscan/markdown.go` — heading-depth scope resolver, fenced-code-block state tracker.
- `internal/linkscan/markdown_test.go`
- `e2e/markdown_embedded_tags_test.go`
- `e2e/testdata/markdown_embedded_tags/` fixture tree.

Optional docs updates (post-implementation, separate commit):

- `auto-doc/CLAUDE.md` — append a short note under "Linking Code to Docs" describing the markdown-embedded variant and when to use it.
- `auto-doc/docs/two-way-freshness.md` — cross-reference this design.

## Deferred Follow-ups

- **BM25 index hygiene for HTML-comment tags**. Tag lines (`<!-- [autodoc(...)] -->`) currently flow through the BM25 normalization pipeline as body tokens. This introduces search noise but does not affect correctness. Deferred as a separate, narrowly-scoped improvement: add an HTML-comment stripping pass in `internal/normalize` before tokenization. Not blocking for this feature.

## Resolved Decisions

1. **`autodoc fixed` skips `scopeHash` rewrite when `docHash` is stale** — it emits a stderr warning and leaves the tag unchanged. Rationale: rewriting under a stale `docHash` would lock in local content against an out-of-date assertion of consistency. Users resolve by running `autodoc fix`, reviewing the target doc, and updating `docHash` first. (Captured in the `autodoc fixed` rewrite algorithm above.)
2. **No `--check` mode in v1**. A generic dry-run pattern may be added later across commands.
3. **`SelfReferencingTag` is an error, not a warning**. A self-referencing tag creates a circular freshness dependency that cannot stabilize (editing the doc changes the scope hash, which changes the doc hash, which changes the scope hash). The frontmatter `hash` mechanism already flags self-drift more cleanly.

