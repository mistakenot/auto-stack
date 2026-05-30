package etlscan

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/parquet-go/parquet-go"
)

// The slim git structs below are column-pruned projections of the canonical
// schema in auto-etl/internal/model/git.go. parquet-go reads only the columns
// named by the parquet tags present on the struct, so declaring a subset prunes
// the rest at read time. The tag names (and dict encoding hints) MUST match the
// production schema EXACTLY so these readers work against real ETL output with
// no fixture-only branches.

// CommitSlim is a column-pruned projection of model.Commit covering the columns
// the co-change query needs.
type CommitSlim struct {
	ID               string `parquet:"id"`
	ShortID          string `parquet:"short_id"`
	RepoID           string `parquet:"repo_id,dict"`
	AuthorName       string `parquet:"author_name,dict"`
	AuthorEmail      string `parquet:"author_email,dict"`
	AuthorDate       int64  `parquet:"author_date"`
	FilesChanged     int32  `parquet:"files_changed"`
	SessionID        string `parquet:"session_id,dict"`
	MessageTruncated string `parquet:"message_truncated"`
}

// CommitFileSlim is a column-pruned projection of model.CommitFile covering the
// columns the co-change query needs.
type CommitFileSlim struct {
	CommitID   string `parquet:"commit_id"`
	RepoID     string `parquet:"repo_id,dict"`
	FilePath   string `parquet:"file_path,dict"`
	ChangeType string `parquet:"change_type,dict"`
	OldPath    string `parquet:"old_path,dict"`
	AuthorDate int64  `parquet:"author_date"`
}

// GitRepoSlim is a column-pruned projection of model.GitRepository covering the
// columns repo resolution needs.
type GitRepoSlim struct {
	RepoID               string `parquet:"repo_id,dict"`
	RepoRemoteNormalized string `parquet:"repo_remote_normalized"`
}

// GitRefSlim is a column-pruned projection of model.GitRef covering the columns
// the ref-tip join needs.
type GitRefSlim struct {
	RepoID    string `parquet:"repo_id,dict"`
	RefName   string `parquet:"ref_name,dict"`
	RefType   string `parquet:"ref_type,dict"`
	CommitID  string `parquet:"commit_id"`
	IsDefault bool   `parquet:"is_default"`
}

// readParquet reads all rows of type T from the parquet file at path, using the
// batched generic-reader pattern. Only the columns named by T's parquet tags are
// read (column pruning).
func readParquet[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	pf, err := parquet.OpenFile(f, stat.Size())
	if err != nil {
		return nil, fmt.Errorf("open parquet %s: %w", path, err)
	}

	reader := parquet.NewGenericReader[T](pf)
	defer func() { _ = reader.Close() }()

	var all []T
	batch := make([]T, 1024)
	for {
		n, err := reader.Read(batch)
		if n > 0 {
			all = append(all, batch[:n]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	}
	return all, nil
}

// ReadCommitsSlim reads all commit rows (column-pruned) from the parquet file at path.
func ReadCommitsSlim(path string) ([]CommitSlim, error) {
	return readParquet[CommitSlim](path)
}

// ReadCommitFilesSlim reads all commit-file rows (column-pruned) from the parquet file at path.
func ReadCommitFilesSlim(path string) ([]CommitFileSlim, error) {
	return readParquet[CommitFileSlim](path)
}

// ReadGitRepos reads all git-repository rows (column-pruned) from the parquet file at path.
func ReadGitRepos(path string) ([]GitRepoSlim, error) {
	return readParquet[GitRepoSlim](path)
}

// ReadGitRefs reads all git-ref rows (column-pruned) from the parquet file at path.
func ReadGitRefs(path string) ([]GitRefSlim, error) {
	return readParquet[GitRefSlim](path)
}
