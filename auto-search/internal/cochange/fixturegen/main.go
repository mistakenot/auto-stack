// Command fixturegen regenerates the checked-in auto-stack co-change snapshot
// fixture and verifies its privacy guarantees.
//
// Generation (default mode):
//  1. Runs `autoetl run --repo-path <repo> --output <tmp_out> --only git` under an
//     isolated temp HOME so the git sync-state resolves to an empty location,
//     guaranteeing a FULL, deterministic extraction and never touching the
//     developer's real ~/.auto.
//  2. Reads the temp output parquet for commits, commit_files, git_repositories
//     and git_refs, projecting to the AC-16 RETAINED columns only.
//  3. Sorts rows by a stable key and writes
//     auto-search/testdata/fixtures/auto-stack-snapshot/<dataset>/<dataset>.parquet
//     with deterministic writer options.
//  4. Writes SHA.txt with `git rev-parse HEAD`.
//  5. Deletes the temp dirs.
//
// Verify mode (-verify):
//
//	Inspects each fixture parquet's schema (column list) and the fixture
//	directory layout, asserting that NONE of the forbidden datasets
//	(messages/, sessions/, commit_hunks/) or forbidden columns
//	(diff, diff_truncated on commit_files; message, trailers_json on commits)
//	appear. Fails loudly naming the offending dataset/column (AC-20).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/parquet-go/parquet-go"
)

// ---- AC-16 retained-column fixture structs ------------------------------
//
// These are WIDER than the slim readers in internal/etlscan: the fixture
// writer must retain every AC-16 column for each dataset so future commands
// can read the snapshot without regenerating. Tag names/types mirror
// auto-etl/internal/model/git.go exactly so the projection round-trips real
// production parquet.

// CommitFixture retains the AC-16 commits columns.
type CommitFixture struct {
	ID                  string `parquet:"id"`
	ShortID             string `parquet:"short_id"`
	RepoID              string `parquet:"repo_id,dict"`
	TreeSHA             string `parquet:"tree_sha"`
	AuthorName          string `parquet:"author_name,dict"`
	AuthorEmail         string `parquet:"author_email,dict"`
	AuthorDate          int64  `parquet:"author_date"`
	AuthorDateOffset    string `parquet:"author_date_offset"`
	CommitterName       string `parquet:"committer_name,dict"`
	CommitterEmail      string `parquet:"committer_email,dict"`
	CommitterDate       int64  `parquet:"committer_date"`
	CommitterDateOffset string `parquet:"committer_date_offset"`
	MessageTruncated    string `parquet:"message_truncated"`
	IsMerge             bool   `parquet:"is_merge"`
	ParentCount         int32  `parquet:"parent_count"`
	ParentSHAs          string `parquet:"parent_shas"`
	FilesChanged        int32  `parquet:"files_changed"`
	Insertions          int32  `parquet:"insertions"`
	Deletions           int32  `parquet:"deletions"`
	SessionID           string `parquet:"session_id,dict"`
	PatchID             string `parquet:"patch_id"`
	Year                int32  `parquet:"year"`
	Month               int32  `parquet:"month"`
	SchemaVersion       int32  `parquet:"schema_version"`
}

// CommitFileFixture retains the AC-16 commit_files columns.
type CommitFileFixture struct {
	CommitID      string `parquet:"commit_id"`
	RepoID        string `parquet:"repo_id,dict"`
	FileIndex     int32  `parquet:"file_index"`
	FilePath      string `parquet:"file_path,dict"`
	ChangeType    string `parquet:"change_type,dict"`
	OldPath       string `parquet:"old_path,dict"`
	Insertions    int32  `parquet:"insertions"`
	Deletions     int32  `parquet:"deletions"`
	IsBinary      bool   `parquet:"is_binary"`
	AuthorDate    int64  `parquet:"author_date"`
	Year          int32  `parquet:"year"`
	Month         int32  `parquet:"month"`
	SchemaVersion int32  `parquet:"schema_version"`
}

// GitRepositoryFixture retains the AC-16 git_repositories columns.
type GitRepositoryFixture struct {
	RepoID                string `parquet:"repo_id,dict"`
	RepoRemote            string `parquet:"repo_remote"`
	RepoRemoteNormalized  string `parquet:"repo_remote_normalized"`
	DefaultBranchObserved string `parquet:"default_branch_observed,dict"`
	SchemaVersion         int32  `parquet:"schema_version"`
}

// GitRefFixture retains the AC-16 git_refs columns (id intentionally dropped).
type GitRefFixture struct {
	RepoID        string `parquet:"repo_id,dict"`
	RefName       string `parquet:"ref_name,dict"`
	RefType       string `parquet:"ref_type,dict"`
	CommitID      string `parquet:"commit_id"`
	IsDefault     bool   `parquet:"is_default"`
	IsRemote      bool   `parquet:"is_remote"`
	SchemaVersion int32  `parquet:"schema_version"`
}

// ---- Production source structs (full schema) ----------------------------
//
// parquet-go prunes to the named columns, so these declare only the columns
// we read out of the production parquet. They are the union of every column
// any fixture struct needs.

type srcCommit struct {
	ID                  string `parquet:"id"`
	ShortID             string `parquet:"short_id"`
	RepoID              string `parquet:"repo_id,dict"`
	TreeSHA             string `parquet:"tree_sha"`
	AuthorName          string `parquet:"author_name,dict"`
	AuthorEmail         string `parquet:"author_email,dict"`
	AuthorDate          int64  `parquet:"author_date"`
	AuthorDateOffset    string `parquet:"author_date_offset"`
	CommitterName       string `parquet:"committer_name,dict"`
	CommitterEmail      string `parquet:"committer_email,dict"`
	CommitterDate       int64  `parquet:"committer_date"`
	CommitterDateOffset string `parquet:"committer_date_offset"`
	MessageTruncated    string `parquet:"message_truncated"`
	IsMerge             bool   `parquet:"is_merge"`
	ParentCount         int32  `parquet:"parent_count"`
	ParentSHAs          string `parquet:"parent_shas"`
	FilesChanged        int32  `parquet:"files_changed"`
	Insertions          int32  `parquet:"insertions"`
	Deletions           int32  `parquet:"deletions"`
	SessionID           string `parquet:"session_id,dict"`
	PatchID             string `parquet:"patch_id"`
	Year                int32  `parquet:"year"`
	Month               int32  `parquet:"month"`
	SchemaVersion       int32  `parquet:"schema_version"`
}

type srcCommitFile struct {
	CommitID      string `parquet:"commit_id"`
	RepoID        string `parquet:"repo_id,dict"`
	FileIndex     int32  `parquet:"file_index"`
	FilePath      string `parquet:"file_path,dict"`
	ChangeType    string `parquet:"change_type,dict"`
	OldPath       string `parquet:"old_path,dict"`
	Insertions    int32  `parquet:"insertions"`
	Deletions     int32  `parquet:"deletions"`
	IsBinary      bool   `parquet:"is_binary"`
	AuthorDate    int64  `parquet:"author_date"`
	Year          int32  `parquet:"year"`
	Month         int32  `parquet:"month"`
	SchemaVersion int32  `parquet:"schema_version"`
}

type srcGitRepo struct {
	RepoID                string `parquet:"repo_id,dict"`
	RepoRemote            string `parquet:"repo_remote"`
	RepoRemoteNormalized  string `parquet:"repo_remote_normalized"`
	DefaultBranchObserved string `parquet:"default_branch_observed,dict"`
	SchemaVersion         int32  `parquet:"schema_version"`
}

type srcGitRef struct {
	RepoID        string `parquet:"repo_id,dict"`
	RefName       string `parquet:"ref_name,dict"`
	RefType       string `parquet:"ref_type,dict"`
	CommitID      string `parquet:"commit_id"`
	IsDefault     bool   `parquet:"is_default"`
	IsRemote      bool   `parquet:"is_remote"`
	SchemaVersion int32  `parquet:"schema_version"`
}

func main() {
	var (
		repoFlag   = flag.String("repo", "", "repo root to snapshot (defaults to git toplevel of cwd)")
		outFlag    = flag.String("out", "", "fixture output dir (defaults to <repo>/auto-search/testdata/fixtures/auto-stack-snapshot)")
		verifyFlag = flag.Bool("verify", false, "verify mode: privacy-guard the existing fixture instead of regenerating")
	)
	flag.Parse()

	repoRoot, err := resolveRepoRoot(*repoFlag)
	if err != nil {
		fatal(err)
	}
	outDir := *outFlag
	if outDir == "" {
		outDir = filepath.Join(repoRoot, "auto-search", "testdata", "fixtures", "auto-stack-snapshot")
	}

	if *verifyFlag {
		if err := verifyFixture(outDir); err != nil {
			fatal(err)
		}
		fmt.Println("privacy guard: OK")
		return
	}

	if err := generate(repoRoot, outDir); err != nil {
		fatal(err)
	}
	fmt.Printf("fixture written to %s\n", outDir)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fixturegen:", err)
	os.Exit(1)
}

// resolveRepoRoot returns the absolute repo root, defaulting to the git
// toplevel of the current working directory.
func resolveRepoRoot(repoFlag string) (string, error) {
	if repoFlag != "" {
		abs, err := filepath.Abs(repoFlag)
		if err != nil {
			return "", fmt.Errorf("resolve -repo: %w", err)
		}
		return abs, nil
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// generate runs the full regeneration pipeline (steps 1-5).
func generate(repoRoot, outDir string) error {
	tmpHome, err := os.MkdirTemp("", "fixturegen-home-")
	if err != nil {
		return fmt.Errorf("mktemp home: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpHome) }()

	tmpOut, err := os.MkdirTemp("", "fixturegen-out-")
	if err != nil {
		return fmt.Errorf("mktemp out: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpOut) }()

	// Step 1: run autoetl under an isolated HOME so GitSyncStatePath() resolves
	// to an empty temp location -> guaranteed full deterministic extraction.
	if err := runAutoetl(repoRoot, tmpOut, tmpHome); err != nil {
		return fmt.Errorf("run autoetl: %w", err)
	}

	// Step 2-3: read, project, sort, write each dataset.
	if err := writeCommits(tmpOut, outDir); err != nil {
		return err
	}
	if err := writeCommitFiles(tmpOut, outDir); err != nil {
		return err
	}
	if err := writeGitRepos(tmpOut, outDir); err != nil {
		return err
	}
	if err := writeGitRefs(tmpOut, outDir); err != nil {
		return err
	}

	// Step 4: SHA.txt.
	sha, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	shaPath := filepath.Join(outDir, "SHA.txt")
	if err := os.WriteFile(shaPath, []byte(strings.TrimSpace(string(sha))+"\n"), 0o644); err != nil {
		return fmt.Errorf("write SHA.txt: %w", err)
	}

	// Step 5: temp dirs removed by deferred RemoveAll.
	return nil
}

// runAutoetl invokes the autoetl ETL from source under an isolated HOME.
func runAutoetl(repoRoot, tmpOut, tmpHome string) error {
	etlDir := filepath.Join(repoRoot, "auto-etl")
	cmd := exec.Command("go", "run", ".",
		"run",
		"--repo-path", repoRoot,
		"--output", tmpOut,
		"--only", "git",
	)
	cmd.Dir = etlDir
	// Isolate HOME so the real ~/.auto is neither read nor mutated and the git
	// sync-state resolves to an empty location (full extraction).
	cmd.Env = append(filteredEnv(), "HOME="+tmpHome)
	cmd.Stdout = os.Stderr // diagnostics to stderr; keep our stdout clean
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// filteredEnv returns the current environment with any existing HOME removed so
// the caller's HOME override is authoritative.
func filteredEnv() []string {
	env := os.Environ()
	out := env[:0]
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ---- per-dataset projection + write -------------------------------------

func writeCommits(tmpOut, outDir string) error {
	rows, err := readDataset[srcCommit](tmpOut, "commits")
	if err != nil {
		return fmt.Errorf("read commits: %w", err)
	}
	out := make([]CommitFixture, 0, len(rows))
	for i := range rows {
		out = append(out, CommitFixture(rows[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return writeFixtureParquet(filepath.Join(outDir, "commits", "commits.parquet"), out)
}

func writeCommitFiles(tmpOut, outDir string) error {
	rows, err := readDataset[srcCommitFile](tmpOut, "commit_files")
	if err != nil {
		return fmt.Errorf("read commit_files: %w", err)
	}
	out := make([]CommitFileFixture, 0, len(rows))
	for i := range rows {
		out = append(out, CommitFileFixture(rows[i]))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CommitID != out[j].CommitID {
			return out[i].CommitID < out[j].CommitID
		}
		return out[i].FileIndex < out[j].FileIndex
	})
	return writeFixtureParquet(filepath.Join(outDir, "commit_files", "commit_files.parquet"), out)
}

func writeGitRepos(tmpOut, outDir string) error {
	rows, err := readDataset[srcGitRepo](tmpOut, "git_repositories")
	if err != nil {
		return fmt.Errorf("read git_repositories: %w", err)
	}
	out := make([]GitRepositoryFixture, 0, len(rows))
	for _, r := range rows {
		out = append(out, GitRepositoryFixture(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RepoID < out[j].RepoID })
	return writeFixtureParquet(filepath.Join(outDir, "git_repositories", "git_repositories.parquet"), out)
}

func writeGitRefs(tmpOut, outDir string) error {
	rows, err := readDataset[srcGitRef](tmpOut, "git_refs")
	if err != nil {
		return fmt.Errorf("read git_refs: %w", err)
	}
	out := make([]GitRefFixture, 0, len(rows))
	for _, r := range rows {
		out = append(out, GitRefFixture(r))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RefName != out[j].RefName {
			return out[i].RefName < out[j].RefName
		}
		return out[i].CommitID < out[j].CommitID
	})
	return writeFixtureParquet(filepath.Join(outDir, "git_refs", "git_refs.parquet"), out)
}

// readDataset reads all parquet files under <tmpOut>/<dataset>/** (recursively,
// across Hive partitions) and concatenates the projected rows.
func readDataset[T any](tmpOut, dataset string) ([]T, error) {
	datasetDir := filepath.Join(tmpOut, dataset)
	var files []string
	err := filepath.Walk(datasetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".parquet") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", datasetDir, err)
	}
	// Sort file list for stable read order before the final stable sort.
	sort.Strings(files)

	var all []T
	for _, p := range files {
		rows, err := readParquetFile[T](p)
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
	}
	return all, nil
}

func readParquetFile[T any](path string) ([]T, error) {
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

// writeFixtureParquet writes rows to a single parquet file with deterministic
// writer options (no embedded created-by/timestamp metadata).
func writeFixtureParquet[T any](path string, rows []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	// Pin a fixed created-by string so the writer does not embed a version that
	// could drift; everything else about the layout is deterministic given a
	// fixed row order.
	w := parquet.NewGenericWriter[T](f, parquet.CreatedBy("auto-stack-fixturegen", "", ""))
	if _, err := w.Write(rows); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

// ---- privacy guard (AC-20) ----------------------------------------------

func verifyFixture(outDir string) error {
	// Forbidden dataset directories anywhere under the fixture root.
	forbiddenDirs := []string{"messages", "sessions", "commit_hunks"}
	var problems []string

	err := filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			for _, fd := range forbiddenDirs {
				if info.Name() == fd {
					problems = append(problems, "forbidden dataset directory present: "+path)
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk fixture root %s: %w", outDir, err)
	}

	// Forbidden columns, checked via parquet schema introspection per dataset.
	forbiddenCols := map[string][]string{
		"commit_files": {"diff", "diff_truncated"},
		"commits":      {"message", "trailers_json"},
	}
	for dataset, banned := range forbiddenCols {
		datasetDir := filepath.Join(outDir, dataset)
		if _, statErr := os.Stat(datasetDir); os.IsNotExist(statErr) {
			continue
		}
		walkErr := filepath.Walk(datasetDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(info.Name(), ".parquet") {
				return nil
			}
			cols, colErr := parquetColumns(path)
			if colErr != nil {
				return colErr
			}
			colSet := make(map[string]bool, len(cols))
			for _, c := range cols {
				colSet[c] = true
			}
			for _, b := range banned {
				if colSet[b] {
					problems = append(problems, fmt.Sprintf("forbidden column %q present in %s parquet: %s", b, dataset, path))
				}
			}
			return nil
		})
		if walkErr != nil {
			return fmt.Errorf("walk %s: %w", datasetDir, walkErr)
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("privacy guard FAILED:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// parquetColumns returns the top-level column (leaf path root) names of a
// parquet file via schema introspection.
func parquetColumns(path string) ([]string, error) {
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
	var cols []string
	for _, field := range pf.Schema().Fields() {
		cols = append(cols, field.Name())
	}
	return cols, nil
}
