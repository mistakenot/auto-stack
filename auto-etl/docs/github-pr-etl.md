---
hash: "881f5403"
id: "badba584"
summary: "Spec for ingesting merged GitHub PR feedback (reviews, comments, diffs, CI checks) into parquet datasets via auto-etl."
title: "GitHub PR Feedback ETL"
---

# GitHub PR Feedback ETL

Spec for ingesting GitHub pull request feedback (reviews, comments, CI status, metadata) into structured parquet tables alongside the existing session/message ETL pipeline.

## Motivation

PR feedback contains valuable signal — code review insights, design decisions, bug context, approval patterns — that is lost if we only index coding session history. By pulling PR data into the same ETL pipeline, we can later correlate review feedback with the sessions that produced the code.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Storage | New dedicated parquet tables | PR data has unique fields (diff hunks, line ranges, review state) that don't fit AgentMessage/AgentSession |
| Content scope | Everything available (for merged PRs) | Reviews, comments, PR body, CI checks, reviewers, labels, merge details, full diff, per-file patches |
| Repo discovery | Auto from git remotes | Scan `~/.auto/etl/settings.json` remote cache — zero config for known repos |
| PR filter | Merged only | Open = WIP, closed-without-merge = abandoned. Successfully synced merged PRs are never re-fetched; failed syncs are retried |
| GitHub client | `google/go-github` library | Native Go, handles pagination and rate limit headers |
| Auth | `GITHUB_TOKEN` env var first, fall back to `gh auth token` | Works in CI (env var) and local dev (gh CLI) |
| CLI surface | Integrated into `autoetl run` with `--only` filter | `--only sessions`, `--only github`, or both; default runs all |
| Identity | Username + display name | Enough to identify people without excessive PII |
| Rate limits | Sleep and retry | Respect `X-RateLimit-Reset`, sleep until reset, continue automatically |
| Testing | Interface-based mock | Define `GitHubClient` interface, mock in tests |
| Spec scope | ETL only | Search indexing is a future concern |

## Data Model

Two new parquet datasets under `~/.auto/etl/output/`. These extend the canonical ETL output alongside `messages` and `sessions`. Downstream tools (`autosearch`, `autoreflect`) should treat these as optional — their absence means GitHub sync hasn't run yet, not an error. The `schema_version` field on each row supports future schema evolution.

```
pull_requests/year=YYYY/month=MM/pull_requests.parquet
pr_comments/year=YYYY/month=MM/pr_comments.parquet
```

Partitioned by month using the PR's `merged_at` timestamp. Since we only ingest merged PRs, this is always set.

### PullRequest table

One row per PR.

```go
type PullRequest struct {
    // Identity
    ID        string `parquet:"id"`         // "{owner}/{repo}#{number}"
    Owner     string `parquet:"owner,dict"`
    Repo      string `parquet:"repo,dict"`
    Number    int32  `parquet:"number"`

    // Metadata
    Title     string `parquet:"title"`
    Body      string `parquet:"body"`
    // Normalized state: always "merged" for our dataset.
    // GitHub API returns "closed" for merged PRs; we normalize to "merged" when merged_at is set.
    State     string `parquet:"state,dict"`     // Always "merged" (normalized from GitHub's "closed" + merged_at != null)
    Draft     bool   `parquet:"draft"`
    BaseBranch string `parquet:"base_branch,dict"`
    HeadBranch string `parquet:"head_branch,dict"`
    BaseSHA   string `parquet:"base_sha"`
    HeadSHA   string `parquet:"head_sha"`
    MergeCommitSHA string `parquet:"merge_commit_sha"`

    // Author
    AuthorLogin       string `parquet:"author_login,dict"`
    AuthorDisplayName string `parquet:"author_display_name"`

    // Reviewers (JSON array of {login, display_name, state} objects)
    ReviewersJSON string `parquet:"reviewers_json"`

    // Labels (JSON array of strings)
    LabelsJSON string `parquet:"labels_json"`

    // CI / checks (JSON array of {name, status, conclusion} objects)
    // Populated best-effort — if token lacks Actions scope, this is empty and a warning is logged
    ChecksJSON string `parquet:"checks_json"`

    // Full PR diff (unified diff format from GitHub's media type application/vnd.github.diff)
    // Contains the patch across all files — no git checkout needed to see what changed.
    // Note: GitHub may truncate diffs for very large PRs (>300 files or >3MB). Empty string if
    // the diff endpoint failed (non-critical). Consumers should not assume completeness.
    Diff string `parquet:"diff"`

    // Changed files with per-file patches (JSON array of {filename, status, additions, deletions, patch} objects)
    // From GET /repos/{owner}/{repo}/pulls/{number}/files.
    // Note: `patch` may be null for binary files or files exceeding GitHub's size limit.
    // The `filename`, `status`, `additions`, `deletions` fields are always present.
    FilesJSON string `parquet:"files_json"`

    // Counts
    Additions    int32 `parquet:"additions"`
    Deletions    int32 `parquet:"deletions"`
    ChangedFiles int32 `parquet:"changed_files"`
    CommentCount int32 `parquet:"comment_count"`
    CommitCount  int32 `parquet:"commit_count"`

    // Timestamps (Unix milliseconds)
    CreatedAt  int64 `parquet:"created_at"`
    UpdatedAt  int64 `parquet:"updated_at"`
    ClosedAt   int64 `parquet:"closed_at"`
    MergedAt   int64 `parquet:"merged_at"`

    // Linkage
    GitRemote string `parquet:"git_remote,dict"` // Full remote URL for joining with sessions
    HostID    string `parquet:"host_id,dict"`

    // Partition
    Year          int32 `parquet:"year"`
    Month         int32 `parquet:"month"`
    SchemaVersion int32 `parquet:"schema_version"`
}
```

### PRComment table

One row per comment. Captures inline review comments, review-level comments, and general issue-style comments, all in one table with a `comment_type` discriminator.

```go
type PRComment struct {
    // Identity
    ID          string `parquet:"id"`           // "{owner}/{repo}#{pr_number}/c/{comment_id}"
    PRID        string `parquet:"pr_id,dict"`   // FK to PullRequest.ID
    CommentID   int64  `parquet:"comment_id"`   // GitHub's numeric comment ID
    InReplyToID int64  `parquet:"in_reply_to_id"` // For threading; 0 if top-level

    // Type
    CommentType string `parquet:"comment_type,dict"` // "review", "review_comment", "issue_comment"

    // Content
    Body string `parquet:"body"`

    // Author
    AuthorLogin       string `parquet:"author_login,dict"`
    AuthorDisplayName string `parquet:"author_display_name"`
    AuthorAssociation string `parquet:"author_association,dict"` // MEMBER, CONTRIBUTOR, etc.

    // Code location (populated for review_comment type)
    Path          string `parquet:"path,dict"`       // File path in the diff
    DiffHunk      string `parquet:"diff_hunk"`       // Surrounding diff context
    CommitSHA     string `parquet:"commit_sha"`      // Exact commit this comment targets
    OriginalLine  int32  `parquet:"original_line"`   // Line in the original file (base)
    Line          int32  `parquet:"line"`             // Line in the new file (head)
    Side          string `parquet:"side,dict"`        // "LEFT" or "RIGHT"
    StartLine     int32  `parquet:"start_line"`       // For multi-line comments
    StartSide     string `parquet:"start_side,dict"`

    // Review context (populated for review and review_comment types)
    ReviewID    int64  `parquet:"review_id"`
    ReviewState string `parquet:"review_state,dict"` // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED

    // Timestamps (Unix milliseconds)
    CreatedAt int64 `parquet:"created_at"`
    UpdatedAt int64 `parquet:"updated_at"`

    // Denormalized from PR
    Owner      string `parquet:"owner,dict"`
    Repo       string `parquet:"repo,dict"`
    PRNumber   int32  `parquet:"pr_number"`
    GitRemote  string `parquet:"git_remote,dict"`
    HostID     string `parquet:"host_id,dict"`

    // Partition
    Year          int32 `parquet:"year"`
    Month         int32 `parquet:"month"`
    SchemaVersion int32 `parquet:"schema_version"`
}
```

### Comment types

| `comment_type` | Source API | What it captures |
|----------------|-----------|-----------------|
| `review` | `GET /repos/{owner}/{repo}/pulls/{number}/reviews` | Review-level body text + approval state (APPROVED, CHANGES_REQUESTED, etc.) |
| `review_comment` | `GET /repos/{owner}/{repo}/pulls/{number}/comments` | Inline code comments attached to a diff line, threaded via `in_reply_to_id` |
| `issue_comment` | `GET /repos/{owner}/{repo}/issues/{number}/comments` | General conversation comments on the PR (not tied to code) |

## Incremental Sync

### Sync state file

```
~/.auto/etl/github/sync-state.json
```

```json
{
  "schema_version": 1,
  "repos": {
    "owner/repo": {
      "high_water_mark": "2026-04-06T12:00:00Z",
      "prs": {
        "123": {
          "synced": true,
          "synced_at": "2026-04-06T12:00:00Z",
          "missing_fields": []
        },
        "124": {
          "synced": true,
          "synced_at": "2026-04-05T09:30:00Z",
          "missing_fields": ["checks_json", "diff"]
        },
        "125": {
          "synced": false,
          "last_attempt_at": "2026-04-06T12:01:00Z",
          "failed_endpoints": ["reviews", "comments"]
        }
      }
    }
  }
}
```

- `synced: true` → all critical endpoints succeeded; PR will be skipped on future runs
- `synced: false` → a critical endpoint failed; PR will be retried on next run
- `missing_fields` → non-critical fields that failed (informational, does not block sync).
  **These gaps are intentionally permanent** — non-critical fields (checks, diff, files) are not retried
  once a PR is marked `synced: true`. To re-fetch, manually delete the PR's entry from sync-state.json
  and re-run. This tradeoff keeps the sync simple and fast.
- `failed_endpoints` → which critical endpoints failed (for diagnostics)

### Sync algorithm

**Target PRs: merged only.** Open PRs (work in progress) and closed-without-merge (abandoned) are skipped entirely.

```
for each repo discovered from git remotes:

  PHASE 1: RETRY previously failed PRs
     - Scan sync-state.json for all entries with synced: false
     - Re-fetch each one using the same critical/non-critical endpoint logic below
     - This runs BEFORE pagination so failed PRs are never missed by the early-stop condition

  PHASE 2: DISCOVER newly merged PRs
     - GET /repos/{owner}/{repo}/pulls?state=closed&sort=updated&direction=desc
     - Filter client-side: only PRs where merged_at is set
       (GitHub state=closed returns both closed and merged)
     - Each page: skip PRs with synced: true in sync-state, collect the rest
       (synced: false entries were already handled in Phase 1)
     - First run (no high-water mark): paginate through ALL pages, no early stop
     - Subsequent runs: stop when updated_at drops below the high-water mark
       (see "High-water mark" below)

  PHASE 3: FETCH each un-synced merged PR (from Phase 1 retries + Phase 2 discoveries)

     a. FETCH full PR data
        Critical endpoints (must all succeed to mark synced):
        - GET /pulls/{n}                    → PR metadata, body, merge info
        - GET /pulls/{n}/reviews            → review bodies + approval states
        - GET /pulls/{n}/comments           → inline review comments (diff_hunk, line, path)
        - GET /issues/{n}/comments          → general conversation comments

        Non-critical endpoints (best-effort, warn on failure):
        - GET /pulls/{n} (Accept: diff)     → full unified diff → `diff` column
        - GET /pulls/{n}/files              → per-file patches → `files_json`
        - GET /commits/{sha}/check-runs     → CI checks (may 403 without Actions scope)

     b. TRANSFORM API responses → PullRequest row + PRComment rows

     c. MARK SYNCED?
        - If ALL critical endpoints succeeded → record in sync-state as synced: true
        - If any critical endpoint failed → record as synced: false, log warning
          (PR will be retried in Phase 1 on next run)
        - Non-critical failures → recorded in missing_fields, do not block sync marking

  PHASE 4: WRITE parquet partitions
     - Determine which monthly partitions were affected (contain new or retried PRs)
     - For each affected partition:
       a. READ existing parquet file for that month (if it exists)
       b. MERGE with newly fetched/retried rows, deduplicating:
          - `pull_requests`: dedupe by PR ID (e.g., `owner/repo#123`). New data wins.
          - `pr_comments`: dedupe by comment ID + comment_type
            (e.g., `owner/repo#123/c/456789`). New data wins.
          - For retried PRs: all old comments for that PR are replaced with the
            fresh fetch (delete-by-PR-ID then insert). This handles edited and
            deleted comments — we don't track individual comment mutations.
       c. WRITE merged result back to the partition file
     - Unaffected partitions are not touched
     - NOTE: unlike session/message ETL, GitHub partitions are NOT immutable.
       This is an intentional exception because failed PRs may be retried across
       runs, requiring updates to historical month partitions.
     - **Downstream impact**: `autosearch` incremental indexing currently assumes
       past partitions don't change. When GitHub PR indexing is added to autosearch,
       it must either re-index all PR partitions on each run, or check file
       modification times to detect rewritten partitions. This is a future concern
       scoped to the autosearch integration spec, not this ETL spec.

  PHASE 5: PERSIST sync-state.json
```

### First run behavior

On first run (no sync-state.json), the sync paginates through all merged PRs for each discovered repo. Progress is logged to stderr (e.g., `fetched 150 merged PRs for owner/repo...`). For repos with thousands of merged PRs this may take several minutes due to API pagination, but only happens once — subsequent runs skip all synced PRs.

### High-water mark

The sync-state stores a `high_water_mark` per repo — the highest `updated_at` timestamp seen across all successfully synced PRs in the previous run.

- **First run**: no high-water mark exists → paginate through all pages (no early stop). This may be slow for repos with thousands of PRs but only happens once.
- **Subsequent runs**: sort by `updated` descending. Stop paginating when `updated_at` drops below the high-water mark. This catches PRs created long ago but merged recently (they appear early because `updated_at` reflects the merge).
- After each successful run, update the high-water mark to the max `updated_at` from this run.

We sort by `updated` (not `created`) because `updated_at` reflects merges, new comments, and label changes — exactly the activity we want to catch. The high-water mark makes this safe: we only stop when we reach PRs that haven't changed since our last sync.

### Why merged-only is sufficient

- Merged PRs are *effectively* immutable — the code, diff, and review decisions are final
- Post-merge activity (late comments, label edits) is rare and low-value for our use case
- **Known tradeoff**: we accept that late post-merge comments will be missed. This is intentional — the cost of re-checking all merged PRs on every run far outweighs the value of catching occasional late comments. Users who need to re-sync a specific PR can delete its entry from sync-state.json and re-run.
- This makes the sync deterministic: each PR is fetched exactly once (assuming critical endpoints succeed)
- Open PRs are work-in-progress with incomplete review threads — low signal
- Closed-without-merge PRs are abandoned — typically low value
- Future enhancement: add `--include-open` flag if open PR data becomes useful

**Note on canonical data**: PR feedback is a supplementary data source, not the canonical journey record. The canonical journey is coding sessions (`messages`/`sessions`). PR data enriches the picture but tolerating bounded staleness for merged PRs is an acceptable tradeoff for sync simplicity.

## Auth

```
1. Check GITHUB_TOKEN environment variable
2. If not set, run `gh auth token` and capture stdout
3. If both fail, log warning to stderr and skip GitHub sync
   (do not fail the entire ETL run)
```

**Exit code behavior**:
- Default run (`autoetl run`): auth failure → warning, skip GitHub sync, exit 0 (session ETL succeeded)
- Explicit `--only github`: auth failure → error, exit 1 (the user explicitly asked for GitHub sync and it can't run)

The token is resolved once at the start of the GitHub sync phase and reused for all API calls in that run.

### Graceful partial auth failures

Auth failures should never halt the pipeline. Handle at every granularity:

| Scope | Failure mode | Behavior |
|-------|-------------|----------|
| **All GitHub** | Both `GITHUB_TOKEN` and `gh auth token` fail | Log warning to stderr, skip entire GitHub sync, session ETL still runs |
| **Per-repo** | 401/403 on a specific repo (e.g., private repo) | Log warning, skip that repo, continue with remaining repos |
| **Per-endpoint** | Token lacks specific scope (e.g., no Actions read for checks) | Log warning, leave that field empty (e.g., `checks_json = "[]"`), continue with rest of PR data |
| **Per-PR** | Transient 500/502 on a single PR fetch | Log warning, record as `synced: false` in sync-state (retried in Phase 1 on next run), continue with remaining PRs |

Report all warnings in a summary block at the end of the GitHub sync phase:

```
GitHub PR sync complete: 3 repos, 47 PRs synced, 2 warnings:
  - owner/private-repo: 403 Forbidden (skipped)
  - owner/repo: checks endpoint returned 403 (token may lack Actions scope), checks_json left empty for 12 PRs
```

The key principle: **fetch everything the token allows, warn about everything it doesn't, never fail the run.**

## Rate Limiting

### Primary rate limits

Use GitHub's response headers:
- `X-RateLimit-Remaining`: requests left in current window
- `X-RateLimit-Reset`: Unix timestamp when the window resets

When `Remaining` hits 0, sleep until `Reset + 1s`, then continue. Log the wait to stderr so the user knows why the run is paused.

The `go-github` library exposes these headers natively via `github.Rate` on every response.

### Secondary (abuse) rate limits

GitHub may return `403` with a `Retry-After` header even when `X-RateLimit-Remaining > 0`. This happens when requests are too rapid or trigger abuse detection.

When a response includes `Retry-After`:
- Sleep for the specified number of seconds + 1s buffer
- Retry the same request
- If `Retry-After` is hit 3 times consecutively for the same request, skip that request and log a warning

The `go-github` library surfaces `*github.AbuseRateLimitError` for these cases.

## CLI Integration

### Existing command changes

```
autoetl run [flags]
```

New flag:

```
--only strings   Run only specified ETL sources. Valid values: sessions, github. Default: all.
```

**Parsing rules** (per project CLI conventions):
- Accepts comma-separated values: `--only sessions,github`
- Also accepts repeated flags: `--only sessions --only github`
- Values are lowercased and trimmed before validation
- Invalid values → fail-fast with error listing valid options: `invalid --only value "foo"; valid values: sessions, github`
- Duplicates are silently deduplicated

Examples:
```bash
autoetl run                      # Run all: session ETL + GitHub PR sync
autoetl run --only sessions      # Session ETL only (current behavior)
autoetl run --only github        # GitHub PR sync only
autoetl run --only sessions,github  # Explicit: both (same as default)
```

No additional GitHub-specific flags. First run fetches all merged PRs (no cutoff).

## Pipeline Integration

The GitHub sync runs as a separate phase after the existing session ETL, within the same `autoetl run` invocation:

```
[Parse] → [Transform] → [Write]     ← existing session ETL
                                ↓
[GitHub Fetch] → [Transform] → [Write]  ← new GitHub phase
                                ↓
[Summary]                        ← combined report
```

Both phases write to the same `~/.auto/etl/output/` tree but into separate dataset directories (`pull_requests/`, `pr_comments/`).

## Repo Discovery

Reuse the existing git remote cache at `~/.auto/etl/settings.json`:

```json
{
  "remotes": {
    "/home/user/projects/myapp": "https://github.com/owner/repo.git"
  }
}
```

Extract unique `owner/repo` pairs from the cached remote URLs. Filter to GitHub remotes using exact hostname matching: `github.com` only (both HTTPS and SSH formats). Deduplicate across workspaces that point to the same repo. Non-GitHub remotes (GitLab, Bitbucket, etc.) are silently ignored.

**Scope**: all GitHub repos in the global cache are synced, including repos from old or inactive workspaces. This is intentional — if you coded in a repo, its PR feedback is relevant context. Old repos simply won't have new merged PRs to discover, so the cost is one API list call per repo per run (which returns empty quickly).

## Output Layout

```
~/.auto/etl/output/
  messages/year=YYYY/week=WW/messages.parquet        ← existing
  sessions/year=YYYY/month=MM/sessions.parquet       ← existing
  pull_requests/year=YYYY/month=MM/pull_requests.parquet  ← new
  pr_comments/year=YYYY/month=MM/pr_comments.parquet      ← new
```

## Testing

### Interface

```go
type GitHubClient interface {
    ListPullRequests(ctx context.Context, owner, repo string, opts *github.PullRequestListOptions) ([]*github.PullRequest, *github.Response, error)
    GetPullRequest(ctx context.Context, owner, repo string, number int) (*github.PullRequest, *github.Response, error)
    ListReviews(ctx context.Context, owner, repo string, number int, opts *github.ListOptions) ([]*github.PullRequestReview, *github.Response, error)
    ListReviewComments(ctx context.Context, owner, repo string, number int, opts *github.PullRequestListCommentsOptions) ([]*github.PullRequestComment, *github.Response, error)
    ListIssueComments(ctx context.Context, owner, repo string, number int, opts *github.IssueListCommentsOptions) ([]*github.IssueComment, *github.Response, error)
    ListPullRequestFiles(ctx context.Context, owner, repo string, number int, opts *github.ListOptions) ([]*github.CommitFile, *github.Response, error)
    GetPullRequestDiff(ctx context.Context, owner, repo string, number int) (string, *github.Response, error)
    ListCheckRunsForRef(ctx context.Context, owner, repo, ref string, opts *github.ListCheckRunsOptions) (*github.ListCheckRunsResults, *github.Response, error)
}
```

A `RealGitHubClient` wraps `*github.Client` and implements this interface. Unit tests use a `MockGitHubClient` that returns canned data.

### Test cases

#### Repo discovery & cache

1. **Parse GitHub HTTPS remote**: Cache has `"https://github.com/owner/repo.git"` → extracts `owner/repo`
2. **Parse GitHub SSH remote**: Cache has `"git@github.com:owner/repo.git"` → extracts `owner/repo`
3. **Filter non-GitHub remotes**: Cache has GitLab/Bitbucket URLs → excluded from GitHub sync
4. **Deduplicate across workspaces**: Two workspaces point to same `owner/repo` → only one sync
5. **Trailing .git normalization**: `owner/repo.git` and `owner/repo` resolve to same repo
6. **Empty cache**: No remotes in settings.json → log warning, skip GitHub sync, no error
7. **Missing settings.json**: File doesn't exist → log warning, skip GitHub sync, no error
8. **Mixed remotes**: Cache has GitHub + GitLab URLs → only GitHub repos synced, GitLab ignored silently

#### Sync state

9. **Sync state read/write roundtrip**: Write sync state → read back → verify PR statuses preserved
10. **Missing sync-state.json**: First run, file doesn't exist → treat as empty state, fetch all merged PRs (full history)
11. **Corrupt sync-state.json**: Invalid JSON → log warning, treat as empty state, re-fetch all merged PRs (full history)

#### Incremental sync

12. **Incremental skip**: Sync state has a merged PR with `synced: true` → verify no API calls for that PR
13. **New merged PR**: PR not in sync-state → verify full fetch + mark synced
14. **Skips non-merged**: Open and closed-without-merge PRs in API response → verify they are filtered out
15. **First run full pagination**: No high-water mark → all pages fetched, no early stop
16. **Subsequent run high-water mark stop**: High-water mark set → stop when `updated_at` drops below it
17. **Critical endpoint failure**: Reviews endpoint returns 500 → PR marked `synced: false`, retried on next run
18. **Non-critical endpoint failure**: Checks endpoint returns 403 → PR marked `synced: true` with `missing_fields: ["checks_json"]`
19. **Retry phase runs first**: `synced: false` PR from previous run → retried in Phase 1 before pagination in Phase 2
20. **Retry success**: Previously failed PR retried → all critical endpoints succeed → marked `synced: true`
21. **Partition read-merge-write**: Existing partition has 5 PRs, new run adds 2 → output partition has all 7
22. **Partition merge dedup**: Re-synced PR (retry) already exists in partition → new data replaces old, no duplicates
23. **Partition idempotency**: Run twice with no new PRs → parquet output is identical (no data loss from rewrite)

#### Transform & output

24. **Transform correctness**: Mock client returns known PR + comments → verify parquet rows match expected values
25. **Comment threading**: Multiple `review_comment` rows with `in_reply_to_id` → verify thread linkage is preserved
26. **Partition correctness**: PR merged in January → lands in `year=YYYY/month=01/` partition
27. **Diff and files stored**: PR fetch includes diff + files → verify `diff` and `files_json` columns populated
28. **Comment types**: Review, review_comment, and issue_comment all present → verify `comment_type` discriminator correct
29. **State normalization**: GitHub returns state="closed" + merged_at set → stored as state="merged"
30. **Truncated diff handling**: GitHub returns empty patch for binary file → `patch` is null in `files_json`, no error

#### Auth

31. **GITHUB_TOKEN preferred**: Env var set → used directly, `gh auth token` not called
32. **Fallback to gh CLI**: No env var → `gh auth token` called and token used
33. **Auth failure**: Both methods fail → warning logged, no panic, session ETL still runs
34. **Per-repo auth failure**: One repo returns 403 → other repos still processed
35. **Per-endpoint auth failure**: Checks endpoint returns 403 → `checks_json` is `"[]"`, rest of PR data intact

#### Rate limiting

36. **Primary rate limit sleep**: Mock returns 403 with rate limit headers → verify sleep until reset + retry
37. **Rate limit mid-run**: Rate limited after 5 PRs → sleeps, then continues with remaining PRs
38. **Secondary abuse limit**: Mock returns 403 with `Retry-After` header → verify sleep for specified duration + retry
39. **Abuse limit max retries**: Same request triggers `Retry-After` 3 times → skip request, log warning

## Example Queries

Once data is ingested, these DuckDB queries become possible:

```sql
-- All review comments on a specific PR with thread ordering
SELECT c.author_login, c.path, c.line, c.body, c.review_state
FROM 'pr_comments/**/*.parquet' c
WHERE c.pr_id = 'owner/repo#123'
ORDER BY c.created_at;

-- PRs with the most review comments (hotspots)
SELECT p.id, p.title, p.comment_count, p.additions + p.deletions as churn
FROM 'pull_requests/**/*.parquet' p
ORDER BY p.comment_count DESC
LIMIT 20;

-- Who reviews the most code?
SELECT c.author_login, COUNT(*) as review_comments
FROM 'pr_comments/**/*.parquet' c
WHERE c.comment_type = 'review_comment'
GROUP BY c.author_login
ORDER BY review_comments DESC;

-- Files that attract the most review feedback
SELECT c.path, COUNT(*) as comments
FROM 'pr_comments/**/*.parquet' c
WHERE c.comment_type = 'review_comment'
GROUP BY c.path
ORDER BY comments DESC
LIMIT 20;

-- Correlate: sessions that touched files with heavy PR feedback
SELECT DISTINCT s.id, s.first_message_at, m.tool_file_path
FROM 'sessions/**/*.parquet' s
JOIN 'messages/**/*.parquet' m ON m.session_id = s.id
JOIN 'pr_comments/**/*.parquet' c ON c.path = m.tool_file_path
WHERE m.tool_name = 'Write'
  AND c.comment_type = 'review_comment';
```

## Development Environment

This environment (`/home/vscode/src/auto-stack`) has `gh` CLI authenticated and can be used for ad-hoc API probing during development. The remote cache at `~/.auto/etl/settings.json` already has repos (e.g. `mistakenot/auto-stack`).

**Note:** The cached repos currently have no merged PRs. To test against real data during development:
- Create a test PR on `mistakenot/auto-stack`, merge it, then probe with `gh api`
- Or add a public repo with merged PRs to the cache for testing

Useful ad-hoc probe commands:
```bash
# Check auth + scopes
gh auth status

# List merged PRs
gh api repos/{owner}/{repo}/pulls?state=closed\&per_page=5 \
  --jq '.[] | select(.merged_at != null) | {number, title, merged_at, merge_commit_sha}'

# Get PR diff
gh api repos/{owner}/{repo}/pulls/{n} -H "Accept: application/vnd.github.diff"

# Get changed files with patches
gh api repos/{owner}/{repo}/pulls/{n}/files --jq '.[].filename'

# Get review comments with line info
gh api repos/{owner}/{repo}/pulls/{n}/comments \
  --jq '.[] | {id, path, line, diff_hunk, body, user: .user.login}'
```

## Implementation Order

1. **Model**: Add `PullRequest` and `PRComment` structs to `internal/model/`
2. **GitHub client**: `internal/github/client.go` — interface, real client, auth resolution
3. **Fetch**: `internal/github/fetch.go` — pagination, rate limiting, sync state management
4. **Transform**: `internal/github/transform.go` — API responses → model structs
5. **Writer extension**: Update `internal/writer/` to handle new table types
6. **CLI integration**: Add `--only` flag to `cmd/run.go`, wire up GitHub sync phase
7. **Tests**: Unit tests with mock client for each component
8. **Sync state**: Read/write `sync-state.json` with proper locking
