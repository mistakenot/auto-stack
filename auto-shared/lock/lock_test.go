package lock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const oneGroupConfig = `{
  "groups": [
    {
      "name": "drizzle-schema",
      "globs": ["db/schema/**", "db/migrations/**"],
      "description": "Ordered migrations clash if two branches change the schema in parallel."
    }
  ]
}
`

// initRepo creates a git repo on branch with one commit and returns its path.
func initRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "t")
	git(t, dir, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "README.md")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeConfig(t *testing.T, repo, body string) {
	t.Helper()
	path := ConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isolateHome points HOME at a fresh temp dir so the store, host id and
// project registry are all test-local, and clears the worker override.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(WorkerEnv, "")
	return home
}

func editPayload(repo, file string) map[string]any {
	return map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "sess-1",
		"cwd":             repo,
		"tool_name":       "Edit",
		"tool_input":      map[string]any{"file_path": filepath.Join(repo, file)},
	}
}

func TestLoadConfigOneGroup(t *testing.T) {
	repo := t.TempDir()
	writeConfig(t, repo, oneGroupConfig)

	cfg, err := LoadConfig(repo)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Identity != IdentityAuto {
		t.Errorf("identity = %q, want default %q", cfg.Identity, IdentityAuto)
	}
	if len(cfg.Groups) != 1 || cfg.Groups[0].Name != "drizzle-schema" {
		t.Fatalf("groups = %+v, want one drizzle-schema group", cfg.Groups)
	}
	if g := cfg.Group("drizzle-schema"); g == nil || len(g.Globs) != 2 {
		t.Errorf("Group lookup = %+v, want two globs", g)
	}
	if got := cfg.MatchGroups("db/schema/users.ts"); len(got) != 1 {
		t.Errorf("MatchGroups(db/schema/users.ts) = %v, want 1 match", got)
	}
	if got := cfg.MatchGroups("src/app.ts"); len(got) != 0 {
		t.Errorf("MatchGroups(src/app.ts) = %v, want no match", got)
	}
}

func TestLoadConfigAbsentIsNilNil(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	if cfg != nil || err != nil {
		t.Errorf("LoadConfig(absent) = %v, %v; want nil, nil", cfg, err)
	}
}

func TestLoadConfigValidation(t *testing.T) {
	repo := t.TempDir()
	writeConfig(t, repo, `{"identity":"bogus","groups":[{"name":"","globs":[],"description":""},{"name":"a","globs":["["],"description":"d"}]}`)

	_, err := LoadConfig(repo)
	var verr *sharedconfig.ValidationErrorsError
	if !errors.As(err, &verr) {
		t.Fatalf("LoadConfig error = %v, want *ValidationErrorsError", err)
	}
	codes := map[string]bool{}
	for _, e := range verr.Errors {
		codes[e.Code] = true
	}
	for _, want := range []string{"invalid_identity", "missing_name", "missing_globs", "missing_description", "invalid_glob"} {
		if !codes[want] {
			t.Errorf("missing validation code %q in %+v", want, verr.Errors)
		}
	}
}

func TestStoreTakeListRoundTrip(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "lock"))
	a := Worker{Project: "p", Holder: Holder{Kind: KindOverride, Host: "h", WorkerID: "worker-a"}}
	b := Worker{Project: "p", Holder: Holder{Kind: KindOverride, Host: "h", WorkerID: "worker-b"}}

	l, err := store.Take("p", "drizzle-schema", a, "add orders")
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if l.Holder.WorkerID != "worker-a" || l.Reason != "add orders" || l.TakenAt == "" {
		t.Errorf("Take returned %+v", l)
	}

	// Idempotent for the same Worker.
	again, err := store.Take("p", "drizzle-schema", a, "ignored")
	if err != nil {
		t.Fatalf("Take (self-held): %v", err)
	}
	if again.TakenAt != l.TakenAt || again.Reason != "add orders" {
		t.Errorf("self-held Take changed the lock: %+v", again)
	}

	// Held by another → *HeldError carrying the holder.
	_, err = store.Take("p", "drizzle-schema", b, "")
	var held *HeldError
	if !errors.As(err, &held) || held.Lock.Holder.WorkerID != "worker-a" {
		t.Fatalf("Take by other = %v, want *HeldError naming worker-a", err)
	}

	locks, err := store.List("p")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(locks) != 1 || locks[0].Group != "drizzle-schema" {
		t.Errorf("List = %+v, want exactly the one lock", locks)
	}
	if other, _ := store.List("other-project"); len(other) != 0 {
		t.Errorf("List(other-project) = %+v, want none", other)
	}
	if _, err := os.Stat(store.Path()); err != nil {
		t.Errorf("locks.json not persisted: %v", err)
	}
}

func TestStoreListMissingIsEmpty(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "nope"))
	locks, err := store.List("")
	if err != nil || len(locks) != 0 {
		t.Errorf("List on missing store = %v, %v; want empty, nil", locks, err)
	}
}

func TestResolveWorkerOverride(t *testing.T) {
	isolateHome(t)
	repo := initRepo(t, "main")
	t.Setenv(WorkerEnv, "worker-a")

	w, err := ResolveWorker(repo, map[string]any{"session_id": "sess-1"}, IdentityAuto)
	if err != nil {
		t.Fatalf("ResolveWorker: %v", err)
	}
	if w.Kind != KindOverride || w.WorkerID != "worker-a" {
		t.Errorf("worker = %+v, want override worker-a", w)
	}
	if w.Host == "" || w.Project == "" || w.SessionID != "sess-1" {
		t.Errorf("worker missing host/project/session: %+v", w)
	}
	// Unregistered repo → project falls back to the repo root path.
	if w.Project != repo {
		t.Errorf("project = %q, want repo root %q", w.Project, repo)
	}
}

func TestResolveWorkerLinkedWorktree(t *testing.T) {
	isolateHome(t)
	repo := initRepo(t, "main")
	wt := filepath.Join(t.TempDir(), "wt-orders")
	git(t, repo, "worktree", "add", "-q", "-b", "feat/orders", wt)

	w, err := ResolveWorker(wt, nil, IdentityAuto)
	if err != nil {
		t.Fatalf("ResolveWorker(worktree): %v", err)
	}
	if w.Kind != KindWorktree || w.Branch != "feat/orders" {
		t.Errorf("worker = %+v, want worktree feat/orders", w)
	}
	if !samePath(w.WorktreePath, wt) {
		t.Errorf("worktree_path = %q, want %q", w.WorktreePath, wt)
	}

	// The same worktree resolves to the same key on a second call (session restart).
	w2, err := ResolveWorker(wt, nil, IdentityAuto)
	if err != nil || !w2.Matches(w.Holder) {
		t.Errorf("second resolve %+v (%v) does not match first %+v", w2, err, w)
	}

	// The main checkout with no override is unresolved in Phase 1.
	if _, err := ResolveWorker(repo, nil, IdentityAuto); !errors.Is(err, ErrUnresolved) {
		t.Errorf("ResolveWorker(main) = %v, want ErrUnresolved", err)
	}
	// Outside any repo is an error, never a panic.
	if _, err := ResolveWorker(t.TempDir(), nil, IdentityAuto); err == nil {
		t.Error("ResolveWorker(non-repo) = nil error, want error")
	}
}

func TestWorkerMatches(t *testing.T) {
	wt := Worker{Holder: Holder{Kind: KindWorktree, Host: "h", Branch: "b", WorktreePath: "/p"}}
	if !wt.Matches(Holder{Kind: KindWorktree, Host: "h", Branch: "b", WorktreePath: "/p", SessionID: "other"}) {
		t.Error("worktree holder should match on branch+path regardless of session")
	}
	if wt.Matches(Holder{Kind: KindWorktree, Host: "h", Branch: "b", WorktreePath: "/q"}) {
		t.Error("different worktree path should not match")
	}
	if wt.Matches(Holder{Kind: KindOverride, Host: "h", WorkerID: "b"}) {
		t.Error("different kind should not match")
	}
	ov := Worker{Holder: Holder{Kind: KindOverride, Host: "h", WorkerID: "a"}}
	if !ov.Matches(Holder{Kind: KindOverride, Host: "h", WorkerID: "a"}) || ov.Matches(Holder{Kind: KindOverride, Host: "h2", WorkerID: "a"}) {
		t.Error("override should match on host+worker_id only")
	}
}

func TestEvaluateDenyUnheld(t *testing.T) {
	isolateHome(t)
	t.Setenv(WorkerEnv, "worker-a")
	repo := initRepo(t, "main")
	writeConfig(t, repo, oneGroupConfig)

	d := Evaluate(repo, editPayload(repo, "db/schema/users.ts"))
	if !d.Deny {
		t.Fatal("expected deny on unheld group")
	}
	for _, want := range []string{
		`db/schema/users.ts is under a serial-update lock group "drizzle-schema"`,
		"Ordered migrations clash",
		"auto lock take drizzle-schema",
		"auto lock release",
	} {
		if !strings.Contains(d.Reason, want) {
			t.Errorf("reason missing %q:\n%s", want, d.Reason)
		}
	}
}

func TestEvaluateAllowSelfHeld(t *testing.T) {
	home := isolateHome(t)
	t.Setenv(WorkerEnv, "worker-a")
	repo := initRepo(t, "main")
	writeConfig(t, repo, oneGroupConfig)

	w, err := ResolveWorker(repo, nil, IdentityAuto)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(home, ".auto", "lock"))
	if _, err := store.Take(w.Project, "drizzle-schema", w, "mine"); err != nil {
		t.Fatal(err)
	}
	if d := Evaluate(repo, editPayload(repo, "db/schema/users.ts")); d.Deny {
		t.Errorf("self-held should allow, got deny: %s", d.Reason)
	}

	// A different Worker is denied with the holder named.
	t.Setenv(WorkerEnv, "worker-b")
	d := Evaluate(repo, editPayload(repo, "db/migrations/0001.sql"))
	if !d.Deny || !strings.Contains(d.Reason, "worker-a") || !strings.Contains(d.Reason, "auto lock clear drizzle-schema") {
		t.Errorf("held-by-other deny = %+v, want holder worker-a + clear remediation", d)
	}
}

func TestEvaluateAllowNoConfigNoMatchNonEdit(t *testing.T) {
	isolateHome(t)
	t.Setenv(WorkerEnv, "worker-a")
	repo := initRepo(t, "main")

	if d := Evaluate(repo, editPayload(repo, "db/schema/users.ts")); d.Deny {
		t.Errorf("no config should allow: %s", d.Reason)
	}
	writeConfig(t, repo, oneGroupConfig)
	if d := Evaluate(repo, editPayload(repo, "src/app.ts")); d.Deny {
		t.Errorf("non-matching path should allow: %s", d.Reason)
	}
	p := editPayload(repo, "db/schema/users.ts")
	p["tool_name"] = "Bash"
	if d := Evaluate(repo, p); d.Deny {
		t.Errorf("non-edit tool should allow: %s", d.Reason)
	}
	p = editPayload(repo, "db/schema/users.ts")
	p["hook_event_name"] = "PostToolUse"
	if d := Evaluate(repo, p); d.Deny {
		t.Errorf("non-PreToolUse event should allow: %s", d.Reason)
	}
	if d := Evaluate(repo, nil); d.Deny {
		t.Errorf("nil payload should allow: %s", d.Reason)
	}
}

func TestEvaluateFailOpen(t *testing.T) {
	home := isolateHome(t)
	t.Setenv(WorkerEnv, "worker-a")
	repo := initRepo(t, "main")
	writeConfig(t, repo, oneGroupConfig)

	// Corrupt store → allow.
	dir := filepath.Join(home, ".auto", "lock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "locks.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := Evaluate(repo, editPayload(repo, "db/schema/users.ts")); d.Deny {
		t.Errorf("corrupt store should fail open: %s", d.Reason)
	}

	// Invalid config → allow.
	writeConfig(t, repo, `{"groups":[{"name":"x"}]}`)
	if err := os.Remove(filepath.Join(dir, "locks.json")); err != nil {
		t.Fatal(err)
	}
	if d := Evaluate(repo, editPayload(repo, "db/schema/users.ts")); d.Deny {
		t.Errorf("invalid config should fail open: %s", d.Reason)
	}

	// Not a git repo → allow.
	if d := Evaluate(t.TempDir(), editPayload(repo, "db/schema/users.ts")); d.Deny {
		t.Errorf("non-repo cwd should fail open: %s", d.Reason)
	}
}

func TestRelativeToRoot(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "r")
	cases := []struct{ p, want string }{
		{filepath.Join(root, "db", "schema", "u.ts"), "db/schema/u.ts"},
		{"db/schema/u.ts", "db/schema/u.ts"},
		{filepath.Join(string(filepath.Separator), "elsewhere", "u.ts"), ""},
		{root, ""},
	}
	for _, c := range cases {
		if got := relativeToRoot(c.p, root, root); got != c.want {
			t.Errorf("relativeToRoot(%q) = %q, want %q", c.p, got, c.want)
		}
	}
}
