package model

// GitRepository represents a discovered git repository.
// One row per repo, keyed by repo_id.
type GitRepository struct {
	RepoID                string `parquet:"repo_id,dict"`
	RepoRemote            string `parquet:"repo_remote"`
	RepoRemoteNormalized  string `parquet:"repo_remote_normalized"`
	RepoPath              string `parquet:"repo_path"`
	WorktreePath          string `parquet:"worktree_path"`
	DefaultBranchObserved string `parquet:"default_branch_observed,dict"`
	HostID                string `parquet:"host_id,dict"`

	// Timestamps (Unix milliseconds)
	FirstSeenAt int64 `parquet:"first_seen_at"`
	LastSeenAt  int64 `parquet:"last_seen_at"`

	ETLRunID      string `parquet:"etl_run_id,dict"`
	CollectedAt   int64  `parquet:"collected_at"`
	SchemaVersion int32  `parquet:"schema_version"`
}

// GitRef represents a branch or tag reference in a repository.
// One row per ref, keyed by id.
type GitRef struct {
	ID        string `parquet:"id"`
	RepoID    string `parquet:"repo_id,dict"`
	RefName   string `parquet:"ref_name,dict"`
	RefType   string `parquet:"ref_type,dict"`
	CommitID  string `parquet:"commit_id"`
	IsDefault bool   `parquet:"is_default"`
	IsRemote  bool   `parquet:"is_remote"`

	ETLRunID      string `parquet:"etl_run_id,dict"`
	CollectedAt   int64  `parquet:"collected_at"`
	SchemaVersion int32  `parquet:"schema_version"`
}

// Commit represents a single git commit.
// One row per commit, partitioned by author_date month.
type Commit struct {
	ID      string `parquet:"id"`
	ShortID string `parquet:"short_id"`
	RepoID  string `parquet:"repo_id,dict"`
	TreeSHA string `parquet:"tree_sha"`

	// Author
	AuthorName       string `parquet:"author_name,dict"`
	AuthorEmail      string `parquet:"author_email,dict"`
	AuthorDate       int64  `parquet:"author_date"`
	AuthorDateOffset string `parquet:"author_date_offset"`

	// Committer
	CommitterName       string `parquet:"committer_name,dict"`
	CommitterEmail      string `parquet:"committer_email,dict"`
	CommitterDate       int64  `parquet:"committer_date"`
	CommitterDateOffset string `parquet:"committer_date_offset"`

	// Content
	Message          string `parquet:"message"`
	MessageTruncated string `parquet:"message_truncated"`

	// Merge info
	IsMerge     bool   `parquet:"is_merge"`
	ParentCount int32  `parquet:"parent_count"`
	ParentSHAs  string `parquet:"parent_shas"`

	// Diff stats
	FilesChanged int32 `parquet:"files_changed"`
	Insertions   int32 `parquet:"insertions"`
	Deletions    int32 `parquet:"deletions"`

	// Metadata
	TrailersJSON string `parquet:"trailers_json"`
	PatchID      string `parquet:"patch_id"`

	ETLRunID      string `parquet:"etl_run_id,dict"`
	CollectedAt   int64  `parquet:"collected_at"`
	Year          int32  `parquet:"year"`
	Month         int32  `parquet:"month"`
	SchemaVersion int32  `parquet:"schema_version"`
}

// CommitFile represents a single file changed in a commit.
// One row per file per commit.
type CommitFile struct {
	ID        string `parquet:"id"`
	CommitID  string `parquet:"commit_id"`
	RepoID    string `parquet:"repo_id,dict"`
	FileIndex int32  `parquet:"file_index"`
	FilePath  string `parquet:"file_path,dict"`

	ChangeType string `parquet:"change_type,dict"`
	OldPath    string `parquet:"old_path,dict"`

	Insertions int32 `parquet:"insertions"`
	Deletions  int32 `parquet:"deletions"`

	OldBlobSHA string `parquet:"old_blob_sha"`
	NewBlobSHA string `parquet:"new_blob_sha"`
	OldMode    string `parquet:"old_mode"`
	NewMode    string `parquet:"new_mode"`
	IsBinary   bool   `parquet:"is_binary"`

	Diff          string `parquet:"diff"`
	DiffTruncated string `parquet:"diff_truncated"`

	// Denormalized from commit
	AuthorDate int64 `parquet:"author_date"`

	ETLRunID      string `parquet:"etl_run_id,dict"`
	CollectedAt   int64  `parquet:"collected_at"`
	Year          int32  `parquet:"year"`
	Month         int32  `parquet:"month"`
	SchemaVersion int32  `parquet:"schema_version"`
}

// CommitHunk represents a single diff hunk within a file change.
// One row per hunk per file per commit.
type CommitHunk struct {
	ID        string `parquet:"id"`
	CommitID  string `parquet:"commit_id"`
	RepoID    string `parquet:"repo_id,dict"`
	FileIndex int32  `parquet:"file_index"`
	HunkIndex int32  `parquet:"hunk_index"`
	FilePath  string `parquet:"file_path,dict"`
	OldPath   string `parquet:"old_path,dict"`

	// Hunk range
	OldStart int32 `parquet:"old_start"`
	OldLines int32 `parquet:"old_lines"`
	NewStart int32 `parquet:"new_start"`
	NewLines int32 `parquet:"new_lines"`

	// Content
	HunkHeader        string `parquet:"hunk_header"`
	HunkText          string `parquet:"hunk_text"`
	HunkTextTruncated string `parquet:"hunk_text_truncated"`
	HunkHash          string `parquet:"hunk_hash"`

	// Denormalized from commit
	AuthorDate int64 `parquet:"author_date"`

	ETLRunID      string `parquet:"etl_run_id,dict"`
	CollectedAt   int64  `parquet:"collected_at"`
	Year          int32  `parquet:"year"`
	Month         int32  `parquet:"month"`
	SchemaVersion int32  `parquet:"schema_version"`
}

// GitETLResult holds the output of the git history ETL phase.
type GitETLResult struct {
	Repositories []GitRepository
	Refs         []GitRef
	Commits      []Commit
	Files        []CommitFile
	Hunks        []CommitHunk
}
