package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-shared/lock"
)

// stubLockGH scripts the PR lookup `auto lock clear` verifies with.
type stubLockGH struct {
	number, state string
	err           error
	branches      []string
}

func (g *stubLockGH) PRState(branch string) (string, string, error) {
	g.branches = append(g.branches, branch)
	return g.number, g.state, g.err
}

// stubLockChecker swaps the gh-backed checker for gh for this test.
func stubLockChecker(t *testing.T, gh *stubLockGH) {
	t.Helper()
	prev := newGHChecker
	newGHChecker = func(string) lock.GHChecker { return gh }
	t.Cleanup(func() { newGHChecker = prev })
}

// runLockErr executes `auto lock <args...>` from dir and returns stdout,
// stderr and the error.
func runLockErr(t *testing.T, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Chdir(dir)
	cmd := newLockCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

// seedWorktreeHolder records a lock on drizzle-schema held by branch
// feat/orders in a worktree that exists (so liveness never reclaims it), and
// returns the project id the store keys on.
func seedWorktreeHolder(t *testing.T, home, repo string) string {
	t.Helper()
	statusOut, err := runLock(t, repo, "status")
	if err != nil {
		t.Fatal(err)
	}
	var st lockStatus
	if err := json.Unmarshal([]byte(statusOut), &st); err != nil {
		t.Fatal(err)
	}
	writeLockStore(t, home, lock.Lock{
		Project: st.Project,
		Group:   "drizzle-schema",
		Holder:  lock.Holder{Kind: lock.KindWorktree, Host: st.Worker.Host, Branch: "feat/orders", WorktreePath: t.TempDir()},
		Reason:  "adding orders table",
		TakenAt: "2026-08-31T14:02:11Z",
	})
	return st.Project
}

func readLockStoreFile(t *testing.T, home string) (locks []lock.Lock, audit []lock.AuditEntry) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".auto", "lock", "locks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sf struct {
		Locks []lock.Lock       `json:"locks"`
		Audit []lock.AuditEntry `json:"audit"`
	}
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("locks.json: %v\n%s", err, data)
	}
	return sf.Locks, sf.Audit
}

// TestLockClearRefusesOpenPR (AC-9): an open PR is a non-zero exit whose
// error names the PR state and --force, with nothing on stdout and the store
// untouched.
func TestLockClearRefusesOpenPR(t *testing.T) {
	home, repo := setupLockRepo(t, true)
	seedWorktreeHolder(t, home, repo)
	gh := &stubLockGH{number: "42", state: "OPEN"}
	stubLockChecker(t, gh)

	stdout, _, err := runLockErr(t, repo, "clear", "drizzle-schema")
	if err == nil {
		t.Fatalf("clear with an open PR succeeded:\n%s", stdout)
	}
	for _, want := range []string{"feat/orders", "PR #42 is still OPEN", "not cleared", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal missing %q: %v", want, err)
		}
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("refusal wrote to stdout: %q", stdout)
	}
	if len(gh.branches) != 1 || gh.branches[0] != "feat/orders" {
		t.Errorf("gh asked for %v, want [feat/orders]", gh.branches)
	}
	locks, audit := readLockStoreFile(t, home)
	if len(locks) != 1 || len(audit) != 0 {
		t.Errorf("refusal changed the store: locks %+v audit %+v", locks, audit)
	}
	// take by this Worker is still blocked.
	if _, err := runLock(t, repo, "take", "drizzle-schema"); err == nil || !strings.Contains(err.Error(), "branch feat/orders") {
		t.Errorf("take after refused clear = %v, want held by feat/orders", err)
	}
}

// TestLockClearMerged (AC-9): a merged PR releases the lock — exit 0, JSON on
// stdout, a cleared audit entry naming the clearing Worker — and the group is
// takeable again.
func TestLockClearMerged(t *testing.T) {
	home, repo := setupLockRepo(t, true)
	project := seedWorktreeHolder(t, home, repo)
	stubLockChecker(t, &stubLockGH{number: "42", state: lock.PRStateMerged})

	stdout, stderr, err := runLockErr(t, repo, "clear", "drizzle-schema")
	if err != nil {
		t.Fatalf("clear: %v\nstderr: %s", err, stderr)
	}
	var res lockClear
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("clear output not JSON: %v\n%s", err, stdout)
	}
	want := lockClear{
		Cleared: true, Project: project, Group: "drizzle-schema",
		Holder: lock.Holder{Kind: lock.KindWorktree, Host: res.Holder.Host, Branch: "feat/orders", WorktreePath: res.Holder.WorktreePath},
		Forced: false, PR: "42", State: lock.PRStateMerged,
	}
	if res != want {
		t.Errorf("clear = %+v, want %+v", res, want)
	}
	locks, audit := readLockStoreFile(t, home)
	if len(locks) != 0 {
		t.Errorf("lock still present: %+v", locks)
	}
	if len(audit) != 1 || audit[0].Action != lock.AuditCleared || audit[0].Forced || audit[0].PR != "42" || audit[0].State != lock.PRStateMerged ||
		audit[0].Holder.Branch != "feat/orders" || audit[0].By.Kind != lock.KindOverride || audit[0].By.WorkerID != "worker-a" {
		t.Errorf("audit = %+v, want one cleared entry by worker-a for feat/orders PR 42 MERGED", audit)
	}
	if _, err := runLock(t, repo, "take", "drizzle-schema"); err != nil {
		t.Errorf("take after clear: %v", err)
	}
}

// TestLockClearForce (AC-9): --force releases despite an open PR; the JSON and
// the audit both say forced, and the PR state gh reported is still recorded.
func TestLockClearForce(t *testing.T) {
	home, repo := setupLockRepo(t, true)
	seedWorktreeHolder(t, home, repo)
	stubLockChecker(t, &stubLockGH{number: "42", state: "OPEN"})

	stdout, _, err := runLockErr(t, repo, "clear", "drizzle-schema", "--force")
	if err != nil {
		t.Fatalf("clear --force: %v", err)
	}
	var res lockClear
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("clear output not JSON: %v\n%s", err, stdout)
	}
	if !res.Cleared || !res.Forced || res.PR != "42" || res.State != "OPEN" || res.Holder.Branch != "feat/orders" {
		t.Errorf("clear --force = %+v, want cleared+forced with PR 42 OPEN", res)
	}
	locks, audit := readLockStoreFile(t, home)
	if len(locks) != 0 || len(audit) != 1 || !audit[0].Forced || audit[0].State != "OPEN" {
		t.Errorf("after --force: locks %+v audit %+v; want empty locks and one forced audit entry", locks, audit)
	}
}

// TestLockClearErrors: an unheld group, an unknown group, and a holder without
// a PR to verify are all non-zero exits with a reason; the gh stub is never
// consulted for those.
func TestLockClearErrors(t *testing.T) {
	home, repo := setupLockRepo(t, true)
	gh := &stubLockGH{err: errors.New("gh must not be called")}
	stubLockChecker(t, gh)

	if _, _, err := runLockErr(t, repo, "clear", "drizzle-schema"); err == nil || !strings.Contains(err.Error(), "not held") {
		t.Errorf("clear of an unheld group = %v, want not-held error", err)
	}
	if _, _, err := runLockErr(t, repo, "clear", "nope"); err == nil || !strings.Contains(err.Error(), `unknown lock group "nope"`) {
		t.Errorf("clear of an unknown group = %v, want unknown-group error", err)
	}

	// An override holder (worker-b) has no branch → --force only.
	if _, err := runLock(t, repo, "take", "drizzle-schema", "--as", "worker-b"); err != nil {
		t.Fatal(err)
	}
	_, _, err := runLockErr(t, repo, "clear", "drizzle-schema")
	if err == nil || !strings.Contains(err.Error(), "no PR to verify") || !strings.Contains(err.Error(), "--force") {
		t.Errorf("clear of an override holder = %v, want no-PR-to-verify refusal", err)
	}
	if len(gh.branches) != 0 {
		t.Errorf("gh consulted: %v", gh.branches)
	}
	stdout, _, err := runLockErr(t, repo, "clear", "drizzle-schema", "--force")
	if err != nil {
		t.Fatalf("clear --force of an override holder: %v", err)
	}
	var res lockClear
	if err := json.Unmarshal([]byte(stdout), &res); err != nil || !res.Forced || res.Holder.WorkerID != "worker-b" || res.PR != "" {
		t.Errorf("clear --force = %+v, %v; want forced clear of worker-b with no PR", res, err)
	}
	if locks, _ := readLockStoreFile(t, home); len(locks) != 0 {
		t.Errorf("lock still present: %+v", locks)
	}
}
