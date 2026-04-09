package github

import (
	"context"
	"net/http"

	gh "github.com/google/go-github/v72/github"
)

// MockClient implements Client for testing.
type MockClient struct {
	PullRequests   []*gh.PullRequest
	Reviews        []*gh.PullRequestReview
	ReviewComments []*gh.PullRequestComment
	IssueComments  []*gh.IssueComment
	Files          []*gh.CommitFile
	Diff           string
	CheckRuns      *gh.ListCheckRunsResults

	// Per-endpoint error injection
	ListPRsErr            error
	GetPRErr              error
	ListReviewsErr        error
	ListReviewCommentsErr error
	ListIssueCommentsErr  error
	ListFilesErr          error
	GetDiffErr            error
	ListCheckRunsErr      error
}

func emptyResp() *gh.Response {
	return &gh.Response{
		Response: &http.Response{StatusCode: http.StatusOK},
	}
}

func (m *MockClient) ListPullRequests(_ context.Context, _, _ string, _ *gh.PullRequestListOptions) ([]*gh.PullRequest, *gh.Response, error) {
	if m.ListPRsErr != nil {
		return nil, emptyResp(), m.ListPRsErr
	}
	return m.PullRequests, emptyResp(), nil
}

func (m *MockClient) GetPullRequest(_ context.Context, _, _ string, _ int) (*gh.PullRequest, *gh.Response, error) {
	if m.GetPRErr != nil {
		return nil, emptyResp(), m.GetPRErr
	}
	if len(m.PullRequests) > 0 {
		return m.PullRequests[0], emptyResp(), nil
	}
	return nil, emptyResp(), nil
}

func (m *MockClient) ListReviews(_ context.Context, _, _ string, _ int, _ *gh.ListOptions) ([]*gh.PullRequestReview, *gh.Response, error) {
	if m.ListReviewsErr != nil {
		return nil, emptyResp(), m.ListReviewsErr
	}
	return m.Reviews, emptyResp(), nil
}

func (m *MockClient) ListReviewComments(_ context.Context, _, _ string, _ int, _ *gh.PullRequestListCommentsOptions) ([]*gh.PullRequestComment, *gh.Response, error) {
	if m.ListReviewCommentsErr != nil {
		return nil, emptyResp(), m.ListReviewCommentsErr
	}
	return m.ReviewComments, emptyResp(), nil
}

func (m *MockClient) ListIssueComments(_ context.Context, _, _ string, _ int, _ *gh.IssueListCommentsOptions) ([]*gh.IssueComment, *gh.Response, error) {
	if m.ListIssueCommentsErr != nil {
		return nil, emptyResp(), m.ListIssueCommentsErr
	}
	return m.IssueComments, emptyResp(), nil
}

func (m *MockClient) ListPullRequestFiles(_ context.Context, _, _ string, _ int, _ *gh.ListOptions) ([]*gh.CommitFile, *gh.Response, error) {
	if m.ListFilesErr != nil {
		return nil, emptyResp(), m.ListFilesErr
	}
	return m.Files, emptyResp(), nil
}

func (m *MockClient) GetPullRequestDiff(_ context.Context, _, _ string, _ int) (string, *gh.Response, error) {
	if m.GetDiffErr != nil {
		return "", emptyResp(), m.GetDiffErr
	}
	return m.Diff, emptyResp(), nil
}

func (m *MockClient) ListCheckRunsForRef(_ context.Context, _, _, _ string, _ *gh.ListCheckRunsOptions) (*gh.ListCheckRunsResults, *gh.Response, error) {
	if m.ListCheckRunsErr != nil {
		return nil, emptyResp(), m.ListCheckRunsErr
	}
	if m.CheckRuns != nil {
		return m.CheckRuns, emptyResp(), nil
	}
	return &gh.ListCheckRunsResults{}, emptyResp(), nil
}
