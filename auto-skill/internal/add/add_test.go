package add

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/cache"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/transport"
	"github.com/mistakenot/auto-skill/internal/trust"
	"gopkg.in/yaml.v3"
)

// ── Helpers ─────────────────────────────────────────────────────────────

// makeGitFixture creates a temp git repo with the given skills.
// skills maps relative subpath to SKILL.md content.
// Returns the repo directory and file:// URL.
func makeGitFixture(t *testing.T, skills map[string]string) (repoDir string, fileURL string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s\n%s", strings.Join(args, " "), err, out)
		}
	}

	git("init")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "test")

	for subpath, content := range skills {
		full := filepath.Join(dir, subpath, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("add", "-A")
	git("commit", "-m", "initial")

	return dir, "file://" + dir
}

// makeEnv creates an Env rooted at a temp dir.
func makeEnv(t *testing.T) skill.Env {
	t.Helper()
	dir := t.TempDir()
	return skill.Env{Root: dir, RootOverride: true}
}

// approveEndpoint pre-approves a file:// endpoint in the trust store.
func approveEndpoint(t *testing.T, env skill.Env, fileURL string) {
	t.Helper()
	store := trust.NewStore(env.TrustPath())
	if err := store.Add(fileURL); err != nil {
		t.Fatalf("approve endpoint: %v", err)
	}
}

// readLock reads and parses the lock file from env.
func readLock(t *testing.T, env skill.Env) *skill.Lock {
	t.Helper()
	data, err := os.ReadFile(env.LockPath())
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	lock, err := skill.ParseLock(data)
	if err != nil {
		t.Fatalf("parse lock: %v", err)
	}
	return lock
}

// readSkillsYAML reads and parses the skills.yaml from env.
func readSkillsYAML(t *testing.T, env skill.Env) *skill.SkillsYAML {
	t.Helper()
	data, err := os.ReadFile(env.SkillsYAMLPath())
	if err != nil {
		t.Fatalf("read skills.yaml: %v", err)
	}
	cfg, err := skill.ParseSkillsYAML(data)
	if err != nil {
		t.Fatalf("parse skills.yaml: %v", err)
	}
	return cfg
}

func skillMD(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n## Workflow\n\n1. Do the thing.\n"
}

// ── Tests ───────────────────────────────────────────────────────────────

func TestFullPipelineHappyPath(t *testing.T) {
	_, fileURL := makeGitFixture(t, map[string]string{
		"skills/my-skill": skillMD("my-skill", "Use when testing the add pipeline."),
	})
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	result, err := Run(env, Options{
		Source:         fileURL,
		TrustRequested: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(result.Added))
	}
	added := result.Added[0]
	if added.Name != "my-skill" {
		t.Errorf("name = %q, want %q", added.Name, "my-skill")
	}
	if added.VersionSpec != "latest" {
		t.Errorf("version_spec = %q, want %q", added.VersionSpec, "latest")
	}
	if added.Commit == "" {
		t.Error("commit is empty")
	}

	// Verify lock.
	lock := readLock(t, env)
	entry, ok := lock.Skills["my-skill"]
	if !ok {
		t.Fatal("my-skill not found in lock")
	}
	if entry.State != "resolved" {
		t.Errorf("lock state = %q, want %q", entry.State, "resolved")
	}
	if entry.Commit != added.Commit {
		t.Errorf("lock commit = %q, want %q", entry.Commit, added.Commit)
	}

	// Verify skills.yaml.
	syaml := readSkillsYAML(t, env)
	sc, ok := syaml.Skills["my-skill"]
	if !ok {
		t.Fatal("my-skill not found in skills.yaml")
	}
	if sc.Version != "latest" {
		t.Errorf("skills.yaml version = %q, want %q", sc.Version, "latest")
	}

	// Verify no target dirs created.
	for _, subdir := range []string{".claude/skills", ".agents/skills"} {
		full := filepath.Join(env.Root, subdir)
		if _, err := os.Stat(full); err == nil {
			t.Errorf("target dir %s should not exist", subdir)
		}
	}
}

func TestListMode(t *testing.T) {
	_, fileURL := makeGitFixture(t, map[string]string{
		"skills/alpha": skillMD("alpha", "Use when testing alpha."),
		"skills/beta":  skillMD("beta", "Use when testing beta."),
	})
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	result, err := Run(env, Options{
		Source:         fileURL,
		List:           true,
		TrustRequested: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Added) != 0 {
		t.Errorf("expected 0 added in list mode, got %d", len(result.Added))
	}
	if len(result.Listed) != 2 {
		t.Fatalf("expected 2 listed, got %d", len(result.Listed))
	}

	// Verify nothing written.
	if _, err := os.Stat(env.LockPath()); err == nil {
		t.Error("lock.json should not exist in list mode")
	}
	if _, err := os.Stat(env.SkillsYAMLPath()); err == nil {
		t.Error("skills.yaml should not exist in list mode")
	}
}

func TestReAddIdempotent(t *testing.T) {
	_, fileURL := makeGitFixture(t, map[string]string{
		"skills/my-skill": skillMD("my-skill", "Use when testing re-add."),
	})
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	// First add.
	_, err := Run(env, Options{
		Source:         fileURL,
		TrustRequested: true,
	})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Manually set a replacement in skills.yaml to verify preservation.
	syaml := readSkillsYAML(t, env)
	var repNode yaml.Node
	repNode.Kind = yaml.ScalarNode
	repNode.Value = "test-replacement"
	repNode.Tag = "!!str"
	sc := syaml.Skills["my-skill"]
	sc.Replacements = map[string]yaml.Node{"greeting": repNode}
	syaml.Skills["my-skill"] = sc
	data, _ := yaml.Marshal(syaml)
	os.WriteFile(env.SkillsYAMLPath(), data, 0o644)

	// Second add — should update commit, preserve replacements.
	_, err = Run(env, Options{
		Source:         fileURL,
		TrustRequested: true,
	})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	syaml2 := readSkillsYAML(t, env)
	sc2, ok := syaml2.Skills["my-skill"]
	if !ok {
		t.Fatal("my-skill not found in skills.yaml after re-add")
	}
	if len(sc2.Replacements) != 1 {
		t.Errorf("replacements lost: got %d, want 1", len(sc2.Replacements))
	}
}

func TestNameCollisionDifferentSource(t *testing.T) {
	_, fileURL1 := makeGitFixture(t, map[string]string{
		"skills/collider": skillMD("collider", "Use when testing collision from source A."),
	})
	_, fileURL2 := makeGitFixture(t, map[string]string{
		"skills/collider": skillMD("collider", "Use when testing collision from source B."),
	})
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL1)
	approveEndpoint(t, env, fileURL2)

	// Add from source 1.
	_, err := Run(env, Options{
		Source:         fileURL1,
		TrustRequested: true,
	})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Add from source 2 with same name — should fail.
	_, err = Run(env, Options{
		Source:         fileURL2,
		TrustRequested: true,
	})
	if err == nil {
		t.Fatal("expected error for name collision from different source")
	}
	var addErr *AddError
	if ok := asAddError(err, &addErr); !ok || addErr.Code != CodeNameCollision {
		t.Errorf("expected AddError with code %q, got: %v", CodeNameCollision, err)
	}
}

func TestAsRename(t *testing.T) {
	_, fileURL := makeGitFixture(t, map[string]string{
		"skills/original": skillMD("original", "Use when testing as rename."),
	})
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	result, err := Run(env, Options{
		Source:         fileURL,
		As:             "my-renamed",
		TrustRequested: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(result.Added))
	}
	if result.Added[0].Name != "my-renamed" {
		t.Errorf("name = %q, want %q", result.Added[0].Name, "my-renamed")
	}

	lock := readLock(t, env)
	if _, ok := lock.Skills["my-renamed"]; !ok {
		t.Error("my-renamed not found in lock")
	}
	if _, ok := lock.Skills["original"]; ok {
		t.Error("original should not be in lock")
	}
}

func TestSkillFilter(t *testing.T) {
	_, fileURL := makeGitFixture(t, map[string]string{
		"skills/alpha": skillMD("alpha", "Use when testing alpha filter."),
		"skills/beta":  skillMD("beta", "Use when testing beta filter."),
	})
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	result, err := Run(env, Options{
		Source:         fileURL,
		Skills:         []string{"alpha"},
		TrustRequested: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(result.Added))
	}
	if result.Added[0].Name != "alpha" {
		t.Errorf("name = %q, want %q", result.Added[0].Name, "alpha")
	}

	lock := readLock(t, env)
	if _, ok := lock.Skills["beta"]; ok {
		t.Error("beta should not be in lock when filtered")
	}
}

func TestSkillFilterMatchNothing(t *testing.T) {
	_, fileURL := makeGitFixture(t, map[string]string{
		"skills/alpha": skillMD("alpha", "Use when testing."),
	})
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	_, err := Run(env, Options{
		Source:         fileURL,
		Skills:         []string{"nonexistent"},
		TrustRequested: true,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent skill filter")
	}
	var addErr *AddError
	if ok := asAddError(err, &addErr); !ok || addErr.Code != CodeSkillNotFound {
		t.Errorf("expected AddError with code %q, got: %v", CodeSkillNotFound, err)
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error should list available skills, got: %s", err.Error())
	}
}

func TestInvalidUpstreamName(t *testing.T) {
	_, fileURL := makeGitFixture(t, map[string]string{
		"skills/Bad_Name": skillMD("Bad_Name", "Use when testing invalid names."),
	})
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	// Without --as, should fail due to invalid name.
	_, err := Run(env, Options{
		Source:         fileURL,
		TrustRequested: true,
	})
	if err == nil {
		t.Fatal("expected error for invalid skill name")
	}
	var addErr *AddError
	if ok := asAddError(err, &addErr); !ok || addErr.Code != CodeInvalidSkillName {
		t.Errorf("expected AddError with code %q, got: %v", CodeInvalidSkillName, err)
	}

	// With --as, should succeed.
	result, err := Run(env, Options{
		Source:         fileURL,
		As:             "good-name",
		TrustRequested: true,
	})
	if err != nil {
		t.Fatalf("Run with --as: %v", err)
	}
	if len(result.Added) != 1 || result.Added[0].Name != "good-name" {
		t.Errorf("expected added skill named good-name, got: %+v", result.Added)
	}
}

func TestLocalGitRepo(t *testing.T) {
	repoDir, _ := makeGitFixture(t, map[string]string{
		"skills/local-skill": skillMD("local-skill", "Use when testing local git."),
	})
	env := makeEnv(t)

	result, err := Run(env, Options{
		Source: repoDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(result.Added))
	}
	added := result.Added[0]
	if !added.Local {
		t.Error("expected local=true")
	}
	if added.Commit == "" {
		t.Error("commit should not be empty for local git")
	}

	lock := readLock(t, env)
	entry, ok := lock.Skills["local-skill"]
	if !ok {
		t.Fatal("local-skill not found in lock")
	}
	if !entry.Local {
		t.Error("lock entry should have local=true")
	}
	if entry.State != "resolved" {
		t.Errorf("lock state = %q, want %q", entry.State, "resolved")
	}

	// Regression (auto-nfx): the lock URL must be a canonical file:// URL, not a
	// bare filesystem path. A bare path is mis-parsed by transport as an
	// empty-host https:// URL that the sync cache cannot open.
	if !strings.HasPrefix(entry.URL, "file://") {
		t.Errorf("lock URL = %q, want a file:// URL", entry.URL)
	}
	if entry.Source != entry.URL {
		t.Errorf("lock source = %q, want it to match URL %q", entry.Source, entry.URL)
	}
	// The stored URL must round-trip through the same canonicalizer sync uses,
	// yielding the local cache identity (the bug produced an empty host).
	canonical, id, err := transport.CanonicalizeURL(entry.URL)
	if err != nil {
		t.Fatalf("sync cannot canonicalize lock URL %q: %v", entry.URL, err)
	}
	if canonical != entry.URL {
		t.Errorf("canonical round-trip = %q, want stable %q", canonical, entry.URL)
	}
	if id.Host != "_local" {
		t.Errorf("cache identity host = %q, want %q (bare-path bug yields empty host)", id.Host, "_local")
	}
	if len(id.Path) == 0 {
		t.Error("cache identity has no path components; bare-path URL would not open")
	}
}

// TestLocalGitReAddLegacyBarePath simulates a lock written by the
// pre-reconciliation add path (bare absolute path URL) and verifies that
// re-adding the same local repo refreshes the entry — upgrading the URL to the
// canonical file:// form — instead of reporting a false name collision.
func TestLocalGitReAddLegacyBarePath(t *testing.T) {
	repoDir, _ := makeGitFixture(t, map[string]string{
		"skills/local-skill": skillMD("local-skill", "Use when testing legacy re-add."),
	})
	env := makeEnv(t)

	if _, err := Run(env, Options{Source: repoDir}); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Downgrade the stored entry to the legacy bare-path URL, as an older add
	// would have written it.
	lock := readLock(t, env)
	entry := lock.Skills["local-skill"]
	entry.Source = repoDir
	entry.URL = repoDir
	lock.Skills["local-skill"] = entry
	if err := writeJSONLock(env.LockPath(), lock); err != nil {
		t.Fatalf("rewrite legacy lock: %v", err)
	}

	// Re-add the same repo: must not collide, and must upgrade the URL.
	if _, err := Run(env, Options{Source: repoDir}); err != nil {
		t.Fatalf("re-add over legacy bare-path entry: %v", err)
	}

	got := readLock(t, env).Skills["local-skill"]
	if !strings.HasPrefix(got.URL, "file://") {
		t.Errorf("URL after re-add = %q, want it upgraded to a file:// URL", got.URL)
	}
}

func TestLocalNonGitImport(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a non-git directory with a skill.
	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skills", "imported")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := skillMD("imported", "Use when testing plain import.")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	env := makeEnv(t)

	result, err := Run(env, Options{
		Source: srcDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(result.Added))
	}
	if !result.Added[0].Local {
		t.Error("expected local=true")
	}

	// Verify files were copied to ./skills/<name>.
	destSkill := filepath.Join(env.SkillsDir(), "imported", "SKILL.md")
	if _, err := os.Stat(destSkill); err != nil {
		t.Errorf("expected copied SKILL.md at %s", destSkill)
	}

	// Verify no lock entry.
	if _, err := os.Stat(env.LockPath()); err == nil {
		// If lock exists, it should not have this skill.
		lock := readLock(t, env)
		if _, ok := lock.Skills["imported"]; ok {
			t.Error("plain import should not create lock entry")
		}
	}
}

func TestImportCollision(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skills", "collided")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte(skillMD("collided", "Use when testing collision.")), 0o644); err != nil {
		t.Fatal(err)
	}

	env := makeEnv(t)

	// Pre-create the destination so we get a collision.
	existing := filepath.Join(env.SkillsDir(), "collided")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "SKILL.md"),
		[]byte(skillMD("collided", "Use when already here.")), 0o644); err != nil {
		t.Fatal(err)
	}

	// Without --force, should fail.
	_, err := Run(env, Options{
		Source: srcDir,
	})
	if err == nil {
		t.Fatal("expected import collision error")
	}
	var addErr *AddError
	if ok := asAddError(err, &addErr); !ok || addErr.Code != CodeImportCollision {
		t.Errorf("expected AddError with code %q, got: %v", CodeImportCollision, err)
	}

	// With --force, should succeed.
	result, err := Run(env, Options{
		Source: srcDir,
		Force:  true,
	})
	if err != nil {
		t.Fatalf("Run with --force: %v", err)
	}
	if len(result.Added) != 1 {
		t.Errorf("expected 1 added with --force, got %d", len(result.Added))
	}
}

func TestTrustFailClosed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	env := makeEnv(t)

	// Use a remote-looking source (github.com) that has NOT been approved.
	// The trust gate should reject before we even try to clone.
	_, err := Run(env, Options{
		Source: "github.com/fake/nonexistent-repo-for-trust-test",
	})
	if err == nil {
		t.Fatal("expected trust error for unapproved endpoint")
	}
	var notApproved *trust.NotApprovedError
	if !asNotApprovedError(err, &notApproved) {
		t.Errorf("expected NotApprovedError, got: %T: %v", err, err)
	}
}

// ── JSON round-trip ─────────────────────────────────────────────────────

func TestResultJSON(t *testing.T) {
	r := Result{
		Added: []AddedSkill{
			{Name: "foo", Subpath: "skills/foo", Commit: "abc123", VersionSpec: "latest"},
		},
		Source: "https://github.com/acme/skills",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Result
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Added) != 1 || decoded.Added[0].Name != "foo" {
		t.Errorf("round-trip failed: %+v", decoded)
	}
}

// TestRepoRefResolver verifies the cache-backed adapter the add pipeline uses
// to split deep-link refs: a real branch resolves, an unknown ref does not.
func TestRepoRefResolver(t *testing.T) {
	repoDir, fileURL := makeGitFixture(t, map[string]string{
		"skills/foo": skillMD("foo", "Use when testing deep-link ref resolution."),
	})
	// Add a named branch the resolver should recognize.
	branchCmd := exec.Command("git", "branch", "release-1")
	branchCmd.Dir = repoDir
	if out, err := branchCmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch: %v\n%s", err, out)
	}

	env := makeEnv(t)
	canonical, id, err := transport.CanonicalizeURL(fileURL)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	repo, err := cache.NewCache(env.UpstreamCacheDir()).Open(id, canonical)
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}

	r := &repoRefResolver{repo: repo}
	if !r.ResolveRef("release-1") {
		t.Errorf("expected branch release-1 to resolve")
	}
	if r.ResolveRef("no-such-ref") {
		t.Errorf("expected no-such-ref to not resolve")
	}
}

// ── Error assertion helpers ─────────────────────────────────────────────

func asAddError(err error, target **AddError) bool {
	return errors.As(err, target)
}

func asNotApprovedError(err error, target **trust.NotApprovedError) bool {
	return errors.As(err, target)
}
