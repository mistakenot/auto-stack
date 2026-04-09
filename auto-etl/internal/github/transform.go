package github

import (
	"encoding/json"
	"fmt"
	"time"

	gh "github.com/google/go-github/v72/github"

	"github.com/mistakenot/auto-etl/internal/model"
)

// TransformPR converts GitHub API PR data into a model.PullRequest row.
func TransformPR(
	pr *gh.PullRequest,
	reviews []*gh.PullRequestReview,
	checkRuns []*gh.CheckRun,
	files []*gh.CommitFile,
	diff string,
	repo RepoRef,
	hostID string,
) model.PullRequest {
	mergedAt := pr.GetMergedAt().Time

	row := model.PullRequest{
		ID:     fmt.Sprintf("%s/%s#%d", repo.Owner, repo.Repo, pr.GetNumber()),
		Owner:  repo.Owner,
		Repo:   repo.Repo,
		Number: int32(pr.GetNumber()),

		Title:          pr.GetTitle(),
		Body:           pr.GetBody(),
		State:          "merged", // Normalized from closed+merged_at
		Draft:          pr.GetDraft(),
		BaseBranch:     pr.GetBase().GetRef(),
		HeadBranch:     pr.GetHead().GetRef(),
		BaseSHA:        pr.GetBase().GetSHA(),
		HeadSHA:        pr.GetHead().GetSHA(),
		MergeCommitSHA: pr.GetMergeCommitSHA(),

		AuthorLogin:       pr.GetUser().GetLogin(),
		AuthorDisplayName: pr.GetUser().GetName(),

		ReviewersJSON: marshalReviewers(reviews),
		LabelsJSON:    marshalLabels(pr.Labels),
		ChecksJSON:    marshalCheckRuns(checkRuns),
		Diff:          diff,
		FilesJSON:     marshalFiles(files),

		Additions:    int32(pr.GetAdditions()),
		Deletions:    int32(pr.GetDeletions()),
		ChangedFiles: int32(pr.GetChangedFiles()),
		CommentCount: int32(pr.GetComments() + pr.GetReviewComments()),
		CommitCount:  int32(pr.GetCommits()),

		CreatedAt: toUnixMillis(pr.GetCreatedAt().Time),
		UpdatedAt: toUnixMillis(pr.GetUpdatedAt().Time),
		ClosedAt:  toUnixMillis(pr.GetClosedAt().Time),
		MergedAt:  toUnixMillis(mergedAt),

		GitRemote: repo.GitRemote,
		HostID:    hostID,

		Year:          int32(mergedAt.Year()),
		Month:         int32(mergedAt.Month()),
		SchemaVersion: model.SchemaVersion,
	}

	return row
}

// TransformComments converts all comment types into model.PRComment rows.
func TransformComments(
	pr *gh.PullRequest,
	reviews []*gh.PullRequestReview,
	reviewComments []*gh.PullRequestComment,
	issueComments []*gh.IssueComment,
	repo RepoRef,
	hostID string,
) []model.PRComment {
	mergedAt := pr.GetMergedAt().Time
	prID := fmt.Sprintf("%s/%s#%d", repo.Owner, repo.Repo, pr.GetNumber())
	prNumber := int32(pr.GetNumber())

	base := model.PRComment{
		PRID:          prID,
		Owner:         repo.Owner,
		Repo:          repo.Repo,
		PRNumber:      prNumber,
		GitRemote:     repo.GitRemote,
		HostID:        hostID,
		Year:          int32(mergedAt.Year()),
		Month:         int32(mergedAt.Month()),
		SchemaVersion: model.SchemaVersion,
	}

	var rows []model.PRComment

	// Review-level comments (type: "review")
	for _, r := range reviews {
		if r.GetBody() == "" && r.GetState() == "PENDING" {
			continue
		}
		c := base
		c.ID = fmt.Sprintf("%s/c/%d", prID, r.GetID())
		c.CommentID = r.GetID()
		c.CommentType = "review"
		c.Body = r.GetBody()
		c.AuthorLogin = r.GetUser().GetLogin()
		c.AuthorDisplayName = r.GetUser().GetName()
		c.AuthorAssociation = r.GetAuthorAssociation()
		c.ReviewID = r.GetID()
		c.ReviewState = r.GetState()
		c.CommitSHA = r.GetCommitID()
		c.CreatedAt = toUnixMillis(r.GetSubmittedAt().Time)
		c.UpdatedAt = toUnixMillis(r.GetSubmittedAt().Time)
		rows = append(rows, c)
	}

	// Inline review comments (type: "review_comment")
	for _, rc := range reviewComments {
		c := base
		c.ID = fmt.Sprintf("%s/c/%d", prID, rc.GetID())
		c.CommentID = rc.GetID()
		c.InReplyToID = rc.GetInReplyTo()
		c.CommentType = "review_comment"
		c.Body = rc.GetBody()
		c.AuthorLogin = rc.GetUser().GetLogin()
		c.AuthorDisplayName = rc.GetUser().GetName()
		c.AuthorAssociation = rc.GetAuthorAssociation()
		c.Path = rc.GetPath()
		c.DiffHunk = rc.GetDiffHunk()
		c.CommitSHA = rc.GetCommitID()
		c.OriginalLine = int32(rc.GetOriginalLine())
		c.Line = int32(rc.GetLine())
		c.Side = rc.GetSide()
		c.StartLine = int32(rc.GetStartLine())
		c.StartSide = rc.GetStartSide()
		c.ReviewID = rc.GetPullRequestReviewID()
		c.CreatedAt = toUnixMillis(rc.GetCreatedAt().Time)
		c.UpdatedAt = toUnixMillis(rc.GetUpdatedAt().Time)
		rows = append(rows, c)
	}

	// General conversation comments (type: "issue_comment")
	for _, ic := range issueComments {
		c := base
		c.ID = fmt.Sprintf("%s/c/%d", prID, ic.GetID())
		c.CommentID = ic.GetID()
		c.CommentType = "issue_comment"
		c.Body = ic.GetBody()
		c.AuthorLogin = ic.GetUser().GetLogin()
		c.AuthorDisplayName = ic.GetUser().GetName()
		c.AuthorAssociation = ic.GetAuthorAssociation()
		c.CreatedAt = toUnixMillis(ic.GetCreatedAt().Time)
		c.UpdatedAt = toUnixMillis(ic.GetUpdatedAt().Time)
		rows = append(rows, c)
	}

	return rows
}

// --- JSON marshaling helpers ---

type reviewerJSON struct {
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
	State       string `json:"state"`
}

func marshalReviewers(reviews []*gh.PullRequestReview) string {
	seen := make(map[string]*reviewerJSON)
	var order []string

	for _, r := range reviews {
		login := r.GetUser().GetLogin()
		if login == "" {
			continue
		}
		if _, ok := seen[login]; !ok {
			order = append(order, login)
		}
		// Last review state wins (most recent)
		seen[login] = &reviewerJSON{
			Login:       login,
			DisplayName: r.GetUser().GetName(),
			State:       r.GetState(),
		}
	}

	result := make([]reviewerJSON, 0, len(order))
	for _, login := range order {
		result = append(result, *seen[login])
	}

	return mustMarshalJSON(result)
}

func marshalLabels(labels []*gh.Label) string {
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		names = append(names, l.GetName())
	}
	return mustMarshalJSON(names)
}

type checkRunJSON struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

func marshalCheckRuns(runs []*gh.CheckRun) string {
	if len(runs) == 0 {
		return "[]"
	}
	result := make([]checkRunJSON, 0, len(runs))
	for _, r := range runs {
		result = append(result, checkRunJSON{
			Name:       r.GetName(),
			Status:     r.GetStatus(),
			Conclusion: r.GetConclusion(),
		})
	}
	return mustMarshalJSON(result)
}

type fileJSON struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch,omitempty"`
}

func marshalFiles(files []*gh.CommitFile) string {
	if len(files) == 0 {
		return "[]"
	}
	result := make([]fileJSON, 0, len(files))
	for _, f := range files {
		result = append(result, fileJSON{
			Filename:  f.GetFilename(),
			Status:    f.GetStatus(),
			Additions: f.GetAdditions(),
			Deletions: f.GetDeletions(),
			Patch:     f.GetPatch(),
		})
	}
	return mustMarshalJSON(result)
}

func mustMarshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func toUnixMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
