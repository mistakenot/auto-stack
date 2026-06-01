// Package scenariofixture builds ephemeral git-dataset parquet fixtures from
// compact, human-authored JSON scenarios so co-change behaviour can be exercised
// end-to-end against real parquet — including coupling shapes the checked-in
// live snapshot cannot reproduce (hot files, deliberate cross-dir coupling,
// large commits, no-history / insufficient-history seeds).
//
// fixturegen (internal/cochange/fixturegen) is `package main` and therefore not
// importable, so this sibling package re-hosts the same four parquet struct
// shapes (CommitFixture, CommitFileFixture, GitRepositoryFixture, GitRefFixture)
// and the parquet-go writer wiring. The struct tags mirror the canonical schema
// in auto-etl/internal/model/git.go exactly so the slim etlscan readers
// (ReadCommitsSlim / ReadCommitFilesSlim / ReadGitRepos / ReadGitRefs) and the
// cochange engine consume the output with no fixture-only branches.
//
// # Scenario JSON schema
//
// A scenario is a flat JSON object:
//
//	{
//	  "repo_id":       "fixture-repo",                 // string: repo id stamped on every row
//	  "origin_remote": "https://github.com/x/y.git",   // string: repo_remote (normalized form too)
//	  "commits": [
//	    {
//	      "sha":             "a1b2c3d4",                 // string: raw commit sha (any unique id)
//	      "author_name":     "Dev",                      // string
//	      "author_email":    "dev@example.com",          // string
//	      "author_date_iso": "2026-01-15",               // string: RFC3339 or YYYY-MM-DD
//	      "subject":         "wire up the thing",        // string: commit subject (message_truncated)
//	      "files": [
//	        {"path": "src/a/hot.go", "change_type": "M"},          // change_type: A/M/D/R...
//	        {"path": "src/a/new.go", "change_type": "R", "old_path": "src/a/old.go"}
//	      ]
//	    }
//	  ],
//	  "refs": [
//	    {"ref_name": "main", "ref_type": "branch", "commit_sha": "a1b2c3d4", "is_default": true}
//	  ]
//	}
//
// The helper expands each commit's files into commit_files rows and computes
// derived fields:
//
//   - CommitFixture.ID and CommitFileFixture.CommitID are "<repo_id>-<sha>"
//     (the cochange engine joins commits.id to commit_files.commit_id and strips
//     the "<repo_id>-" prefix when matching git_refs.commit_id, which is the RAW
//     sha — so GitRefFixture.CommitID is the raw "<commit_sha>").
//   - FilesChanged = len(files).
//   - author_date_iso is parsed to a unix-millisecond timestamp (UTC); Year and
//     Month are derived from the parsed date.
//
// # Fixture privacy guard
//
// Scenario JSON lives under internal/cochange/testdata/scenarios/ and is written
// to t.TempDir() at test time. It is intentionally NOT placed under
// testdata/fixtures/auto-stack-snapshot/, so `make verify-fixtures` (which
// privacy-guards and size-budgets only the live snapshot directory) never picks
// these synthetic scenarios up.
package scenariofixture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

// ---- parquet struct shapes (copied from fixturegen/main.go) --------------
//
// These mirror auto-etl/internal/model/git.go exactly so the etlscan slim
// readers and the cochange engine round-trip them with no fixture-only paths.

// CommitFixture is one commits-dataset row.
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

// CommitFileFixture is one commit_files-dataset row.
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

// GitRepositoryFixture is one git_repositories-dataset row.
type GitRepositoryFixture struct {
	RepoID                string `parquet:"repo_id,dict"`
	RepoRemote            string `parquet:"repo_remote"`
	RepoRemoteNormalized  string `parquet:"repo_remote_normalized"`
	DefaultBranchObserved string `parquet:"default_branch_observed,dict"`
	SchemaVersion         int32  `parquet:"schema_version"`
}

// GitRefFixture is one git_refs-dataset row.
type GitRefFixture struct {
	RepoID        string `parquet:"repo_id,dict"`
	RefName       string `parquet:"ref_name,dict"`
	RefType       string `parquet:"ref_type,dict"`
	CommitID      string `parquet:"commit_id"`
	IsDefault     bool   `parquet:"is_default"`
	IsRemote      bool   `parquet:"is_remote"`
	SchemaVersion int32  `parquet:"schema_version"`
}

// ---- scenario JSON model -------------------------------------------------

type scenario struct {
	RepoID       string           `json:"repo_id"`
	OriginRemote string           `json:"origin_remote"`
	Commits      []scenarioCommit `json:"commits"`
	Refs         []scenarioRef    `json:"refs"`
}

type scenarioCommit struct {
	SHA           string         `json:"sha"`
	AuthorName    string         `json:"author_name"`
	AuthorEmail   string         `json:"author_email"`
	AuthorDateISO string         `json:"author_date_iso"`
	Subject       string         `json:"subject"`
	Files         []scenarioFile `json:"files"`
}

type scenarioFile struct {
	Path       string `json:"path"`
	ChangeType string `json:"change_type"`
	OldPath    string `json:"old_path"`
}

type scenarioRef struct {
	RefName   string `json:"ref_name"`
	RefType   string `json:"ref_type"`
	CommitSHA string `json:"commit_sha"`
	IsDefault bool   `json:"is_default"`
}

const schemaVersion = 1

// LoadScenario decodes internal/cochange/testdata/scenarios/<name>.json
// (resolved relative to this source file via runtime.Caller, so it works
// regardless of the calling test's working directory), expands it into the four
// git parquet datasets, writes each to t.TempDir()/<dataset>/<dataset>.parquet
// (the same layout etlscan.DiscoverDatasets walks), and returns the temp root.
func LoadScenario(t *testing.T, name string) (rootDir string) {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("scenariofixture: runtime.Caller failed")
	}
	jsonPath := filepath.Join(filepath.Dir(thisFile), "..", "testdata", "scenarios", name+".json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("scenariofixture: read %s: %v", jsonPath, err)
	}

	var sc scenario
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sc); err != nil {
		t.Fatalf("scenariofixture: decode %s: %v", jsonPath, err)
	}

	commits, files := expandCommits(t, &sc)
	repos := []GitRepositoryFixture{{
		RepoID:                sc.RepoID,
		RepoRemote:            sc.OriginRemote,
		RepoRemoteNormalized:  normalizeRemote(sc.OriginRemote),
		DefaultBranchObserved: "main",
		SchemaVersion:         schemaVersion,
	}}
	refs := expandRefs(&sc)

	root := t.TempDir()
	writeParquet(t, filepath.Join(root, "commits", "commits.parquet"), commits)
	writeParquet(t, filepath.Join(root, "commit_files", "commit_files.parquet"), files)
	writeParquet(t, filepath.Join(root, "git_repositories", "git_repositories.parquet"), repos)
	writeParquet(t, filepath.Join(root, "git_refs", "git_refs.parquet"), refs)
	return root
}

// expandCommits turns scenario commits into commits + commit_files rows,
// computing every derived field.
func expandCommits(t *testing.T, sc *scenario) ([]CommitFixture, []CommitFileFixture) {
	t.Helper()
	var commits []CommitFixture
	var files []CommitFileFixture
	for ci := range sc.Commits {
		c := &sc.Commits[ci]
		ts, err := parseDate(c.AuthorDateISO)
		if err != nil {
			t.Fatalf("scenariofixture: commit %s bad author_date_iso %q: %v", c.SHA, c.AuthorDateISO, err)
		}
		unixMs := ts.UnixMilli()
		year := int32(ts.Year())
		month := int32(ts.Month())
		commitID := sc.RepoID + "-" + c.SHA

		commits = append(commits, CommitFixture{
			ID:               commitID,
			ShortID:          c.SHA,
			RepoID:           sc.RepoID,
			AuthorName:       c.AuthorName,
			AuthorEmail:      c.AuthorEmail,
			AuthorDate:       unixMs,
			CommitterName:    c.AuthorName,
			CommitterEmail:   c.AuthorEmail,
			CommitterDate:    unixMs,
			MessageTruncated: c.Subject,
			FilesChanged:     int32(len(c.Files)),
			Year:             year,
			Month:            month,
			SchemaVersion:    schemaVersion,
		})

		for fi := range c.Files {
			f := &c.Files[fi]
			files = append(files, CommitFileFixture{
				CommitID:      commitID,
				RepoID:        sc.RepoID,
				FileIndex:     int32(fi),
				FilePath:      f.Path,
				ChangeType:    f.ChangeType,
				OldPath:       f.OldPath,
				AuthorDate:    unixMs,
				Year:          year,
				Month:         month,
				SchemaVersion: schemaVersion,
			})
		}
	}
	return commits, files
}

// expandRefs turns scenario refs into git_refs rows. git_refs.commit_id is the
// RAW sha (not the "<repo_id>-<sha>" form), matching the production schema.
func expandRefs(sc *scenario) []GitRefFixture {
	out := make([]GitRefFixture, 0, len(sc.Refs))
	for i := range sc.Refs {
		r := &sc.Refs[i]
		out = append(out, GitRefFixture{
			RepoID:        sc.RepoID,
			RefName:       r.RefName,
			RefType:       r.RefType,
			CommitID:      r.CommitSHA,
			IsDefault:     r.IsDefault,
			SchemaVersion: schemaVersion,
		})
	}
	return out
}

// parseDate accepts an RFC3339 timestamp or a bare YYYY-MM-DD date (interpreted
// as UTC midnight).
func parseDate(s string) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts.UTC(), nil
	}
	ts, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	return ts.UTC(), nil
}

// normalizeRemote is a deliberately trivial normalizer: scenario repos are
// resolved via RepoIDOverride in tests, so the normalized form only needs to be
// stable and non-empty. It returns the remote verbatim.
func normalizeRemote(remote string) string {
	return remote
}

// writeParquet writes rows to a single parquet file with the same deterministic
// writer wiring fixturegen uses.
func writeParquet[T any](t *testing.T, path string, rows []T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("scenariofixture: mkdir %s: %v", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("scenariofixture: create %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	w := parquet.NewGenericWriter[T](f, parquet.CreatedBy("auto-stack-scenariofixture", "", ""))
	if _, err := w.Write(rows); err != nil {
		t.Fatalf("scenariofixture: write %s: %v", path, err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("scenariofixture: close %s: %v", path, err)
	}
}
