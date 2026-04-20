---
hash: "52d728ec"
id: "7c2f9e1a"
read_when: "adding document tagging and filtering functionality to autodoc"
summary: "Requirements for adding frontmatter tags and tag-based list/filter output in autodoc."
title: "Doc Tags and List Filtering Requirements"
---

# Overview

This document defines requirements for adding frontmatter `tags` support and a tag-based listing command in `autodoc`.

# Scope

- Add support for `tags` in markdown frontmatter.
- Add a new `autodoc list` command with tag filters.
- Add strict, shared validation logic reused across command flows.
- Extend `autodoc fix` to enforce tag rules and auto-dedupe duplicate tags.

# Frontmatter Requirements

- Frontmatter parsing should use a YAML parser.
- `tags` must be supported as YAML arrays in both styles:
- Inline: `tags: ["a", "b"]`
- Multiline:
  ```yaml
  tags:
    - a
    - b
  ```
- Serialization should write tags in inline style.
- `tags` is optional.
- Required frontmatter fields are `id`, `title`, `summary`, and `hash`.
- `id` and `hash` must match `^[0-9a-f]{8}$`.
- Tag values must match `^[a-z0-9]+(?:-[a-z0-9]+)*$`.
- Tags must have no duplicates.
- `tags` is metadata and must be excluded from hash/stale-content computation.

# Shared Validation

- Implement one strict shared `validate()` path reused across relevant tasks.
- Validation results should be returned as an array of error objects.
- Error object shape:
- `code`
- `path`
- `field`
- `message`
- `value` (optional)
- Required fields are validated for both presence and format.
- `autodoc list` validates hash format only (no content re-hash/staleness check).

# `autodoc list` Command Requirements

- Command: `autodoc list`
- Remove `--tags` entirely.
- Supported filter flags:
- `--tag <value>`
- `--tags-all <csv>`
- `--tags-any <csv>`
- Only one filter mode may be provided at a time.
- If no tag filter is provided, list all docs.
- Tag filter values are CSV for `--tags-all` and `--tags-any`.
- Filter values should be normalized (trim/lowercase/dedupe), matched case-insensitively, and validated against tag format rules.
- Discovery must reuse the same shared docs discovery/ignore behavior as existing doc-reading commands.

# `autodoc list` Output Requirements

- Default output is text.
- Text format is one line per doc:
- `<path> | <title> | <tag1,tag2>`
- If a doc has no tags, the third column is empty.
- Add `--json` for JSON output.
- JSON item fields:
- `path`
- `id`
- `title`
- `summary`
- `tags`

# `autodoc list` Error and Exit Behavior

- Command should print valid docs first, then report validation errors.
- Exit code is non-zero (`1`) when any validation errors are present.
- In `--json` mode:
- `stdout` must remain valid JSON data (valid docs only).
- Validation errors go to `stderr`.
- In non-JSON mode:
- Validation errors are appended after normal output in human-readable markdown format.
- Invalid CLI usage (for example multiple filter flags) should use standard Cobra argument errors.
- Validation error messaging should include remediation guidance (for example: run `autodoc fix`).

# `autodoc fix` Requirements for Tags

- Keep current behavior of scanning broadly and reporting all issues together.
- Tag validation issues should be reported as errors.
- Duplicate tags should be auto-rewritten in frontmatter by `fix`.
- Auto-dedupe should preserve original order.
- `fix` should explicitly report each file it auto-rewrote.

# Non-Goals

- No expensive content re-hash checks in `autodoc list`.
- No legacy `--tags` compatibility alias.
