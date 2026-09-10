package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	pinTmuxPanes(t, "%3", "%4")

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

// overrideWorker is a Worker of override kind on host h — no liveness token.
func overrideWorker(project, id string) Worker {
	return Worker{Project: project, Holder: Holder{Kind: KindOverride, Host: "h", WorkerID: id}}
}

// liveStore is a store on host "h" whose probes say every holder is live, so
// tests of Take/List/Clear are not disturbed by the real probes seeing the
// synthetic holders' paths and panes.
func liveStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "lock"))
	s.Host = "h"
	s.WorktreeExists = func(string) bool { return true }
	s.TmuxPanes = func() ([]string, error) { return nil, errors.New("tmux not probed in this test") }
	return s
}

// readStoreFile parses locks.json straight off disk.
func readStoreFile(t *testing.T, s *Store) storeFile {
	t.Helper()
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read %s: %v", s.Path(), err)
	}
	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("locks.json is not valid JSON: %v\n%s", err, data)
	}
	return sf
}

// TestStoreConcurrentTakeSameGroup (AC-8) is the race proof, task-063
// discipline: four takers — not two, which hides a lost write as "ambiguous"
// — each with its OWN Store handle, released together on a barrier, run
// under -race. Exactly one must win, the other three must see the winner in
// their *HeldError, and locks.json must parse with exactly that one lock.
// Validated against a deliberately broken build (withLock without the flock):
// without the flock all four read an empty store and all four "win".
func TestStoreConcurrentTakeSameGroup(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lock")
	const takers = 4

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, takers)
	for i := range takers {
		wg.Go(func() {
			store := NewStore(dir)
			store.Host = "h"
			<-start
			_, results[i] = store.Take("p", "drizzle-schema", overrideWorker("p", fmt.Sprintf("worker-%d", i)), "")
		})
	}
	close(start)
	wg.Wait()

	winner := ""
	var losers []*HeldError
	for i, err := range results {
		var held *HeldError
		switch {
		case err == nil:
			if winner != "" {
				t.Fatalf("two takers won: %s and worker-%d", winner, i)
			}
			winner = fmt.Sprintf("worker-%d", i)
		case errors.As(err, &held):
			losers = append(losers, held)
		default:
			t.Fatalf("taker %d: unexpected error %v", i, err)
		}
	}
	if winner == "" {
		t.Fatal("no taker won")
	}
	if len(losers) != takers-1 {
		t.Fatalf("losers = %d, want %d", len(losers), takers-1)
	}
	for _, held := range losers {
		if held.Lock.Holder.WorkerID != winner {
			t.Errorf("loser saw holder %q, want the winner %q", held.Lock.Holder.WorkerID, winner)
		}
	}
	sf := readStoreFile(t, NewStore(dir))
	if len(sf.Locks) != 1 || sf.Locks[0].Holder.WorkerID != winner {
		t.Errorf("locks.json = %+v, want exactly one lock held by %s", sf.Locks, winner)
	}
}

// TestStoreConcurrentTakeDifferentGroups (AC-8): N parallel takes of N
// distinct groups through N store handles all land — no update is lost to a
// concurrent read-modify-write.
func TestStoreConcurrentTakeDifferentGroups(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lock")
	const takers = 8

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, takers)
	for i := range takers {
		wg.Go(func() {
			store := NewStore(dir)
			store.Host = "h"
			<-start
			_, errs[i] = store.Take("p", fmt.Sprintf("group-%d", i), overrideWorker("p", fmt.Sprintf("worker-%d", i)), "")
		})
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("taker %d: %v", i, err)
		}
	}
	sf := readStoreFile(t, NewStore(dir))
	got := map[string]string{}
	for _, l := range sf.Locks {
		got[l.Group] = l.Holder.WorkerID
	}
	if len(got) != takers {
		t.Fatalf("locks.json has %d locks, want %d (lost write): %+v", len(got), takers, sf.Locks)
	}
	for i := range takers {
		if got[fmt.Sprintf("group-%d", i)] != fmt.Sprintf("worker-%d", i) {
			t.Errorf("group-%d held by %q, want worker-%d", i, got[fmt.Sprintf("group-%d", i)], i)
		}
	}
}

// TestReclaimWorktreeGone (AC-10 a) — a worktree holder whose worktree_path
// was deleted is reclaimed on the next List, the next Take by another Worker
// succeeds, and the audit records the reclaim with its reason.
func TestReclaimWorktreeGone(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "lock"))
	store.Host = "h"
	wt := filepath.Join(t.TempDir(), "wt-orders")
	if err := os.Mkdir(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	dead := Worker{Project: "p", Holder: Holder{Kind: KindWorktree, Host: "h", Branch: "feat/orders", WorktreePath: wt}}
	if _, err := store.Take("p", "drizzle-schema", dead, "add orders"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	locks, err := store.List("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 0 {
		t.Fatalf("List after worktree removal = %+v, want the lock reclaimed", locks)
	}
	if l, err := store.Take("p", "drizzle-schema", overrideWorker("p", "worker-b"), ""); err != nil || l.Holder.WorkerID != "worker-b" {
		t.Errorf("Take after reclaim = %+v, %v; want worker-b to hold it", l, err)
	}
	sf := readStoreFile(t, store)
	if len(sf.Audit) != 1 {
		t.Fatalf("audit = %+v, want one reclaimed entry", sf.Audit)
	}
	a := sf.Audit[0]
	if a.Action != AuditReclaimed || a.Group != "drizzle-schema" || a.Holder.Branch != "feat/orders" || a.By.Host != "h" || !strings.Contains(a.Note, wt) {
		t.Errorf("reclaimed audit entry = %+v", a)
	}
}

// TestReclaimLiveIdleWorktreeSurvives (AC-10 b, the negative) — a worktree
// holder whose path still exists but shows no activity at all is live, is
// NOT reclaimed, and still blocks the next taker. A reap test that only
// checks the dead case can pass while encoding a false-reap bug; this one
// pins the other half.
func TestReclaimLiveIdleWorktreeSurvives(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "lock"))
	store.Host = "h"
	wt := filepath.Join(t.TempDir(), "wt-orders")
	if err := os.Mkdir(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	idle := Worker{Project: "p", Holder: Holder{Kind: KindWorktree, Host: "h", Branch: "feat/orders", WorktreePath: wt}}
	if _, err := store.Take("p", "drizzle-schema", idle, ""); err != nil {
		t.Fatal(err)
	}

	locks, err := store.List("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 1 || locks[0].Holder.Branch != "feat/orders" {
		t.Fatalf("List = %+v, want the idle holder still recorded", locks)
	}
	var held *HeldError
	if _, err := store.Take("p", "drizzle-schema", overrideWorker("p", "worker-b"), ""); !errors.As(err, &held) || held.Lock.Holder.Branch != "feat/orders" {
		t.Errorf("Take while an idle-but-live worktree holds it = %v, want *HeldError naming feat/orders", err)
	}
	if sf := readStoreFile(t, store); len(sf.Audit) != 0 {
		t.Errorf("audit = %+v, want nothing reclaimed", sf.Audit)
	}
}

// TestReclaimTmuxPane (AC-10 c–f) walks the pane probe outcomes for an agent
// holder keyed on tmux pane %7 plus the holders that carry no liveness token.
func TestReclaimTmuxPane(t *testing.T) {
	cases := []struct {
		name      string
		holder    Holder
		panes     []string
		probeErr  error
		reclaimed bool
	}{
		{name: "pane absent → reclaimed", holder: Holder{Kind: KindAgent, Host: "h", WorkerID: "%7"}, panes: []string{"%1", "%3"}, reclaimed: true},
		{name: "pane present → survives", holder: Holder{Kind: KindAgent, Host: "h", WorkerID: "%7"}, panes: []string{"%1", "%7"}},
		{name: "probe error → survives", holder: Holder{Kind: KindAgent, Host: "h", WorkerID: "%7"}, probeErr: errors.New("no server running")},
		{name: "probe error with empty list → survives", holder: Holder{Kind: KindAgent, Host: "h", WorkerID: "%7"}, panes: []string{}, probeErr: errors.New("timeout")},
		{name: "NTM label holder → never reclaimed", holder: Holder{Kind: KindAgent, Host: "h", WorkerID: "ntm:b1/2"}, panes: []string{}},
		{name: "override holder → never reclaimed", holder: Holder{Kind: KindOverride, Host: "h", WorkerID: "worker-a"}, panes: []string{}},
		{name: "other host's pane → never reclaimed", holder: Holder{Kind: KindAgent, Host: "elsewhere", WorkerID: "%7"}, panes: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewStore(filepath.Join(t.TempDir(), "lock"))
			store.Host = "h"
			probed := 0
			store.TmuxPanes = func() ([]string, error) {
				probed++
				return tc.panes, tc.probeErr
			}
			if _, err := store.Take("p", "drizzle-schema", Worker{Project: "p", Holder: tc.holder}, ""); err != nil {
				t.Fatal(err)
			}
			probed = 0

			locks, err := store.List("p")
			if err != nil {
				t.Fatal(err)
			}
			if got := len(locks) == 0; got != tc.reclaimed {
				t.Errorf("reclaimed = %v, want %v (locks %+v)", got, tc.reclaimed, locks)
			}
			wantsProbe := tc.holder.Kind == KindAgent && tc.holder.Host == "h" && strings.HasPrefix(tc.holder.WorkerID, "%")
			if wantsProbe && probed != 1 {
				t.Errorf("tmux probed %d times, want exactly once per List", probed)
			}
			if !wantsProbe && probed != 0 {
				t.Errorf("tmux probed %d times for a holder with no pane token, want 0", probed)
			}
			_, err = store.Take("p", "drizzle-schema", overrideWorker("p", "worker-b"), "")
			var held *HeldError
			if tc.reclaimed && err != nil {
				t.Errorf("Take after reclaim = %v, want success", err)
			}
			if !tc.reclaimed && !errors.As(err, &held) {
				t.Errorf("Take while a live holder holds it = %v, want *HeldError", err)
			}
			sf := readStoreFile(t, store)
			if tc.reclaimed && (len(sf.Audit) != 1 || sf.Audit[0].Action != AuditReclaimed || sf.Audit[0].Holder.WorkerID != "%7") {
				t.Errorf("audit = %+v, want one reclaimed entry for %%7", sf.Audit)
			}
			if !tc.reclaimed && len(sf.Audit) != 0 {
				t.Errorf("audit = %+v, want none", sf.Audit)
			}
		})
	}
}

// TestReclaimRealProbesDefault verifies the nil probes fall back to the real
// ones: a worktree holder at a path that never existed is reclaimed, and a
// tmux failure (PATH emptied, so tmux cannot be found) keeps a pane holder.
func TestReclaimRealProbesDefault(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "lock"))
	store.Host = "h"
	t.Setenv("PATH", t.TempDir())
	gone := Worker{Project: "p", Holder: Holder{Kind: KindWorktree, Host: "h", Branch: "b", WorktreePath: filepath.Join(t.TempDir(), "never")}}
	pane := Worker{Project: "p", Holder: Holder{Kind: KindAgent, Host: "h", WorkerID: "%999"}}
	for _, w := range []Worker{gone, pane} {
		if _, err := store.Take("p", "g-"+w.Kind, w, ""); err != nil {
			t.Fatal(err)
		}
	}
	locks, err := store.List("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 1 || locks[0].Holder.Kind != KindAgent {
		t.Errorf("List = %+v, want only the pane holder (tmux unavailable = live)", locks)
	}
}

// TestEvaluateReclaimsDeadHolder (AC-10) — the guard's lookup reclaims a dead
// worktree holder, so the next taker is not blocked by it: the deny it gets is
// the "unheld, take it" shape, not "held by feat/orders".
func TestEvaluateReclaimsDeadHolder(t *testing.T) {
	home := isolateHome(t)
	t.Setenv(WorkerEnv, "worker-b")
	repo := initRepo(t, "main")
	writeConfig(t, repo, oneGroupConfig)
	w, err := ResolveWorker(repo, nil, IdentityAuto)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(home, ".auto", "lock"))
	dead := Worker{Project: w.Project, Holder: Holder{Kind: KindWorktree, Host: w.Host, Branch: "feat/orders", WorktreePath: filepath.Join(t.TempDir(), "gone")}}
	if _, err := store.Take(w.Project, "drizzle-schema", dead, ""); err != nil {
		t.Fatal(err)
	}
	d := Evaluate(repo, editPayload(repo, "db/schema/users.ts"))
	if !d.Deny || strings.Contains(d.Reason, "feat/orders") || !strings.Contains(d.Reason, "auto lock take drizzle-schema") {
		t.Errorf("guard after a dead holder = %+v, want the unheld deny, not held-by feat/orders", d)
	}
	if _, err := store.Take(w.Project, "drizzle-schema", w, ""); err != nil {
		t.Errorf("Take after the guard reclaimed = %v", err)
	}
}

// stubGH scripts the PR lookup for Clear.
type stubGH struct {
	number, state string
	err           error
	calls         []string
}

func (g *stubGH) PRState(branch string) (string, string, error) {
	g.calls = append(g.calls, branch)
	return g.number, g.state, g.err
}

// TestStoreClear (AC-9) — Clear refuses on an open PR, no PR, a gh failure,
// and an agent holder, leaving the store untouched; releases on MERGED or
// --force with a cleared audit entry carrying holder/by/forced/pr/state.
func TestStoreClear(t *testing.T) {
	worktreeHolder := Holder{Kind: KindWorktree, Host: "h", Branch: "feat/orders", WorktreePath: "/wt"}
	agentHolder := Holder{Kind: KindAgent, Host: "h", WorkerID: "%3"}
	by := Holder{Kind: KindOverride, Host: "h", WorkerID: "worker-b"}

	cases := []struct {
		name        string
		holder      Holder
		gh          *stubGH
		force       bool
		wantErr     []string
		wantPR      string
		wantState   string
		wantGHCalls int
	}{
		{name: "open PR refuses", holder: worktreeHolder, gh: &stubGH{number: "42", state: "OPEN"}, wantErr: []string{"PR #42 is still OPEN", "not cleared", "--force"}, wantGHCalls: 1},
		{name: "closed PR refuses", holder: worktreeHolder, gh: &stubGH{number: "42", state: "CLOSED"}, wantErr: []string{"PR #42 is still CLOSED", "--force"}, wantGHCalls: 1},
		{name: "no PR refuses", holder: worktreeHolder, gh: &stubGH{err: ErrNoPR}, wantErr: []string{"no PR found for holder branch feat/orders", "--force"}, wantGHCalls: 1},
		{name: "gh failure refuses", holder: worktreeHolder, gh: &stubGH{err: errors.New("gh: not logged in")}, wantErr: []string{"cannot verify holder branch feat/orders", "gh: not logged in", "--force"}, wantGHCalls: 1},
		{name: "agent holder refuses without force", holder: agentHolder, gh: &stubGH{number: "1", state: PRStateMerged}, wantErr: []string{"worker %3", "no PR to verify", "--force"}, wantGHCalls: 0},
		{name: "merged releases", holder: worktreeHolder, gh: &stubGH{number: "42", state: PRStateMerged}, wantPR: "42", wantState: PRStateMerged, wantGHCalls: 1},
		{name: "force releases an open PR", holder: worktreeHolder, gh: &stubGH{number: "42", state: "OPEN"}, force: true, wantPR: "42", wantState: "OPEN", wantGHCalls: 1},
		{name: "force releases when gh fails", holder: worktreeHolder, gh: &stubGH{err: errors.New("boom")}, force: true, wantGHCalls: 1},
		{name: "force releases an agent holder", holder: agentHolder, gh: &stubGH{}, force: true, wantGHCalls: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := liveStore(t)
			if _, err := store.Take("p", "drizzle-schema", Worker{Project: "p", Holder: tc.holder}, "why"); err != nil {
				t.Fatal(err)
			}
			before := readStoreFile(t, store)

			res, err := store.Clear("p", "drizzle-schema", by, tc.force, tc.gh)
			if len(tc.gh.calls) != tc.wantGHCalls {
				t.Errorf("gh called %d times (%v), want %d", len(tc.gh.calls), tc.gh.calls, tc.wantGHCalls)
			}
			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("Clear = %+v, nil; want refusal", res)
				}
				for _, want := range tc.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal missing %q: %v", want, err)
					}
				}
				after := readStoreFile(t, store)
				if len(after.Locks) != 1 || len(after.Audit) != 0 || after.Locks[0].TakenAt != before.Locks[0].TakenAt {
					t.Errorf("refusal changed the store: %+v", after)
				}
				return
			}
			if err != nil {
				t.Fatalf("Clear: %v", err)
			}
			if res.Forced != tc.force || res.PR != tc.wantPR || res.State != tc.wantState || res.Lock.Holder != tc.holder {
				t.Errorf("ClearResult = %+v, want forced %v pr %q state %q holder %+v", res, tc.force, tc.wantPR, tc.wantState, tc.holder)
			}
			after := readStoreFile(t, store)
			if len(after.Locks) != 0 {
				t.Errorf("lock still present after clear: %+v", after.Locks)
			}
			if len(after.Audit) != 1 {
				t.Fatalf("audit = %+v, want one cleared entry", after.Audit)
			}
			a := after.Audit[0]
			if a.Action != AuditCleared || a.Project != "p" || a.Group != "drizzle-schema" || a.Holder != tc.holder || a.By != by || a.Forced != tc.force || a.PR != tc.wantPR || a.State != tc.wantState || a.At == "" {
				t.Errorf("cleared audit entry = %+v", a)
			}
			// The group is takeable again.
			if _, err := store.Take("p", "drizzle-schema", overrideWorker("p", "worker-b"), ""); err != nil {
				t.Errorf("Take after clear: %v", err)
			}
		})
	}
}

// TestStoreClearNotHeld — clearing an unheld group is an error and writes
// nothing (the store file is never even created).
func TestStoreClearNotHeld(t *testing.T) {
	store := liveStore(t)
	gh := &stubGH{number: "1", state: PRStateMerged}
	_, err := store.Clear("p", "drizzle-schema", Holder{Host: "h"}, false, gh)
	if err == nil || !strings.Contains(err.Error(), "not held") {
		t.Errorf("Clear(unheld) = %v, want a not-held error", err)
	}
	if len(gh.calls) != 0 {
		t.Errorf("gh consulted for an unheld group: %v", gh.calls)
	}
	if _, err := os.Stat(store.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("refusal created %s: %v", store.Path(), err)
	}
}

// TestConfigValidateTable (AC-12): each offending field yields exactly the
// expected entries from the one shared validator, keyed by code, path and
// field; a valid config yields none.
func TestConfigValidateTable(t *testing.T) {
	type want struct{ code, path, field string }
	good := Group{Name: "drizzle-schema", Globs: []string{"db/schema/**"}, Description: "ordered migrations"}
	cases := []struct {
		name string
		cfg  Config
		want []want
	}{
		{name: "valid", cfg: Config{Identity: IdentityAuto, Groups: []Group{good}}},
		{name: "valid empty identity and underscore name", cfg: Config{Groups: []Group{{Name: "api_v2", Globs: []string{"api/**"}, Description: "d"}}}},
		{name: "bad identity", cfg: Config{Identity: "pane", Groups: []Group{good}},
			want: []want{{"invalid_identity", "identity", "identity"}}},
		{name: "missing name", cfg: Config{Groups: []Group{{Name: "  ", Globs: good.Globs, Description: "d"}}},
			want: []want{{"missing_name", "groups[0].name", "name"}}},
		{name: "name not a slug", cfg: Config{Groups: []Group{{Name: "Drizzle Schema", Globs: good.Globs, Description: "d"}}},
			want: []want{{"invalid_name", "groups[0].name", "name"}}},
		{name: "name with trailing separator", cfg: Config{Groups: []Group{{Name: "schema-", Globs: good.Globs, Description: "d"}}},
			want: []want{{"invalid_name", "groups[0].name", "name"}}},
		{name: "duplicate name", cfg: Config{Groups: []Group{good, good}},
			want: []want{{"duplicate_name", "groups[1].name", "name"}}},
		{name: "missing description", cfg: Config{Groups: []Group{{Name: "a", Globs: good.Globs, Description: " "}}},
			want: []want{{"missing_description", "groups[0].description", "description"}}},
		{name: "no globs", cfg: Config{Groups: []Group{{Name: "a", Description: "d"}}},
			want: []want{{"missing_globs", "groups[0].globs", "globs"}}},
		{name: "empty glob entry", cfg: Config{Groups: []Group{{Name: "a", Globs: []string{"db/**", " "}, Description: "d"}}},
			want: []want{{"empty_glob", "groups[0].globs[1]", "globs"}}},
		{name: "invalid glob pattern", cfg: Config{Groups: []Group{{Name: "a", Globs: []string{"db/[**"}, Description: "d"}}},
			want: []want{{"invalid_glob", "groups[0].globs[0]", "globs"}}},
		{name: "everything wrong at once", cfg: Config{Identity: "x", Groups: []Group{{}, {Name: "a", Globs: []string{""}, Description: "d"}}},
			want: []want{
				{"invalid_identity", "identity", "identity"},
				{"missing_name", "groups[0].name", "name"},
				{"missing_description", "groups[0].description", "description"},
				{"missing_globs", "groups[0].globs", "globs"},
				{"empty_glob", "groups[1].globs[0]", "globs"},
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.cfg.Validate()
			got := make([]want, 0, len(errs))
			for _, e := range errs {
				got = append(got, want{e.Code, e.Path, e.Field})
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("Validate() = %v\nwant %v\n(full: %+v)", got, tc.want, errs)
			}
		})
	}
}

// TestGroupNameRules: the slug rule and the normalization take/clear apply to
// their argument agree with Validate, so a CLI argument is checked against
// the same schema as the stored config (CLAUDE.md).
func TestGroupNameRules(t *testing.T) {
	for _, ok := range []string{"a", "drizzle-schema", "api_v2", "a1-b2_c3"} {
		if !ValidGroupName(ok) {
			t.Errorf("ValidGroupName(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "Drizzle", "a--b", "-a", "a-", "a b", "a/b", "a.b"} {
		if ValidGroupName(bad) {
			t.Errorf("ValidGroupName(%q) = true, want false", bad)
		}
	}
	if got := NormalizeGroupName("  Drizzle-Schema \n"); got != "drizzle-schema" {
		t.Errorf("NormalizeGroupName = %q, want drizzle-schema", got)
	}
}

// TestLoadConfigSurfacesAllErrors (AC-12): LoadConfig returns every field
// error together, not just the first, and keeps the file path.
func TestLoadConfigSurfacesAllErrors(t *testing.T) {
	repo := t.TempDir()
	writeConfig(t, repo, `{"identity":"pane","groups":[{"name":"Bad Name","globs":["db/**"],"description":"d"},{"name":"ok","globs":[],"description":""}]}`)

	cfg, err := LoadConfig(repo)
	var verr *sharedconfig.ValidationErrorsError
	if !errors.As(err, &verr) {
		t.Fatalf("LoadConfig error = %v, want *ValidationErrorsError", err)
	}
	if cfg != nil {
		t.Errorf("LoadConfig returned a config alongside validation errors: %+v", cfg)
	}
	if verr.Path != ConfigPath(repo) {
		t.Errorf("Path = %q, want %q", verr.Path, ConfigPath(repo))
	}
	var codes []string
	for _, e := range verr.Errors {
		codes = append(codes, e.Code)
	}
	want := "[invalid_identity invalid_name missing_description missing_globs]"
	if fmt.Sprint(codes) != want {
		t.Errorf("codes = %v, want %s", codes, want)
	}
}

// TestEvaluateInvalidConfigFailsOpen (AC-12 / D-10): a config that fails
// validation — here on a group name — is Allow from the guard, even for a
// path its glob would otherwise cover.
func TestEvaluateInvalidConfigFailsOpen(t *testing.T) {
	isolateHome(t)
	t.Setenv(WorkerEnv, "worker-a")
	repo := initRepo(t, "main")
	writeConfig(t, repo, `{"groups":[{"name":"Drizzle Schema","globs":["db/schema/**"],"description":"d"}]}`)

	if d := Evaluate(repo, editPayload(repo, "db/schema/users.ts")); d.Deny {
		t.Errorf("invalid config should fail open: %s", d.Reason)
	}
}

// pinTmuxPanes makes every Store built during the test (including the ones
// Evaluate opens for itself) see exactly these panes as live, so the outcome
// never depends on whether the test process runs under a real tmux server.
func pinTmuxPanes(t *testing.T, panes ...string) {
	t.Helper()
	prev := DefaultTmuxPanes
	DefaultTmuxPanes = func() ([]string, error) { return panes, nil }
	t.Cleanup(func() { DefaultTmuxPanes = prev })
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"db/schema/**", "db/schema/users.ts", true},
		{"db/schema/**", "db/schema/nested/deep/users.ts", true},
		{"db/schema/**", "db/schema", true},
		{"db/schema/**", "db/schemas/users.ts", false},
		{"db/schema/**", "src/db/schema/users.ts", false},
		{"**/migrations/*.sql", "db/migrations/001.sql", true},
		{"**/migrations/*.sql", "migrations/001.sql", true},
		{"**/migrations/*.sql", "db/migrations/nested/001.sql", false},
		{"db/*.ts", "db/schema.ts", true},
		{"db/*.ts", "db/schema/users.ts", false},
		{"db/schema/*.ts", "db/schema/users.ts", true},
		{"db/[a-s]*/**", "db/schema/users.ts", true},
		{"db/[a-s]*/**", "db/tables/users.ts", false},
		{"**", "anything/at/all", true},
		{"db/**/*.ts", "db/users.ts", true},
		{"db/**/*.ts", "db/a/b/users.ts", true},
		{"db/**/*.ts", "db/a/b/users.sql", false},
	}
	for _, tc := range cases {
		got, err := matchGlob(tc.pattern, tc.name)
		if err != nil {
			t.Errorf("matchGlob(%q, %q) error: %v", tc.pattern, tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
	if validGlob("[") {
		t.Error("validGlob(\"[\") = true, want false")
	}
	if !validGlob("db/schema/**") {
		t.Error("validGlob(\"db/schema/**\") = false, want true")
	}
}
