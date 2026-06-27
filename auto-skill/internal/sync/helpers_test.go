package sync

import (
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

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found, skipping test")
	}
}

// fixture is a real on-disk git repo reachable over file://.
type fixture struct {
	t   *testing.T
	dir string
	url string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	f := &fixture{t: t, dir: dir, url: "file://" + dir}
	f.git("init")
	f.git("config", "user.email", "test@test.com")
	f.git("config", "user.name", "test")
	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitSkill writes skills/<name>/SKILL.md and commits, returning the new HEAD.
func (f *fixture) commitSkill(name, body string) string {
	f.t.Helper()
	full := filepath.Join(f.dir, "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		f.t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: Use when testing.\n---\n\n" + body + "\n"
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.git("add", "-A")
	f.git("commit", "-m", "commit "+name+" "+body)
	return f.head()
}

func (f *fixture) head() string {
	f.t.Helper()
	return f.git("rev-parse", "HEAD")
}

func (f *fixture) branch(name string) {
	f.t.Helper()
	f.git("branch", name)
}

func (f *fixture) tag(name string) {
	f.t.Helper()
	f.git("tag", "-f", name)
}

// remove deletes the upstream repo so any subsequent fetch fails.
func (f *fixture) remove() {
	f.t.Helper()
	if err := os.RemoveAll(f.dir); err != nil {
		f.t.Fatal(err)
	}
}

func newEnv(t *testing.T) skill.Env {
	t.Helper()
	return skill.Env{Root: t.TempDir(), RootOverride: true}
}

func approve(t *testing.T, env skill.Env, url string) {
	t.Helper()
	store := trust.NewStore(env.TrustPath())
	if err := store.Add(url); err != nil {
		t.Fatalf("approve %s: %v", url, err)
	}
}

// writeLock writes a lock.json with a single skill entry.
func writeLock(t *testing.T, env skill.Env, entries map[string]skill.LockEntry) {
	t.Helper()
	if err := os.MkdirAll(env.SkillsConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := &skill.Lock{Version: 1, Skills: entries}
	data, err := skill.EncodeJSON(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.LockPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSkillsYAML(t *testing.T, env skill.Env, cfg *skill.SkillsYAML) {
	t.Helper()
	if err := os.MkdirAll(env.SkillsConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.SkillsYAMLPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// realizeCommit pre-populates the upstream cache with a commit's objects so a
// later offline CommitPresent succeeds (simulates an already-fetched skill).
func realizeCommit(t *testing.T, env skill.Env, url, commit string) {
	t.Helper()
	_, cacheID, err := transport.CanonicalizeURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c := cache.NewCache(env.UpstreamCacheDir())
	repo, err := c.Open(cacheID, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Realize(commit); err != nil {
		t.Fatal(err)
	}
}

func mustCanonical(t *testing.T, url string) (string, transport.CacheIdentity, string) {
	t.Helper()
	canonical, id, err := transport.CanonicalizeURL(url)
	if err != nil {
		t.Fatal(err)
	}
	ep, err := transport.Endpoint(url)
	if err != nil {
		t.Fatal(err)
	}
	return canonical, id, ep
}

func findSkill(plan *Plan, name string) (SkillPlan, bool) {
	for i := range plan.Skills {
		if plan.Skills[i].Name == name {
			return plan.Skills[i], true
		}
	}
	return SkillPlan{}, false
}
