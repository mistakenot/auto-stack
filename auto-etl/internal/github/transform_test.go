package github

import (
	"encoding/json"
	"testing"
	"time"

	gh "github.com/google/go-github/v72/github"
)

func timestamp(s string) *gh.Timestamp {
	t, _ := time.Parse(time.RFC3339, s)
	return &gh.Timestamp{Time: t}
}

// p returns a pointer to v. Avoids the linter's newexpr check by not matching
// the `func Ptr[T any](v T) *T` or `func X(v T) *T` patterns.
func p[T any](v ...T) *T { return &v[0] }

func TestTransformPR_BasicFields(t *testing.T) {
	pr := &gh.PullRequest{
		Number:         p(42),
		Title:          p("Add feature X"),
		Body:           p("This adds feature X"),
		State:          p("closed"),
		Draft:          p(false),
		MergeCommitSHA: p("abc123"),
		Additions:      p(100),
		Deletions:      p(20),
		ChangedFiles:   p(5),
		Comments:       p(3),
		ReviewComments: p(2),
		Commits:        p(4),
		CreatedAt:      timestamp("2026-03-01T10:00:00Z"),
		UpdatedAt:      timestamp("2026-04-01T12:00:00Z"),
		ClosedAt:       timestamp("2026-04-01T12:00:00Z"),
		MergedAt:       timestamp("2026-04-01T12:00:00Z"),
		User:           &gh.User{Login: p("author"), Name: p("Author Name")},
		Base:           &gh.PullRequestBranch{Ref: p("main"), SHA: p("base123")},
		Head:           &gh.PullRequestBranch{Ref: p("feature-x"), SHA: p("head456")},
		Labels:         []*gh.Label{{Name: p("enhancement")}},
	}

	repo := RepoRef{Owner: "owner", Repo: "repo", GitRemote: "https://github.com/owner/repo.git"}
	row := TransformPR(pr, nil, nil, nil, "diff content", repo, "host1")

	if row.ID != "owner/repo#42" {
		t.Errorf("ID = %q, want owner/repo#42", row.ID)
	}
	if row.State != "merged" {
		t.Errorf("State = %q, want merged", row.State)
	}
	if row.Title != "Add feature X" {
		t.Errorf("Title = %q", row.Title)
	}
	if row.AuthorLogin != "author" {
		t.Errorf("AuthorLogin = %q", row.AuthorLogin)
	}
	if row.Diff != "diff content" {
		t.Errorf("Diff = %q", row.Diff)
	}
	if row.Additions != 100 || row.Deletions != 20 {
		t.Errorf("Additions=%d, Deletions=%d", row.Additions, row.Deletions)
	}
	if row.Year != 2026 || row.Month != 4 {
		t.Errorf("Year=%d Month=%d, want 2026/4", row.Year, row.Month)
	}
	if row.CommentCount != 5 {
		t.Errorf("CommentCount = %d, want 5", row.CommentCount)
	}

	// Labels
	var labels []string
	json.Unmarshal([]byte(row.LabelsJSON), &labels)
	if len(labels) != 1 || labels[0] != "enhancement" {
		t.Errorf("Labels = %v", labels)
	}
}

func TestTransformPR_Reviewers(t *testing.T) {
	reviews := []*gh.PullRequestReview{
		{
			ID:    p(int64(1)),
			State: p("COMMENTED"),
			User:  &gh.User{Login: p("reviewer1"), Name: p("R1")},
		},
		{
			ID:    p(int64(2)),
			State: p("APPROVED"),
			User:  &gh.User{Login: p("reviewer1"), Name: p("R1")},
		},
		{
			ID:    p(int64(3)),
			State: p("CHANGES_REQUESTED"),
			User:  &gh.User{Login: p("reviewer2")},
		},
	}

	pr := &gh.PullRequest{
		Number:   p(1),
		MergedAt: timestamp("2026-04-01T12:00:00Z"),
		User:     &gh.User{Login: p("author")},
		Base:     &gh.PullRequestBranch{},
		Head:     &gh.PullRequestBranch{},
	}

	repo := RepoRef{Owner: "owner", Repo: "repo"}
	row := TransformPR(pr, reviews, nil, nil, "", repo, "host1")

	var reviewers []reviewerJSON
	json.Unmarshal([]byte(row.ReviewersJSON), &reviewers)

	if len(reviewers) != 2 {
		t.Fatalf("got %d reviewers, want 2", len(reviewers))
	}
	if reviewers[0].Login != "reviewer1" || reviewers[0].State != "APPROVED" {
		t.Errorf("reviewer1: %+v", reviewers[0])
	}
	if reviewers[1].Login != "reviewer2" || reviewers[1].State != "CHANGES_REQUESTED" {
		t.Errorf("reviewer2: %+v", reviewers[1])
	}
}

func TestTransformComments_AllTypes(t *testing.T) {
	pr := &gh.PullRequest{
		Number:   p(1),
		MergedAt: timestamp("2026-04-01T12:00:00Z"),
		User:     &gh.User{Login: p("author")},
		Base:     &gh.PullRequestBranch{},
		Head:     &gh.PullRequestBranch{},
	}

	reviews := []*gh.PullRequestReview{
		{
			ID:                p(int64(100)),
			State:             p("APPROVED"),
			Body:              p("LGTM"),
			User:              &gh.User{Login: p("reviewer")},
			AuthorAssociation: p("MEMBER"),
			SubmittedAt:       timestamp("2026-04-01T11:00:00Z"),
		},
	}

	reviewComments := []*gh.PullRequestComment{
		{
			ID:                  p(int64(200)),
			Body:                p("nit: rename this"),
			User:                &gh.User{Login: p("reviewer")},
			Path:                p("main.go"),
			Line:                p(42),
			Side:                p("RIGHT"),
			DiffHunk:            p("@@ -10,3 +10,4 @@"),
			PullRequestReviewID: p(int64(100)),
			CreatedAt:           timestamp("2026-04-01T11:00:00Z"),
			UpdatedAt:           timestamp("2026-04-01T11:00:00Z"),
		},
	}

	issueComments := []*gh.IssueComment{
		{
			ID:        p(int64(300)),
			Body:      p("Great work!"),
			User:      &gh.User{Login: p("commenter")},
			CreatedAt: timestamp("2026-04-01T11:30:00Z"),
			UpdatedAt: timestamp("2026-04-01T11:30:00Z"),
		},
	}

	repo := RepoRef{Owner: "owner", Repo: "repo"}
	comments := TransformComments(pr, reviews, reviewComments, issueComments, repo, "host1")

	if len(comments) != 3 {
		t.Fatalf("got %d comments, want 3", len(comments))
	}

	types := map[string]int{}
	for _, c := range comments {
		types[c.CommentType]++
	}
	if types["review"] != 1 || types["review_comment"] != 1 || types["issue_comment"] != 1 {
		t.Errorf("comment types: %v", types)
	}

	for _, c := range comments {
		if c.CommentType == "review_comment" {
			if c.Path != "main.go" {
				t.Errorf("Path = %q, want main.go", c.Path)
			}
			if c.Line != 42 {
				t.Errorf("Line = %d, want 42", c.Line)
			}
			if c.ReviewID != 100 {
				t.Errorf("ReviewID = %d, want 100", c.ReviewID)
			}
		}
		if c.CommentType == "review" {
			if c.ReviewState != "APPROVED" {
				t.Errorf("ReviewState = %q, want APPROVED", c.ReviewState)
			}
		}
	}
}

func TestTransformComments_Threading(t *testing.T) {
	pr := &gh.PullRequest{
		Number:   p(1),
		MergedAt: timestamp("2026-04-01T12:00:00Z"),
		User:     &gh.User{Login: p("author")},
		Base:     &gh.PullRequestBranch{},
		Head:     &gh.PullRequestBranch{},
	}

	reviewComments := []*gh.PullRequestComment{
		{
			ID:        p(int64(200)),
			Body:      p("original comment"),
			User:      &gh.User{Login: p("reviewer")},
			CreatedAt: timestamp("2026-04-01T11:00:00Z"),
			UpdatedAt: timestamp("2026-04-01T11:00:00Z"),
		},
		{
			ID:        p(int64(201)),
			Body:      p("reply"),
			User:      &gh.User{Login: p("author")},
			InReplyTo: p(int64(200)),
			CreatedAt: timestamp("2026-04-01T11:05:00Z"),
			UpdatedAt: timestamp("2026-04-01T11:05:00Z"),
		},
	}

	repo := RepoRef{Owner: "owner", Repo: "repo"}
	comments := TransformComments(pr, nil, reviewComments, nil, repo, "host1")

	if len(comments) != 2 {
		t.Fatalf("got %d comments, want 2", len(comments))
	}
	if comments[0].InReplyToID != 0 {
		t.Errorf("first comment InReplyToID = %d, want 0", comments[0].InReplyToID)
	}
	if comments[1].InReplyToID != 200 {
		t.Errorf("reply InReplyToID = %d, want 200", comments[1].InReplyToID)
	}
}

func TestTransformPR_StateNormalization(t *testing.T) {
	pr := &gh.PullRequest{
		Number:   p(1),
		State:    p("closed"),
		MergedAt: timestamp("2026-04-01T12:00:00Z"),
		User:     &gh.User{Login: p("author")},
		Base:     &gh.PullRequestBranch{},
		Head:     &gh.PullRequestBranch{},
	}

	repo := RepoRef{Owner: "owner", Repo: "repo"}
	row := TransformPR(pr, nil, nil, nil, "", repo, "host1")

	if row.State != "merged" {
		t.Errorf("State = %q, want 'merged' (normalized)", row.State)
	}
}

func TestTransformPR_CheckRuns(t *testing.T) {
	checkRuns := []*gh.CheckRun{
		{Name: p("CI"), Status: p("completed"), Conclusion: p("success")},
		{Name: p("Lint"), Status: p("completed"), Conclusion: p("failure")},
	}

	pr := &gh.PullRequest{
		Number:   p(1),
		MergedAt: timestamp("2026-04-01T12:00:00Z"),
		User:     &gh.User{Login: p("author")},
		Base:     &gh.PullRequestBranch{},
		Head:     &gh.PullRequestBranch{},
	}

	repo := RepoRef{Owner: "owner", Repo: "repo"}
	row := TransformPR(pr, nil, checkRuns, nil, "", repo, "host1")

	var checks []checkRunJSON
	json.Unmarshal([]byte(row.ChecksJSON), &checks)
	if len(checks) != 2 {
		t.Fatalf("got %d checks, want 2", len(checks))
	}
	if checks[0].Name != "CI" || checks[0].Conclusion != "success" {
		t.Errorf("check 0: %+v", checks[0])
	}
}

func TestTransformPR_Files(t *testing.T) {
	files := []*gh.CommitFile{
		{Filename: p("main.go"), Status: p("modified"), Additions: p(10), Deletions: p(2), Patch: p("@@ diff")},
		{Filename: p("image.png"), Status: p("added"), Additions: p(0), Deletions: p(0)},
	}

	pr := &gh.PullRequest{
		Number:   p(1),
		MergedAt: timestamp("2026-04-01T12:00:00Z"),
		User:     &gh.User{Login: p("author")},
		Base:     &gh.PullRequestBranch{},
		Head:     &gh.PullRequestBranch{},
	}

	repo := RepoRef{Owner: "owner", Repo: "repo"}
	row := TransformPR(pr, nil, nil, files, "", repo, "host1")

	var parsedFiles []fileJSON
	json.Unmarshal([]byte(row.FilesJSON), &parsedFiles)
	if len(parsedFiles) != 2 {
		t.Fatalf("got %d files, want 2", len(parsedFiles))
	}
	if parsedFiles[0].Patch != "@@ diff" {
		t.Errorf("file 0 patch = %q", parsedFiles[0].Patch)
	}
	if parsedFiles[1].Patch != "" {
		t.Errorf("binary file should have empty patch, got %q", parsedFiles[1].Patch)
	}
}
