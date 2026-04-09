package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	gh "github.com/google/go-github/v72/github"

	"github.com/mistakenot/auto-etl/internal/model"
)

const perPage = 100

// FetchConfig holds parameters for the GitHub sync.
type FetchConfig struct {
	HostID        string
	SyncStatePath string
}

// SyncWarning captures a non-fatal issue during sync.
type SyncWarning struct {
	Repo    string
	PR      int
	Message string
}

// SyncSummary is the result of a full GitHub sync run.
type SyncSummary struct {
	ReposProcessed int
	PRsSynced      int
	Warnings       []SyncWarning
}

// FetchAll runs the 5-phase sync algorithm for all discovered repos.
func FetchAll(ctx context.Context, client Client, repos []RepoRef, cfg FetchConfig) (*model.GitHubSyncResult, *SyncSummary, error) {
	state := LoadSyncState(cfg.SyncStatePath)
	summary := &SyncSummary{}
	result := &model.GitHubSyncResult{}

	for _, repo := range repos {
		prs, comments, warnings, err := fetchRepo(ctx, client, repo, state, cfg)
		if err != nil {
			// Per-repo auth failure: skip, continue with others
			var ghErr *gh.ErrorResponse
			if errors.As(err, &ghErr) && (ghErr.Response.StatusCode == http.StatusForbidden || ghErr.Response.StatusCode == http.StatusUnauthorized) {
				summary.Warnings = append(summary.Warnings, SyncWarning{
					Repo:    repo.OwnerRepo(),
					Message: fmt.Sprintf("%d %s (skipped)", ghErr.Response.StatusCode, http.StatusText(ghErr.Response.StatusCode)),
				})
				continue
			}
			return nil, nil, fmt.Errorf("fetch repo %s: %w", repo.OwnerRepo(), err)
		}
		result.PullRequests = append(result.PullRequests, prs...)
		result.Comments = append(result.Comments, comments...)
		summary.Warnings = append(summary.Warnings, warnings...)
		summary.ReposProcessed++
		summary.PRsSynced += len(prs)
	}

	// Phase 5: Persist sync state
	if err := state.Save(cfg.SyncStatePath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save sync state: %v\n", err)
	}

	return result, summary, nil
}

func fetchRepo(ctx context.Context, client Client, repo RepoRef, state *SyncState, cfg FetchConfig) ([]model.PullRequest, []model.PRComment, []SyncWarning, error) {
	repoState := state.GetRepo(repo.OwnerRepo())
	var allPRs []model.PullRequest
	var allComments []model.PRComment
	var warnings []SyncWarning

	// Phase 1: Retry previously failed PRs
	failedIDs := repoState.FailedPRNumbers()
	for _, prID := range failedIDs {
		// Extract PR number from ID format "owner/repo#number"
		num, err := parsePRNumber(prID)
		if err != nil {
			continue
		}
		fmt.Fprintf(os.Stderr, "retrying %s...\n", prID)
		pr, comments, w, fetchErr := fetchSinglePR(ctx, client, repo, num, cfg)
		warnings = append(warnings, w...)
		if fetchErr != nil {
			warnings = append(warnings, SyncWarning{Repo: repo.OwnerRepo(), PR: num, Message: fmt.Sprintf("retry failed: %v", fetchErr)})
			continue
		}
		if pr != nil {
			allPRs = append(allPRs, *pr)
			allComments = append(allComments, comments...)
			repoState.MarkSynced(prID, collectMissingFields(w))
		}
	}

	// Phase 2: Discover newly merged PRs
	hwm := repoState.HighWaterMarkTime()
	hasHWM := !hwm.IsZero()
	var maxUpdated time.Time

	opts := &gh.PullRequestListOptions{
		State:     "closed",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: gh.ListOptions{
			PerPage: perPage,
		},
	}

	var toFetch []int // PR numbers to fetch

pagination:
	for {
		prs, resp, err := clientListPRsWithRetry(ctx, client, repo.Owner, repo.Repo, opts)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("list PRs page %d: %w", opts.Page, err)
		}

		for _, pr := range prs {
			// Only merged PRs
			if pr.MergedAt == nil || pr.GetMergedAt().IsZero() {
				continue
			}

			updatedAt := pr.GetUpdatedAt().Time
			if updatedAt.After(maxUpdated) {
				maxUpdated = updatedAt
			}

			// Early stop: if we have a high-water mark and this PR's updated_at is below it
			if hasHWM && updatedAt.Before(hwm) {
				break pagination
			}

			prID := fmt.Sprintf("%s/%s#%d", repo.Owner, repo.Repo, pr.GetNumber())

			// Skip already synced
			if info, ok := repoState.PRs[prID]; ok && info.Synced {
				continue
			}
			// Skip failed (already handled in Phase 1)
			if info, ok := repoState.PRs[prID]; ok && !info.Synced {
				continue
			}

			toFetch = append(toFetch, pr.GetNumber())
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage

		drainBody(resp)
	}

	// Log progress
	if len(toFetch) > 0 {
		fmt.Fprintf(os.Stderr, "fetching %d merged PRs for %s...\n", len(toFetch), repo.OwnerRepo())
	}

	// Phase 3: Fetch each un-synced merged PR
	for _, num := range toFetch {
		prID := fmt.Sprintf("%s/%s#%d", repo.Owner, repo.Repo, num)
		pr, comments, w, err := fetchSinglePR(ctx, client, repo, num, cfg)
		warnings = append(warnings, w...)
		if err != nil {
			repoState.MarkFailed(prID, extractEndpointNames(err))
			warnings = append(warnings, SyncWarning{Repo: repo.OwnerRepo(), PR: num, Message: fmt.Sprintf("fetch failed: %v", err)})
			continue
		}
		if pr != nil {
			allPRs = append(allPRs, *pr)
			allComments = append(allComments, comments...)
			repoState.MarkSynced(prID, collectMissingFields(w))
		}
	}

	// Update high-water mark
	if !maxUpdated.IsZero() {
		repoState.HighWaterMark = maxUpdated.UTC().Format(time.RFC3339)
	}

	return allPRs, allComments, warnings, nil
}

// fetchSinglePR fetches all data for one PR (Phase 3).
// Returns the PR model, comments, warnings for non-critical failures, and error for critical failures.
func fetchSinglePR(ctx context.Context, client Client, repo RepoRef, number int, cfg FetchConfig) (*model.PullRequest, []model.PRComment, []SyncWarning, error) {
	var warnings []SyncWarning
	var criticalErr error

	// Critical: Get full PR data
	pr, _, err := clientGetPRWithRetry(ctx, client, repo.Owner, repo.Repo, number)
	if err != nil {
		return nil, nil, warnings, fmt.Errorf("get PR: %w", err)
	}

	// Critical: Reviews
	reviews, err := paginateAll(func(opts *gh.ListOptions) ([]*gh.PullRequestReview, *gh.Response, error) {
		return clientListReviewsWithRetry(ctx, client, repo.Owner, repo.Repo, number, opts)
	})
	if err != nil {
		criticalErr = fmt.Errorf("reviews: %w", err)
		return nil, nil, warnings, criticalErr
	}

	// Critical: Review comments (inline)
	reviewComments, err := paginateAllReviewComments(ctx, client, repo.Owner, repo.Repo, number)
	if err != nil {
		criticalErr = fmt.Errorf("review_comments: %w", err)
		return nil, nil, warnings, criticalErr
	}

	// Critical: Issue comments (general)
	issueComments, err := paginateAllIssueComments(ctx, client, repo.Owner, repo.Repo, number)
	if err != nil {
		criticalErr = fmt.Errorf("issue_comments: %w", err)
		return nil, nil, warnings, criticalErr
	}

	// Non-critical: Diff
	diff, _, diffErr := clientGetDiffWithRetry(ctx, client, repo.Owner, repo.Repo, number)
	if diffErr != nil {
		warnings = append(warnings, SyncWarning{Repo: repo.OwnerRepo(), PR: number, Message: fmt.Sprintf("diff endpoint failed: %v", diffErr)})
	}

	// Non-critical: Files
	files, filesErr := paginateAllFiles(ctx, client, repo.Owner, repo.Repo, number)
	if filesErr != nil {
		warnings = append(warnings, SyncWarning{Repo: repo.OwnerRepo(), PR: number, Message: fmt.Sprintf("files endpoint failed: %v", filesErr)})
	}

	// Non-critical: Check runs
	var checkRuns []*gh.CheckRun
	if pr.GetHead().GetSHA() != "" {
		cr, checksErr := fetchCheckRuns(ctx, client, repo.Owner, repo.Repo, pr.GetHead().GetSHA())
		if checksErr != nil {
			warnings = append(warnings, SyncWarning{Repo: repo.OwnerRepo(), PR: number, Message: fmt.Sprintf("checks endpoint failed: %v", checksErr)})
		} else {
			checkRuns = cr
		}
	}

	// Transform
	prRow := TransformPR(pr, reviews, checkRuns, files, diff, repo, cfg.HostID)
	commentRows := TransformComments(pr, reviews, reviewComments, issueComments, repo, cfg.HostID)

	return &prRow, commentRows, warnings, nil
}

// --- Pagination helpers ---

func paginateAll[T any](fn func(opts *gh.ListOptions) ([]T, *gh.Response, error)) ([]T, error) {
	var all []T
	opts := &gh.ListOptions{PerPage: perPage}
	for {
		items, resp, err := fn(opts)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		drainBody(resp)
	}
	return all, nil
}

func paginateAllReviewComments(ctx context.Context, client Client, owner, repo string, number int) ([]*gh.PullRequestComment, error) {
	var all []*gh.PullRequestComment
	opts := &gh.PullRequestListCommentsOptions{ListOptions: gh.ListOptions{PerPage: perPage}}
	for {
		items, resp, err := clientListReviewCommentsWithRetry(ctx, client, owner, repo, number, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		drainBody(resp)
	}
	return all, nil
}

func paginateAllIssueComments(ctx context.Context, client Client, owner, repo string, number int) ([]*gh.IssueComment, error) {
	var all []*gh.IssueComment
	opts := &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{PerPage: perPage}}
	for {
		items, resp, err := clientListIssueCommentsWithRetry(ctx, client, owner, repo, number, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		drainBody(resp)
	}
	return all, nil
}

func paginateAllFiles(ctx context.Context, client Client, owner, repo string, number int) ([]*gh.CommitFile, error) {
	var all []*gh.CommitFile
	opts := &gh.ListOptions{PerPage: perPage}
	for {
		items, resp, err := clientListFilesWithRetry(ctx, client, owner, repo, number, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		drainBody(resp)
	}
	return all, nil
}

func fetchCheckRuns(ctx context.Context, client Client, owner, repo, ref string) ([]*gh.CheckRun, error) {
	var all []*gh.CheckRun
	opts := &gh.ListCheckRunsOptions{ListOptions: gh.ListOptions{PerPage: perPage}}
	for {
		result, resp, err := clientListCheckRunsWithRetry(ctx, client, owner, repo, ref, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, result.CheckRuns...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		drainBody(resp)
	}
	return all, nil
}

// --- Rate-limit-aware wrappers ---

func clientListPRsWithRetry(ctx context.Context, client Client, owner, repo string, opts *gh.PullRequestListOptions) ([]*gh.PullRequest, *gh.Response, error) {
	return withRateRetry(func() ([]*gh.PullRequest, *gh.Response, error) {
		return client.ListPullRequests(ctx, owner, repo, opts)
	})
}

func clientGetPRWithRetry(ctx context.Context, client Client, owner, repo string, number int) (*gh.PullRequest, *gh.Response, error) {
	return withRateRetry(func() (*gh.PullRequest, *gh.Response, error) {
		return client.GetPullRequest(ctx, owner, repo, number)
	})
}

func clientListReviewsWithRetry(ctx context.Context, client Client, owner, repo string, number int, opts *gh.ListOptions) ([]*gh.PullRequestReview, *gh.Response, error) {
	return withRateRetry(func() ([]*gh.PullRequestReview, *gh.Response, error) {
		return client.ListReviews(ctx, owner, repo, number, opts)
	})
}

func clientListReviewCommentsWithRetry(ctx context.Context, client Client, owner, repo string, number int, opts *gh.PullRequestListCommentsOptions) ([]*gh.PullRequestComment, *gh.Response, error) {
	return withRateRetry(func() ([]*gh.PullRequestComment, *gh.Response, error) {
		return client.ListReviewComments(ctx, owner, repo, number, opts)
	})
}

func clientListIssueCommentsWithRetry(ctx context.Context, client Client, owner, repo string, number int, opts *gh.IssueListCommentsOptions) ([]*gh.IssueComment, *gh.Response, error) {
	return withRateRetry(func() ([]*gh.IssueComment, *gh.Response, error) {
		return client.ListIssueComments(ctx, owner, repo, number, opts)
	})
}

func clientGetDiffWithRetry(ctx context.Context, client Client, owner, repo string, number int) (string, *gh.Response, error) {
	return withRateRetry(func() (string, *gh.Response, error) {
		return client.GetPullRequestDiff(ctx, owner, repo, number)
	})
}

func clientListFilesWithRetry(ctx context.Context, client Client, owner, repo string, number int, opts *gh.ListOptions) ([]*gh.CommitFile, *gh.Response, error) {
	return withRateRetry(func() ([]*gh.CommitFile, *gh.Response, error) {
		return client.ListPullRequestFiles(ctx, owner, repo, number, opts)
	})
}

func clientListCheckRunsWithRetry(ctx context.Context, client Client, owner, repo, ref string, opts *gh.ListCheckRunsOptions) (*gh.ListCheckRunsResults, *gh.Response, error) {
	return withRateRetry(func() (*gh.ListCheckRunsResults, *gh.Response, error) {
		return client.ListCheckRunsForRef(ctx, owner, repo, ref, opts)
	})
}

const maxAbuseRetries = 3

// withRateRetry handles both primary rate limits (X-RateLimit-Reset) and
// secondary abuse limits (Retry-After header).
func withRateRetry[T any](fn func() (T, *gh.Response, error)) (T, *gh.Response, error) {
	for attempt := 0; attempt <= maxAbuseRetries; attempt++ {
		result, resp, err := fn()
		if err == nil {
			return result, resp, nil
		}

		// Primary rate limit
		var rateLimitErr *gh.RateLimitError
		if errors.As(err, &rateLimitErr) {
			resetAt := rateLimitErr.Rate.Reset.Time
			sleepDuration := time.Until(resetAt) + time.Second
			if sleepDuration > 0 {
				fmt.Fprintf(os.Stderr, "rate limited, sleeping %s until reset...\n", sleepDuration.Round(time.Second))
				time.Sleep(sleepDuration)
			}
			continue
		}

		// Secondary (abuse) rate limit
		var abuseErr *gh.AbuseRateLimitError
		if errors.As(err, &abuseErr) {
			if attempt >= maxAbuseRetries-1 {
				var zero T
				return zero, resp, fmt.Errorf("abuse rate limit after %d retries: %w", maxAbuseRetries, err)
			}
			retryAfter := abuseErr.GetRetryAfter()
			if retryAfter == 0 {
				retryAfter = 60 * time.Second
			}
			sleepDuration := retryAfter + time.Second
			fmt.Fprintf(os.Stderr, "abuse rate limit, sleeping %s...\n", sleepDuration.Round(time.Second))
			time.Sleep(sleepDuration)
			continue
		}

		// Not a rate limit error, return immediately
		var zero T
		return zero, resp, err
	}

	// Should not reach here
	var zero T
	return zero, nil, errors.New("exceeded max retries")
}

// --- Helpers ---

func parsePRNumber(prID string) (int, error) {
	// Format: "owner/repo#123"
	for i := len(prID) - 1; i >= 0; i-- {
		if prID[i] == '#' {
			n, err := strconv.Atoi(prID[i+1:])
			if err != nil {
				return 0, err
			}
			return n, nil
		}
	}
	return 0, fmt.Errorf("invalid PR ID format: %s", prID)
}

func extractEndpointNames(err error) []string {
	if err == nil {
		return nil
	}
	// Simple extraction from error message
	return []string{err.Error()}
}

func collectMissingFields(warnings []SyncWarning) []string {
	var fields []string
	for _, w := range warnings {
		if w.Message != "" {
			// Map warning messages to field names
			switch {
			case contains(w.Message, "checks"):
				fields = append(fields, "checks_json")
			case contains(w.Message, "diff"):
				fields = append(fields, "diff")
			case contains(w.Message, "files"):
				fields = append(fields, "files_json")
			}
		}
	}
	return fields
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
