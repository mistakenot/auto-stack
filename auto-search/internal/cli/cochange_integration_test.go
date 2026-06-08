package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mistakenot/auto-search/internal/cochange/scenariofixture"
	"github.com/mistakenot/auto-search/internal/etlscan"
)

// cliApproxTokens recomputes the engine's approxTokens metric (ceil(runes/4)).
// approxTokens is unexported in package cochange and these CLI tests live in
// package cli_test, so the formula is recomputed inline here.
func cliApproxTokens(s string) int {
	return (utf8.RuneCountInString(s) + 3) / 4
}

// snapshotFixtureRoot returns the absolute path to the checked-in co-change
// snapshot fixture (commits/commit_files/git_repositories/git_refs parquet).
func snapshotFixtureRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "fixtures", "auto-stack-snapshot")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("resolve snapshot root: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("snapshot fixture not found: %v", err)
	}
	return abs
}

// snapshotRepoID reads the single repo id from the snapshot's git_repositories
// parquet so the CLI tests can resolve hermetically via --repo-id.
func snapshotRepoID(t *testing.T, root string) string {
	t.Helper()
	sources, err := etlscan.DiscoverDatasets(root, []string{"git_repositories"})
	if err != nil {
		t.Fatalf("discover git_repositories: %v", err)
	}
	for _, s := range sources {
		if s.Dataset != "git_repositories" {
			continue
		}
		repos, err := etlscan.ReadGitRepos(s.Path)
		if err != nil {
			t.Fatalf("read git_repositories: %v", err)
		}
		if len(repos) > 0 && repos[0].RepoID != "" {
			return repos[0].RepoID
		}
	}
	t.Fatal("snapshot has no usable repo id")
	return ""
}

func gitToplevel(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not inside a git repo: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// AC-1, AC-15: `co-change --help` lists all flags.
func TestCoChangeHelpListsAllFlags(t *testing.T) {
	stdout, stderr, code := runCLI(t, "co-change", "--help")
	if code != 0 {
		t.Fatalf("co-change --help failed: code=%d stderr=%s", code, stderr)
	}
	out := stdout + stderr
	for _, flag := range []string{"--repo-id", "--budget", "--all", "--json", "--decay-tau", "--no-decay", "--input", "--request-id"} {
		if !strings.Contains(out, flag) {
			t.Errorf("co-change --help missing flag %q\noutput:\n%s", flag, out)
		}
	}
}

// AC-15: quickstart output contains a co-change example.
func TestQuickstartMentionsCoChange(t *testing.T) {
	stdout, stderr, code := runCLI(t, "quickstart")
	if code != 0 {
		t.Fatalf("quickstart failed: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "co-change") {
		t.Errorf("quickstart output does not mention co-change\noutput:\n%s", stdout)
	}
}

// AC-13: the quickstart co-change section explains the two-phase router usage,
// advertises --budget/--all/--json, and no longer mentions the removed --limit.
func TestQuickstartCoChangeSection(t *testing.T) {
	stdout, stderr, code := runCLI(t, "quickstart")
	if code != 0 {
		t.Fatalf("quickstart failed: code=%d stderr=%s", code, stderr)
	}
	// Isolate the co-change section: from its heading to the next "## " heading
	// or end of doc. AC-13 scopes the --limit prohibition to this section, while
	// --limit is still a valid flag advertised for other commands (search, stats).
	const heading = "### 9. Find files that change together (co-change)"
	_, rest, found := strings.Cut(stdout, heading)
	if !found {
		t.Fatalf("quickstart missing co-change section heading %q\noutput:\n%s", heading, stdout)
	}
	section := rest
	if before, _, ok := strings.Cut(rest, "\n## "); ok {
		section = before
	}

	lower := strings.ToLower(section)
	if !strings.Contains(lower, "phase one") && !strings.Contains(lower, "two-phase") {
		t.Errorf("quickstart co-change section should describe the two-phase workflow\nsection:\n%s", section)
	}
	for _, want := range []string{"--budget", "--all", "--json"} {
		if !strings.Contains(section, want) {
			t.Errorf("quickstart co-change section should contain %q\nsection:\n%s", want, section)
		}
	}
	if !strings.Contains(stdout, "co-change") {
		t.Errorf("quickstart should still mention co-change\noutput:\n%s", stdout)
	}
	if strings.Contains(section, "--limit") {
		t.Errorf("quickstart co-change section should not mention removed --limit flag\nsection:\n%s", section)
	}
}

// AC-1, AC-4, AC-5: the CLI emits conforming JSON to stdout against the snapshot
// for a known file (resolved hermetically via --repo-id + --input).
func TestCoChangeCLIKnownFileJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := snapshotFixtureRoot(t)
	repoID := snapshotRepoID(t, root)
	top := gitToplevel(t)
	inputAbs := filepath.Join(top, "auto-etl/internal/git/extract.go")

	stdout, stderr, code := runCLI(t,
		"co-change", inputAbs,
		"--repo-id", repoID,
		"--input", root,
		"--request-id", "cli-known",
		"--json",
	)
	if code != 0 {
		t.Fatalf("co-change failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["metadata"].(map[string]any)
	if meta["resolved_path"] != "auto-etl/internal/git/extract.go" {
		t.Errorf("resolved_path = %v, want auto-etl/internal/git/extract.go", meta["resolved_path"])
	}
	if tc, _ := meta["total_commits"].(float64); tc <= 0 {
		t.Errorf("total_commits = %v, want > 0", meta["total_commits"])
	}
	related, ok := out["related_files"].([]any)
	if !ok || len(related) == 0 {
		t.Fatalf("expected non-empty related_files, got %v", out["related_files"])
	}
	// The adjacent test file should appear.
	var found bool
	for _, r := range related {
		rf := r.(map[string]any)
		if rf["path"] == "auto-etl/internal/git/extract_test.go" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected extract_test.go in related_files; got %v", related)
	}
	envelope := out["_meta"].(map[string]any)
	if envelope["request_id"] != "cli-known" {
		t.Errorf("_meta.request_id = %v, want cli-known", envelope["request_id"])
	}
}

// AC-1, AC-9: an EXISTING file and a NON-EXISTENT file inside the same repo both
// resolve the repo correctly (path->dir algorithm). The non-existent path
// yields a metadata-only payload, exit 0.
func TestCoChangeCLIExistingAndNonExistentPaths(t *testing.T) {
	root := snapshotFixtureRoot(t)
	repoID := snapshotRepoID(t, root)
	top := gitToplevel(t)

	// Existing file (in history).
	existing := filepath.Join(top, "auto-etl/internal/git/extract.go")
	stdout, stderr, code := runCLI(t, "co-change", existing, "--repo-id", repoID, "--input", root, "--json")
	if code != 0 {
		t.Fatalf("co-change on existing file failed: code=%d stderr=%s", code, stderr)
	}
	out := decodeJSON(t, stdout)
	if meta := out["metadata"].(map[string]any); meta["resolved_path"] != "auto-etl/internal/git/extract.go" {
		t.Errorf("existing resolved_path = %v", meta["resolved_path"])
	}

	// Non-existent file inside the repo: still resolves the repo (exit 0,
	// metadata-only).
	missing := filepath.Join(top, "auto-etl/internal/git/does-not-exist.go")
	stdout, stderr, code = runCLI(t, "co-change", missing, "--repo-id", repoID, "--input", root, "--json")
	if code != 0 {
		t.Fatalf("co-change on non-existent file should exit 0 (AC-9): code=%d stderr=%s", code, stderr)
	}
	out = decodeJSON(t, stdout)
	meta := out["metadata"].(map[string]any)
	if meta["resolved_path"] != "auto-etl/internal/git/does-not-exist.go" {
		t.Errorf("missing resolved_path = %v, want repo-relative path", meta["resolved_path"])
	}
	if tc, _ := meta["total_commits"].(float64); tc != 0 {
		t.Errorf("total_commits = %v, want 0 for untracked path", meta["total_commits"])
	}
}

// AC-10: input path outside any git repo -> non-zero exit with remediation, and
// stdout stays empty/parseable.
func TestCoChangeCLIOutsideRepo(t *testing.T) {
	root := snapshotFixtureRoot(t)

	// A path in a temp dir that is NOT a git repo.
	nonRepo := t.TempDir()
	target := filepath.Join(nonRepo, "file.go")
	if err := os.WriteFile(target, []byte("package x"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	stdout, stderr, code := runCLI(t, "co-change", target, "--input", root, "--json")
	if code == 0 {
		t.Fatal("expected non-zero exit for path outside any git repo")
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "not inside a git repository") {
		t.Errorf("stderr missing outside-repo condition, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "--repo-id") && !strings.Contains(stderr, "cd into") {
		t.Errorf("stderr missing remediation hint, got:\n%s", stderr)
	}
}

// AC-10: missing parquet data -> non-zero exit with remediation naming
// `auto etl run --only git`.
func TestCoChangeCLIMissingParquet(t *testing.T) {
	top := gitToplevel(t)
	inputAbs := filepath.Join(top, "auto-etl/internal/git/extract.go")

	// An empty input root: no git parquet datasets at all.
	emptyRoot := t.TempDir()

	stdout, stderr, code := runCLI(t,
		"co-change", inputAbs,
		"--repo-id", "anything",
		"--input", emptyRoot,
		"--json",
	)
	if code == 0 {
		t.Fatal("expected non-zero exit when git parquet is missing")
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "auto etl run --only git") {
		t.Errorf("stderr missing 'auto etl run --only git' remediation, got:\n%s", stderr)
	}
}

// AC-10: no origin remote and no --repo-id -> non-zero exit with remediation
// naming --repo-id. Exercised against a temp git repo with no origin remote.
func TestCoChangeCLINoOriginRemote(t *testing.T) {
	root := snapshotFixtureRoot(t)

	// Build a throwaway git repo with a commit but NO origin remote.
	repoDir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	target := filepath.Join(repoDir, "main.go")
	if err := os.WriteFile(target, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", "main.go")
	run("commit", "-m", "init")

	stdout, stderr, code := runCLI(t, "co-change", target, "--input", root, "--json")
	if code == 0 {
		t.Fatal("expected non-zero exit for repo with no origin remote and no --repo-id")
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "origin remote") {
		t.Errorf("stderr missing no-origin condition, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "--repo-id") {
		t.Errorf("stderr missing --repo-id remediation, got:\n%s", stderr)
	}
}

// AC-10: an origin remote that matches no indexed repo -> non-zero with
// remediation naming --repo-id (and auto etl run). Uses a temp git repo whose
// origin is a fabricated remote not present in the snapshot.
func TestCoChangeCLINoRepoMatch(t *testing.T) {
	root := snapshotFixtureRoot(t)

	repoDir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("remote", "add", "origin", "https://github.com/nobody/not-indexed.git")
	target := filepath.Join(repoDir, "main.go")
	if err := os.WriteFile(target, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", "main.go")
	run("commit", "-m", "init")

	stdout, stderr, code := runCLI(t, "co-change", target, "--input", root, "--json")
	if code == 0 {
		t.Fatal("expected non-zero exit when origin remote matches no indexed repo")
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "no match") && !strings.Contains(stderr, "not found") {
		t.Errorf("stderr missing no-match condition, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "--repo-id") {
		t.Errorf("stderr missing --repo-id remediation, got:\n%s", stderr)
	}
}

// AC-12g: the JSON envelope no longer carries the deleted params_used fields
// (large_commit_cutoff was dropped with the binary cutoff; limit was dropped
// with the --limit flag).
func TestCoChangeCLIKnownFileJSON_NoDeletedParamsFields(t *testing.T) {
	root := snapshotFixtureRoot(t)
	repoID := snapshotRepoID(t, root)
	top := gitToplevel(t)
	inputAbs := filepath.Join(top, "auto-etl/internal/git/extract.go")

	stdout, stderr, code := runCLI(t, "co-change", inputAbs, "--repo-id", repoID, "--input", root, "--json")
	if code != 0 {
		t.Fatalf("co-change failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["metadata"].(map[string]any)
	paramsUsed := meta["params_used"].(map[string]any)
	if _, ok := paramsUsed["large_commit_cutoff"]; ok {
		t.Errorf("params_used should not contain large_commit_cutoff, got: %v", paramsUsed)
	}
	if _, ok := paramsUsed["limit"]; ok {
		t.Errorf("params_used should not contain limit, got: %v", paramsUsed)
	}
}

// AC-2, AC-3: default (no --json) output is the compact text format: a header
// line with the resolved path, a row line for a sibling file at d0 carrying the
// × glyph, and no JSON braces.
func TestCoChangeCLIKnownFileText(t *testing.T) {
	root := snapshotFixtureRoot(t)
	repoID := snapshotRepoID(t, root)
	top := gitToplevel(t)
	inputAbs := filepath.Join(top, "auto-etl/internal/git/extract.go")

	stdout, stderr, code := runCLI(t, "co-change", inputAbs, "--repo-id", repoID, "--input", root)
	if code != 0 {
		t.Fatalf("co-change failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	resolved := "auto-etl/internal/git/extract.go"
	if !strings.HasPrefix(stdout, resolved+"\n") {
		t.Errorf("stdout should start with %q\\n; got:\n%s", resolved, stdout)
	}
	if strings.Contains(stdout, "{") {
		t.Errorf("text output should contain no JSON braces; got:\n%s", stdout)
	}
	// The adjacent test file is a same-dir sibling (d0) and should appear as a
	// row carrying the × co-commit glyph and a d0 label.
	var sawSiblingRow bool
	for line := range strings.SplitSeq(stdout, "\n") {
		if strings.Contains(line, "auto-etl/internal/git/extract_test.go") &&
			strings.Contains(line, "×") && strings.Contains(line, "d0") {
			sawSiblingRow = true
			break
		}
	}
	if !sawSiblingRow {
		t.Errorf("expected a d0 sibling row for extract_test.go with × glyph; got:\n%s", stdout)
	}

	// A tight budget forces truncation, surfacing the disclosure line.
	stdout, stderr, code = runCLI(t, "co-change", inputAbs, "--repo-id", repoID, "--input", root, "--budget", "50")
	if code != 0 {
		t.Fatalf("co-change --budget 50 failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "more hidden") {
		t.Errorf("expected truncation disclosure 'more hidden'; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "run with --all") {
		t.Errorf("expected disclosure to mention 'run with --all'; got:\n%s", stdout)
	}
}

// AC-9: --all bypasses the budget, so even a tiny --budget emits every row with
// no truncation disclosure.
func TestCoChangeCLI_AllBypassesBudget(t *testing.T) {
	root := snapshotFixtureRoot(t)
	repoID := snapshotRepoID(t, root)
	top := gitToplevel(t)
	inputAbs := filepath.Join(top, "auto-etl/internal/git/extract.go")

	stdout, stderr, code := runCLI(t, "co-change", inputAbs, "--repo-id", repoID, "--input", root, "--all", "--budget", "1")
	if code != 0 {
		t.Fatalf("co-change --all failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if strings.Contains(stdout, "more hidden") {
		t.Errorf("--all should bypass the budget (no 'more hidden'); got:\n%s", stdout)
	}
}

// AC-10: --limit is removed and rejected as an unknown flag.
func TestCoChangeCLI_LimitFlagRejected(t *testing.T) {
	root := snapshotFixtureRoot(t)
	repoID := snapshotRepoID(t, root)
	top := gitToplevel(t)
	inputAbs := filepath.Join(top, "auto-etl/internal/git/extract.go")

	_, stderr, code := runCLI(t, "co-change", inputAbs, "--repo-id", repoID, "--input", root, "--limit", "10")
	if code == 0 {
		t.Fatal("expected non-zero exit for removed --limit flag")
	}
	if !strings.Contains(stderr, "unknown flag") {
		t.Errorf("stderr should report unknown flag; got:\n%s", stderr)
	}
}

// AC-12h: flags that survive the task 011 changes still work through the JSON
// path (request-id echo, decay-tau parsing, no-decay).
func TestCoChangeCLI_SurvivingFlagsStillWork(t *testing.T) {
	root := snapshotFixtureRoot(t)
	repoID := snapshotRepoID(t, root)
	top := gitToplevel(t)
	inputAbs := filepath.Join(top, "auto-etl/internal/git/extract.go")

	stdout, stderr, code := runCLI(t,
		"co-change", inputAbs,
		"--repo-id", repoID,
		"--input", root,
		"--decay-tau", "30d",
		"--no-decay",
		"--request-id", "smoke",
		"--json",
	)
	if code != 0 {
		t.Fatalf("co-change with surviving flags failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	envelope := out["_meta"].(map[string]any)
	if envelope["request_id"] != "smoke" {
		t.Errorf("_meta.request_id = %v, want smoke", envelope["request_id"])
	}
	meta := out["metadata"].(map[string]any)
	paramsUsed := meta["params_used"].(map[string]any)
	if tau, _ := paramsUsed["decay_tau_days"].(float64); tau != 30 {
		t.Errorf("params_used.decay_tau_days = %v, want 30", paramsUsed["decay_tau_days"])
	}
}

// hotFileSeedArg builds the absolute seed path the CLI must be handed for the
// hot_file scenario. The seed is joined onto the git toplevel so the engine's
// lexical path-relativisation reproduces the repo-relative "src/a/hot.go" the
// scenario parquet's file_path column carries (a bare relative arg would resolve
// against the test process's cwd and miss the scenario rows).
func hotFileSeedArg(t *testing.T) string {
	t.Helper()
	return filepath.Join(gitToplevel(t), "src", "a", "hot.go")
}

// AC-15 (CLI-level): with no --budget flag, the cobra default of 500 bounds the
// compact text output for the hot-file scenario.
func TestCoChangeCLI_HotFile_TokenBudgetBound(t *testing.T) {
	root := scenariofixture.LoadScenario(t, "hot_file")
	seed := hotFileSeedArg(t)

	stdout, stderr, code := runCLI(t, "co-change", seed, "--repo-id", "fixture-repo", "--input", root)
	if code != 0 {
		t.Fatalf("co-change failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if tok := cliApproxTokens(stdout); tok > 500 {
		t.Errorf("default-budget CLI output is %d approx tokens, want <= 500\noutput:\n%s", tok, stdout)
	}
}

// AC-15 (CLI-level): --all bypasses the budget, so the hot-file output exceeds
// the 500-token bound.
func TestCoChangeCLI_HotFile_AllBypassesBudget(t *testing.T) {
	root := scenariofixture.LoadScenario(t, "hot_file")
	seed := hotFileSeedArg(t)

	stdout, stderr, code := runCLI(t, "co-change", seed, "--repo-id", "fixture-repo", "--input", root, "--all")
	if code != 0 {
		t.Fatalf("co-change --all failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if tok := cliApproxTokens(stdout); tok <= 500 {
		t.Errorf("--all CLI output is %d approx tokens, want > 500 (budget should be bypassed)", tok)
	}
}

// AC-15 (CLI-level): the compact text form is far smaller than the JSON
// envelope for the same query.
func TestCoChangeCLI_HotFile_TextVsJSONSize(t *testing.T) {
	root := scenariofixture.LoadScenario(t, "hot_file")
	seed := hotFileSeedArg(t)

	textOut, stderr, code := runCLI(t, "co-change", seed, "--repo-id", "fixture-repo", "--input", root)
	if code != 0 {
		t.Fatalf("co-change (text) failed: code=%d stderr=%s", code, stderr)
	}
	jsonOut, stderr, code := runCLI(t, "co-change", seed, "--repo-id", "fixture-repo", "--input", root, "--json")
	if code != 0 {
		t.Fatalf("co-change --json failed: code=%d stderr=%s", code, stderr)
	}
	if textRunes := utf8.RuneCountInString(textOut); textRunes > len(jsonOut)/4 {
		t.Errorf("text is %d runes, want <= json/4 = %d", textRunes, len(jsonOut)/4)
	}
}
