package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-shared/lock"
)

// lockSettingsOneGroup is the .auto/lock/settings.json fixture: one group,
// drizzle-schema, covering db/schema/**.
const lockSettingsOneGroup = `{
  "groups": [
    {
      "name": "drizzle-schema",
      "globs": ["db/schema/**"],
      "description": "Ordered migrations clash if two branches change the schema in parallel."
    }
  ]
}
`

// setupLockRepo isolates HOME, clears every identity variable (the test
// process may itself run under tmux/ntm) and pins the Worker identity via
// AUTO_LOCK_WORKER, then creates a real git repo and (optionally) writes the
// lock config.
func setupLockRepo(t *testing.T, withConfig bool) (home, repo string) {
	t.Helper()
	home, repo = setupBareLockRepo(t, withConfig)
	t.Setenv(lock.WorkerEnv, "worker-a")
	return home, repo
}

// setupBareLockRepo is setupLockRepo without the override: a main checkout
// with no pane and no AUTO_LOCK_WORKER, i.e. the D-6 bare case.
func setupBareLockRepo(t *testing.T, withConfig bool) (home, repo string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:0") // no daemon; post drops silently
	for _, key := range lock.IdentityEnv {
		t.Setenv(key, "")
	}
	repo = initGitRepo(t, "feat/orders")
	if withConfig {
		writeLockSettings(t, repo, lockSettingsOneGroup)
	}
	return home, repo
}

// writeLockStore replaces the isolated store with exactly the given locks.
func writeLockStore(t *testing.T, home string, locks ...lock.Lock) {
	t.Helper()
	dir := filepath.Join(home, ".auto", "lock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"version": 1, "locks": locks, "audit": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "locks.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLockSettings(t *testing.T, repo, body string) {
	t.Helper()
	path := lock.ConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// preToolUseEditPayload is a Claude PreToolUse Edit payload on file (relative to
// repo) with cwd set to repo so provenance resolves.
func preToolUseEditPayload(repo, file string) string {
	return toJSON(map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "sess-abc123",
		"cwd":             repo,
		"tool_name":       "Edit",
		"tool_input": map[string]any{
			"file_path":  filepath.Join(repo, file),
			"old_string": "a",
			"new_string": "b",
		},
	})
}

// decodeDeny parses stdout as a preToolUseDecision, failing if empty/invalid.
func decodeDeny(t *testing.T, out string) preToolUseDecision {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected deny JSON on stdout, got empty")
	}
	var d preToolUseDecision
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("unmarshal deny %q: %v", out, err)
	}
	return d
}

// runLock executes `auto lock <args...>` from inside dir and returns stdout.
func runLock(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	cmd := newLockCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	return out.String(), err
}

// TestFireLockWalkingSkeleton (AC-1, AC-2) is the Phase 1 end-to-end path:
// config + no lock → PreToolUse Edit under the glob is denied with the group's
// description and the take command; after `auto lock take` the same edit is
// allowed (empty stdout).
func TestFireLockWalkingSkeleton(t *testing.T) {
	_, repo := setupLockRepo(t, true)

	// 1. Unheld → deny.
	out := runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts"))
	d := decodeDeny(t, out)
	if d.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", d.HookSpecificOutput.HookEventName)
	}
	if d.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("permissionDecision = %q, want deny", d.HookSpecificOutput.PermissionDecision)
	}
	reason := d.HookSpecificOutput.PermissionDecisionReason
	for _, want := range []string{
		`db/schema/users.ts is under a serial-update lock group "drizzle-schema"`,
		"Ordered migrations clash if two branches change the schema in parallel.",
		"auto lock take drizzle-schema",
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("deny reason missing %q:\n%s", want, reason)
		}
	}
	// Exactly one JSON object on stdout.
	if n := strings.Count(strings.TrimSpace(out), "\n"); n != 0 {
		t.Errorf("expected a single JSON line on stdout, got:\n%s", out)
	}

	// 2. Take the lock via the CLI, as the same Worker.
	takeOut, err := runLock(t, repo, "take", "drizzle-schema", "--reason", "add orders table")
	if err != nil {
		t.Fatalf("auto lock take: %v", err)
	}
	var taken lock.Lock
	if err := json.Unmarshal([]byte(takeOut), &taken); err != nil {
		t.Fatalf("take output not JSON: %v\n%s", err, takeOut)
	}
	if taken.Group != "drizzle-schema" || taken.Holder.Kind != lock.KindOverride || taken.Holder.WorkerID != "worker-a" || taken.Reason != "add orders table" {
		t.Errorf("take returned %+v", taken)
	}

	// 3. Held by self → allow (empty stdout).
	if out := runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")); strings.TrimSpace(out) != "" {
		t.Errorf("expected empty stdout (allow) when self-held, got: %q", out)
	}

	// 4. status reports the lock as held by you.
	statusOut, err := runLock(t, repo, "status")
	if err != nil {
		t.Fatalf("auto lock status: %v", err)
	}
	var st lockStatus
	if err := json.Unmarshal([]byte(statusOut), &st); err != nil {
		t.Fatalf("status output not JSON: %v\n%s", err, statusOut)
	}
	if len(st.Groups) != 1 || len(st.Locks) != 1 || !st.Locks[0].HeldByYou {
		t.Errorf("status = %+v, want one group and one lock held by you", st)
	}

	// 5. A different Worker is denied, and take fails naming the holder.
	t.Setenv(lock.WorkerEnv, "worker-b")
	d = decodeDeny(t, runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")))
	if r := d.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(r, "worker-a") || !strings.Contains(r, "auto lock clear drizzle-schema") {
		t.Errorf("held-by-other reason should name worker-a and the clear path:\n%s", r)
	}
	if _, err := runLock(t, repo, "take", "drizzle-schema"); err == nil || !strings.Contains(err.Error(), "worker-a") {
		t.Errorf("take by worker-b = %v, want held error naming worker-a", err)
	}
}

// TestFireLockAllowsNonMatchingAndNonEdit (AC-4) verifies paths outside every
// glob, non-edit tools and non-PreToolUse events stay silent.
func TestFireLockAllowsNonMatchingAndNonEdit(t *testing.T) {
	_, repo := setupLockRepo(t, true)

	if out := runFire(t, "claude", preToolUseEditPayload(repo, "src/app.ts")); strings.TrimSpace(out) != "" {
		t.Errorf("non-matching path should allow, got: %q", out)
	}
	bash := toJSON(map[string]any{
		"hook_event_name": "PreToolUse",
		"cwd":             repo,
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "sed -i s/a/b/ db/schema/users.ts"},
	})
	if out := runFire(t, "claude", bash); strings.TrimSpace(out) != "" {
		t.Errorf("Bash tool should allow (documented gap), got: %q", out)
	}
	// PostToolUse on a locked path is not the guard's event: no deny, and with
	// no hints config nothing else either.
	post := toJSON(map[string]any{
		"hook_event_name": "PostToolUse",
		"cwd":             repo,
		"tool_name":       "Edit",
		"tool_input":      map[string]any{"file_path": filepath.Join(repo, "db/schema/users.ts")},
	})
	if out := runFire(t, "claude", post); strings.TrimSpace(out) != "" {
		t.Errorf("PostToolUse should not deny, got: %q", out)
	}
}

// TestFireLockAllowsMissingConfig (AC-4) verifies a project with no
// .auto/lock/settings.json is never blocked.
func TestFireLockAllowsMissingConfig(t *testing.T) {
	_, repo := setupLockRepo(t, false)
	if out := runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")); strings.TrimSpace(out) != "" {
		t.Errorf("missing config should allow, got: %q", out)
	}
	if _, err := runLock(t, repo, "status"); err == nil {
		t.Error("auto lock status without config should error")
	}
}

// TestFireLockFailsOpenOnCorruptStore (AC-4) verifies a corrupt
// ~/.auto/lock/locks.json degrades to allow, and fire still returns nil.
func TestFireLockFailsOpenOnCorruptStore(t *testing.T) {
	home, repo := setupLockRepo(t, true)
	dir := filepath.Join(home, ".auto", "lock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "locks.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")); strings.TrimSpace(out) != "" {
		t.Errorf("corrupt store should fail open, got: %q", out)
	}
	// Malformed config too.
	writeLockSettings(t, repo, "{not json")
	if err := os.Remove(filepath.Join(dir, "locks.json")); err != nil {
		t.Fatal(err)
	}
	if out := runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")); strings.TrimSpace(out) != "" {
		t.Errorf("malformed config should fail open, got: %q", out)
	}
}

// TestLockTakeUnknownGroup verifies take rejects a group that is not configured.
func TestLockTakeUnknownGroup(t *testing.T) {
	_, repo := setupLockRepo(t, true)
	if _, err := runLock(t, repo, "take", "nope"); err == nil || !strings.Contains(err.Error(), "unknown lock group") {
		t.Errorf("take nope = %v, want unknown-group error", err)
	}
}

// TestFireLockHeldByOtherBranch (AC-3) verifies the deny for a group held by a
// worktree Worker on another branch names the branch, PR, reason, taken time,
// host and the merge-verified clear path.
func TestFireLockHeldByOtherBranch(t *testing.T) {
	home, repo := setupLockRepo(t, true)
	// The project id of an unregistered repo is its main worktree path, which
	// is what `auto lock status` reports; seed the holder under it.
	statusOut, err := runLock(t, repo, "status")
	if err != nil {
		t.Fatal(err)
	}
	var st lockStatus
	if err := json.Unmarshal([]byte(statusOut), &st); err != nil {
		t.Fatal(err)
	}
	// Take never records a PR (that is Phase 3's gh lookup), so seed the
	// holder — including its PR — straight into the store.
	writeLockStore(t, home, lock.Lock{
		Project: st.Project,
		Group:   "drizzle-schema",
		Holder:  lock.Holder{Kind: lock.KindWorktree, Host: st.Worker.Host, Branch: "feat/orders", WorktreePath: "/wt/orders"},
		Reason:  "adding orders table",
		PR:      "42",
		TakenAt: "2026-08-31T14:02:11Z",
	})

	d := decodeDeny(t, runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")))
	if d.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("permissionDecision = %q, want deny", d.HookSpecificOutput.PermissionDecision)
	}
	reason := d.HookSpecificOutput.PermissionDecisionReason
	for _, want := range []string{
		`"drizzle-schema" is locked by branch feat/orders (PR #42, "adding orders table")`,
		"taken 2026-08-31 14:02 on host " + st.Worker.Host,
		"Wait for that PR to merge, then:  auto lock clear drizzle-schema",
		"clear refuses unless PR #42 is merged",
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("deny reason missing %q:\n%s", want, reason)
		}
	}
	// take by this Worker names the holder branch.
	if _, err := runLock(t, repo, "take", "drizzle-schema"); err == nil || !strings.Contains(err.Error(), "branch feat/orders") {
		t.Errorf("take while held = %v, want held error naming branch feat/orders", err)
	}
}

// TestFireLockBareMainBlocks (AC-6, D-6) verifies an unidentifiable Worker —
// main checkout, no pane, no AUTO_LOCK_WORKER — is denied on a lockable file
// with the remediation, while unmatched files stay allowed and the CLI reports
// the same remediation.
func TestFireLockBareMainBlocks(t *testing.T) {
	_, repo := setupBareLockRepo(t, true)

	d := decodeDeny(t, runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")))
	if d.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("permissionDecision = %q, want deny", d.HookSpecificOutput.PermissionDecision)
	}
	reason := d.HookSpecificOutput.PermissionDecisionReason
	for _, want := range []string{
		`db/schema/users.ts is under a serial-update lock group "drizzle-schema"`,
		"cannot be identified",
		"linked git worktree",
		lock.WorkerEnv + "=<name>",
		"auto lock take drizzle-schema",
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("bare deny reason missing %q:\n%s", want, reason)
		}
	}
	if out := runFire(t, "claude", preToolUseEditPayload(repo, "src/app.ts")); strings.TrimSpace(out) != "" {
		t.Errorf("bare worker on an unmatched file should allow, got: %q", out)
	}
	if _, err := runLock(t, repo, "take", "drizzle-schema"); err == nil || !strings.Contains(err.Error(), lock.WorkerEnv) {
		t.Errorf("bare take = %v, want the D-6 remediation", err)
	}
	// A pane is enough to identify the Worker again.
	t.Setenv("TMUX_PANE", "%3")
	takeOut, err := runLock(t, repo, "take", "drizzle-schema")
	if err != nil {
		t.Fatalf("take with TMUX_PANE: %v", err)
	}
	var taken lock.Lock
	if err := json.Unmarshal([]byte(takeOut), &taken); err != nil {
		t.Fatal(err)
	}
	if taken.Holder.Kind != lock.KindAgent || taken.Holder.WorkerID != "%3" {
		t.Errorf("take with TMUX_PANE recorded %+v, want agent %%3", taken.Holder)
	}
	if out := runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")); strings.TrimSpace(out) != "" {
		t.Errorf("holder pane should be allowed, got: %q", out)
	}
}

// TestLockReleaseLifecycle (AC-7) verifies take → allow → release → blocked
// again, that release frees every group the Worker holds and nobody else's,
// and that --as stands in for AUTO_LOCK_WORKER on both take and release.
func TestLockReleaseLifecycle(t *testing.T) {
	_, repo := setupLockRepo(t, true)
	writeLockSettings(t, repo, `{"groups":[
	  {"name":"drizzle-schema","globs":["db/schema/**"],"description":"serial"},
	  {"name":"openapi","globs":["api/**"],"description":"serial"}]}`)

	for _, g := range []string{"drizzle-schema", "openapi"} {
		if _, err := runLock(t, repo, "take", g, "--reason", "mine"); err != nil {
			t.Fatalf("take %s: %v", g, err)
		}
	}
	// worker-b holds nothing here but takes elsewhere via --as, untouched by
	// worker-a's release.
	if _, err := runLock(t, repo, "take", "openapi", "--as", "worker-b"); err == nil || !strings.Contains(err.Error(), "worker-a") {
		t.Errorf("take --as worker-b on a held group = %v, want held error naming worker-a", err)
	}
	if out := runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")); strings.TrimSpace(out) != "" {
		t.Errorf("self-held should allow, got: %q", out)
	}

	// Release nothing for a Worker that holds nothing: success, 0.
	out, err := runLock(t, repo, "release", "--as", "worker-b")
	if err != nil {
		t.Fatalf("release --as worker-b: %v", err)
	}
	var rel lockRelease
	if err := json.Unmarshal([]byte(out), &rel); err != nil {
		t.Fatalf("release output not JSON: %v\n%s", err, out)
	}
	if rel.Released != 0 || rel.Worker.WorkerID != "worker-b" {
		t.Errorf("release --as worker-b = %+v, want released 0 as worker-b", rel)
	}
	// Unknown group is a usage error.
	if _, err := runLock(t, repo, "release", "nope"); err == nil || !strings.Contains(err.Error(), "unknown lock group") {
		t.Errorf("release nope = %v, want unknown-group error", err)
	}

	// Release everything worker-a holds.
	out, err = runLock(t, repo, "release")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &rel); err != nil {
		t.Fatal(err)
	}
	if rel.Released != 2 || rel.Group != "" || rel.Worker.WorkerID != "worker-a" {
		t.Errorf("release = %+v, want released 2 as worker-a", rel)
	}
	d := decodeDeny(t, runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")))
	if r := d.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(r, "auto lock take drizzle-schema") {
		t.Errorf("after release the edit should be blocked as unheld:\n%s", r)
	}
	statusOut, err := runLock(t, repo, "status")
	if err != nil {
		t.Fatal(err)
	}
	var st lockStatus
	if err := json.Unmarshal([]byte(statusOut), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Locks) != 0 {
		t.Errorf("status after release = %+v, want no locks", st.Locks)
	}

	// Single-group release leaves the other group held.
	for _, g := range []string{"drizzle-schema", "openapi"} {
		if _, err := runLock(t, repo, "take", g); err != nil {
			t.Fatal(err)
		}
	}
	out, err = runLock(t, repo, "release", "openapi")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &rel); err != nil {
		t.Fatal(err)
	}
	if rel.Released != 1 || rel.Group != "openapi" {
		t.Errorf("release openapi = %+v, want released 1", rel)
	}
	if out := runFire(t, "claude", preToolUseEditPayload(repo, "db/schema/users.ts")); strings.TrimSpace(out) != "" {
		t.Errorf("drizzle-schema should still be self-held, got: %q", out)
	}
}
