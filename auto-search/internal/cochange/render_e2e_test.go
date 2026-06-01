package cochange

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mistakenot/auto-search/internal/cochange/scenariofixture"
)

// scenarioRepoID is the repo_id stamped on every scenario fixture row; tests
// pass it as the RepoIDOverride so resolution bypasses git-remote matching.
const scenarioRepoID = "fixture-repo"

// gitToplevel returns the git toplevel of the test's working directory. The
// scenario seed paths are joined onto it so ResolveRepo's lexical
// path-relativisation reproduces the exact repo-relative seed path the parquet
// file_path column carries.
func testGitToplevel(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not inside a git repo: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// runScenario loads a scenario, runs the engine against the given repo-relative
// seed path, and returns the result.
func runScenario(t *testing.T, name, seedPath string, opts func(*Options)) *Result {
	t.Helper()
	root := scenariofixture.LoadScenario(t, name)
	top := testGitToplevel(t)
	o := &Options{
		InputPath:      filepath.Join(top, filepath.FromSlash(seedPath)),
		RepoIDOverride: scenarioRepoID,
		InputRoot:      root,
	}
	if opts != nil {
		opts(o)
	}
	res, err := Run(o)
	if err != nil {
		t.Fatalf("cochange.Run(%s): %v", name, err)
	}
	return res
}

// firstLine returns the first line of s (no trailing newline).
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func TestScenario_LoadAndRender_hot_file(t *testing.T) {
	res := runScenario(t, "hot_file", "src/a/hot.go", nil)
	out := Render(res, RenderOptions{})

	if got := firstLine(out); got != "src/a/hot.go" {
		t.Errorf("header line 1 = %q, want src/a/hot.go", got)
	}
	if res.Metadata.TotalCommits <= 0 {
		t.Fatalf("expected the seed to have history, total_commits=%d", res.Metadata.TotalCommits)
	}
	if len(res.RelatedFiles) == 0 {
		t.Fatal("expected related files for the hot seed")
	}
	// At least one same-dir sibling (d0) row carries the × glyph.
	if !strings.Contains(out, "×") {
		t.Errorf("expected co-commit × glyph in rendered rows; got:\n%s", out)
	}
	// d2 cross-package siblings should be discoverable (the engine ranks them).
	var sawCross bool
	for _, rf := range res.RelatedFiles {
		if strings.HasPrefix(rf.Path, "src/b/") {
			sawCross = true
			break
		}
	}
	if !sawCross {
		t.Errorf("expected at least one src/b cross-package sibling in related files")
	}
}

func TestScenario_LoadAndRender_cross_dir_coupling(t *testing.T) {
	res := runScenario(t, "cross_dir_coupling", "src/a/main.go", nil)
	out := Render(res, RenderOptions{})

	if got := firstLine(out); got != "src/a/main.go" {
		t.Errorf("header line 1 = %q, want src/a/main.go", got)
	}
	// The deliberate cross-dir coupling at infra/pipeline/runner.go must surface.
	var couplingDist int = -1
	for _, rf := range res.RelatedFiles {
		if rf.Path == "infra/pipeline/runner.go" {
			couplingDist = treeDistance(rf.Path, res.Metadata.ResolvedPath)
		}
	}
	if couplingDist < 4 {
		t.Errorf("expected infra/pipeline/runner.go coupling at d>=4, got d=%d (related=%v)", couplingDist, res.RelatedFiles)
	}
}

func TestScenario_LoadAndRender_large_commit(t *testing.T) {
	res := runScenario(t, "large_commit", "src/a/common.go", nil)
	out := Render(res, RenderOptions{})

	if got := firstLine(out); got != "src/a/common.go" {
		t.Errorf("header line 1 = %q, want src/a/common.go", got)
	}
	// The 100-file commit must contribute (its co-changed file appears) but must
	// not dominate the 2-file commits' shared file (continuous weighting, not a
	// binary cutoff).
	var smallScore, bigScore float64
	var sawSmall, sawBig bool
	for _, rf := range res.RelatedFiles {
		if rf.Path == "src/a/small_partner.go" {
			sawSmall = true
			smallScore = rf.Score
		}
		if rf.Path == "src/a/big_partner.go" {
			sawBig = true
			bigScore = rf.Score
		}
	}
	if !sawSmall {
		t.Errorf("expected the 2-file-commit partner src/a/small_partner.go in related files: %v", res.RelatedFiles)
	}
	if !sawBig {
		t.Errorf("expected the 100-file-commit partner src/a/big_partner.go to still contribute: %v", res.RelatedFiles)
	}
	// big_partner only ever co-changed inside 100-file commits, so its weight
	// must be strictly positive (it contributes) yet below the small partner's
	// (it does not dominate).
	if sawBig && bigScore <= 0 {
		t.Errorf("big_partner score = %v, want strictly > 0", bigScore)
	}
	if sawSmall && sawBig && bigScore >= smallScore {
		t.Errorf("big_partner score %v should be below small_partner %v (large commits weighted down)", bigScore, smallScore)
	}
}

func TestScenario_LoadAndRender_no_history(t *testing.T) {
	res := runScenario(t, "no_history", "src/missing.go", nil)
	out := Render(res, RenderOptions{})

	if res.Metadata.TotalCommits != 0 {
		t.Fatalf("expected total_commits=0 for an untouched seed, got %d", res.Metadata.TotalCommits)
	}
	want := "src/missing.go\nno history for this file\n"
	if out != want {
		t.Errorf("no-history render = %q, want %q", out, want)
	}
}

func TestScenario_LoadAndRender_insufficient_history(t *testing.T) {
	res := runScenario(t, "insufficient_history", "src/a/seed.go", nil)
	out := Render(res, RenderOptions{})

	if got := firstLine(out); got != "src/a/seed.go" {
		t.Errorf("header line 1 = %q, want src/a/seed.go", got)
	}
	if res.Metadata.Warning != "insufficient history" {
		t.Fatalf("expected insufficient-history warning, got metadata=%+v", res.Metadata)
	}
	if !strings.HasSuffix(out, "insufficient history\n") {
		t.Errorf("expected trailing 'insufficient history' line; got:\n%s", out)
	}
}

// ---- AC-15 engine-level budget bounds ------------------------------------

func TestHotFile_TokenBudgetBound(t *testing.T) {
	res := runScenario(t, "hot_file", "src/a/hot.go", nil)
	out := Render(res, RenderOptions{}) // default budget (500)
	if tok := approxTokens(out); tok > 500 {
		t.Errorf("default-budget output is %d approx tokens, want <= 500\noutput:\n%s", tok, out)
	}
}

func TestHotFile_AllBypassesBudget(t *testing.T) {
	res := runScenario(t, "hot_file", "src/a/hot.go", nil)
	out := Render(res, RenderOptions{All: true})
	if tok := approxTokens(out); tok <= 500 {
		t.Errorf("--all output is %d approx tokens, want > 500 (budget should be bypassed)", tok)
	}
}

func TestHotFile_TextVsJSONSize(t *testing.T) {
	res := runScenario(t, "hot_file", "src/a/hot.go", nil)
	textOut := Render(res, RenderOptions{})
	jsonOut, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	textRunes := utf8.RuneCountInString(textOut)
	if textRunes > len(jsonOut)/4 {
		t.Errorf("text is %d runes, want <= json/4 = %d", textRunes, len(jsonOut)/4)
	}
}
