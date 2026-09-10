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
// project registry are all test-local, and clears every identity variable so
// a test process running under tmux/ntm resolves the same as a bare one.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range IdentityEnv {
		t.Setenv(key, "")
	}
	return home
}

// addWorktree adds a linked worktree of repo on a new branch and returns it.
func addWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "wt-"+strings.ReplaceAll(branch, "/", "-"))
	git(t, repo, "worktree", "add", "-q", "-b", branch, wt)
	return wt
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
	// Unregistered repo → project falls back to the main worktree path.
	if !samePath(w.Project, repo) {
		t.Errorf("project = %q, want repo root %q", w.Project, repo)
	}
}

// TestResolveRepoWorktreesShareProject verifies every linked worktree of an
// unregistered repo keys the store on the same project (the main worktree), so
// two branches of one repo actually serialize.
func TestResolveRepoWorktreesShareProject(t *testing.T) {
	isolateHome(t)
	repo := initRepo(t, "main")
	wt := addWorktree(t, repo, "feat/orders")

	mainRepo, err := ResolveRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := ResolveRepo(wt)
	if err != nil {
		t.Fatal(err)
	}
	if mainRepo.Linked() || !linked.Linked() {
		t.Errorf("Linked: main %v, worktree %v; want false, true", mainRepo.Linked(), linked.Linked())
	}
	if linked.Project != mainRepo.Project || !samePath(linked.Project, repo) {
		t.Errorf("projects differ: main %q, worktree %q; want both the main worktree", mainRepo.Project, linked.Project)
	}
	if !samePath(linked.Root, wt) || linked.Branch != "feat/orders" {
		t.Errorf("worktree repo = %+v, want root %s on feat/orders", linked, wt)
	}
}

func TestResolveWorkerLinkedWorktree(t *testing.T) {
	isolateHome(t)
	repo := initRepo(t, "main")
	wt := addWorktree(t, repo, "feat/orders")

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
	// Outside any repo is an error, never a panic.
	if _, err := ResolveWorker(t.TempDir(), nil, IdentityAuto); err == nil {
		t.Error("ResolveWorker(non-repo) = nil error, want error")
	}
}

// TestResolveWorkerChain (D-1, D-6) walks the identity chain table-driven over
// synthetic env × identity mode × checkout kind.
func TestResolveWorkerChain(t *testing.T) {
	isolateHome(t)
	repo := initRepo(t, "main")
	wt := addWorktree(t, repo, "feat/orders")

	cases := []struct {
		name     string
		cwd      string
		identity string
		env      map[string]string
		wantKind string
		wantID   string
		wantBare bool
	}{
		{name: "override wins on main", cwd: repo, identity: IdentityAuto, env: map[string]string{WorkerEnv: "worker-a", "TMUX_PANE": "%3"}, wantKind: KindOverride, wantID: "worker-a"},
		{name: "override wins in worktree", cwd: wt, identity: IdentityAuto, env: map[string]string{WorkerEnv: "worker-a"}, wantKind: KindOverride, wantID: "worker-a"},
		{name: "override wins in agent mode", cwd: wt, identity: IdentityAgent, env: map[string]string{WorkerEnv: "worker-a", "TMUX_PANE": "%3"}, wantKind: KindOverride, wantID: "worker-a"},
		{name: "override is trimmed", cwd: repo, identity: IdentityAuto, env: map[string]string{WorkerEnv: "  worker-a "}, wantKind: KindOverride, wantID: "worker-a"},
		{name: "linked worktree → worktree kind", cwd: wt, identity: IdentityAuto, wantKind: KindWorktree},
		{name: "empty identity defaults to auto", cwd: wt, identity: "", wantKind: KindWorktree},
		{name: "worktree mode ignores the pane in a worktree", cwd: wt, identity: IdentityWorktree, env: map[string]string{"TMUX_PANE": "%3"}, wantKind: KindWorktree},
		{name: "auto mode ignores the pane in a worktree", cwd: wt, identity: IdentityAuto, env: map[string]string{"TMUX_PANE": "%3"}, wantKind: KindWorktree},
		{name: "main + TMUX_PANE → agent", cwd: repo, identity: IdentityAuto, env: map[string]string{"TMUX_PANE": "%3"}, wantKind: KindAgent, wantID: "%3"},
		{name: "main + TMUX_PANE in worktree mode → agent", cwd: repo, identity: IdentityWorktree, env: map[string]string{"TMUX_PANE": "%3"}, wantKind: KindAgent, wantID: "%3"},
		{name: "TMUX_PANE beats the NTM label", cwd: repo, identity: IdentityAuto, env: map[string]string{"TMUX_PANE": "%3", "NTM_SPAWN_BATCH_ID": "b1", "NTM_SPAWN_ORDER": "2"}, wantKind: KindAgent, wantID: "%3"},
		{name: "main + NTM batch/order → agent", cwd: repo, identity: IdentityAuto, env: map[string]string{"NTM_SPAWN_BATCH_ID": "b1", "NTM_SPAWN_ORDER": "2"}, wantKind: KindAgent, wantID: "ntm:b1/2"},
		{name: "main + NTM batch only → agent", cwd: repo, identity: IdentityAuto, env: map[string]string{"NTM_SPAWN_BATCH_ID": "b1"}, wantKind: KindAgent, wantID: "ntm:b1"},
		{name: "NTM order alone is not a label", cwd: repo, identity: IdentityAuto, env: map[string]string{"NTM_SPAWN_ORDER": "2"}, wantBare: true},
		{name: "agent mode in a worktree with a pane → agent", cwd: wt, identity: IdentityAgent, env: map[string]string{"TMUX_PANE": "%4"}, wantKind: KindAgent, wantID: "%4"},
		{name: "agent mode in a worktree without a pane → bare", cwd: wt, identity: IdentityAgent, wantBare: true},
		{name: "main, no pane, no override → bare", cwd: repo, identity: IdentityAuto, wantBare: true},
		{name: "main, worktree mode, no pane → bare", cwd: repo, identity: IdentityWorktree, wantBare: true},
		{name: "empty env values count as unset", cwd: repo, identity: IdentityAuto, env: map[string]string{WorkerEnv: " ", "TMUX_PANE": "", "NTM_SPAWN_BATCH_ID": ""}, wantBare: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range IdentityEnv {
				t.Setenv(key, tc.env[key])
			}
			w, err := ResolveWorker(tc.cwd, nil, tc.identity)
			if tc.wantBare {
				var bare *BareError
				if !errors.As(err, &bare) {
					t.Fatalf("ResolveWorker = %+v, %v; want *BareError", w, err)
				}
				for _, want := range []string{WorkerEnv + "=<name>", "retry"} {
					if !strings.Contains(bare.Error(), want) {
						t.Errorf("bare error missing %q: %s", want, bare.Error())
					}
				}
				if tc.identity == IdentityAgent && strings.Contains(bare.Error(), "worktree") {
					t.Errorf("agent-mode bare error should not suggest a worktree: %s", bare.Error())
				}
				if tc.identity != IdentityAgent && !strings.Contains(bare.Error(), "linked git worktree") {
					t.Errorf("bare error should suggest a worktree: %s", bare.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveWorker: %v", err)
			}
			if w.Kind != tc.wantKind || w.WorkerID != tc.wantID {
				t.Errorf("worker = kind %q id %q, want kind %q id %q", w.Kind, w.WorkerID, tc.wantKind, tc.wantID)
			}
			if w.Kind == KindWorktree && (w.Branch != "feat/orders" || !samePath(w.WorktreePath, wt)) {
				t.Errorf("worktree worker = %+v, want branch feat/orders at %s", w, wt)
			}
			if w.Host == "" || w.Project == "" {
				t.Errorf("worker missing host/project: %+v", w)
			}
		})
	}
}

// TestResolveWorkerAsOverridesEnv verifies the take --as spelling of the
// override beats AUTO_LOCK_WORKER without touching the process env.
func TestResolveWorkerAsOverridesEnv(t *testing.T) {
	isolateHome(t)
	repo := initRepo(t, "main")
	t.Setenv(WorkerEnv, "worker-env")

	w, err := ResolveWorkerAs(repo, nil, IdentityAuto, "worker-flag")
	if err != nil || w.Kind != KindOverride || w.WorkerID != "worker-flag" {
		t.Errorf("ResolveWorkerAs = %+v, %v; want override worker-flag", w, err)
	}
	if got := os.Getenv(WorkerEnv); got != "worker-env" {
		t.Errorf("ResolveWorkerAs mutated %s to %q", WorkerEnv, got)
	}
	w, err = ResolveWorkerAs(repo, nil, IdentityAuto, "")
	if err != nil || w.WorkerID != "worker-env" {
		t.Errorf("ResolveWorkerAs(\"\") = %+v, %v; want env override", w, err)
	}
}

// TestWorktreeIdentitySurvivesSessionRestart (AC-5): the same worktree branch
// resolves to the same holder key across a session restart, so a Lock taken
// before the restart is still self-held after it; a second worktree branch is
// a different Worker and is blocked.
func TestWorktreeIdentitySurvivesSessionRestart(t *testing.T) {
	isolateHome(t)
	repo := initRepo(t, "main")
	writeConfig(t, repo, oneGroupConfig)
	wtA := addWorktree(t, repo, "feat/orders")
	wtB := addWorktree(t, repo, "feat/invoices")
	store := NewStore(filepath.Join(t.TempDir(), "lock"))

	before, err := ResolveWorker(wtA, map[string]any{"session_id": "sess-before"}, IdentityAuto)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ResolveWorker(wtA, map[string]any{"session_id": "sess-after"}, IdentityAuto)
	if err != nil {
		t.Fatal(err)
	}
	if before.SessionID == after.SessionID || !after.Matches(before.Holder) || !before.Matches(after.Holder) {
		t.Errorf("same branch across sessions should match: before %+v after %+v", before, after)
	}

	if _, err := store.Take(before.Project, "drizzle-schema", before, "add orders"); err != nil {
		t.Fatalf("Take: %v", err)
	}
	if l, err := store.Take(after.Project, "drizzle-schema", after, ""); err != nil || l.Holder.SessionID != "sess-before" {
		t.Errorf("Take after restart = %+v, %v; want the existing lock", l, err)
	}
	other, err := ResolveWorker(wtB, nil, IdentityAuto)
	if err != nil {
		t.Fatal(err)
	}
	if other.Matches(before.Holder) {
		t.Errorf("distinct worktree branches must not match: %+v vs %+v", other, before)
	}
	var held *HeldError
	if _, err := store.Take(other.Project, "drizzle-schema", other, ""); !errors.As(err, &held) || held.Lock.Holder.Branch != "feat/orders" {
		t.Errorf("Take by the other branch = %v, want *HeldError naming feat/orders", err)
	}
}

// TestSharedCheckoutPanesSerialize (AC-6): two panes on the same main checkout
// are distinct Workers — one takes, the other's guard denies naming the holder
// pane — and the same pane after a restart is still the holder.
func TestSharedCheckoutPanesSerialize(t *testing.T) {
	home := isolateHome(t)
	repo := initRepo(t, "main")
	writeConfig(t, repo, oneGroupConfig)
	store := NewStore(filepath.Join(home, ".auto", "lock"))

	t.Setenv("TMUX_PANE", "%3")
	pane3, err := ResolveWorker(repo, nil, IdentityAuto)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Take(pane3.Project, "drizzle-schema", pane3, "pane 3 work"); err != nil {
		t.Fatalf("Take: %v", err)
	}
	if d := Evaluate(repo, editPayload(repo, "db/schema/users.ts")); d.Deny {
		t.Errorf("holder pane should be allowed: %s", d.Reason)
	}

	t.Setenv("TMUX_PANE", "%4")
	pane4, err := ResolveWorker(repo, nil, IdentityAuto)
	if err != nil {
		t.Fatal(err)
	}
	if pane4.Matches(pane3.Holder) {
		t.Errorf("panes %%3 and %%4 must be distinct Workers: %+v vs %+v", pane4, pane3)
	}
	d := Evaluate(repo, editPayload(repo, "db/schema/users.ts"))
	if !d.Deny {
		t.Fatal("other pane should be denied")
	}
	for _, want := range []string{"worker %3", `"pane 3 work"`, "auto lock clear drizzle-schema --force", "no PR to verify"} {
		if !strings.Contains(d.Reason, want) {
			t.Errorf("held-by-pane reason missing %q:\n%s", want, d.Reason)
		}
	}
	if strings.Contains(d.Reason, "Wait for that PR") {
		t.Errorf("an agent holder has no PR; reason should not ask to wait for one:\n%s", d.Reason)
	}
}

// TestEvaluateDenyBare (AC-6, D-6): a lockable edit from a bare Worker is
// denied with the remediation, whether the group is unheld or held.
func TestEvaluateDenyBare(t *testing.T) {
	home := isolateHome(t)
	repo := initRepo(t, "main")
	writeConfig(t, repo, oneGroupConfig)

	d := Evaluate(repo, editPayload(repo, "db/schema/users.ts"))
	if !d.Deny {
		t.Fatal("bare worker on a lockable file should be denied")
	}
	for _, want := range []string{
		`db/schema/users.ts is under a serial-update lock group "drizzle-schema"`,
		"cannot be identified",
		"Ordered migrations clash",
		"linked git worktree",
		WorkerEnv + "=<name>",
		"auto lock take drizzle-schema",
	} {
		if !strings.Contains(d.Reason, want) {
			t.Errorf("bare reason missing %q:\n%s", want, d.Reason)
		}
	}

	// Held by someone else: still the D-6 message, since a bare worker cannot
	// act on the holder information either way.
	store := NewStore(filepath.Join(home, ".auto", "lock"))
	other := Worker{Project: repo, Holder: Holder{Kind: KindOverride, Host: "h", WorkerID: "worker-a"}}
	if _, err := store.Take(repo, "drizzle-schema", other, ""); err != nil {
		t.Fatal(err)
	}
	if d := Evaluate(repo, editPayload(repo, "db/schema/users.ts")); !d.Deny || !strings.Contains(d.Reason, WorkerEnv) {
		t.Errorf("bare worker on a held group = %+v, want D-6 deny", d)
	}
	// A bare worker editing an unmatched file is still allowed.
	if d := Evaluate(repo, editPayload(repo, "src/app.ts")); d.Deny {
		t.Errorf("bare worker on an unmatched file should allow: %s", d.Reason)
	}
}

// TestEvaluateHeldByWorktreeReason (AC-3): the held-by-another message names
// the holder branch, PR, reason, taken_at and the merge-verified clear path.
func TestEvaluateHeldByWorktreeReason(t *testing.T) {
	l := Lock{
		Project: "p", Group: "drizzle-schema",
		Holder:  Holder{Kind: KindWorktree, Host: "laptop-charlie", Branch: "feat/orders", WorktreePath: "/wt"},
		Reason:  "adding orders table",
		PR:      "42",
		TakenAt: "2026-08-31T14:02:11Z",
	}
	got := heldReason(l)
	for _, want := range []string{
		`✗ BLOCKED: "drizzle-schema" is locked by branch feat/orders (PR #42, "adding orders table")`,
		"taken 2026-08-31 14:02 on host laptop-charlie",
		"Wait for that PR to merge, then:  auto lock clear drizzle-schema",
		"clear refuses unless PR #42 is merged",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("held reason missing %q:\n%s", want, got)
		}
	}
	l.PR = ""
	if got := heldReason(l); !strings.Contains(got, "unless its PR is merged") || strings.Contains(got, "PR #") {
		t.Errorf("held reason without a PR should say 'its PR':\n%s", got)
	}
}

func TestStoreRelease(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "lock"))
	a := Worker{Project: "p", Holder: Holder{Kind: KindOverride, Host: "h", WorkerID: "worker-a"}}
	b := Worker{Project: "p", Holder: Holder{Kind: KindOverride, Host: "h", WorkerID: "worker-b"}}
	aElsewhere := Worker{Project: "q", Holder: a.Holder}
	for _, take := range []struct {
		w     Worker
		group string
	}{{a, "drizzle-schema"}, {a, "prisma"}, {b, "openapi"}, {aElsewhere, "drizzle-schema"}} {
		if _, err := store.Take(take.w.Project, take.group, take.w, ""); err != nil {
			t.Fatalf("Take %s/%s: %v", take.w.Project, take.group, err)
		}
	}

	// One group.
	if n, err := store.Release(a, "prisma"); err != nil || n != 1 {
		t.Errorf("Release(a, prisma) = %d, %v; want 1", n, err)
	}
	// Nothing to release is not an error.
	if n, err := store.Release(a, "prisma"); err != nil || n != 0 {
		t.Errorf("Release(a, prisma) again = %d, %v; want 0", n, err)
	}
	// Everything a holds on p — not b's, not a's on q.
	if n, err := store.Release(a, ""); err != nil || n != 1 {
		t.Errorf("Release(a, \"\") = %d, %v; want 1", n, err)
	}
	locks, err := store.List("")
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, l := range locks {
		left = append(left, l.Project+"/"+l.Group+"@"+l.Holder.WorkerID)
	}
	want := []string{"p/openapi@worker-b", "q/drizzle-schema@worker-a"}
	if strings.Join(left, ",") != strings.Join(want, ",") {
		t.Errorf("after release, locks = %v, want %v", left, want)
	}
	// A released group is takeable by the other Worker.
	if _, err := store.Take("p", "drizzle-schema", b, ""); err != nil {
		t.Errorf("Take after release: %v", err)
	}
	// Release on a missing store is 0, nil.
	if n, err := NewStore(filepath.Join(t.TempDir(), "nope")).Release(a, ""); err != nil || n != 0 {
		t.Errorf("Release on missing store = %d, %v; want 0, nil", n, err)
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
	if ov.Matches(Holder{Kind: KindAgent, Host: "h", WorkerID: "a"}) {
		t.Error("override and agent with the same id are different kinds and must not match")
	}
	ag := Worker{Holder: Holder{Kind: KindAgent, Host: "h", WorkerID: "%3", Branch: "main"}}
	if !ag.Matches(Holder{Kind: KindAgent, Host: "h", WorkerID: "%3", Branch: "feat/x", SessionID: "s2"}) {
		t.Error("agent should match on host+pane regardless of branch/session")
	}
	if ag.Matches(Holder{Kind: KindAgent, Host: "h", WorkerID: "%4"}) {
		t.Error("different panes must not match")
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
