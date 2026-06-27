package cache

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-skill/internal/transport"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found, skipping test")
	}
}

// remoteSkillsRepo is a real public repo served by a git host (github.com) that
// advertises the partial-clone (--filter) capability. This matters: local path
// and file:// fixtures silently DROP --filter=blob:none ("--filter is ignored in
// local clones" / "filtering not recognized by server, ignoring"), so they clone
// every blob and can never exercise the blobless fetch path the cache hits in
// production. Only a real remote honors the filter, so only a real remote can
// catch regressions in Realize/CommitPresent against a partial clone.
const remoteSkillsRepo = "https://github.com/mistakenot/skills"

// requireRemote skips the test when git, the network, or the remote is
// unavailable, and returns the remote URL plus its current HEAD commit. This
// keeps the suite green offline (a skip, not a failure) while exercising the
// real partial-clone transport whenever the remote is reachable.
func requireRemote(t *testing.T) (url, headSHA string) {
	t.Helper()
	requireGit(t)
	out, err := exec.Command("git", "ls-remote", remoteSkillsRepo, "HEAD").CombinedOutput()
	if err != nil {
		t.Skipf("remote %s unreachable, skipping: %v\n%s", remoteSkillsRepo, err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || len(fields[0]) != 40 {
		t.Skipf("could not parse HEAD sha from ls-remote output: %q", out)
	}
	return remoteSkillsRepo, fields[0]
}

func createFixtureRepo(t *testing.T) (repoPath string, headSHA string) {
	t.Helper()
	requireGit(t)

	dir := t.TempDir()
	repoPath = filepath.Join(dir, "fixture.git")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init")
	skillDir := filepath.Join(repoPath, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Test Skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	return repoPath, string(out[:len(out)-1])
}

func TestCacheOpenAndClone(t *testing.T) {
	requireGit(t)
	fixtureRepo, _ := createFixtureRepo(t)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "repo"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := os.Stat(repo.Path); err != nil {
		t.Fatalf("repo path doesn't exist: %v", err)
	}
	// Bare repo has no .git subdir.
	if _, err := os.Stat(filepath.Join(repo.Path, ".git")); !os.IsNotExist(err) {
		t.Fatal("expected bare repo (no .git subdir)")
	}
	if _, err := os.Stat(filepath.Join(repo.Path, "HEAD")); err != nil {
		t.Fatal("expected HEAD in bare repo")
	}
}

func TestCacheRealizeAndCommitPresent(t *testing.T) {
	// Uses a real remote so --filter=blob:none is honored by the server; a local
	// fixture would silently full-clone and never exercise the blobless path.
	url, headSHA := requireRemote(t)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "github.com", Path: []string{"mistakenot", "skills"}}
	repo, err := c.Open(id, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}
	present, err := repo.CommitPresent(headSHA)
	if err != nil {
		t.Fatalf("CommitPresent: %v", err)
	}
	if !present {
		t.Fatal("expected commit to be present after Realize")
	}
}

func TestCommitPresentFailsForMissing(t *testing.T) {
	requireGit(t)
	fixtureRepo, _ := createFixtureRepo(t)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "repo"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	bogus := "0000000000000000000000000000000000000000"
	_, err = repo.CommitPresent(bogus)
	if err == nil {
		t.Fatal("expected error for non-existent commit")
	}
	errMsg := err.Error()
	if !contains(errMsg, "incomplete cache") {
		t.Fatalf("expected 'incomplete cache', got: %v", err)
	}
	if !contains(errMsg, "auto skill sync") {
		t.Fatalf("expected 'auto skill sync' remediation, got: %v", err)
	}
}

func TestCacheResolveRef(t *testing.T) {
	requireGit(t)
	fixtureRepo, headSHA := createFixtureRepo(t)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "repo"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}
	sha, err := repo.ResolveRef(headSHA)
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if sha != headSHA {
		t.Errorf("ResolveRef = %q, want %q", sha, headSHA)
	}
}

func TestSafeComponent(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"normal", "acme", false},
		{"with-hyphen", "my-repo", false},
		{"empty", "", true},
		{"dot", ".", true},
		{"dotdot", "..", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := safeComponent(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("safeComponent(%q) err = %v, wantErr = %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestSafeComponentEncodesControlChars(t *testing.T) {
	result, err := safeComponent("a\x00b")
	if err != nil {
		t.Fatal(err)
	}
	if result == "a\x00b" {
		t.Error("expected control char to be encoded")
	}
}

func TestSafeComponentEncodesPlatformReserved(t *testing.T) {
	result, err := safeComponent("CON")
	if err != nil {
		t.Fatal(err)
	}
	if result == "CON" {
		t.Error("expected CON to be encoded")
	}
}

func TestPathEscapesRoot(t *testing.T) {
	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "github.com", Path: []string{"acme", "skills"}}
	_, err := c.repoPath(id)
	if err != nil {
		t.Fatalf("expected no error: %v", err)
	}
}

func TestIsSubpathSafe(t *testing.T) {
	if isSubpathSafe("/root/cache", "/root/cache/../escape") {
		t.Error("expected path traversal to be unsafe")
	}
	if !isSubpathSafe("/root/cache", "/root/cache/safe/path") {
		t.Error("expected valid subpath to be safe")
	}
}

func TestCacheList(t *testing.T) {
	requireGit(t)
	fixtureRepo, _ := createFixtureRepo(t)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "repo"}}
	_, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	repos, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0].SizeBytes <= 0 {
		t.Error("expected positive size")
	}
}

func TestCachePruneDryRun(t *testing.T) {
	requireGit(t)
	fixtureRepo, _ := createFixtureRepo(t)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "repo"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	result, err := c.Prune(PruneOptions{MaxAge: 1 * time.Nanosecond, DryRun: true})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(result.Evicted) != 1 {
		t.Fatalf("expected 1 evicted in dry-run, got %d", len(result.Evicted))
	}
	if _, err := os.Stat(repo.Path); err != nil {
		t.Fatal("repo should still exist after dry-run")
	}
}

func TestCachePruneUnreferenced(t *testing.T) {
	requireGit(t)
	fixtureRepo, _ := createFixtureRepo(t)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "repo"}}
	_, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	result, err := c.Prune(PruneOptions{
		Unreferenced:  true,
		ReferencedIDs: map[string]bool{"other/repo": true},
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(result.Evicted) != 1 {
		t.Fatalf("expected 1 evicted, got %d", len(result.Evicted))
	}
}

func TestCacheConcurrency(t *testing.T) {
	// Real remote: concurrent Realize must converge to a commit whose objects are
	// genuinely present, which only a filter-honoring server can verify.
	url, headSHA := requireRemote(t)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "github.com", Path: []string{"mistakenot", "skills"}}
	repo, err := c.Open(id, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	n := 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = repo.Realize(headSHA)
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: %v", i, e)
		}
	}

	present, err := repo.CommitPresent(headSHA)
	if err != nil {
		t.Fatalf("CommitPresent: %v", err)
	}
	if !present {
		t.Fatal("commit not present after concurrent Realize")
	}
}

func TestCacheOpenReuseExisting(t *testing.T) {
	requireGit(t)
	fixtureRepo, _ := createFixtureRepo(t)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "repo"}}
	url := "file://" + fixtureRepo

	repo1, err := c.Open(id, url)
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}
	repo2, err := c.Open(id, url)
	if err != nil {
		t.Fatalf("Open 2: %v", err)
	}
	if repo1.Path != repo2.Path {
		t.Errorf("paths differ: %q vs %q", repo1.Path, repo2.Path)
	}
}

func TestPathEscapeDetection(t *testing.T) {
	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "evil.com", Path: []string{".."}}
	_, err := c.repoPath(id)
	if err == nil {
		t.Fatal("expected error for '..' path component")
	}

	id2 := transport.CacheIdentity{Host: "..", Path: []string{"escape"}}
	_, err = c.repoPath(id2)
	if err == nil {
		t.Fatal("expected error for '..' host component")
	}
}

func TestEmptyListOnMissingRoot(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), "nonexistent"))
	repos, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(repos))
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
