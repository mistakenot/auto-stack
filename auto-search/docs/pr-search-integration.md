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
| Diff indexing | `diff_hunk` on comments only; full `diff` and `files_json` stored for retrieval, not FTS-indexed | Comment diff hunks are reviewer-curated and high-signal; monolithic diff is too noisy; per-file patch indexing deferred |
| Cross-scope search | `--scope all` unions results from all FTS tables | The killer feature: one query finds the PR, the review comment, and the session |
| JSON field handling | Denormalize `labels_json` and `reviewers_json` at index time | Enables column-level filtering without JSON parsing at query time |
| Score normalization | Per-table min-max normalization for `--scope all` | BM25 scores from different FTS tables aren't directly comparable |
| Graceful degradation | PR tables are optional in the index | If `autoetl` hasn't run `--only github`, PR tables are empty — search still works for sessions/messages |
| Backward compatibility | Schema version bump triggers full rebuild | Existing v1 indexes rebuild automatically when PR tables are added |

**FK and orphan handling**: `pr_comments.pr_id` references `pull_requests(pr_id)` and `foreign_keys=ON` is enabled. The indexer must process `pull_requests` parquet files before `pr_comments` parquet files within each incremental run to avoid FK violations. If a `pr_comments` partition references a PR not yet in the index (e.g. comment partition arrives before PR partition due to partitioning by different time columns), the indexer should log a warning and skip the orphan comment rows — they will be picked up on the next run when the PR partition lands. This is consistent with "graceful degradation": missing PR data means missing comments, not a crash.

## 3. Input: Parquet Schema

`autosearch` reads two new parquet datasets from `~/.auto/etl/output/`:

```
pull_requests/year=YYYY/month=MM/pull_requests.parquet
pull_request_comments/year=YYYY/month=MM/pull_request_comments.parquet
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
| `files_json` | string | **Store for retrieval only** — per-file patch indexing deferred (see section 9) |
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
CREATE TABLE IF NOT EXISTS pull_requests (
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
CREATE TABLE IF NOT EXISTS pr_comments (
  doc_id INTEGER PRIMARY KEY,
  partition_source_path TEXT NOT NULL,
  comment_id TEXT NOT NULL UNIQUE,     -- "owner/repo#pr_number/c/github_id"
  pr_id TEXT NOT NULL REFERENCES pull_requests(pr_id),
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
CREATE INDEX IF NOT EXISTS idx_pr_pr_id ON pull_requests(pr_id);
CREATE INDEX IF NOT EXISTS idx_pr_owner_repo ON pull_requests(owner, repo);
CREATE INDEX IF NOT EXISTS idx_pr_author_login ON pull_requests(author_login);
CREATE INDEX IF NOT EXISTS idx_pr_git_remote ON pull_requests(git_remote);
CREATE INDEX IF NOT EXISTS idx_pr_merged_at ON pull_requests(merged_at);

CREATE INDEX IF NOT EXISTS idx_prc_comment_id ON pr_comments(comment_id);
CREATE INDEX IF NOT EXISTS idx_prc_pr_id ON pr_comments(pr_id);
CREATE INDEX IF NOT EXISTS idx_prc_comment_type ON pr_comments(comment_type);
CREATE INDEX IF NOT EXISTS idx_prc_review_state ON pr_comments(review_state);
CREATE INDEX IF NOT EXISTS idx_prc_path ON pr_comments(path);
CREATE INDEX IF NOT EXISTS idx_prc_author_login ON pr_comments(author_login);
CREATE INDEX IF NOT EXISTS idx_prc_git_remote ON pr_comments(git_remote);
CREATE INDEX IF NOT EXISTS idx_prc_created_at ON pr_comments(created_at);
CREATE INDEX IF NOT EXISTS idx_prc_in_reply_to ON pr_comments(in_reply_to_id);

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

**Column naming**: The SQLite schema renames upstream parquet `id` to `pr_id` (pull requests) and `id` to `comment_id` (comments) to avoid ambiguity in joins and to be self-documenting. The upstream `comment_id` (int64) is stored as `github_comment_id` to distinguish it from the string composite ID. This divergence is intentional — the indexer maps between naming conventions in the parquet reader, same as it does for sessions (`id` → `session_id`) and messages (`id` → `message_id`).

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

CREATE TRIGGER pull_requests_au AFTER UPDATE ON pull_requests BEGIN
  INSERT INTO pull_requests_fts(pull_requests_fts, rowid, title, body, reviewers, labels)
  VALUES ('delete', old.doc_id, old.title, old.body, old.reviewers, old.labels);
  INSERT INTO pull_requests_fts(rowid, title, body, reviewers, labels)
  VALUES (new.doc_id, new.title, new.body, new.reviewers, new.labels);
END;

CREATE TRIGGER pr_comments_au AFTER UPDATE ON pr_comments BEGIN
  INSERT INTO pr_comments_fts(pr_comments_fts, rowid, body, diff_hunk, path)
  VALUES ('delete', old.doc_id, old.body, old.diff_hunk, old.path);
  INSERT INTO pr_comments_fts(rowid, body, diff_hunk, path)
  VALUES (new.doc_id, new.body, new.diff_hunk, new.path);
END;
```

## 5. Indexing

### 5.1 Discovery

Extend `etlscan/discover.go` to walk two additional directories:

```
~/.auto/etl/output/pull_requests/**/*.parquet
~/.auto/etl/output/pull_request_comments/**/*.parquet
```

Each discovered file gets the same `(dataset, partition_key, source_path, size, mtime)` treatment as messages/sessions.

**Indexer dispatch**: The current `indexdb/indexer.go` switches on dataset name (`sessions`, `messages`) with no default branch. This must be extended to handle `pull_requests` and `pull_request_comments`, and add a default branch that returns an error for unknown datasets to prevent writing `index_state` rows with zero indexed records.

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

- `reviewers_json` → `reviewers`: space-separated login names (e.g. `"alice bob charlie"`).
- `labels_json` → `labels`: space-separated label names (e.g. `"bug security p0"`).
- `files_json` → not denormalized into separate rows for v1. The `path` field on `pr_comments` already gives file-level search for code review comments. Full file-patch indexing is deferred.

**Filter implementation for denormalized fields**: The `--label` and `--reviewer` filters do **not** use FTS `MATCH`. Instead, they use SQL `WHERE` clauses with word-boundary matching on the base table: `WHERE ' ' || labels || ' ' LIKE '% value %'`. This avoids touching the FTS query compiler (`compile_fts.go`) which only handles the user's free-text search expression. The pattern pads the space-separated string to ensure exact token matching (e.g. `"p0"` won't match `"xp0x"`). These filters compose with an optional FTS `MATCH` clause via `AND` in the final SQL.

### 5.4 Incremental Policy

Same rules as sessions/messages:

- Always reindex the newest `pull_requests` partition and newest `pull_request_comments` partition
- Reindex older partitions when source metadata changes
- Full rebuild on schema version bump

This aligns with the ETL's dedup semantics — PRs and comments can be updated on re-sync (failed retries, new comments), so the newest partition is always dirty.

### 5.5 Graceful Absence

If `pull_requests/` or `pull_request_comments/` directories don't exist under the input path, indexing skips them silently. The tables are created in the schema regardless (empty tables are fine). This means `autosearch index` works the same whether or not `autoetl run --only github` has ever been run.

## 6. Search

### 6.1 New Scopes

Add two new search scopes alongside `messages` and `sessions`:

| Scope | FTS Table | Time Filter Column | Description |
|-------|-----------|-------------------|-------------|
| `pr` | `pull_requests_fts` | `merged_at` | Search PR titles and bodies |
| `pr_comments` | `pr_comments_fts` | `created_at` | Search review comments and code feedback |

The existing `--mode` flag (currently accepts but ignores values) should validate that only `bm25` is accepted for now. The new filter-only execution path (when no query is provided) sets `_meta.mode = "filter"` automatically — `--mode` is not applicable and should be rejected if explicitly passed without a query.

### 6.2 Scope-Specific Filters

**`--scope pr`**

| Flag | Column | Notes |
|------|--------|-------|
| `--remote` | `git_remote` | Shared with session/message scopes |
| `--author` | `author_login` | PR author |
| `--label` | `labels` | FTS token match on denormalized label field (exact token, not substring) |
| `--since`, `--after`, `--before` | `merged_at` | Same date semantics as other scopes |

**`--scope pr_comments`**

| Flag | Column | Notes |
|------|--------|-------|
| `--remote` | `git_remote` | Shared |
| `--review-state` | `review_state` | `APPROVED`, `CHANGES_REQUESTED`, `COMMENTED`, `DISMISSED` |
| `--comment-type` | `comment_type` | `review`, `review_comment`, `issue_comment` |
| `--path` | `path` | Prefix match via `path LIKE 'value%'`. Supports directory prefixes (e.g. `internal/auth/`), not glob patterns |
| `--author` | `author_login` | Comment author |
| `--since`, `--after`, `--before` | `created_at` | Same date semantics |

### 6.3 Cross-Scope Search (`--scope all`)

`--scope all` searches all four FTS tables and returns a unified result list.

**Query execution**: Run the compiled FTS query against each table independently, collect top 50 per table.

**Score normalization**: SQLite FTS5 `bm25()` returns negative values where lower (more negative) is better. For cross-scope merging, negate scores so higher is better, then apply per-table min-max scaling to [0, 1]. This matches the existing single-scope behavior (which sorts ascending on raw `bm25()`) while giving a uniform scale for merging. The `score` field in cross-scope results uses the normalized [0, 1] scale; single-scope results continue to use raw `bm25()` values for backward compatibility.

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

**`--cwd` behavior**: PR data has no `workspace` column — PRs are identified by `git_remote`, not local path. For `--scope pr` and `--scope pr_comments`, `--cwd` is not supported and should produce a clear error suggesting `--remote` instead. For `--scope all`, `--cwd` applies only to the session/message sub-queries; PR sub-queries are unfiltered (or the caller should use `--remote` to filter everything uniformly). The existing `--cwd`/`--remote` mutual exclusivity rule continues to apply.

**Deferred**: `--scope all` does not support scope-specific filters like `--review-state` or `--comment-type`. These only work with their matching scope. `--scope all` supports only the shared filters: `--remote`, `--since`, `--after`, `--before`.

**Result types**: `--scope all` returns a new `UnifiedSearchResult` type with `_meta.scope = "all"` and a `hits` array of polymorphic objects discriminated by `type`. Existing `MessageSearchResult` and `SessionSearchResult` are unchanged — single-scope queries continue to return their current envelope shapes. New `PRSearchResult` and `PRCommentSearchResult` types are added for `--scope pr` and `--scope pr_comments` respectively. The `UnifiedSearchResult` embeds hits from all four types.

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

**Shell quoting**: PR IDs contain `#` which shells interpret as comment start. All CLI examples and documentation must use single quotes: `autosearch pr get 'mistakenot/auto-stack#42'`. The same applies to comment IDs. Consider also accepting numeric shorthand (`autosearch pr get 42 --repo mistakenot/auto-stack`) as an ergonomic alternative, but this is deferred — quoted full IDs are the v1 interface.

### 7.1 `autosearch pr get <pr_id>`

Renders a full PR view mimicking the GitHub PR experience: metadata header, per-file diffs with review comments anchored inline, and a conversation timeline. Output is structured text (same approach as `session get`).

The view has three sections:

1. **Header** — status line with key metadata at a glance
2. **Files** — each changed file with its diff and any review comments anchored to their lines
3. **Conversation** — timeline of issue comments and top-level review submissions (APPROVED, CHANGES_REQUESTED)

```
┌─────────────────────────────────────────────────────────────────┐
│ mistakenot/auto-stack#42  MERGED                                │
│ Add auth middleware to API routes                               │
│                                                                 │
│ Author: agent-bot        Merged: 2026-04-09T14:09:06Z          │
│ Branch: feature/auth-middleware → main                          │
│ Reviewers: alice ✓  bob ✗→✓                                    │
│ Labels: security, api                                           │
│ Files: 6 changed  +150 -20  Commits: 3                         │
└─────────────────────────────────────────────────────────────────┘

## Description

<pr body text>

## Files

### internal/auth/middleware.go (+45 -3)

```diff
@@ -38,6 +38,12 @@ func NewAuthMiddleware(store SessionStore) *AuthMiddleware {
     return &AuthMiddleware{store: store}
 }

+func (m *AuthMiddleware) Validate(ctx context.Context, token string) error {
+    session, err := m.store.Get(ctx, token)
+    if err != nil {
+        return fmt.Errorf("auth validation: %w", err)
+    }
+    if session.Expired() {
```

  ┌── bob (CHANGES_REQUESTED) at line 42 ──────────────────────
  │ Don't mock the database here — use the test helper.
  │
  │   └── agent-bot replied:
  │       Agreed, fixed in the next push.
  └────────────────────────────────────────────────────────────

```diff
@@ -78,3 +84,9 @@ func (m *AuthMiddleware) Handler(next http.Handler) http.Handler {
+    if err := m.Validate(r.Context(), token); err != nil {
+        http.Error(w, "unauthorized", http.StatusUnauthorized)
+        return
+    }
```

### internal/auth/middleware_test.go (+95 -0)

```diff
@@ -0,0 +1,95 @@
+package auth_test
+
+func TestAuthMiddleware_Validate(t *testing.T) {
...
```

  (no comments on this file)

### cmd/server/main.go (+10 -1)

  ...

## Conversation

<comment author="agent-bot" at="2026-04-09T11:00:00Z">
  Ready for review. This adds auth middleware to all API routes.
</comment>

<review author="bob" state="CHANGES_REQUESTED" at="2026-04-09T12:00:00Z">
  A few things to fix — see inline comments.
</review>

<review author="alice" state="APPROVED" at="2026-04-09T13:30:00Z">
  LGTM, clean implementation.
</review>
```

**Rendering rules**:

- **Header**: Reviewer status uses shorthand: `✓` = APPROVED, `✗` = CHANGES_REQUESTED, `✗→✓` = requested changes then approved. Derived from `pr_comments` rows where `comment_type = "review"`.
- **Files section**: Files are listed from `files_json` (parsed at render time, not index time). Each file shows its per-file patch. Review comments (`comment_type = "review_comment"`) are anchored below the diff hunk they reference, using `path` and `line` to position them. Threaded replies (`in_reply_to_id != 0`) are nested under their parent.
- **Conversation section**: Issue comments (`comment_type = "issue_comment"`) and top-level review submissions (`comment_type = "review"`) are shown in chronological order.
- **Truncation**: For large diffs (>200 lines per file), mid-truncate with `... [+N more lines — run: autosearch pr diff 'owner/repo#42' --file path/to/file]`. For PRs with >20 files, show the first 20 and note `... and N more files`.
- **No-diff fallback**: If `diff` is empty (non-critical fetch failed in ETL), show file list from `files_json` without patches, with a note that diff data is unavailable.

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

**Query argument change**: The current CLI requires exactly one positional query argument (`cobra.ExactArgs(1)`). This design requires relaxing that to `cobra.MaximumNArgs(1)`. When no query is provided but filters are present, search uses a **filter-only execution path** that bypasses the FTS pipeline entirely:

- CLI validation: at least one filter flag is required when no query is given (otherwise error with usage hint)
- No call to `query.Parse()` — the current parser errors on empty input and this is correct behavior; filter-only mode should not invoke it
- SQL: query the base table directly with `WHERE` clauses from filters, ordered by the scope's time column descending, `LIMIT 50`
- No snippet extraction (no query terms to match)
- Hit shape: same as FTS results but with `score: 0` and empty `snippet`/offset fields
- `_meta.mode`: `"filter"` (not `"bm25"`) to distinguish from ranked results

This enables filter-only browsing patterns essential for reflection workflows.

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
autosearch comment get 'mistakenot/auto-stack#42/c/12345'

# Get the full PR for broader context
autosearch pr get 'mistakenot/auto-stack#42'

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
# All feedback on auth code
autosearch search --scope pr_comments --path "internal/auth/"

# Search for test-related feedback by keyword instead of path pattern
autosearch search "test" --scope pr_comments --review-state CHANGES_REQUESTED
```

Different parts of the codebase have different conventions. File-scoped queries let the agent build area-specific rules.

### 8.6 Thread Resolution

"What did the reviewer and author agree on?"

```bash
# Get a comment with its thread replies
autosearch comment get 'mistakenot/auto-stack#42/c/12345'
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
- **PR ↔ Session linkage**: Automatic correlation between PRs and the sessions that produced them. Branch names won't work — Claude Code worktrees use auto-generated names (`worktree-agent-*`) that don't match the PR's `head_branch`. Commit SHAs are available on both sides but require text extraction: PRs have structured `head_sha`/`merge_commit_sha` fields, while sessions only contain SHAs as substrings in `git commit`/`git push` tool output. Best practical approach is **time window + `git_remote`** (session `last_message_at` near PR `created_at` on same remote), optionally strengthened by regex-extracting short SHAs from bash tool results and matching against PR `head_sha`. Stretch goal — not blocking search or reflection workflows.
- **Git push hooks for structured linkage data**: A Claude Code hook firing after `git push` bash commands could capture the pushed branch, commit SHA range, and remote into a structured sidecar file. `autoetl` would then pick this up and write it as proper columns on the session/message parquet (e.g. `pushed_branch`, `pushed_commits`), giving `autosearch` a clean join key for PR ↔ session linkage without text extraction. This is an ETL-level concern — the hook writes the data, ETL normalizes it, search consumes it.
- **Per-file patch indexing**: Indexing individual file patches from `files_json` as separate FTS rows. Adds complexity; `path` on `pr_comments` covers the main use case.
- **Semantic search**: Vector embeddings over PR content. Deferred alongside session semantic search.
- **Review round counting**: Derived "how many rounds of review" metric. Useful for stats, not needed for search.

## 10. Implementation Order

1. Add `ParquetPullRequestRow` and `ParquetPRCommentRow` structs to `auto-search/internal/model/`
2. Extend `etlscan/discover.go` to find `pull_requests/` and `pull_request_comments/` parquet files
3. Add parquet readers for both new datasets
4. Add new SQLite tables, indexes, FTS tables, and triggers to `indexdb/schema.go`
5. Extend indexer to process PR and comment parquet files (with JSON denormalization)
6. Add `--scope pr` and `--scope pr_comments` to search command
7. Add scope-specific filters (`--review-state`, `--comment-type`, `--path`, `--author`, `--label`)
8. Add `pr get`, `pr describe`, `comment get`, `comment describe` helper commands
9. Add `--scope all` cross-scope search with score normalization
10. Extend `indexdb/state.go`: add `pull_requests` and `pull_request_comments` to `DeleteRowsBySource` and `RowCounts` dataset handling
11. Register `pr` and `comment` subcommands in `cli/root.go`
12. Update existing tests that hard-code dataset lists (`indexer_integration_test.go`, `indexdb_test.go`) to include `pull_requests` and `pull_request_comments`
13. Add test fixtures using `mistakenot/auto-stack#1` (the stable test PR from the ETL spec)
14. Update `quickstart` output to cover PR search

## 11. Test Plan

### Fixtures

Tests must not depend on the GitHub API or `~/.auto/` directories. All test data comes from two sources:

**Unit / integration tests**: Deterministic fixture generators in `internal/testutil` (matching the existing pattern for session/message fixtures) that produce `ParquetPullRequestRow` and `ParquetPRCommentRow` structs programmatically. Extend `internal/testutil/fixtures.go` to expose `PullRequestsPath()` and `PRCommentsPath()` helpers alongside the existing `SessionsPath()` and `MessagesPath()`, so all integration tests locate fixtures through one consistent API.

**E2E tests**: Use `auto-search/.tmp/etl-output/` which contains a snapshot of real data copied from `~/.auto/etl/output/` (git-ignored, not committed). This directory already contains all four datasets (`messages/`, `sessions/`, `pull_requests/`, `pull_request_comments/`). E2E tests run the full `autosearch index` + `autosearch search` pipeline against this snapshot. The data includes:
- 2 PRs (`mistakenot/auto-stack#2`, `#3`)
- 4 PR comments on #2 (2 reviews, 2 review comments with threading and file paths)
- 0 comments on #3 (clean approval)

To refresh the snapshot after re-running `autoetl`:
```bash
rsync -a --delete ~/.auto/etl/output/ auto-search/.tmp/etl-output/
```

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
