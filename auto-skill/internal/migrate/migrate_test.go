package migrate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/cache"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/transport"
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
	// gitDir IS the worktree top-level, so Subpath is empty.
	if m.Migrated[0].Subpath != "" {
		t.Errorf("local-git subpath = %q, want empty (source is the repo root)", m.Migrated[0].Subpath)
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

// TestPlanLocalGitURLIsResolvable reproduces Bug 2: migrate emits a bare absolute
// filesystem path as the URL for a local git repo (e.g. "/home/user/src/repo").
// sync canonicalizes that URL and derives the cache path from the resulting
// identity — but a bare absolute path lands in transport's "bare host/path" branch
// and yields an EMPTY Host, which cache.repoPath rejects with "invalid path
// component". So every local-git lock entry migrate produces is unopenable by sync.
//
// The migrated URL must round-trip through the exact transport→cache path sync
// uses without an empty component. The fix is for migrate to emit a file:// URL
// (which transport already maps to a usable identity), not a bare path.
func TestPlanLocalGitURLIsResolvable(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, "git-repo")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", gitDir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	v := VercelLock{
		Version: 1,
		Skills: map[string]VercelEntry{
			"local-git": {Source: gitDir, SourceType: sourceTypeLocal},
		},
	}
	m, err := Plan(v, root)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(m.Migrated) != 1 || !m.Migrated[0].Local {
		t.Fatalf("want exactly one local migrated entry, got %+v", m.Migrated)
	}

	url := m.Migrated[0].URL
	canon, id, err := transport.CanonicalizeURL(url)
	if err != nil {
		t.Fatalf("migrated local URL %q is not canonicalizable (Bug 2): %v", url, err)
	}
	// This is the exact call sync makes (cache.Open → repoPath) and where the
	// "invalid path component" surfaces.
	if _, err := cache.NewCache(t.TempDir()).RepoPath(id); err != nil {
		t.Fatalf("migrated local URL %q canonicalized to %q (Host=%q) but is unopenable by the cache (Bug 2): %v",
			url, canon, id.Host, err)
	}
}

// TestPlanLocalGitSubdir covers a vercel local source pointing at a subdirectory
// inside a worktree (the real-corpus shape, e.g. <repo>/skills). The lock entry
// must resolve URL to the worktree top-level and carry the relative subdir as
// Subpath, so a later sync can clone the repo root rather than a non-repo subdir.
func TestPlanLocalGitSubdir(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	// git --show-toplevel canonicalizes symlinks (e.g. macOS /tmp); resolve the
	// repo path the same way for a stable expectation.
	topWant, err := filepath.EvalSymlinks(repo)
	if err != nil {
		topWant = repo
	}

	subDir := filepath.Join(repo, "skills", "my-skill")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte("# my skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := VercelLock{
		Version: 1,
		Skills: map[string]VercelEntry{
			"my-skill": {Source: subDir, SourceType: sourceTypeLocal},
		},
	}
	m, err := Plan(v, repo)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(m.Migrated) != 1 {
		t.Fatalf("migrated = %+v, want exactly one entry", m.Migrated)
	}
	e := m.Migrated[0]
	if !e.Local {
		t.Error("Local = false, want true")
	}
	if e.State != "unresolved" {
		t.Errorf("state = %q, want unresolved", e.State)
	}
	// URL is the resolvable form (file:// so sync can canonicalize it); Source
	// stays the bare worktree path for human-readable provenance.
	if wantURL := "file://" + topWant; e.URL != wantURL {
		t.Errorf("URL = %q, want %q", e.URL, wantURL)
	}
	if e.Source != topWant {
		t.Errorf("Source = %q, want worktree top-level %q", e.Source, topWant)
	}
	if e.Subpath != "skills/my-skill" {
		t.Errorf("Subpath = %q, want %q", e.Subpath, "skills/my-skill")
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

// buildLocalSplitMigration constructs a Migration with one git-repo entry, one
// non-git import, and one missing path, all under root.
func buildLocalSplitMigration(t *testing.T, root string) Migration {
	t.Helper()

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
	if err := os.WriteFile(filepath.Join(plainDir, "SKILL.md"), []byte("# plain skill\n"), 0o644); err != nil {
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
	return m
}

func TestApplyWritesAndRoundTrips(t *testing.T) {
	v := openLock(t, syntheticLockPath)
	root := t.TempDir()
	m, err := Plan(v, root)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	res, err := m.Apply(false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Failed {
		t.Error("Result.Failed = false, want true (unsupported entries skipped)")
	}

	// lock.json: parses + validates with ZERO errors (AC-1).
	lockData, err := os.ReadFile(filepath.Join(root, ".auto", "skills", "lock.json"))
	if err != nil {
		t.Fatalf("read lock.json: %v", err)
	}
	lock, err := skill.ParseLock(lockData)
	if err != nil {
		t.Fatalf("ParseLock: %v", err)
	}
	if errs := skill.ValidateLock(lock); len(errs) != 0 {
		t.Fatalf("ValidateLock: %+v", errs)
	}
	if lock.Version != 1 {
		t.Errorf("lock version = %d, want 1", lock.Version)
	}

	// Each migrated github entry: unresolved, no commit, credential-free url.
	for _, name := range []string{"gh-default", "gh-tag", "gh-branch", "gh-sha", "gl-repo"} {
		e, ok := lock.Skills[name]
		if !ok {
			t.Errorf("lock missing %q", name)
			continue
		}
		if e.State != "unresolved" {
			t.Errorf("%q state = %q, want unresolved", name, e.State)
		}
		if e.Commit != "" {
			t.Errorf("%q commit = %q, want empty", name, e.Commit)
		}
		if strings.Contains(e.URL, "@") {
			t.Errorf("%q url %q looks credentialed", name, e.URL)
		}
		if e.Local {
			t.Errorf("%q local = true, want false (remote)", name)
		}
	}
	if got := lock.Skills["gh-default"].URL; got != "https://github.com/acme/skills" {
		t.Errorf("gh-default url = %q", got)
	}

	// skills.yaml: parses + validates with ZERO errors, seeds version intent (AC-2).
	yamlData, err := os.ReadFile(filepath.Join(root, ".auto", "skills", "skills.yaml"))
	if err != nil {
		t.Fatalf("read skills.yaml: %v", err)
	}
	cfg, err := skill.ParseSkillsYAML(yamlData)
	if err != nil {
		t.Fatalf("ParseSkillsYAML: %v", err)
	}
	if errs := skill.ValidateSkillsYAML(cfg); len(errs) != 0 {
		t.Fatalf("ValidateSkillsYAML: %+v", errs)
	}
	wantVersion := map[string]string{
		"gh-default": "latest",
		"gh-tag":     "v1.2.3",
		"gh-branch":  "branch:main",
		"gh-sha":     "0123456789abcdef0123456789abcdef01234567",
		"gl-repo":    "latest",
	}
	for name, want := range wantVersion {
		sc, ok := cfg.Skills[name]
		if !ok {
			t.Errorf("skills.yaml missing %q", name)
			continue
		}
		if sc.Version != want {
			t.Errorf("%q version = %q, want %q", name, sc.Version, want)
		}
		if len(sc.Replacements) != 0 {
			t.Errorf("%q replacements = %v, want empty", name, sc.Replacements)
		}
	}
}

func TestApplyDryRunWritesNothing(t *testing.T) {
	v := openLock(t, syntheticLockPath)
	root := t.TempDir()
	m, err := Plan(v, root)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	res, err := m.Apply(true)
	if err != nil {
		t.Fatalf("Apply(dryRun): %v", err)
	}
	// Result is fully computed even on dry-run.
	if len(res.Migrated) != 5 {
		t.Errorf("dry-run migrated = %d, want 5", len(res.Migrated))
	}
	if len(res.Skipped) != 4 {
		t.Errorf("dry-run skipped = %d, want 4", len(res.Skipped))
	}

	// Nothing written: .auto/ and skills/ must not exist.
	if _, err := os.Stat(filepath.Join(root, ".auto")); !os.IsNotExist(err) {
		t.Errorf(".auto exists after dry-run (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills")); !os.IsNotExist(err) {
		t.Errorf("skills/ exists after dry-run (err=%v)", err)
	}
}

func TestApplyLocalSplitImportsAndSkips(t *testing.T) {
	root := t.TempDir()
	m := buildLocalSplitMigration(t, root)

	res, err := m.Apply(false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// git repo → lock entry (local:true, unresolved).
	lockData, err := os.ReadFile(filepath.Join(root, ".auto", "skills", "lock.json"))
	if err != nil {
		t.Fatalf("read lock.json: %v", err)
	}
	lock, err := skill.ParseLock(lockData)
	if err != nil {
		t.Fatalf("ParseLock: %v", err)
	}
	if errs := skill.ValidateLock(lock); len(errs) != 0 {
		t.Fatalf("ValidateLock: %+v", errs)
	}
	e, ok := lock.Skills["local-git"]
	if !ok || !e.Local || e.State != "unresolved" {
		t.Errorf("local-git entry = %+v, want local unresolved", e)
	}
	if _, ok := lock.Skills["local-plain"]; ok {
		t.Error("non-git import must not get a lock entry")
	}

	// non-git dir → authored import under ./skills/<name>/.
	if _, err := os.Stat(filepath.Join(root, "skills", "local-plain", "SKILL.md")); err != nil {
		t.Errorf("authored import missing: %v", err)
	}
	if res.Imported == nil || len(res.Imported) != 1 || res.Imported[0] != "local-plain" {
		t.Errorf("Imported = %v, want [local-plain]", res.Imported)
	}

	// missing path → skipped.
	if countSkipReason(res.Skipped, ReasonMissingPath) != 1 {
		t.Errorf("missing-path skips = %d, want 1", countSkipReason(res.Skipped, ReasonMissingPath))
	}
}

func TestApplyAdditivePreservesExistingAndSource(t *testing.T) {
	root := t.TempDir()

	// Pre-seed an unrelated lock.json entry.
	configDir := filepath.Join(root, ".auto", "skills")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	preLock := &skill.Lock{
		Version: 1,
		Skills: map[string]skill.LockEntry{
			"pre-existing": {
				Source:      "owner/existing",
				URL:         "https://github.com/owner/existing",
				VersionSpec: "latest",
				State:       "unresolved",
			},
		},
	}
	preData, err := skill.EncodeJSON(preLock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "lock.json"), preData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Copy the synthetic source into the temp tree and snapshot its bytes.
	srcBytes, err := os.ReadFile(syntheticLockPath)
	if err != nil {
		t.Fatal(err)
	}
	srcCopy := filepath.Join(root, "skills-lock.json")
	if err := os.WriteFile(srcCopy, srcBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(srcCopy)
	if err != nil {
		t.Fatal(err)
	}

	v := openLock(t, srcCopy)
	m, err := Plan(v, root)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := m.Apply(false); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Source skills-lock.json is byte-for-byte unchanged (AC-5).
	after, err := os.ReadFile(srcCopy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("source skills-lock.json was modified; migration must not touch it")
	}

	// Both the pre-existing and the newly migrated entries are present (additive).
	lockData, err := os.ReadFile(filepath.Join(configDir, "lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := skill.ParseLock(lockData)
	if err != nil {
		t.Fatalf("ParseLock: %v", err)
	}
	if _, ok := lock.Skills["pre-existing"]; !ok {
		t.Error("pre-existing entry was dropped; merge must be additive")
	}
	if _, ok := lock.Skills["gh-default"]; !ok {
		t.Error("newly migrated gh-default entry missing")
	}
}

func TestApplyDoesNotOverwriteExistingEntry(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".auto", "skills")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-seed an entry whose key collides with a migrated one.
	preLock := &skill.Lock{
		Version: 1,
		Skills: map[string]skill.LockEntry{
			"gh-default": {
				Source:      "someone/else",
				URL:         "https://github.com/someone/else",
				VersionSpec: "branch:dev",
				State:       "resolved",
				Commit:      "abc1234",
			},
		},
	}
	preData, _ := skill.EncodeJSON(preLock)
	if err := os.WriteFile(filepath.Join(configDir, "lock.json"), preData, 0o644); err != nil {
		t.Fatal(err)
	}

	v := openLock(t, syntheticLockPath)
	m, err := Plan(v, root)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	res, err := m.Apply(false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	lockData, _ := os.ReadFile(filepath.Join(configDir, "lock.json"))
	lock, _ := skill.ParseLock(lockData)
	if got := lock.Skills["gh-default"].URL; got != "https://github.com/someone/else" {
		t.Errorf("existing gh-default was overwritten: url = %q", got)
	}
	if countSkipReason(res.Skipped, ReasonAlreadyPresent) < 1 {
		t.Error("expected an already_present skip for the colliding entry")
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
