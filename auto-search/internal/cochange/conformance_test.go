package cochange

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mistakenot/auto-search/internal/etlscan"
)

// Conformance tests run the full co-change pipeline (resolve -> load -> query ->
// score -> assemble) against the checked-in auto-stack snapshot fixture. They
// cover AC-1 (reads git parquet), AC-4/AC-5 (output schema), AC-9 (unknown-file
// metadata-only), AC-16 (fixture size), and AC-19 (real-snapshot conformance).
//
// They are hermetic: the repo id is read from the fixture's git_repositories
// parquet and passed as RepoIDOverride, so the assertions do not depend on the
// ambient git remote. (One test additionally exercises live origin-remote
// resolution since the worktree origin happens to match the fixture.)

// fixtureRoot returns the absolute path to the checked-in snapshot fixture root.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "fixtures", "auto-stack-snapshot")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("fixture root missing: %v", err)
	}
	return abs
}

// fixtureRepoID reads the single repo id from the fixture's git_repositories
// parquet so tests can resolve hermetically via RepoIDOverride.
func fixtureRepoID(t *testing.T, root string) string {
	t.Helper()
	sources, err := etlscan.DiscoverDatasets(root, []string{"git_repositories"})
	if err != nil {
		t.Fatalf("discover git_repositories: %v", err)
	}
	var repos []etlscan.GitRepoSlim
	for _, s := range sources {
		if s.Dataset != "git_repositories" {
			continue
		}
		r, err := etlscan.ReadGitRepos(s.Path)
		if err != nil {
			t.Fatalf("read git_repositories: %v", err)
		}
		repos = append(repos, r...)
	}
	if len(repos) == 0 {
		t.Fatal("fixture has no git_repositories rows")
	}
	if repos[0].RepoID == "" {
		t.Fatal("fixture repo id is empty")
	}
	return repos[0].RepoID
}

// gitToplevelForTest returns the git toplevel of the test's working tree. The
// snapshot was regenerated from this repo, so an absolute path under the
// toplevel resolves to a fixture path (resolved_path matches).
func gitToplevelForTest(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not inside a git repo: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// AC-1, AC-4, AC-5, AC-19: full pipeline against the snapshot for a known file
// with sufficient history. Asserts valid JSON to the documented schema, that an
// expected related file appears in the top results, and that the metadata block
// is fully populated.
func TestConformance_KnownFile(t *testing.T) {
	root := fixtureRoot(t)
	repoID := fixtureRepoID(t, root)
	top := gitToplevelForTest(t)

	// auto-etl/internal/git/extract.go has 7 commits in the snapshot; its
	// adjacent test file co-changes in 5 of them (verified by inspecting the
	// fixture before writing this assertion).
	const inputRel = "auto-etl/internal/git/extract.go"
	const wantRelated = "auto-etl/internal/git/extract_test.go"
	inputAbs := filepath.Join(top, inputRel)

	res, err := Run(Options{
		InputPath:      inputAbs,
		RepoIDOverride: repoID,
		InputRoot:      root,
		Limit:          50,
		RequestID:      "conf-known",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// (a) Output is valid JSON conforming to the documented schema: marshal and
	// re-unmarshal into the Result struct round-trips cleanly.
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var back Result
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal result: %v\nraw:\n%s", err, raw)
	}
	// Spot-check the documented snake_case keys are present in the JSON.
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	if _, ok := generic["metadata"]; !ok {
		t.Error("missing top-level metadata block")
	}
	if _, ok := generic["related_files"]; !ok {
		t.Error("missing top-level related_files array")
	}

	// (b) An expected related file appears in the top results, with grounded
	// stats (co_commits == 5, confidence_b_to_a == 1.0 — verified against the
	// fixture).
	var rel *RelatedFile
	for i := range res.RelatedFiles {
		if res.RelatedFiles[i].Path == wantRelated {
			rel = &res.RelatedFiles[i]
			break
		}
	}
	if rel == nil {
		var got []string
		for _, r := range res.RelatedFiles {
			got = append(got, r.Path)
		}
		t.Fatalf("expected related file %q not found; got: %v", wantRelated, got)
	}
	if rel.CoCommits != 5 {
		t.Errorf("%s co_commits = %d, want 5", wantRelated, rel.CoCommits)
	}
	if rel.Score <= 0 {
		t.Errorf("%s score = %v, want > 0", wantRelated, rel.Score)
	}
	if rel.ConfidenceAtoB <= 0 || rel.ConfidenceAtoB > 1 {
		t.Errorf("%s confidence_a_to_b = %v, want in (0,1]", wantRelated, rel.ConfidenceAtoB)
	}
	if rel.ConfidenceBtoA <= 0 || rel.ConfidenceBtoA > 1 {
		t.Errorf("%s confidence_b_to_a = %v, want in (0,1]", wantRelated, rel.ConfidenceBtoA)
	}
	if rel.Lift <= 1 {
		t.Errorf("%s lift = %v, want > 1 (coupled)", wantRelated, rel.Lift)
	}
	if rel.LastCoChange == "" {
		t.Errorf("%s last_co_change is empty", wantRelated)
	}
	// AC-4: per-file arrays are non-nil (JSON arrays, never null).
	if rel.TopAuthors == nil {
		t.Error("related top_authors is nil")
	}
	if rel.TopSessions == nil {
		t.Error("related top_sessions is nil")
	}
	if rel.SampleCommits == nil {
		t.Error("related sample_commits is nil")
	}
	if len(rel.SampleCommits) > 3 {
		t.Errorf("sample_commits = %d, want <= 3", len(rel.SampleCommits))
	}

	// AC-8: related files are sorted by score descending.
	for i := 1; i < len(res.RelatedFiles); i++ {
		if res.RelatedFiles[i-1].Score < res.RelatedFiles[i].Score {
			t.Errorf("related_files not sorted desc at %d: %v < %v", i, res.RelatedFiles[i-1].Score, res.RelatedFiles[i].Score)
		}
	}

	// (c) Metadata block fields are all populated (AC-5).
	m := res.Metadata
	if m.File != inputAbs {
		t.Errorf("metadata.file = %q, want %q", m.File, inputAbs)
	}
	if m.ResolvedPath != inputRel {
		t.Errorf("metadata.resolved_path = %q, want %q", m.ResolvedPath, inputRel)
	}
	if !m.ExistsInWorkspace {
		t.Error("metadata.exists_in_workspace = false; the file exists in this worktree")
	}
	if m.Language != "go" {
		t.Errorf("metadata.language = %q, want go", m.Language)
	}
	if m.Repo == "" {
		t.Error("metadata.repo is empty")
	}
	if m.TotalCommits != 7 {
		t.Errorf("metadata.total_commits = %d, want 7", m.TotalCommits)
	}
	if m.FirstTouched == "" || m.LastTouched == "" {
		t.Errorf("metadata first/last touched empty: %q / %q", m.FirstTouched, m.LastTouched)
	}
	if len(m.TopAuthors) == 0 {
		t.Error("metadata.top_authors is empty")
	}
	for _, a := range m.TopAuthors {
		if a.Name == "" || a.Count <= 0 {
			t.Errorf("metadata author entry malformed: %+v", a)
		}
	}
	if m.AvgFilesPerCommit <= 0 {
		t.Errorf("metadata.avg_files_per_commit = %v, want > 0", m.AvgFilesPerCommit)
	}
	if m.TopSessions == nil {
		t.Error("metadata.top_sessions is nil")
	}
	if m.RenamedFrom == nil {
		t.Error("metadata.renamed_from is nil")
	}
	if m.RefTipsAtTouchedCommits == nil {
		t.Error("metadata.ref_tips_at_touched_commits is nil")
	}
	if m.RelatedFilesFound != len(res.RelatedFiles) {
		t.Errorf("metadata.related_files_found = %d, want %d", m.RelatedFilesFound, len(res.RelatedFiles))
	}
	if m.Warning != "" {
		t.Errorf("unexpected warning for a high-history file: %q", m.Warning)
	}
	// params_used reflects the documented defaults / requested limit.
	if m.ParamsUsed.DecayTauDays != 90 {
		t.Errorf("params_used.decay_tau_days = %v, want 90", m.ParamsUsed.DecayTauDays)
	}
	if m.ParamsUsed.LargeCommitCutoff != LargeCommitCutoff {
		t.Errorf("params_used.large_commit_cutoff = %d, want %d", m.ParamsUsed.LargeCommitCutoff, LargeCommitCutoff)
	}
	if m.ParamsUsed.MinCoCommits != MinCoCommits {
		t.Errorf("params_used.min_co_commits = %d, want %d", m.ParamsUsed.MinCoCommits, MinCoCommits)
	}
	if m.ParamsUsed.MinCommitsA != MinCommitsA {
		t.Errorf("params_used.min_commits_a = %d, want %d", m.ParamsUsed.MinCommitsA, MinCommitsA)
	}
	if m.ParamsUsed.Limit != 50 {
		t.Errorf("params_used.limit = %d, want 50", m.ParamsUsed.Limit)
	}

	// _meta envelope.
	if res.Meta.Command != "co-change" {
		t.Errorf("_meta.command = %q, want co-change", res.Meta.Command)
	}
	if res.Meta.RequestID != "conf-known" {
		t.Errorf("_meta.request_id = %q, want conf-known", res.Meta.RequestID)
	}
}

// AC-1: live origin-remote resolution (no --repo-id). The worktree origin
// normalises to the same remote stored in the fixture, so resolution should
// succeed and produce the same result shape as the override path.
func TestConformance_LiveRemoteResolution(t *testing.T) {
	root := fixtureRoot(t)
	top := gitToplevelForTest(t)
	if _, err := exec.Command("git", "-C", top, "remote", "get-url", "origin").Output(); err != nil {
		t.Skip("no origin remote on this worktree; skipping live-resolution test")
	}

	inputAbs := filepath.Join(top, "auto-etl/internal/git/extract.go")
	res, err := Run(Options{
		InputPath: inputAbs,
		InputRoot: root,
		Limit:     50,
	})
	if err != nil {
		t.Fatalf("Run (live resolution): %v", err)
	}
	if res.Metadata.TotalCommits == 0 {
		t.Fatalf("live resolution found no history; remote did not match the fixture")
	}
	if res.Metadata.RelatedFilesFound == 0 {
		t.Error("expected related files from live resolution")
	}
}

// AC-9: an untracked/unknown path inside the repo returns a metadata-only
// payload with total_commits 0, related_files_found 0, and no error.
func TestConformance_UnknownFile(t *testing.T) {
	root := fixtureRoot(t)
	repoID := fixtureRepoID(t, root)
	top := gitToplevelForTest(t)

	// A path that does not appear in the snapshot's commit history.
	inputAbs := filepath.Join(top, "auto-search/internal/cochange/this-file-never-existed.go")

	res, err := Run(Options{
		InputPath:      inputAbs,
		RepoIDOverride: repoID,
		InputRoot:      root,
		Limit:          50,
	})
	if err != nil {
		t.Fatalf("Run on unknown file should not error (AC-9): %v", err)
	}
	if res.Metadata.TotalCommits != 0 {
		t.Errorf("total_commits = %d, want 0 for unknown file", res.Metadata.TotalCommits)
	}
	if res.Metadata.RelatedFilesFound != 0 {
		t.Errorf("related_files_found = %d, want 0 for unknown file", res.Metadata.RelatedFilesFound)
	}
	if len(res.RelatedFiles) != 0 {
		t.Errorf("related_files len = %d, want 0 for unknown file", len(res.RelatedFiles))
	}
	// The path is still resolved against the repo (AC-9).
	if res.Metadata.ResolvedPath != "auto-search/internal/cochange/this-file-never-existed.go" {
		t.Errorf("resolved_path = %q, want the repo-relative unknown path", res.Metadata.ResolvedPath)
	}
	// Arrays must serialise as [] not null.
	if res.RelatedFiles == nil {
		t.Error("related_files is nil; want empty slice")
	}
}

// AC-3c: a file with fewer than MinCommitsA commits returns a metadata-only
// payload with the insufficient-history warning and an empty related list.
func TestConformance_InsufficientHistory(t *testing.T) {
	root := fixtureRoot(t)
	repoID := fixtureRepoID(t, root)
	top := gitToplevelForTest(t)

	// This task's requirements doc was added in a single commit in the snapshot.
	inputAbs := filepath.Join(top, "docs/tasks/010-autosearch-co-change/requirements.md")

	res, err := Run(Options{
		InputPath:      inputAbs,
		RepoIDOverride: repoID,
		InputRoot:      root,
		Limit:          50,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Metadata.TotalCommits >= MinCommitsA {
		t.Skipf("file unexpectedly has >= %d commits (%d); fixture changed", MinCommitsA, res.Metadata.TotalCommits)
	}
	if res.Metadata.TotalCommits == 0 {
		t.Skip("file not in snapshot history; cannot exercise insufficient-history path")
	}
	if res.Metadata.Warning != "insufficient history" {
		t.Errorf("warning = %q, want 'insufficient history'", res.Metadata.Warning)
	}
	if res.Metadata.RelatedFilesFound != 0 {
		t.Errorf("related_files_found = %d, want 0", res.Metadata.RelatedFilesFound)
	}
	if len(res.RelatedFiles) != 0 {
		t.Errorf("related_files len = %d, want 0", len(res.RelatedFiles))
	}
}

// AC-16: the checked-in fixture total size is under 1 MB.
func TestConformance_FixtureSizeUnder1MB(t *testing.T) {
	root := fixtureRoot(t)
	var total int64
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture: %v", err)
	}
	const oneMB = 1 << 20
	if total >= oneMB {
		t.Errorf("fixture total size = %d bytes, want < %d (1 MB)", total, oneMB)
	}
}
