package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	gh "github.com/google/go-github/v72/github"
)

// Client is the interface for GitHub API interactions.
// Unit tests use a mock implementation; production uses RealClient.
type Client interface {
	ListPullRequests(ctx context.Context, owner, repo string, opts *gh.PullRequestListOptions) ([]*gh.PullRequest, *gh.Response, error)
	GetPullRequest(ctx context.Context, owner, repo string, number int) (*gh.PullRequest, *gh.Response, error)
	ListReviews(ctx context.Context, owner, repo string, number int, opts *gh.ListOptions) ([]*gh.PullRequestReview, *gh.Response, error)
	ListReviewComments(ctx context.Context, owner, repo string, number int, opts *gh.PullRequestListCommentsOptions) ([]*gh.PullRequestComment, *gh.Response, error)
	ListIssueComments(ctx context.Context, owner, repo string, number int, opts *gh.IssueListCommentsOptions) ([]*gh.IssueComment, *gh.Response, error)
	ListPullRequestFiles(ctx context.Context, owner, repo string, number int, opts *gh.ListOptions) ([]*gh.CommitFile, *gh.Response, error)
	GetPullRequestDiff(ctx context.Context, owner, repo string, number int) (string, *gh.Response, error)
	ListCheckRunsForRef(ctx context.Context, owner, repo, ref string, opts *gh.ListCheckRunsOptions) (*gh.ListCheckRunsResults, *gh.Response, error)
}

// RealClient wraps *github.Client and implements Client.
type RealClient struct {
	client *gh.Client
}

// NewRealClient creates a RealClient from a GitHub API token.
func NewRealClient(token string) *RealClient {
	return &RealClient{
		client: gh.NewClient(nil).WithAuthToken(token),
	}
}

func (c *RealClient) ListPullRequests(ctx context.Context, owner, repo string, opts *gh.PullRequestListOptions) ([]*gh.PullRequest, *gh.Response, error) {
	return c.client.PullRequests.List(ctx, owner, repo, opts)
}

func (c *RealClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (*gh.PullRequest, *gh.Response, error) {
	return c.client.PullRequests.Get(ctx, owner, repo, number)
}

func (c *RealClient) ListReviews(ctx context.Context, owner, repo string, number int, opts *gh.ListOptions) ([]*gh.PullRequestReview, *gh.Response, error) {
	return c.client.PullRequests.ListReviews(ctx, owner, repo, number, opts)
}

func (c *RealClient) ListReviewComments(ctx context.Context, owner, repo string, number int, opts *gh.PullRequestListCommentsOptions) ([]*gh.PullRequestComment, *gh.Response, error) {
	return c.client.PullRequests.ListComments(ctx, owner, repo, number, opts)
}

func (c *RealClient) ListIssueComments(ctx context.Context, owner, repo string, number int, opts *gh.IssueListCommentsOptions) ([]*gh.IssueComment, *gh.Response, error) {
	return c.client.Issues.ListComments(ctx, owner, repo, number, opts)
}

func (c *RealClient) ListPullRequestFiles(ctx context.Context, owner, repo string, number int, opts *gh.ListOptions) ([]*gh.CommitFile, *gh.Response, error) {
	return c.client.PullRequests.ListFiles(ctx, owner, repo, number, opts)
}

func (c *RealClient) GetPullRequestDiff(ctx context.Context, owner, repo string, number int) (string, *gh.Response, error) {
	raw, resp, err := c.client.PullRequests.GetRaw(ctx, owner, repo, number, gh.RawOptions{Type: gh.Diff})
	if err != nil {
		return "", resp, err
	}
	return raw, resp, nil
}

func (c *RealClient) ListCheckRunsForRef(ctx context.Context, owner, repo, ref string, opts *gh.ListCheckRunsOptions) (*gh.ListCheckRunsResults, *gh.Response, error) {
	return c.client.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, opts)
}

// ResolveToken returns a GitHub token from GITHUB_TOKEN env var or gh CLI.
// Returns ("", nil) if no token is available (not an error, just skip).
func ResolveToken() (string, error) {
	// 1. Try GITHUB_TOKEN env var — handled by caller reading os.Getenv
	// This function handles the gh CLI fallback.
	cmd := exec.Command("gh", "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token failed: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", errors.New("gh auth token returned empty")
	}
	return token, nil
}

// drainBody reads and discards the response body to allow connection reuse.
func drainBody(resp *gh.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
}
