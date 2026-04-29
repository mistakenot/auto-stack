---
hash: "a4defc73"
id: "fb-rule-synth-v1"
read_when: "designing autoreflect v1 to combine rule memory with feedback capture"
summary: "Synthesis design for autoreflect v1: preserve rule create/lookup while adding append-only feedback events for helpful/harmful/missing signals."
title: "Auto-Reflect V1 Synthesis: Rule Memory + Feedback Events"
---

# Auto-Reflect V1 Synthesis: Rule Memory + Feedback Events

## Requirements (Normative)

1. Keep existing repository rule memory primitives:
   - `autoreflect rule create --content ... --category ... [--tag ...]`
   - `autoreflect lookup "<query>"`
2. Add feedback capture as append-only events, not direct rule mutation:
   - canonical write surface: `autoreflect feedback add`
   - canonical read surface: `autoreflect feedback list`
3. Do not add wrapper write aliases (`good`, `bad`, `missing`) in v1.
4. Default output mode is JSON; text mode is opt-in.
5. Keep `stdout` parseable JSON payloads only on success; diagnostics/errors go to `stderr`.
6. Store project-local state under `.auto/reflect/`.
7. Feedback storage is append-only JSONL for auditability and replay.
8. Preserve provenance on every feedback event:
   - timestamp
   - git commit hash at capture time
   - git tree hash at capture time
   - normalized `git_remote` as the primary project identity
   - optional `workspace_name` (folder basename) as convenience metadata
   - optional `workspace_path` as local debug metadata only (non-canonical)
   - optional session/agent metadata when available
9. File-targeted feedback should use repo-relative paths.
10. Line ranges are optional; when present, the command must read the target file and auto-capture `content_snippet` from `start..end` at annotation time.
11. No LLM calls in v1 command execution paths.
12. Do not auto-promote single feedback events into durable playbook rules.
13. Span validation must fail fast with remediation hints:
   - `--file` is required when `--start` or `--end` is provided
   - `start_line >= 1`
   - `end_line >= start_line`
   - `end_line` must not exceed file length
14. File-span events must include content-addressed anchoring so annotations remain replayable after later edits:
   - line span (`start_line`, `end_line`) and byte span (`start_byte`, `end_byte`)
   - captured `content_snippet` plus `snippet_sha256`
   - `head_blob_sha` for the file at `HEAD` when available
   - `observed_blob_sha` for the bytes actually observed at capture time
   - `capture_source` set to `head` or `working_tree`
   - `worktree_dirty` boolean

## API Usage Examples

### Rule memory (existing v1)

```bash
autoreflect rule create \
  --content "Keep passing test logs short so failing E2E tests are easier to debug" \
  --category testing \
  --tag e2e \
  --tag logs \
  --tag flaky

autoreflect lookup "e2e flaky logs"
```

### Canonical feedback API (recommended)

```bash
# File span marked helpful
autoreflect feedback add \
  --kind helpful \
  --file docs/auth.md \
  --start 10 \
  --end 13 \
  --comment "clear boundary example prevented a wrong middleware rewrite"

# File span marked harmful
autoreflect feedback add \
  --kind harmful \
  --file docs/setup.md \
  --start 4 \
  --end 6 \
  --comment "outdated instructions caused repeated install failures"

# Missing context feedback (no file required)
autoreflect feedback add \
  --kind missing \
  --comment "no docs for middleware chaining order; had to inspect 4 files"

# If start/end are provided, snippet is auto-read from file at capture time.
# No manual snippet flag is required in v1.
autoreflect feedback add \
  --kind helpful \
  --file docs/user-journey.md \
  --start 415 \
  --end 420 \
  --comment "small dogfood loop section gave the exact implementation sequence"

# Project identity metadata is auto-captured from repo state:
# - git_remote (normalized) as canonical cross-machine identity
# - workspace_name as convenience
# - workspace_path optional, local-debug only
```

### Query feedback

```bash
# all recent feedback
autoreflect feedback list

# filter by kind
autoreflect feedback list --kind harmful

# filter by file path
autoreflect feedback list --file docs/auth.md

# filter by time window
autoreflect feedback list --since 7d
autoreflect feedback list --after 2026-04-01 --before 2026-04-29

# combined filters
autoreflect feedback list --kind helpful --file docs/ --since 30d --limit 100
```

## Saved File Format Examples

### 1) Rule playbook (JSON)
Path: `.auto/reflect/playbook.json`

```json
{
  "schema_version": 1,
  "rules": [
    {
      "id": "r-1a2b3c4d",
      "content": "Keep passing test logs short so failing E2E tests are easier to debug",
      "category": "testing",
      "tags": ["e2e", "logs", "flaky"],
      "created_at": "2026-04-29T14:22:00Z",
      "updated_at": "2026-04-29T14:22:00Z"
    }
  ]
}
```

### 2) Feedback event log (JSONL)
Path: `.auto/reflect/feedback.jsonl`

One JSON object per line:

```json
{"id":"f-7f3e2c10","kind":"helpful","comment":"clear boundary example prevented a wrong middleware rewrite","timestamp":"2026-04-29T14:25:01Z","git_hash":"a1b2c3d4e5f6a7b8c9d0e1f234567890abcdef12","git_remote":"github.com/example/auto-stack","workspace_name":"auto-stack","workspace_path":"/home/vscode/src/auto-stack","session_id":null,"agent":null,"subject":{"type":"file_span","file":"docs/auth.md","start_line":10,"end_line":13,"content_snippet":"Middleware order: auth runs before rate limiting..."}}
{"id":"f-e23a91bd","kind":"harmful","comment":"outdated instructions caused repeated install failures","timestamp":"2026-04-29T14:28:42Z","git_hash":"a1b2c3d4e5f6a7b8c9d0e1f234567890abcdef12","git_remote":"github.com/example/auto-stack","workspace_name":"auto-stack","workspace_path":null,"session_id":null,"agent":null,"subject":{"type":"file_span","file":"docs/setup.md","start_line":4,"end_line":6,"content_snippet":"Install tool X with deprecated flag --legacy-mode"}}
{"id":"f-51da9a6c","kind":"missing","comment":"no docs for middleware chaining order; had to inspect 4 files","timestamp":"2026-04-29T14:31:10Z","git_hash":"a1b2c3d4e5f6a7b8c9d0e1f234567890abcdef12","git_remote":"github.com/example/auto-stack","workspace_name":"auto-stack","workspace_path":null,"session_id":null,"agent":null,"subject":{"type":"missing_context","file":null}}
```

## V1 Implementation Plan (Feedback Capture Only)

This implementation plan is intentionally scoped to feedback event capture and retrieval.

`rule create` and `lookup` remain in the product direction, but are out of scope for this build slice.

### API Surface To Implement

```bash
autoreflect feedback add \
  --kind helpful|harmful|missing \
  [--file <repo-relative-path>] \
  [--start <line>] \
  [--end <line>] \
  --comment "<text>" \
  [--context "<1-2 sentence workflow intent>"] \
  [--format json|text]

autoreflect feedback list \
  [--kind helpful|harmful|missing] \
  [--file <substring>] \
  [--since <duration>] \
  [--after <iso-date> --before <iso-date>] \
  [--limit <n>] \
  [--format json|text]
```

Behavior notes:
- `--kind` is required for `feedback add`.
- `--comment` is required, trimmed, non-empty.
- `--context` is optional but recommended for `harmful` and `missing`.
- `--file` is required when either `--start` or `--end` is provided.
- when a span is provided, snippet + span/provenance fields are auto-captured per requirements above.
- `--format` defaults to `json` for both `feedback add` and `feedback list`.

### Data Files In Scope

- `.auto/reflect/feedback.jsonl` (append-only event log)
- no mutation of `.auto/reflect/playbook.json` in this slice

### Implementation Steps

1. Add feedback domain package:
   - event types/schema
   - validation + normalization
   - span extraction and snippet hashing
   - git provenance capture (`git_hash`, `git_tree_sha`, `head_blob_sha`, `observed_blob_sha`, `capture_source`, `worktree_dirty`)
2. Add JSONL storage package:
   - append event (atomic write path for one line)
   - stream/list with filters (`kind`, `file`, time window, limit)
3. Add CLI commands:
   - `feedback add`
   - `feedback list`
   - JSON and text formatters
4. Wire command registration in root CLI and help text.
5. Add test fixtures/helpers and complete unit + integration + e2e coverage.

### Test Plan

Follow established module patterns:
- `internal/cli/cli_integration_test.go` style used in `auto-search` and `auto-skill`.
- binary-level e2e style used in `auto-etl/e2e_test.go` (build once in `TestMain`, run black-box commands).

#### Unit tests

Target packages and cases:
- `internal/feedback/validate_test.go`
  - required fields and enum checks
  - span boundary rules (`start >= 1`, `end >= start`, end within file)
  - required `--file` when span flags are used
- `internal/feedback/span_test.go`
  - line/byte span extraction
  - snippet capture correctness
  - `snippet_sha256` determinism
- `internal/feedback/provenance_test.go`
  - git metadata capture behavior for clean vs dirty worktrees
  - `capture_source` selection and blob-sha fallbacks
- `internal/feedback/filter_test.go`
  - `since` parsing
  - `after/before` range handling
  - combined filter semantics
- `internal/store/jsonl_test.go`
  - append/read roundtrip
  - invalid JSONL line handling behavior
  - ordering and limit behavior

#### CLI integration tests

In `internal/cli/cli_integration_test.go`:
- `feedback add` success with `helpful` file span writes one JSONL event
- `feedback add` success with `missing` and no file/span
- `feedback add` failure cases (invalid kind, empty comment, invalid span)
- `feedback list` default JSON output shape and newest-first order
- `feedback list` filters by kind/file/time/limit
- text output mode checks
- JSON mode keeps stdout parseable and writes errors to stderr

Use temp directories and temp git repos per test fixture; no shared state between tests.

#### End-to-end tests

Add black-box tests under `cmd/autoreflect/`:
- `e2e_feedback_test.go`
- `e2e_helpers_test.go`

E2E scenarios:
- build binary once, run `feedback add` then `feedback list` in a temp repo
- verify on-disk `.auto/reflect/feedback.jsonl` format and required fields
- verify provenance fields are present and sensible in clean and dirty repo states
- verify span capture reads the actual file contents
- verify non-zero exit and remediation hints for invalid spans

### Acceptance Criteria

- `feedback add` and `feedback list` are fully functional in JSON mode by default.
- all required event fields and provenance fields are present in saved records.
- unit tests pass for validation, span extraction, provenance, and storage behavior.
- CLI integration tests pass for command behavior and output contracts.
- e2e tests pass for real binary execution against temp git repositories.
