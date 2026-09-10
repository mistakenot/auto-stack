package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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

// doctorChecks executes `auto lock doctor` from dir and returns the checks
// keyed by name plus the command error (non-nil ⇔ a check failed).
func doctorChecks(t *testing.T, dir string) (map[string]lockDoctorCheck, error) {
	t.Helper()
	stdout, stderr, err := runLockErr(t, dir, "doctor")
	var checks []lockDoctorCheck
	if jerr := json.Unmarshal([]byte(stdout), &checks); jerr != nil {
		t.Fatalf("doctor output not JSON: %v\nstdout: %s\nstderr: %s", jerr, stdout, stderr)
	}
	byName := map[string]lockDoctorCheck{}
	for _, c := range checks {
		if c.Status == "fail" && c.Hint == "" {
			t.Errorf("failing check %q carries no hint: %+v", c.Check, c)
		}
		byName[c.Check] = c
	}
	return byName, err
}

// restrictPath points PATH at a directory holding only the named tools (git
// is always needed), so a test can decide whether gh is discoverable.
func restrictPath(t *testing.T, tools ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range tools {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

func installClaudeLockHook(t *testing.T, repo string) {
	t.Helper()
	if _, _, _, err := installAgentHooks(claudeSettingsPath(repo), claudeFireCommand, []string{"PreToolUse"}); err != nil {
		t.Fatal(err)
	}
}

// TestLockDoctorEnforceability (AC-11, D-13): without the Claude PreToolUse
// hook doctor fails and exits non-zero with the install remediation; with it
// installed the check passes and doctor exits 0. The Codex hook is a warning
// whether or not it is present — never a pass.
func TestLockDoctorEnforceability(t *testing.T) {
	_, repo := setupLockRepo(t, true)
	restrictPath(t, "git")

	checks, err := doctorChecks(t, repo)
	if err == nil {
		t.Fatal("doctor without the Claude hook exited 0")
	}
	if c := checks["claude-hook"]; c.Status != "fail" || c.Hint != "run auto hooks install" {
		t.Errorf("claude-hook = %+v, want fail with the install hint", c)
	}
	if c := checks["codex-hook"]; c.Status != "warn" {
		t.Errorf("codex-hook (absent) = %+v, want warn", c)
	}
	if c := checks["config"]; c.Status != "pass" || !strings.Contains(c.Message, "drizzle-schema") {
		t.Errorf("config = %+v, want pass naming drizzle-schema", c)
	}
	if c := checks["identity"]; c.Status != "pass" || !strings.Contains(c.Message, "worker-a") {
		t.Errorf("identity = %+v, want pass naming worker-a", c)
	}
	if c := checks["store"]; c.Status != "pass" || !strings.Contains(c.Message, "created") {
		t.Errorf("store (absent) = %+v, want pass saying it will be created", c)
	}
	if c := checks["gh"]; c.Status != "warn" || !strings.Contains(c.Message, "--force") {
		t.Errorf("gh (absent) = %+v, want warn mentioning --force", c)
	}

	installClaudeLockHook(t, repo)
	if _, _, _, err := installAgentHooks(codexHooksPath(repo), codexFireCommand, []string{"PreToolUse"}); err != nil {
		t.Fatal(err)
	}
	checks, err = doctorChecks(t, repo)
	if err != nil {
		t.Fatalf("doctor with the Claude hook installed failed: %v\n%+v", err, checks)
	}
	if c := checks["claude-hook"]; c.Status != "pass" {
		t.Errorf("claude-hook = %+v, want pass", c)
	}
	if c := checks["codex-hook"]; c.Status != "warn" || !strings.Contains(c.Message, "not verifiable") {
		t.Errorf("codex-hook (present) = %+v, want warn / not verifiable", c)
	}
}

// TestLockDoctorConfigStoreGhIdentity covers the remaining doctor rows:
// config missing / invalid, a corrupt store, gh present, and a bare Worker.
func TestLockDoctorConfigStoreGhIdentity(t *testing.T) {
	t.Run("config missing", func(t *testing.T) {
		_, repo := setupLockRepo(t, false)
		checks, err := doctorChecks(t, repo)
		if err == nil {
			t.Error("doctor exited 0 without a config")
		}
		if c := checks["config"]; c.Status != "fail" || c.Hint != "run auto lock init --project" {
			t.Errorf("config = %+v, want fail with the init hint", c)
		}
		if c := checks["identity"]; c.Status != "pass" {
			t.Errorf("identity without a config = %+v, want pass (mode auto)", c)
		}
	})
	t.Run("config invalid", func(t *testing.T) {
		_, repo := setupLockRepo(t, true)
		writeLockSettings(t, repo, `{"groups":[{"name":"Bad Name","globs":[],"description":"d"}]}`)
		checks, err := doctorChecks(t, repo)
		if err == nil {
			t.Error("doctor exited 0 with an invalid config")
		}
		c := checks["config"]
		if c.Status != "fail" || !strings.Contains(c.Message, "groups[0].name") || !strings.Contains(c.Message, "groups[0].globs") {
			t.Errorf("config = %+v, want fail listing groups[0].name and groups[0].globs", c)
		}
	})
	t.Run("store corrupt", func(t *testing.T) {
		home, repo := setupLockRepo(t, true)
		installClaudeLockHook(t, repo)
		dir := filepath.Join(home, ".auto", "lock")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "locks.json"), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		checks, err := doctorChecks(t, repo)
		if err == nil {
			t.Error("doctor exited 0 with a corrupt store")
		}
		if c := checks["store"]; c.Status != "fail" || !strings.Contains(c.Hint, "locks.json") {
			t.Errorf("store = %+v, want fail with a hint naming locks.json", c)
		}
	})
	t.Run("store present and gh present", func(t *testing.T) {
		home, repo := setupLockRepo(t, true)
		installClaudeLockHook(t, repo)
		writeLockStore(t, home)
		bin := restrictPath(t, "git")
		if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		checks, err := doctorChecks(t, repo)
		if err != nil {
			t.Fatalf("doctor: %v\n%+v", err, checks)
		}
		if c := checks["store"]; c.Status != "pass" || !strings.Contains(c.Message, "0 locks") {
			t.Errorf("store = %+v, want pass with 0 locks", c)
		}
		if c := checks["gh"]; c.Status != "pass" {
			t.Errorf("gh = %+v, want pass", c)
		}
	})
	t.Run("bare identity", func(t *testing.T) {
		_, repo := setupBareLockRepo(t, true)
		installClaudeLockHook(t, repo)
		checks, err := doctorChecks(t, repo)
		if err != nil {
			t.Errorf("bare identity is a warning, but doctor failed: %v", err)
		}
		if c := checks["identity"]; c.Status != "warn" || !strings.Contains(c.Hint, lock.WorkerEnv) {
			t.Errorf("identity = %+v, want warn with the %s remediation", c, lock.WorkerEnv)
		}
	})
	t.Run("outside a repo", func(t *testing.T) {
		setupLockRepo(t, true)
		checks, err := doctorChecks(t, t.TempDir())
		if err == nil {
			t.Error("doctor exited 0 outside a git repository")
		}
		if c := checks["repo"]; c.Status != "fail" {
			t.Errorf("repo = %+v, want fail", c)
		}
		if _, ok := checks["store"]; !ok {
			t.Error("store check skipped outside a repo")
		}
	})
}

// TestLockInitProject: init --project scaffolds a valid settings.json with
// the example group, reports the hook state, never overwrites on a second
// run, and refuses to run without --project.
func TestLockInitProject(t *testing.T) {
	_, repo := setupLockRepo(t, false)

	stdout, stderr, err := runLockErr(t, repo, "init", "--project")
	if err != nil {
		t.Fatalf("init --project: %v\n%s", err, stderr)
	}
	var res lockInit
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("init output not JSON: %v\n%s", err, stdout)
	}
	if !res.Created || res.Path != lock.ConfigPath(repo) || res.Identity != lock.IdentityAuto || len(res.Groups) != 1 || res.Groups[0] != "example-group" {
		t.Errorf("init = %+v, want created example-group with identity auto at %s", res, lock.ConfigPath(repo))
	}
	if res.ClaudeHookInstalled || !strings.Contains(res.Hint, "auto hooks install") || !strings.Contains(stderr, "auto hooks install") {
		t.Errorf("init should report the missing Claude hook on stdout and stderr: %+v\nstderr: %s", res, stderr)
	}
	cfg, err := lock.LoadConfig(repo)
	if err != nil || cfg == nil || cfg.Group("example-group") == nil {
		t.Fatalf("scaffold does not load as a valid config: %v %+v", err, cfg)
	}
	// The scaffold is usable straight away: status lists the example group.
	statusOut, err := runLock(t, repo, "status")
	if err != nil {
		t.Fatal(err)
	}
	var st lockStatus
	if err := json.Unmarshal([]byte(statusOut), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Groups) != 1 || st.Groups[0].Name != "example-group" || st.Groups[0].Held {
		t.Errorf("status after init = %+v, want the unheld example group", st.Groups)
	}

	// Second run: untouched.
	custom := strings.ReplaceAll(lockSettingsOneGroup, "drizzle-schema", "my-group")
	writeLockSettings(t, repo, custom)
	installClaudeLockHook(t, repo)
	stdout, stderr, err = runLockErr(t, repo, "init", "--project")
	if err != nil {
		t.Fatalf("second init: %v", err)
	}
	var again lockInit
	if err := json.Unmarshal([]byte(stdout), &again); err != nil {
		t.Fatal(err)
	}
	if again.Created || !again.ClaudeHookInstalled || again.Hint != "" || len(again.Groups) != 1 || again.Groups[0] != "my-group" {
		t.Errorf("second init = %+v, want created=false, hook installed, existing my-group", again)
	}
	if !strings.Contains(stderr, "left untouched") {
		t.Errorf("second init stderr = %q, want left-untouched notice", stderr)
	}
	if data, _ := os.ReadFile(lock.ConfigPath(repo)); string(data) != custom {
		t.Errorf("second init rewrote the config:\n%s", data)
	}

	// --project is required.
	if _, _, err := runLockErr(t, repo, "init"); err == nil || !strings.Contains(err.Error(), "project") {
		t.Errorf("init without --project = %v, want a required-flag error", err)
	}
}

// TestLockStatusListsUnheldGroups (AC-12): every configured group is listed,
// held or not, with the holder only on the held one.
func TestLockStatusListsUnheldGroups(t *testing.T) {
	_, repo := setupLockRepo(t, true)
	writeLockSettings(t, repo, `{"groups":[
	  {"name":"drizzle-schema","globs":["db/schema/**"],"description":"schema"},
	  {"name":"api-routes","globs":["api/**"],"description":"routes"}]}`)
	if _, err := runLock(t, repo, "take", "drizzle-schema", "--reason", "add orders"); err != nil {
		t.Fatal(err)
	}
	stdout, err := runLock(t, repo, "status")
	if err != nil {
		t.Fatal(err)
	}
	var st lockStatus
	if err := json.Unmarshal([]byte(stdout), &st); err != nil {
		t.Fatalf("status not JSON: %v\n%s", err, stdout)
	}
	if len(st.Groups) != 2 || len(st.Locks) != 1 {
		t.Fatalf("status = %+v, want 2 groups and 1 lock", st)
	}
	held, free := st.Groups[0], st.Groups[1]
	if held.Name != "drizzle-schema" || !held.Held || !held.HeldByYou || held.Holder == nil || held.Holder.WorkerID != "worker-a" || held.Reason != "add orders" || held.TakenAt == "" {
		t.Errorf("held group = %+v, want held by you (worker-a) with reason and taken_at", held)
	}
	if free.Name != "api-routes" || free.Held || free.HeldByYou || free.Holder != nil || len(free.Globs) != 1 {
		t.Errorf("free group = %+v, want unheld with its globs", free)
	}
}

// TestLockGroupArgValidation: a group argument is normalized and checked
// against the config's slug rule before lookup.
func TestLockGroupArgValidation(t *testing.T) {
	_, repo := setupLockRepo(t, true)
	if _, _, err := runLockErr(t, repo, "take", "Drizzle Schema"); err == nil || !strings.Contains(err.Error(), "invalid lock group name") {
		t.Errorf("take with a malformed name = %v, want invalid-name error", err)
	}
	stdout, err := runLock(t, repo, "take", " DRIZZLE-SCHEMA ")
	if err != nil {
		t.Fatalf("take with an unnormalized name: %v", err)
	}
	var l lock.Lock
	if err := json.Unmarshal([]byte(stdout), &l); err != nil || l.Group != "drizzle-schema" {
		t.Errorf("take = %+v (%v), want drizzle-schema", l, err)
	}
	if _, _, err := runLockErr(t, repo, "release", "nope"); err == nil || !strings.Contains(err.Error(), `unknown lock group "nope"`) {
		t.Errorf("release of an unknown group = %v, want unknown-group error", err)
	}
}

// TestLockInvalidConfigIsAnError: a config that fails validation makes every
// command exit non-zero with every offending field named (nothing on stdout).
func TestLockInvalidConfigIsAnError(t *testing.T) {
	_, repo := setupLockRepo(t, true)
	writeLockSettings(t, repo, `{"identity":"pane","groups":[{"name":"a","globs":["db/**"],"description":""}]}`)
	stdout, _, err := runLockErr(t, repo, "status")
	if err == nil {
		t.Fatalf("status with an invalid config succeeded:\n%s", stdout)
	}
	for _, want := range []string{"identity:", "groups[0].description:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on error, got %q", stdout)
	}
}

// TestLockTextOutput smokes --text on status, doctor, take and release: a
// human rendering on stdout, no JSON.
func TestLockTextOutput(t *testing.T) {
	_, repo := setupLockRepo(t, true)
	installClaudeLockHook(t, repo)

	stdout, _, err := runLockErr(t, repo, "take", "drizzle-schema", "--text")
	if err != nil || stdout != "✓ took lock \"drizzle-schema\" as worker-a (override)\n" {
		t.Errorf("take --text = %q, %v", stdout, err)
	}
	stdout, _, err = runLockErr(t, repo, "status", "--text")
	if err != nil || !strings.HasPrefix(stdout, "drizzle-schema  HELD by worker-a (you)  since ") {
		t.Errorf("status --text = %q, %v", stdout, err)
	}
	stdout, _, err = runLockErr(t, repo, "doctor", "--text")
	if err != nil {
		t.Errorf("doctor --text: %v\n%s", err, stdout)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if !strings.HasPrefix(lines[0], "✓ ") || !strings.Contains(stdout, "! codex-hook") || strings.Contains(stdout, "{") {
		t.Errorf("doctor --text should list ✓ lines first then ! warnings, no JSON:\n%s", stdout)
	}
	if idx := strings.Index(stdout, "!"); idx >= 0 && strings.Contains(stdout[idx:], "\n✓") {
		t.Errorf("doctor --text prints a pass after a warning:\n%s", stdout)
	}
	stdout, _, err = runLockErr(t, repo, "release", "--text")
	if err != nil || stdout != "✓ released 1 lock\n" {
		t.Errorf("release --text = %q, %v", stdout, err)
	}
	stdout, _, err = runLockErr(t, repo, "status", "--text")
	if err != nil || stdout != "drizzle-schema  free\n" {
		t.Errorf("status --text after release = %q, %v", stdout, err)
	}
}
