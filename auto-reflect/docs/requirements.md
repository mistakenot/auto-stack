---
hash: "fdfe75f3"
id: "67c76c0e"
read_when: "when building autoreflect v1 or implementing rule persistence and retrieval"
summary: "V1 requirements for autoreflect — a tool for persisting and retrieving learned repository rules via a local playbook."
title: "Auto-Reflect V1 Requirements"
---

# Auto-Reflect V1 Requirements

## Purpose

`autoreflect` exists to persist and retrieve learned repository rules for humans and coding agents in a deterministic, low-friction way.

V1 is intentionally narrow.

It should not try to be the full long-term reflection system yet.

The first useful slice is:

- a repository-local playbook of rules
- an explicit rule creation command
- a deterministic lookup command
- JSON-first output suitable for coding agents

This gives us a clean primitive that later versions can build on with:

- richer reflection
- provenance
- feedback loops
- session history integration
- context assembly

## Position In The Stack

The tool boundaries should be:

- `auto-etl`: canonicalize raw coding session data
- `auto-search`: search raw session history
- `auto-reflect`: persist learned rules and retrieve them later

For V1:

- `autosearch` helps a human or agent discover lessons in raw history
- `autoreflect rule create` writes the distilled lesson into repository memory
- `autoreflect lookup` retrieves matching learned rules later

Future `autoreflect context` may combine learned rules and historical search results, but that is not part of V1.

## V1 Summary

V1 must provide two primary user-facing commands:

```bash
autoreflect rule create --content "<text>" --category <category> [--tag <tag> ...]
autoreflect lookup "<query>"
```

`rule create`:

- creates a repository-local rule in the playbook
- creates the playbook if it does not exist yet
- does not call an LLM
- does not search raw session history

`lookup`:

- searches the repository-local rule playbook
- returns matching rules as JSON by default
- does not call an LLM
- does not search raw session history

Together, these commands give an agent a minimal end-to-end loop:

1. use `autosearch` to find the lesson
2. use `autoreflect rule create` to persist it
3. use `autoreflect lookup` later to retrieve it

## Goals

V1 goals:

- give coding agents a simple way to persist repository-relevant rules without hand-editing JSON
- give coding agents a simple way to retrieve relevant learned rules for the current repository
- keep creation and retrieval deterministic and easy to reason about
- use strict schemas and machine-readable JSON output
- establish a playbook format that later reflection and feedback features can build on
- keep the first slice independent from the full ETL/search/feedback loop

## Non-Goals

V1 non-goals:

- automatic rule generation from transcripts
- session-history search inside `autoreflect`
- a combined `context` command
- rule update, merge, deprecate, or provenance inspection commands
- explicit feedback capture
- inline feedback comment parsing
- implicit outcome scoring
- global rule merge and multi-scope retrieval
- MCP server support
- semantic search or embedding-based ranking

Those belong in later versions. See:

- `auto-reflect/doc/v2-feedback-and-learning-loop.md`
- `auto-reflect/docs/cass-memory-system-analysis.md`

## Core Concepts

### Playbook

The playbook is the repository-local durable store of learned rules.

For V1, the playbook is:

- project-local
- JSON
- written by `rule create`
- read by `lookup`

Proposed location:

```text
.auto/reflect/playbook.json
```

This aligns with the stack-wide convention that project-local tool state lives under `.auto/<tool>/`.

### Rule

A rule is a reusable, repository-relevant instruction or heuristic.

Examples:

- "Check JWT expiry before debugging auth failures"
- "Keep passing test logs short so failing tests are easier to debug"
- "Prefer repository helper X over direct shelling out to tool Y"

V1 rules are intentionally simple and active-by-default.

Future lifecycle, feedback, and provenance fields may be added later.

## Playbook Schema

V1 requires a strict JSON schema for the playbook.

Proposed top-level shape:

```json
{
  "schema_version": 1,
  "rules": [
    {
      "id": "r-1a2b3c4d",
      "content": "Check JWT expiry before debugging auth failures",
      "category": "security",
      "tags": ["auth", "jwt"],
      "created_at": "2026-03-21T12:34:56Z",
      "updated_at": "2026-03-21T12:34:56Z"
    }
  ]
}
```

### Rule Validation Requirements

Each rule must satisfy:

- `id`: required, exact regex `^r-[0-9a-f]{8}$`
- `content`: required, trimmed non-empty string
- `category`: required, lowercase normalized identifier using regex `^[a-z0-9]+(?:-[a-z0-9]+)*$`
- `tags`: optional array of normalized tag values using regex `^[a-z0-9]+(?:-[a-z0-9]+)*$`
- `created_at`: required ISO 8601 timestamp
- `updated_at`: required ISO 8601 timestamp

Normalization rules:

- `content`, `category`, and `tags` are trimmed
- `category` and `tags` are lowercase
- duplicate tags after normalization are invalid

### Generated Fields

`rule create` should generate these fields automatically:

- `id`
- `created_at`
- `updated_at`

The user should not have to provide them.

## Playbook Loading And Validation Behavior

Both commands must use one shared validation path for the playbook schema.

### `lookup`

Behavior rules:

- if the playbook file does not exist, treat the playbook as empty
- if the playbook exists and all rules are valid, return matches normally
- if some rules are invalid, return matches from valid rules, report validation errors to `stderr`, and exit non-zero
- if the playbook file is not parseable JSON, fail with a hard error and a concrete remediation hint

This follows the stack guidance:

- JSON output remains parseable on `stdout`
- diagnostics and validation errors go to `stderr`
- valid results should still be returned when possible

### `rule create`

Behavior rules:

- if the playbook file does not exist, create parent directories as needed and initialize a new playbook
- if the playbook file exists and is valid, append one new rule
- if the playbook file is not parseable JSON, fail with a hard error and do not modify it
- if any existing rule is invalid, fail with a hard error and do not modify the playbook
- validate the proposed new rule with the same schema rules used for stored data

Exact duplicate handling:

- if an existing rule has the same normalized `content` and `category`, fail with a concrete remediation hint
- V1 does not need update or merge behavior yet

## Repository Resolution

V1 commands operate on the current project.

`lookup` resolution should be:

1. start from the current working directory
2. search upward for `.auto/reflect/playbook.json`
3. use the nearest matching file
4. if none exists, treat the playbook as empty

`rule create` resolution should be:

1. start from the current working directory
2. search upward for `.auto/reflect/playbook.json`
3. if one exists, write to the nearest matching file
4. if none exists, create `.auto/reflect/playbook.json` under the current working directory

V1 does not need:

- a global playbook
- merged global and repo scope
- `--scope repo|global|all`

Those are later-stage features.

## User-Facing API

## `autoreflect rule create`

```bash
autoreflect rule create --content "<text>" --category <category> [--tag <tag> ...] [--format json|text]
```

### Required flags

- `--content <text>`: the rule text
- `--category <category>`: normalized category identifier

### Optional flags

- `--tag <tag>`: repeatable tag flag
- `--format json|text`: output format

Defaults:

- output format defaults to `json`

### Create Semantics

`rule create` must:

- trim `content`
- normalize `category` and `tags` to lowercase
- preserve input tag order
- reject duplicate tags after normalization
- generate `id`, `created_at`, and `updated_at`
- persist the rule into the repository playbook

### JSON Response Shape

Recommended response shape:

```json
{
  "created": true,
  "scope": "repo",
  "path": ".auto/reflect/playbook.json",
  "rule": {
    "id": "r-1a2b3c4d",
    "content": "Keep passing test logs short so failing E2E tests are easier to debug",
    "category": "testing",
    "tags": ["e2e", "logs", "flaky"],
    "created_at": "2026-03-21T12:34:56Z",
    "updated_at": "2026-03-21T12:34:56Z"
  }
}
```

Required top-level fields:

- `created`
- `scope`
- `path`
- `rule`

### Text Mode

Text mode should:

- print the created rule id
- print the rule content and category
- mention the path written
- print readable errors or remediation hints after successful output if needed

## `autoreflect lookup`

```bash
autoreflect lookup "<query>" [--limit <n>] [--format json|text]
```

### Required argument

- `<query>`: a non-empty string

### Optional flags

- `--limit <n>`: maximum number of rules to return
- `--format json|text`: output format

Defaults:

- output format defaults to `json`
- `limit` defaults to a small useful value, recommended: `10`

### No aliases

Use explicit surfaces:

- `rule create`
- `lookup`

Do not add ambiguous aliases in V1.

## Query Semantics

V1 query semantics are intentionally simple.

The query is treated as a keyword query, not as a boolean or semantic search language.

Normalization rules:

- trim leading and trailing whitespace
- lowercase
- split into keywords
- dedupe repeated keywords

Example:

```text
" Auth   JWT jwt rate limit "
```

normalizes to:

```json
["auth", "jwt", "rate", "limit"]
```

V1 does not need:

- boolean operators
- phrase search
- wildcard syntax
- semantic embedding search

## Matching Semantics

`lookup` must match query keywords against:

- rule `content`
- rule `tags`
- rule `category`

Matching is:

- case-insensitive
- deterministic
- keyword-based

V1 ranking should be simple and explainable:

- higher keyword overlap ranks higher
- content matches should matter more than tag or category matches
- ties must be broken deterministically, recommended: by `id` ascending

Future versions may add confidence or helpfulness boosts, but V1 should not depend on feedback-derived scoring.

## Output Requirements

### `lookup` JSON mode

JSON mode is the default.

`stdout` must contain parseable JSON only.

Recommended response shape:

```json
{
  "query": "auth rate limit jwt",
  "keywords": ["auth", "rate", "limit", "jwt"],
  "scope": "repo",
  "rules": [
    {
      "id": "r-1a2b3c4d",
      "content": "Check JWT expiry before debugging auth failures",
      "category": "security",
      "tags": ["auth", "jwt"],
      "match_score": 0.82
    }
  ]
}
```

Required top-level fields:

- `query`
- `keywords`
- `scope`
- `rules`

Required rule fields in the response:

- `id`
- `content`
- `category`
- `tags`
- `match_score`

### `lookup` text mode

Text mode should:

- print matching rules in rank order
- include rule id and category
- keep the output short and easy to scan
- print successful results first
- print warnings or remediation hints after the results

## Empty Results Behavior

If no rules match:

- return success
- return an empty `rules` array in JSON mode
- do not treat this as an error

Example:

```json
{
  "query": "playwright mcp flaky",
  "keywords": ["playwright", "mcp", "flaky"],
  "scope": "repo",
  "rules": []
}
```

## Error Behavior

Invalid CLI usage should fail fast.

Examples:

- missing query
- missing `--content`
- missing `--category`
- invalid `--limit`
- invalid `--format`
- invalid tag format

In JSON mode:

- `stdout` remains parseable JSON only if the command succeeds
- errors go to `stderr`

All hard errors should include a concrete remediation hint.

Examples:

- playbook parse error: "Fix `.auto/reflect/playbook.json` or recreate it"
- duplicate rule: "Run `autoreflect lookup` first or wait for a future rule update command"
- invalid limit: "Use `--limit <n>` where `n >= 1`"

## Examples

### Create a rule

```bash
autoreflect rule create \
  --content "Keep passing test logs short so failing E2E tests are easier to debug" \
  --category testing \
  --tag e2e \
  --tag logs \
  --tag flaky
```

### Basic lookup

```bash
autoreflect lookup "auth rate limit jwt"
```

### Limited results

```bash
autoreflect lookup "e2e flaky logs" --limit 5
```

### Text mode

```bash
autoreflect lookup "jwt auth" --format text
```

### Small dogfood loop

```bash
autosearch search "npm run e2e" --scope sessions --since 2w
autosearch session get $sessionId
autoreflect lookup "e2e flaky logs"
autoreflect rule create \
  --content "Keep passing test logs short so failing E2E tests are easier to debug" \
  --category testing \
  --tag e2e \
  --tag logs \
  --tag flaky
autoreflect lookup "e2e flaky logs"
```

## Relationship To Future Commands

V1 provides two low-level primitives:

- `autoreflect rule create`
- `autoreflect lookup`

Later commands may build on top of them:

- `autoreflect context "<task>"`
- `autoreflect feedback <rule-id> --helpful|--harmful`
- `autoreflect rule update <rule-id> ...`
- reflection and playbook maintenance commands

The intended conceptual split is:

- `autosearch`: raw history
- `autoreflect rule create`: persist learned rules
- `autoreflect lookup`: retrieve learned rules
- later `autoreflect context`: assembled guidance for a task

## Implementation Guidance

V1 should stay deterministic and cheap:

- no LLM calls
- no ETL or search dependency in the hot path of `rule create`
- no ETL or search dependency in the hot path of `lookup`
- no background jobs required

It is acceptable for V1 rule creation to be driven by a human or agent that previously used `autosearch` to inspect history.

The important thing is that the API and file format are stable enough for later reflection features to target.

## Out Of Scope But Important

The following are intentionally deferred:

- automatic transcript-to-rule reflection inside `autoreflect`
- explicit feedback capture
- inline feedback markers
- implicit outcome scoring
- rule provenance and trust signals
- repository plus global merged retrieval
- similarity search and semantic ranking
- MCP integration

These are documented separately in:

- `auto-reflect/doc/v2-feedback-and-learning-loop.md`

## Recommendation

Ship V1 as the smallest end-to-end slice that is still genuinely useful:

- a project-local playbook
- a strict JSON schema
- one explicit `rule create` command
- one deterministic `lookup` command
- JSON-first agent output

Do not overbuild the learning loop in the first implementation.

The important thing is to make the loop complete:

1. an agent can discover a lesson using `autosearch`
2. an agent can persist a rule using `autoreflect`
3. an agent can retrieve that rule later using `autoreflect`

Feedback, provenance, and richer reflection should layer on top of that clean base.
