package model

// PullRequest represents a merged GitHub pull request.
// One row per PR, partitioned by merged_at month.
type PullRequest struct {
	// Identity
	ID     string `parquet:"id"` // "{owner}/{repo}#{number}"
	Owner  string `parquet:"owner,dict"`
	Repo   string `parquet:"repo,dict"`
	Number int32  `parquet:"number"`

	// Metadata
	Title          string `parquet:"title"`
	Body           string `parquet:"body"`
	State          string `parquet:"state,dict"` // Always "merged" (normalized)
	Draft          bool   `parquet:"draft"`
	BaseBranch     string `parquet:"base_branch,dict"`
	HeadBranch     string `parquet:"head_branch,dict"`
	BaseSHA        string `parquet:"base_sha"`
	HeadSHA        string `parquet:"head_sha"`
	MergeCommitSHA string `parquet:"merge_commit_sha"`

	// Author
	AuthorLogin       string `parquet:"author_login,dict"`
	AuthorDisplayName string `parquet:"author_display_name"`

	// Reviewers (JSON array of {login, display_name, state} objects)
	ReviewersJSON string `parquet:"reviewers_json"`

	// Labels (JSON array of strings)
	LabelsJSON string `parquet:"labels_json"`

	// CI / checks (JSON array of {name, status, conclusion} objects)
	ChecksJSON string `parquet:"checks_json"`

	// Full PR diff (unified diff format)
	Diff string `parquet:"diff"`

	// Changed files with per-file patches (JSON array of {filename, status, additions, deletions, patch} objects)
	FilesJSON string `parquet:"files_json"`

	// Counts
	Additions    int32 `parquet:"additions"`
	Deletions    int32 `parquet:"deletions"`
	ChangedFiles int32 `parquet:"changed_files"`
	CommentCount int32 `parquet:"comment_count"`
	CommitCount  int32 `parquet:"commit_count"`

	// Timestamps (Unix milliseconds)
	CreatedAt int64 `parquet:"created_at"`
	UpdatedAt int64 `parquet:"updated_at"`
	ClosedAt  int64 `parquet:"closed_at"`
	MergedAt  int64 `parquet:"merged_at"`

	// Linkage
	GitRemote string `parquet:"git_remote,dict"`
	HostID    string `parquet:"host_id,dict"`

	// Partition
	Year          int32 `parquet:"year"`
	Month         int32 `parquet:"month"`
	SchemaVersion int32 `parquet:"schema_version"`
}

// PRComment represents a comment on a pull request.
// One row per comment, with comment_type discriminator.
type PRComment struct {
	// Identity
	ID          string `parquet:"id"`             // "{owner}/{repo}#{pr_number}/c/{comment_id}"
	PRID        string `parquet:"pr_id,dict"`     // FK to PullRequest.ID
	CommentID   int64  `parquet:"comment_id"`     // GitHub's numeric comment ID
	InReplyToID int64  `parquet:"in_reply_to_id"` // For threading; 0 if top-level

	// Type: "review", "review_comment", "issue_comment"
	CommentType string `parquet:"comment_type,dict"`

	// Content
	Body string `parquet:"body"`

	// Author
	AuthorLogin       string `parquet:"author_login,dict"`
	AuthorDisplayName string `parquet:"author_display_name"`
	AuthorAssociation string `parquet:"author_association,dict"` // MEMBER, CONTRIBUTOR, etc.

	// Code location (populated for review_comment type)
	Path         string `parquet:"path,dict"`
	DiffHunk     string `parquet:"diff_hunk"`
	CommitSHA    string `parquet:"commit_sha"`
	OriginalLine int32  `parquet:"original_line"`
	Line         int32  `parquet:"line"`
	Side         string `parquet:"side,dict"`
	StartLine    int32  `parquet:"start_line"`
	StartSide    string `parquet:"start_side,dict"`

	// Review context
	ReviewID    int64  `parquet:"review_id"`
	ReviewState string `parquet:"review_state,dict"` // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED

	// Timestamps (Unix milliseconds)
	CreatedAt int64 `parquet:"created_at"`
	UpdatedAt int64 `parquet:"updated_at"`

	// Denormalized from PR
	Owner     string `parquet:"owner,dict"`
	Repo      string `parquet:"repo,dict"`
	PRNumber  int32  `parquet:"pr_number"`
	GitRemote string `parquet:"git_remote,dict"`
	HostID    string `parquet:"host_id,dict"`

	// Partition
	Year          int32 `parquet:"year"`
	Month         int32 `parquet:"month"`
	SchemaVersion int32 `parquet:"schema_version"`
}

// GitHubSyncResult holds the output of the GitHub sync phase.
type GitHubSyncResult struct {
	PullRequests []PullRequest
	Comments     []PRComment
}
