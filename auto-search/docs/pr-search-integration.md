---
hash: "00000000"
id: "a3f7e291"
summary: "Design for indexing GitHub PR feedback in autosearch: schema, FTS strategy, query access patterns, CLI surface, and reflection-agent workflows."
title: "PR Search Integration"
---

# PR Search Integration

Design for indexing GitHub PR feedback (from `autoetl`) in `autosearch`, enabling full-text search across pull requests and review comments alongside existing session/message search.

Read alongside:
- [autosearch v1 solution](./solution.md) — existing indexing and search architecture
- [GitHub PR Feedback ETL](../../auto-etl/docs/github-pr-etl.md) — upstream parquet schema and sync pipeline
- [autoetl model](../../auto-etl/internal/model/github.go) — canonical `PullRequest` and `PRComment` structs

## 1. Motivation

PR review feedback is the highest-signal source of human judgment on agent-produced code. A reviewer writing "don't mock the database here" or "this abstraction is premature" is exactly the kind of input that a self-reflecting agent should learn from.

Today `autosearch` indexes coding sessions and messages. Adding PR data enables:

- **Feedback mining**: surface recurring review themes across PRs
- **Positive reinforcement**: find approved PRs and patterns that reviewers liked
- **Code-context retrieval**: search review comments with their associated diff hunks and file paths
- **Cross-domain correlation**: connect a review comment to the session that produced the code
- **Reflection automation**: `autoreflect` can query both "what did the agent do" (sessions) and "what did reviewers think" (PRs) in one tool

## 2. Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Table strategy | First-class `pull_requests` + `pr_comments` tables | PR data has unique fields (diff hunks, review state, file paths) that don't fit session/message schema |
| FTS targets | PR: `title`, `body`. Comment: `body`, `diff_hunk` | High-signal text fields. Full diff is too noisy for FTS — store for retrieval only |
| Diff indexing | Per-file patches from `files_json`, not monolithic `diff` | File-level granularity, bounded size, matches how reviewers think about changes |
| Cross-scope search | `--scope all` unions results from all FTS tables | The killer feature: one query finds the PR, the review comment, and the session |
| JSON field handling | Denormalize `labels_json` and `reviewers_json` at index time | Enables column-level filtering without JSON parsing at query time |
| Score normalization | Per-table min-max normalization for `--scope all` | BM25 scores from different FTS tables aren't directly comparable |
| Graceful degradation | PR tables are optional in the index | If `autoetl` hasn't run `--only github`, PR tables are empty — search still works for sessions/messages |
| Backward compatibility | Schema version bump triggers full rebuild | Existing v1 indexes rebuild automatically when PR tables are added |

## 3. Input: Parquet Schema

`autosearch` reads two new parquet datasets from `~/.auto/etl/output/`:

```
pull_requests/year=YYYY/month=MM/pull_requests.parquet
pr_comments/year=YYYY/month=MM/pr_comments.parquet
```

### PullRequest fields (from `auto-etl/internal/model/github.go`)

| Field | Type | Use in autosearch |
|-------|------|-------------------|
| `id` | string | `"{owner}/{repo}#{number}"` — primary key |
| `owner` | string | Filter, denormalize |
| `repo` | string | Filter, denormalize |
| `number` | int32 | Display |
| `title` | string | **FTS indexed** |
| `body` | string | **FTS indexed** |
| `state` | string | Always `"merged"` — no filter needed |
| `draft` | bool | Filter (exclude drafts from reflection if desired) |
| `base_branch` | string | Filter |
| `head_branch` | string | Filter |
| `base_sha`, `head_sha`, `merge_commit_sha` | string | Retrieval, cross-reference |
| `author_login` | string | Filter |
| `author_display_name` | string | Display |
| `reviewers_json` | string | **Denormalize at index time** → `reviewers` text column for FTS, structured for filter |
| `labels_json` | string | **Denormalize at index time** → `labels` text column for filter |
| `checks_json` | string | Store as-is, query via retrieval only |
| `diff` | string | **Store for retrieval only** — too large/noisy for FTS |
| `files_json` | string | **Parse at index time** → index per-file patches in `pr_file_patches` FTS |
| `additions`, `deletions`, `changed_files` | int32 | Metadata, stats |
| `comment_count`, `commit_count` | int32 | Metadata, stats |
| `created_at`, `updated_at`, `closed_at`, `merged_at` | int64 | Time filters (use `merged_at`) |
| `git_remote` | string | Filter (shared with session/message) |
| `host_id` | string | Filter |
| `year`, `month`, `schema_version` | int32 | Partitioning, incremental indexing |

### PRComment fields

| Field | Type | Use in autosearch |
|-------|------|-------------------|
| `id` | string | `"{owner}/{repo}#{pr_number}/c/{comment_id}"` — primary key |
| `pr_id` | string | FK to `pull_requests.id` |
| `comment_id` | int64 | GitHub numeric ID |
| `in_reply_to_id` | int64 | Thread traversal (0 = top-level) |
| `comment_type` | string | **Filter**: `"review"`, `"review_comment"`, `"issue_comment"` |
| `body` | string | **FTS indexed** |
| `author_login` | string | Filter |
| `author_display_name` | string | Display |
| `author_association` | string | **Filter**: `MEMBER`, `CONTRIBUTOR`, etc. |
| `path` | string | **Filter + FTS indexed** — file path for code comments |
| `diff_hunk` | string | **FTS indexed** — code context around the comment |
| `commit_sha` | string | Cross-reference |
| `original_line`, `line`, `side` | int32/string | Display, retrieval |
| `start_line`, `start_side` | int32/string | Multi-line comment ranges |
| `review_id` | int64 | Group comments by review |
| `review_state` | string | **Filter**: `APPROVED`, `CHANGES_REQUESTED`, `COMMENTED`, `DISMISSED` |
| `created_at`, `updated_at` | int64 | Time filters |
| `owner`, `repo`, `pr_number` | string/int32 | Denormalized from PR |
| `git_remote` | string | Filter |
| `host_id` | string | Filter |
| `year`, `month`, `schema_version` | int32 | Partitioning |

## 4. SQLite Schema Additions

New tables added alongside existing `sessions`, `messages`, and their FTS counterparts. Schema version bumps to trigger a full rebuild for existing indexes.

```sql
-- Pull requests: one row per merged PR
CREATE TABLE pull_requests (
  doc_id INTEGER PRIMARY KEY,
  partition_source_path TEXT NOT NULL,
  pr_id TEXT NOT NULL UNIQUE,          -- "owner/repo#number"
  owner TEXT NOT NULL,
  repo TEXT NOT NULL,
  number INTEGER NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  draft INTEGER NOT NULL,
  base_branch TEXT NOT NULL,
  head_branch TEXT NOT NULL,
  base_sha TEXT NOT NULL,
  head_sha TEXT NOT NULL,
  merge_commit_sha TEXT NOT NULL,
  author_login TEXT NOT NULL,
  author_display_name TEXT NOT NULL,
  reviewers TEXT NOT NULL,             -- denormalized: "login1 login2 login3"
  labels TEXT NOT NULL,                -- denormalized: "label1 label2 label3"
  checks_json TEXT NOT NULL,           -- raw JSON, retrieval only
  diff TEXT NOT NULL,                  -- full unified diff, retrieval only
  files_json TEXT NOT NULL,            -- raw JSON, retrieval only
  additions INTEGER NOT NULL,
  deletions INTEGER NOT NULL,
  changed_files INTEGER NOT NULL,
  comment_count INTEGER NOT NULL,
  commit_count INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  closed_at INTEGER NOT NULL,
  merged_at INTEGER NOT NULL,          -- primary time filter
  git_remote TEXT NOT NULL,
  host_id TEXT NOT NULL,
  schema_version INTEGER NOT NULL
);

-- PR comments: one row per comment (review, review_comment, issue_comment)
CREATE TABLE pr_comments (
  doc_id INTEGER PRIMARY KEY,
  partition_source_path TEXT NOT NULL,
  comment_id TEXT NOT NULL UNIQUE,     -- "owner/repo#pr_number/c/github_id"
  pr_id TEXT NOT NULL,                 -- FK to pull_requests.pr_id
  github_comment_id INTEGER NOT NULL,
  in_reply_to_id INTEGER NOT NULL,     -- 0 = top-level
  comment_type TEXT NOT NULL,          -- "review", "review_comment", "issue_comment"
  body TEXT NOT NULL,
  author_login TEXT NOT NULL,
  author_display_name TEXT NOT NULL,
  author_association TEXT NOT NULL,
  path TEXT NOT NULL,                  -- file path (empty for non-code comments)
  diff_hunk TEXT NOT NULL,
  commit_sha TEXT NOT NULL,
  original_line INTEGER NOT NULL,
  line INTEGER NOT NULL,
  side TEXT NOT NULL,
  start_line INTEGER NOT NULL,
  start_side TEXT NOT NULL,
  review_id INTEGER NOT NULL,
  review_state TEXT NOT NULL,          -- APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  owner TEXT NOT NULL,
  repo TEXT NOT NULL,
  pr_number INTEGER NOT NULL,
  git_remote TEXT NOT NULL,
  host_id TEXT NOT NULL,
  schema_version INTEGER NOT NULL
);

-- Indexes for column filters
CREATE INDEX idx_pr_pr_id ON pull_requests(pr_id);
CREATE INDEX idx_pr_owner_repo ON pull_requests(owner, repo);
CREATE INDEX idx_pr_author_login ON pull_requests(author_login);
CREATE INDEX idx_pr_git_remote ON pull_requests(git_remote);
CREATE INDEX idx_pr_merged_at ON pull_requests(merged_at);

CREATE INDEX idx_prc_comment_id ON pr_comments(comment_id);
CREATE INDEX idx_prc_pr_id ON pr_comments(pr_id);
CREATE INDEX idx_prc_comment_type ON pr_comments(comment_type);
CREATE INDEX idx_prc_review_state ON pr_comments(review_state);
CREATE INDEX idx_prc_path ON pr_comments(path);
CREATE INDEX idx_prc_author_login ON pr_comments(author_login);
CREATE INDEX idx_prc_git_remote ON pr_comments(git_remote);
CREATE INDEX idx_prc_created_at ON pr_comments(created_at);
CREATE INDEX idx_prc_in_reply_to ON pr_comments(in_reply_to_id);

-- FTS5: PR title + body
CREATE VIRTUAL TABLE pull_requests_fts USING fts5(
  title,
  body,
  reviewers,
  labels,
  content='pull_requests',
  content_rowid='doc_id',
  tokenize='unicode61'
);

-- FTS5: comment body + diff hunk + file path
CREATE VIRTUAL TABLE pr_comments_fts USING fts5(
  body,
  diff_hunk,
  path,
  content='pr_comments',
  content_rowid='doc_id',
  tokenize='unicode61'
);
```

Auto-sync triggers (same pattern as existing `sessions_fts`/`messages_fts`):

```sql
CREATE TRIGGER pull_requests_ai AFTER INSERT ON pull_requests BEGIN
  INSERT INTO pull_requests_fts(rowid, title, body, reviewers, labels)
  VALUES (new.doc_id, new.title, new.body, new.reviewers, new.labels);
END;

CREATE TRIGGER pull_requests_ad AFTER DELETE ON pull_requests BEGIN
  INSERT INTO pull_requests_fts(pull_requests_fts, rowid, title, body, reviewers, labels)
  VALUES ('delete', old.doc_id, old.title, old.body, old.reviewers, old.labels);
END;

CREATE TRIGGER pr_comments_ai AFTER INSERT ON pr_comments BEGIN
  INSERT INTO pr_comments_fts(rowid, body, diff_hunk, path)
  VALUES (new.doc_id, new.body, new.diff_hunk, new.path);
END;

CREATE TRIGGER pr_comments_ad AFTER DELETE ON pr_comments BEGIN
  INSERT INTO pr_comments_fts(pr_comments_fts, rowid, body, diff_hunk, path)
  VALUES ('delete', old.doc_id, old.body, old.diff_hunk, old.path);
END;
```

## 5. Indexing

### 5.1 Discovery

Extend `etlscan/discover.go` to walk two additional directories:

```
~/.auto/etl/output/pull_requests/**/*.parquet
~/.auto/etl/output/pr_comments/**/*.parquet
```

Each discovered file gets the same `(dataset, partition_key, source_path, size, mtime)` treatment as messages/sessions.

### 5.2 Parquet Read Model

New structs in `auto-search/internal/model/`:

```go
// parquet_pr.go
type ParquetPullRequestRow struct { /* mirrors auto-etl PullRequest */ }
type ParquetPRCommentRow struct { /* mirrors auto-etl PRComment */ }
```

Same approach as existing `ParquetSessionRow`/`ParquetMessageRow` — mirror the upstream schema, normalize missing values to empty strings.

### 5.3 Index-Time Denormalization

Before inserting into SQLite, transform these JSON fields:

- `reviewers_json` → `reviewers`: space-separated login names (e.g. `"alice bob charlie"`). This makes reviewer names searchable via FTS and filterable via `LIKE`.
- `labels_json` → `labels`: space-separated label names (e.g. `"bug security p0"`). Same rationale.
- `files_json` → not denormalized into separate rows for v1. The `path` field on `pr_comments` already gives file-level search for code review comments. Full file-patch indexing is deferred.

### 5.4 Incremental Policy

Same rules as sessions/messages:

- Always reindex the newest `pull_requests` partition and newest `pr_comments` partition
- Reindex older partitions when source metadata changes
- Full rebuild on schema version bump

This aligns with the ETL's dedup semantics — PRs and comments can be updated on re-sync (failed retries, new comments), so the newest partition is always dirty.

### 5.5 Graceful Absence

If `pull_requests/` or `pr_comments/` directories don't exist under the input path, indexing skips them silently. The tables are created in the schema regardless (empty tables are fine). This means `autosearch index` works the same whether or not `autoetl run --only github` has ever been run.

## 6. Search

### 6.1 New Scopes

Add two new search scopes alongside `messages` and `sessions`:

| Scope | FTS Table | Time Filter Column | Description |
|-------|-----------|-------------------|-------------|
| `pr` | `pull_requests_fts` | `merged_at` | Search PR titles and bodies |
| `pr_comments` | `pr_comments_fts` | `created_at` | Search review comments and code feedback |

### 6.2 Scope-Specific Filters

**`--scope pr`**

| Flag | Column | Notes |
|------|--------|-------|
| `--remote` | `git_remote` | Shared with session/message scopes |
| `--author` | `author_login` | PR author |
| `--label` | `labels` | Substring match on denormalized label string |
| `--since`, `--after`, `--before` | `merged_at` | Same date semantics as other scopes |

**`--scope pr_comments`**

| Flag | Column | Notes |
|------|--------|-------|
| `--remote` | `git_remote` | Shared |
| `--review-state` | `review_state` | `APPROVED`, `CHANGES_REQUESTED`, `COMMENTED`, `DISMISSED` |
| `--comment-type` | `comment_type` | `review`, `review_comment`, `issue_comment` |
| `--path` | `path` | File path prefix match |
| `--author` | `author_login` | Comment author |
| `--since`, `--after`, `--before` | `created_at` | Same date semantics |

### 6.3 Cross-Scope Search (`--scope all`)

`--scope all` searches all four FTS tables and returns a unified result list.

**Query execution**: Run the compiled FTS query against each table independently, collect top 50 per table.

**Score normalization**: BM25 scores from different FTS tables have different distributions. Normalize per-table using min-max scaling to [0, 1], then merge and sort.

**Result envelope**:

```json
{
  "_meta": {
    "request_id": "...",
    "scope": "all",
    "mode": "bm25",
    "query": "auth middleware",
    "elapsed_ms": 18,
    "total_hits": 12
  },
  "hits": [
    {
      "type": "pr_comment",
      "id": "stable-hash",
      "prId": "mistakenot/auto-stack#1",
      "commentId": "mistakenot/auto-stack#1/c/12345",
      "reviewState": "CHANGES_REQUESTED",
      "score": 0.92,
      "snippet": "don't mock the **auth middleware** in these tests",
      "snippetStartIndex": 12,
      "snippetEndIndex": 58
    },
    {
      "type": "message",
      "id": "stable-hash",
      "sessionId": "abc123",
      "messageId": "abc123-42",
      "messageType": "assistant",
      "score": 0.87,
      "snippet": "implementing **auth middleware** protection",
      "snippetStartIndex": 40,
      "snippetEndIndex": 80
    },
    {
      "type": "pull_request",
      "id": "stable-hash",
      "prId": "mistakenot/auto-stack#2",
      "score": 0.71,
      "snippet": "Add **auth middleware** to API routes",
      "snippetStartIndex": 0,
      "snippetEndIndex": 38
    }
  ]
}
```

Each hit includes a `type` discriminator so consumers can handle the different shapes.

**Deferred**: `--scope all` does not support scope-specific filters like `--review-state` or `--comment-type`. These only work with their matching scope. `--scope all` supports only the shared filters: `--remote`, `--since`, `--after`, `--before`.

### 6.4 PR-Scope Hit Shape

```json
{
  "type": "pull_request",
  "id": "stable-hash",
  "prId": "mistakenot/auto-stack#42",
  "score": 1.8,
  "snippet": "matched text in title or body",
  "snippetStartIndex": 10,
  "snippetEndIndex": 45,
  "owner": "mistakenot",
  "repo": "auto-stack",
  "number": 42,
  "authorLogin": "agent-bot",
  "mergedAt": 1712678400000,
  "additions": 150,
  "deletions": 20,
  "commentCount": 5
}
```

### 6.5 PR Comment Hit Shape

```json
{
  "type": "pr_comment",
  "id": "stable-hash",
  "prId": "mistakenot/auto-stack#42",
  "commentId": "mistakenot/auto-stack#42/c/12345",
  "commentType": "review_comment",
  "reviewState": "CHANGES_REQUESTED",
  "score": 2.1,
  "snippet": "matched text in comment body or diff hunk",
  "snippetStartIndex": 0,
  "snippetEndIndex": 42,
  "authorLogin": "reviewer",
  "path": "internal/auth/middleware.go",
  "createdAt": 1712678400000
}
```

## 7. Helper Commands

### 7.1 `autosearch pr get <pr_id>`

Renders a full PR view. Output is markdown-like text (same approach as `session get`).

```
# mistakenot/auto-stack#42 — Add auth middleware to API routes

**Author:** agent-bot
**Merged:** 2026-04-09T14:09:06Z
**Branch:** feature/auth-middleware → main
**Reviewers:** alice (APPROVED), bob (CHANGES_REQUESTED, APPROVED)
**Labels:** security, api
**Files changed:** 6 (+150 -20)

## Body

<pr body text>

## Reviews

<review state="CHANGES_REQUESTED" author="bob" at="2026-04-09T12:00:00Z">
  <comment path="internal/auth/middleware.go" line="42">
    Don't mock the database here — use the test helper.

    ```diff
    <diff_hunk>
    ```
  </comment>
  <comment path="internal/auth/middleware.go" line="78" in_reply_to="previous">
    Agreed, fixed in the next push.
  </comment>
</review>

<review state="APPROVED" author="alice" at="2026-04-09T13:30:00Z">
  LGTM, clean implementation.
</review>

## Issue Comments

<comment author="agent-bot" at="2026-04-09T11:00:00Z">
  Ready for review. This adds auth middleware to all API routes.
</comment>
```

Comments are grouped by review (using `review_id`), with review comments threaded via `in_reply_to_id`. Issue comments appear separately.

### 7.2 `autosearch pr describe <pr_id>`

Returns structured JSON metadata.

```json
{
  "_meta": { "request_id": "", "elapsed_ms": 2 },
  "pr": {
    "id": "mistakenot/auto-stack#42",
    "owner": "mistakenot",
    "repo": "auto-stack",
    "number": 42,
    "title": "Add auth middleware to API routes",
    "authorLogin": "agent-bot",
    "draft": false,
    "baseBranch": "main",
    "headBranch": "feature/auth-middleware",
    "mergedAt": 1712678400000,
    "createdAt": 1712674800000,
    "additions": 150,
    "deletions": 20,
    "changedFiles": 6,
    "commentCount": 5,
    "commitCount": 3,
    "reviewers": ["alice", "bob"],
    "labels": ["security", "api"],
    "reviewSummary": {
      "approved": 1,
      "changesRequested": 1,
      "commented": 0,
      "dismissed": 0
    },
    "bodySummary": "Ready for review. This adds auth middleware..."
  }
}
```

### 7.3 `autosearch comment get <comment_id>`

Returns the full comment with code context.

```json
{
  "_meta": { "request_id": "", "elapsed_ms": 1 },
  "comment": {
    "id": "mistakenot/auto-stack#42/c/12345",
    "prId": "mistakenot/auto-stack#42",
    "commentType": "review_comment",
    "reviewState": "CHANGES_REQUESTED",
    "body": "Don't mock the database here — use the test helper.",
    "authorLogin": "bob",
    "authorAssociation": "MEMBER",
    "path": "internal/auth/middleware.go",
    "line": 42,
    "side": "RIGHT",
    "diffHunk": "@ -38,6 +38,12 @@ func TestAuthMiddleware...",
    "createdAt": 1712678400000,
    "inReplyToId": 0,
    "threadReplies": [
      {
        "id": "mistakenot/auto-stack#42/c/12346",
        "authorLogin": "agent-bot",
        "body": "Agreed, fixed in the next push.",
        "createdAt": 1712679000000
      }
    ]
  }
}
```

`threadReplies` is populated by querying `pr_comments` where `in_reply_to_id` matches the comment's `github_comment_id`. This is a shallow lookup — one level of replies only.

### 7.4 `autosearch comment describe <comment_id>`

Lighter version of `comment get` — metadata only, body as preview.

## 8. Reflection-Agent Access Patterns

These are the primary query patterns a self-reflecting agent would use. Each maps to existing or proposed CLI surfaces.

### 8.1 Surface Recent Feedback

"What review feedback did I get recently?"

```bash
autosearch search --scope pr_comments --since 2w
autosearch search --scope pr_comments --review-state CHANGES_REQUESTED --since 2w
```

Starting point for any reflection pass. The agent skims recent feedback looking for recurring themes.

### 8.2 Find Recurring Themes

"What kinds of mistakes keep getting flagged?"

```bash
autosearch search "test mock" --scope pr_comments --review-state CHANGES_REQUESTED
autosearch search "error handling OR nil check" --scope pr_comments
autosearch search "naming OR convention OR style" --scope pr_comments
```

One comment is anecdotal. Three comments about the same thing is a pattern worth codifying into a skill or rule.

### 8.3 Understand the Code Context

"What code triggered this feedback?"

```bash
# Get full comment with diff hunk and thread
autosearch comment get mistakenot/auto-stack#42/c/12345

# Get the full PR for broader context
autosearch pr get mistakenot/auto-stack#42

# Find the session that produced this PR
autosearch search "auto-stack#42" --scope messages
```

The comment alone isn't enough. The agent needs the diff hunk (what code was written), the thread (what was discussed), and optionally the session (what the agent was thinking).

### 8.4 Find Positive Examples

"What does good look like?"

```bash
# Comments with approval signal
autosearch search --scope pr_comments --review-state APPROVED --since 4w

# Small PRs that got approved quickly (low comment count = smooth review)
autosearch search --scope pr --since 4w
# then filter client-side by commentCount and time between createdAt and mergedAt
```

Skills should encode what works, not just what to avoid.

### 8.5 File-Scoped Feedback

"What feedback patterns exist for this area of the codebase?"

```bash
autosearch search --scope pr_comments --path "internal/auth"
autosearch search --scope pr_comments --path "*_test.go" --review-state CHANGES_REQUESTED
```

Different parts of the codebase have different conventions. File-scoped queries let the agent build area-specific rules.

### 8.6 Thread Resolution

"What did the reviewer and author agree on?"

```bash
# Get a comment with its thread replies
autosearch comment get mistakenot/auto-stack#42/c/12345
# → includes threadReplies showing the discussion and resolution
```

The lesson isn't just "reviewer said X" — it's "they discussed it and agreed on Z." The resolution is the actual rule to extract.

### 8.7 Dedup Against Existing Rules

"Has this pattern already been learned?"

```bash
# Search sessions for prior skill creation
autosearch search "autoskill create" --scope messages --since 4w

# Search for prior feedback on the same topic
autosearch search "mock database" --scope pr_comments
```

Avoid creating duplicate skills.

### 8.8 Cross-Domain Discovery

"Find everything related to auth middleware — sessions, PRs, and comments."

```bash
autosearch search "auth middleware" --scope all
```

This is the highest-value pattern. One query surfaces the session where the code was written, the PR where it was reviewed, and the comment where the reviewer gave feedback.

## 9. Deferred

These are explicitly out of scope for this iteration:

- **`autosearch stats`**: Aggregation commands (top files by feedback count, review turnaround, etc.). Valuable for reflection but separate design.
- **PR ↔ Session linkage**: Automatic correlation between PRs and the sessions that produced them. Requires heuristics (time window + git remote + commit SHA matching). Future feature.
- **Per-file patch indexing**: Indexing individual file patches from `files_json` as separate FTS rows. Adds complexity; `path` on `pr_comments` covers the main use case.
- **Semantic search**: Vector embeddings over PR content. Deferred alongside session semantic search.
- **Review round counting**: Derived "how many rounds of review" metric. Useful for stats, not needed for search.

## 10. Implementation Order

1. Add `ParquetPullRequestRow` and `ParquetPRCommentRow` structs to `auto-search/internal/model/`
2. Extend `etlscan/discover.go` to find `pull_requests/` and `pr_comments/` parquet files
3. Add parquet readers for both new datasets
4. Add new SQLite tables, indexes, FTS tables, and triggers to `indexdb/schema.go`
5. Extend indexer to process PR and comment parquet files (with JSON denormalization)
6. Add `--scope pr` and `--scope pr_comments` to search command
7. Add scope-specific filters (`--review-state`, `--comment-type`, `--path`, `--author`, `--label`)
8. Add `pr get`, `pr describe`, `comment get`, `comment describe` helper commands
9. Add `--scope all` cross-scope search with score normalization
10. Add test fixtures using `mistakenot/auto-stack#1` (the stable test PR from the ETL spec)
11. Update `quickstart` and `docs` commands to cover PR search

## 11. Test Plan

### Fixtures

Use `mistakenot/auto-stack#1` as the canonical test PR. The ETL spec documents its exact shape:
- 4 reviews (COMMENTED ×2, CHANGES_REQUESTED ×1, APPROVED ×1)
- 4 review comments (with threading via `in_reply_to_id`)
- 1 issue comment
- Labels: `easter-egg`, `test-data`

Generate parquet fixtures via `autoetl run --only github` against this PR, then commit a trimmed version to `auto-search/testdata/etl-output/pull_requests/` and `pr_comments/`.

### Unit Tests

- JSON denormalization: `reviewers_json` → space-separated logins
- JSON denormalization: `labels_json` → space-separated labels
- Score normalization for cross-scope merge
- PR-scope snippet extraction (from title vs. body)
- Comment-scope snippet extraction (from body vs. diff_hunk)
- Thread reply lookup logic

### Integration Tests

- Schema creation includes all new tables and FTS
- Full rebuild indexes PR + comment parquet alongside sessions/messages
- Incremental reindex handles dirty PR partitions
- `--scope pr` BM25 search returns hits with correct shape
- `--scope pr_comments` BM25 search returns hits with correct shape
- `--scope pr_comments --review-state CHANGES_REQUESTED` filters correctly
- `--scope pr_comments --path "internal/"` filters correctly
- `--scope all` returns mixed-type results
- `pr get` renders review threads in correct order
- `pr describe` computes `reviewSummary` correctly
- `comment get` includes `threadReplies`
- Empty PR tables don't break session/message search
