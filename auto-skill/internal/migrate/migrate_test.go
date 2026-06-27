package migrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// realLockPath resolves the repo-root skills-lock.json from the test's package
// directory (auto-skill/internal/migrate → up three to the worktree root).
const realLockPath = "../../../skills-lock.json"

const syntheticLockPath = "testdata/synthetic-skills-lock.json"

func openLock(t *testing.T, path string) VercelLock {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	v, errs := ParseVercelLock(f)
	if len(errs) != 0 {
		t.Fatalf("ParseVercelLock(%s) returned errors: %+v", path, errs)
	}
	return v
}

func TestVersionFromRef(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		want string
	}{
		{"empty", "", "latest"},
		{"whitespace", "   ", "latest"},
		{"branch", "branch:main", "branch:main"},
		{"sha40", "0123456789abcdef0123456789abcdef01234567", "0123456789abcdef0123456789abcdef01234567"},
		{"sha7", "abc1234", "abc1234"},
		{"tag", "v1.2.3", "v1.2.3"},
		{"named-tag", "release-2026", "release-2026"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := versionFromRef(tc.ref)
			if got != tc.want {
				t.Errorf("versionFromRef(%q) = %q, want %q", tc.ref, got, tc.want)
			}
			// Conformance: every output must be a valid native version spec.
			if ve := skill.ValidateVersionSpec(got); ve != nil {
				t.Errorf("versionFromRef(%q) = %q failed ValidateVersionSpec: %+v", tc.ref, got, ve)
			}
		})
	}
}

func TestParseVercelLockRealCorpus(t *testing.T) {
	v := openLock(t, realLockPath)
	if v.Version != 1 {
		t.Errorf("version = %d, want 1", v.Version)
	}
	var github, local int
	for _, e := range v.Skills {
		switch e.SourceType {
		case sourceTypeGitHub:
			github++
		case sourceTypeLocal:
			local++
		}
	}
	if github != 37 {
		t.Errorf("github entries = %d, want 37", github)
	}
	if local != 9 {
		t.Errorf("local entries = %d, want 9", local)
	}
}

func TestPlanRealCorpus(t *testing.T) {
	v := openLock(t, realLockPath)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Plan(v, root)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	var remoteMigrated, localMigrated int
	for _, e := range m.Migrated {
		if e.State != "unresolved" {
			t.Errorf("entry %q state = %q, want unresolved", e.Name, e.State)
		}
		if ve := skill.ValidateVersionSpec(e.VersionSpec); ve != nil {
			t.Errorf("entry %q version_spec %q invalid: %+v", e.Name, e.VersionSpec, ve)
		}
		if e.Local {
			localMigrated++
			continue
		}
		remoteMigrated++
		if !strings.HasPrefix(e.URL, "https://github.com/") {
			t.Errorf("github entry %q URL = %q, want https://github.com/ prefix", e.Name, e.URL)
		}
	}

	if remoteMigrated != 37 {
		t.Errorf("remote migrated = %d, want 37", remoteMigrated)
	}

	// The 9 local entries are machine-dependent (git repo / non-git / missing).
	// Assert all 9 are accounted for across the three local outcomes.
	localHandled := localMigrated + len(m.Imports) + countSkipReason(m.Skipped, ReasonMissingPath)
	if localHandled != 9 {
		t.Errorf("local entries handled = %d (migrated=%d imports=%d missing=%d), want 9",
			localHandled, localMigrated, len(m.Imports), countSkipReason(m.Skipped, ReasonMissingPath))
	}
}

func TestPlanSynthetic(t *testing.T) {
	v := openLock(t, syntheticLockPath)
	m, err := Plan(v, t.TempDir())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	byName := make(map[string]Entry)
	for _, e := range m.Migrated {
		byName[e.Name] = e
	}

	// Version-intent mapping from refs.
	wantVersion := map[string]string{
		"gh-default": "latest",
		"gh-tag":     "v1.2.3",
		"gh-branch":  "branch:main",
		"gh-sha":     "0123456789abcdef0123456789abcdef01234567",
		"gl-repo":    "latest",
	}
	for name, want := range wantVersion {
		e, ok := byName[name]
		if !ok {
			t.Errorf("expected %q to be migrated", name)
			continue
		}
		if e.VersionSpec != want {
			t.Errorf("%q version_spec = %q, want %q", name, e.VersionSpec, want)
		}
		if e.State != "unresolved" {
			t.Errorf("%q state = %q, want unresolved", name, e.State)
		}
		if ve := skill.ValidateVersionSpec(e.VersionSpec); ve != nil {
			t.Errorf("%q version_spec invalid: %+v", name, ve)
		}
	}

	// URL derivation (credential-free, host prepended for bare owner/repo).
	if got := byName["gh-default"].URL; got != "https://github.com/acme/skills" {
		t.Errorf("gh-default URL = %q, want https://github.com/acme/skills", got)
	}
	if got := byName["gl-repo"].URL; got != "https://gitlab.com/group/subgroup/skills" {
		t.Errorf("gl-repo URL = %q, want https://gitlab.com/group/subgroup/skills", got)
	}

	// Subpath derivation from skillPath.
	if got := byName["gh-default"].Subpath; got != "skills/gh-default" {
		t.Errorf("gh-default subpath = %q, want skills/gh-default", got)
	}

	// Unsupported source types: warned + skipped + listed; Failed set.
	wantSkipped := map[string]bool{"nm-skill": true, "wk-skill": true, "hf-skill": true, "ml-skill": true}
	gotSkipped := make(map[string]bool)
	for _, s := range m.Skipped {
		if s.Reason != ReasonUnsupported {
			t.Errorf("skip %q reason = %q, want %q", s.Name, s.Reason, ReasonUnsupported)
		}
		if s.Message == "" {
			t.Errorf("skip %q has empty message", s.Name)
		}
		gotSkipped[s.Name] = true
	}
	for name := range wantSkipped {
		if !gotSkipped[name] {
			t.Errorf("expected %q to be skipped (unsupported)", name)
		}
	}

	res := m.Result()
	if !res.Failed {
		t.Error("Result.Failed = false, want true (some entries unsupported)")
	}
	if len(res.Migrated) != 5 {
		t.Errorf("Result.Migrated = %d, want 5", len(res.Migrated))
	}
	if len(res.Skipped) != 4 {
		t.Errorf("Result.Skipped = %d, want 4", len(res.Skipped))
	}
}

func TestPlanLocalSplit(t *testing.T) {
	root := t.TempDir()

	gitDir := filepath.Join(root, "git-repo")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", gitDir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	plainDir := filepath.Join(root, "plain-dir")
	if err := os.MkdirAll(plainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plainDir, "SKILL.md"), []byte("# skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	missingDir := filepath.Join(root, "does-not-exist")

	v := VercelLock{
		Version: 1,
		Skills: map[string]VercelEntry{
			"local-git":     {Source: gitDir, SourceType: sourceTypeLocal},
			"local-plain":   {Source: plainDir, SourceType: sourceTypeLocal},
			"local-missing": {Source: missingDir, SourceType: sourceTypeLocal},
		},
	}

	m, err := Plan(v, root)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// git repo → local lock entry (non-portable).
	if len(m.Migrated) != 1 || m.Migrated[0].Name != "local-git" {
		t.Fatalf("migrated = %+v, want exactly local-git", m.Migrated)
	}
	if !m.Migrated[0].Local {
		t.Error("local-git entry Local = false, want true")
	}
	if m.Migrated[0].State != "unresolved" {
		t.Errorf("local-git state = %q, want unresolved", m.Migrated[0].State)
	}

	// non-git dir → authored import.
	if len(m.Imports) != 1 || m.Imports[0].Name != "local-plain" {
		t.Fatalf("imports = %+v, want exactly local-plain", m.Imports)
	}
	if m.Imports[0].SourcePath != plainDir {
		t.Errorf("import source = %q, want %q", m.Imports[0].SourcePath, plainDir)
	}

	// missing path → reported + skipped.
	if len(m.Skipped) != 1 || m.Skipped[0].Name != "local-missing" {
		t.Fatalf("skipped = %+v, want exactly local-missing", m.Skipped)
	}
	if m.Skipped[0].Reason != ReasonMissingPath {
		t.Errorf("local-missing reason = %q, want %q", m.Skipped[0].Reason, ReasonMissingPath)
	}

	if imported := m.Result().Imported; len(imported) != 1 || imported[0] != "local-plain" {
		t.Errorf("Result.Imported = %v, want [local-plain]", imported)
	}
}

func TestParseVercelLockGarbled(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"not-json", "this is not json"},
		{"truncated", `{"version":1,"skills":{`},
		{"unknown-field", `{"version":1,"skills":{},"extra":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := ParseVercelLock(strings.NewReader(tc.input))
			if len(errs) == 0 {
				t.Fatalf("expected validation errors for %q", tc.input)
			}
			ve := errs[0]
			if ve.Code == "" || ve.Path == "" || ve.Message == "" {
				t.Errorf("structured error missing fields: %+v", ve)
			}
		})
	}
}

func TestParseVercelLockNilReader(t *testing.T) {
	_, errs := ParseVercelLock(nil)
	if len(errs) != 1 || errs[0].Code != CodeParseError {
		t.Errorf("nil reader errs = %+v, want one parse_error", errs)
	}
}

func TestParseVercelLockMissingFields(t *testing.T) {
	input := `{"version":1,"skills":{"bad":{"sourceType":"github"}}}`
	_, errs := ParseVercelLock(strings.NewReader(input))
	if len(errs) == 0 {
		t.Fatal("expected required-field errors for entry missing source")
	}
	found := false
	for _, ve := range errs {
		if ve.Field == "source" && ve.Code == skill.CodeRequired {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a required error for source, got %+v", errs)
	}
}

func countSkipReason(skips []Skip, reason string) int {
	n := 0
	for _, s := range skips {
		if s.Reason == reason {
			n++
		}
	}
	return n
}
