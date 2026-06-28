package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-reflect/internal/store"
)

// doctorCheckResp mirrors one element of the `doctor` top-level array.
type doctorCheckResp struct {
	Check   string `json:"check"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

func runDoctor(t *testing.T, repo string) (checks []doctorCheckResp, stdout, stderr string, code int) {
	t.Helper()
	stdout, stderr, code = runCLIAt(t, repo, "doctor")
	if err := json.Unmarshal([]byte(stdout), &checks); err != nil {
		t.Fatalf("decode doctor output: %v\nraw:\n%s", err, stdout)
	}
	return checks, stdout, stderr, code
}

func findCheck(t *testing.T, checks []doctorCheckResp, name string) doctorCheckResp {
	t.Helper()
	for i := range checks {
		if checks[i].Check == name {
			return checks[i]
		}
	}
	t.Fatalf("doctor output missing check %q: %#v", name, checks)
	return doctorCheckResp{}
}

// initReflect runs `auto reflect init --project` to seed a healthy state dir
// (events/ + playbook.json) inside an already-initialized git repo.
func initReflect(t *testing.T, repo string) {
	t.Helper()
	_, stderr, code := runCLIAt(t, repo, "init", "--project")
	if code != 0 {
		t.Fatalf("reflect init failed: code=%d\nstderr:\n%s", code, stderr)
	}
}

// TestDoctorHealthyAllPass asserts a freshly initialized state reports every
// check as pass and exits 0.
func TestDoctorHealthyAllPass(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")
	initReflect(t, repo)

	checks, _, stderr, code := runDoctor(t, repo)
	if code != 0 {
		t.Fatalf("expected exit 0 on healthy state, got %d\nstderr:\n%s", code, stderr)
	}
	for i := range checks {
		if checks[i].Status != "pass" {
			t.Fatalf("expected all checks to pass, but %q is %q: %#v", checks[i].Check, checks[i].Status, checks[i])
		}
	}
	// The four expected checks are all present.
	for _, name := range []string{"state_dir", "events", "playbook_snapshot", "legacy_feedback"} {
		findCheck(t, checks, name)
	}
}

// TestDoctorMissingEventsFails asserts a state dir without events/ yields a
// fail with a remediation hint and a non-zero exit code.
func TestDoctorMissingEventsFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")
	initReflect(t, repo)

	// Remove the events/ directory to simulate partial/stale init state.
	if err := os.RemoveAll(store.EventsDir(repo)); err != nil {
		t.Fatalf("remove events dir: %v", err)
	}

	checks, stdout, _, code := runDoctor(t, repo)
	if code == 0 {
		t.Fatalf("expected non-zero exit when events/ is missing, got 0\nstdout:\n%s", stdout)
	}
	events := findCheck(t, checks, "events")
	if events.Status != "fail" {
		t.Fatalf("expected events check to fail, got %q: %#v", events.Status, events)
	}
	if events.Hint == "" {
		t.Fatalf("expected a remediation hint on the events failure: %#v", events)
	}
}

// TestDoctorStaleSnapshotWarns asserts a playbook.json that lags the folded
// event log (a rule event appended after the snapshot was written) warns and
// points at rebuild, without flipping the overall exit code (warn != fail).
func TestDoctorStaleSnapshotWarns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")
	initReflect(t, repo)

	// Create a rule (writes a rule_created event and a fresh snapshot), then
	// clobber playbook.json with an empty folded_through so the snapshot lags
	// the log's rule high-water mark — exactly the stale condition.
	createTestRuleWith(t, repo,
		"--use-when", "writing go cli flags with cobra",
		"--content", "Use cobra StringSliceVar for repeatable flags",
		"--causal-note", "manual parsing dropped values",
		"--domain", "cli", "--type", "soft")
	stale := `{"schema_version":1,"folded_through":{},"rules":[]}`
	writeFile(t, store.PlaybookPath(repo), stale)

	checks, _, stderr, code := runDoctor(t, repo)
	if code != 0 {
		t.Fatalf("expected exit 0 (warn, not fail) for a stale snapshot, got %d\nstderr:\n%s", code, stderr)
	}
	snap := findCheck(t, checks, "playbook_snapshot")
	if snap.Status != "warn" {
		t.Fatalf("expected playbook_snapshot to warn, got %q: %#v", snap.Status, snap)
	}
	if snap.Hint == "" {
		t.Fatalf("expected a rebuild hint on the stale snapshot warning: %#v", snap)
	}
}

// TestDoctorLegacyFeedbackWarns asserts a leftover 0-byte feedback.jsonl in the
// state dir is flagged with a warn + hint (without failing the overall run).
func TestDoctorLegacyFeedbackWarns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")
	initReflect(t, repo)

	stateDir := filepath.Dir(store.EventsDir(repo))
	writeFile(t, filepath.Join(stateDir, "feedback.jsonl"), "")

	checks, _, stderr, code := runDoctor(t, repo)
	if code != 0 {
		t.Fatalf("expected exit 0 (warn, not fail) for legacy feedback.jsonl, got %d\nstderr:\n%s", code, stderr)
	}
	legacy := findCheck(t, checks, "legacy_feedback")
	if legacy.Status != "warn" {
		t.Fatalf("expected legacy_feedback to warn, got %q: %#v", legacy.Status, legacy)
	}
	if legacy.Hint == "" {
		t.Fatalf("expected a remediation hint on legacy_feedback warning: %#v", legacy)
	}
}
