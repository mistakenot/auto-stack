package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	watchconfig "github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/testutil"
)

// runFixture opens a store under the env's HOME and seeds three runs covering
// the states the run resource must project: a completed bash run, a failed
// (session_lost, exit_code null) claude run with a worktree and a large output
// log, and a still-running run.
type runFixture struct {
	env        *testutil.Env
	completedB int64
	failedC    int64
	running    int64
	failedLog  string
}

func seedRuns(t *testing.T) runFixture {
	t.Helper()
	env := testutil.NewEnv(t)
	if err := watchconfig.EnsureGlobalDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	dbPath, err := watchconfig.DBPath()
	if err != nil {
		t.Fatalf("db path: %v", err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	base := time.Date(2026, 10, 24, 16, 0, 0, 0, time.UTC)

	// 1) Completed bash run (oldest). No worktree (bash → worktree_path null).
	completedB := reserveStart(t, db, &store.ReserveRunInput{
		ProjectID: "demo", ProjectPath: "/repo", TriggerID: "cron10", TriggerType: "cron",
		TaskID: "run-etl", TaskType: "bash", ResourceKey: "rk-done", StartedAt: base,
	}, &store.RunStartUpdate{
		SessionName: "autowatch-run-1--run-etl", RuntimeDir: "/runs/1",
		OutputPath: "/runs/1/output.log", ExitPath: "/runs/1/exit-code",
		Backend: "tmux", BackendTarget: "autowatch-run-1--run-etl",
	})
	zero := 0
	ok, err := db.MarkRunTerminal(ctx, completedB, model.RunCompleted, &zero, base.Add(time.Minute), "")
	if err != nil || !ok {
		t.Fatalf("finish completed run: ok=%v err=%v", ok, err)
	}

	// 2) Failed claude run (session_lost, exit_code null), with a worktree and a
	// large output log so the get projection exercises the breadcrumb.
	failedLog := filepath.Join(t.TempDir(), "output.log")
	writeLargeLog(t, failedLog)
	failedC := reserveStart(t, db, &store.ReserveRunInput{
		ProjectID: "demo", ProjectPath: "/repo", TriggerID: "onpush", TriggerType: "file_created",
		TaskID: "review", TaskType: "claude", ResourceKey: "rk-fail", StartedAt: base.Add(2 * time.Minute),
		Branch: "feature/x",
	}, &store.RunStartUpdate{
		SessionName: "autowatch-run-2--review", RuntimeDir: "/runs/2",
		OutputPath: failedLog, ExitPath: "/runs/2/exit-code",
		WorktreePath: "/repo/.auto/watch/worktrees/2", Branch: "feature/x",
		Backend: "tmux", BackendTarget: "autowatch-run-2--review",
		DeadlineAt: base.Add(2*time.Minute + 2*time.Hour),
	})
	ok, err = db.MarkRunFailedWithReason(ctx, failedC, nil, base.Add(5*time.Minute), "session vanished", "session_lost")
	if err != nil || !ok {
		t.Fatalf("finish failed run: ok=%v err=%v", ok, err)
	}

	// 3) Still-running run (newest): exit_code/completed/failure_reason null.
	running := reserveStart(t, db, &store.ReserveRunInput{
		ProjectID: "demo", ProjectPath: "/repo", TriggerID: "cron10", TriggerType: "cron",
		TaskID: "run-etl", TaskType: "bash", ResourceKey: "rk-run", StartedAt: base.Add(10 * time.Minute),
	}, &store.RunStartUpdate{
		SessionName: "autowatch-run-3--run-etl", RuntimeDir: "/runs/3",
		OutputPath: "/runs/3/output.log", ExitPath: "/runs/3/exit-code",
		Backend: "tmux", BackendTarget: "autowatch-run-3--run-etl",
	})

	return runFixture{env: env, completedB: completedB, failedC: failedC, running: running, failedLog: failedLog}
}

func reserveStart(t *testing.T, db *store.Store, in *store.ReserveRunInput, start *store.RunStartUpdate) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := db.ReserveRun(ctx, in)
	if err != nil {
		t.Fatalf("reserve run: %v", err)
	}
	if err := db.UpdateRunStarted(ctx, id, start); err != nil {
		t.Fatalf("start run: %v", err)
	}
	return id
}

func writeLargeLog(t *testing.T, path string) {
	t.Helper()
	var sb strings.Builder
	for range 20000 {
		sb.WriteString("progress tick\r")
	}
	sb.WriteString("final output line")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write large log: %v", err)
	}
}

// TestRunListJSONKeysAndOrder (AC-13): `run list` with no flags prints a JSON
// array of every run's metadata, newest first, with the exact key set and null
// semantics.
func TestRunListJSONKeysAndOrder(t *testing.T) {
	fx := seedRuns(t)
	stdout, stderr, code := fx.env.RunCLI(fx.env.Home, "run", "list")
	if code != 0 {
		t.Fatalf("run list exit %d, stderr: %s", code, stderr)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d:\n%s", len(rows), stdout)
	}
	wantKeys := []string{"id", "project", "trigger", "task", "state", "failure_reason", "exit_code", "backend", "started_at", "completed_at"}
	assertExactKeys(t, rows[0], wantKeys)

	// Newest first: the running run (started +10m) is row 0.
	if got := string(rows[0]["state"]); got != `"running"` {
		t.Fatalf("expected newest-first ordering with running run first, got state %s", got)
	}
	// Running run: exit_code, completed_at and failure_reason are explicit null.
	for _, k := range []string{"exit_code", "completed_at", "failure_reason"} {
		if got := string(rows[0][k]); got != "null" {
			t.Fatalf("running run %s should be null, got %s", k, got)
		}
	}
}

// TestRunListFilters (AC-13): --state narrows the list and an invalid value exits
// non-zero listing the valid values; --reason behaves the same.
func TestRunListFilters(t *testing.T) {
	fx := seedRuns(t)

	// --state failed → just the failed run (normalised: upper-case input).
	stdout, stderr, code := fx.env.RunCLI(fx.env.Home, "run", "list", "--state", "FAILED")
	if code != 0 {
		t.Fatalf("run list --state failed exit %d, stderr: %s", code, stderr)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if len(rows) != 1 || string(rows[0]["state"]) != `"failed"` {
		t.Fatalf("expected one failed run, got:\n%s", stdout)
	}

	// --reason session_lost → the failed run.
	stdout, _, code = fx.env.RunCLI(fx.env.Home, "run", "list", "--reason", "session_lost")
	if code != 0 {
		t.Fatalf("run list --reason exit %d", code)
	}
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("expected one session_lost run, got:\n%s (err %v)", stdout, err)
	}

	// Invalid --state exits non-zero and lists the valid values on stderr.
	stdout, stderr, code = fx.env.RunCLI(fx.env.Home, "run", "list", "--state", "bogus")
	if code == 0 {
		t.Fatalf("invalid --state must exit non-zero; stdout: %s", stdout)
	}
	if !strings.Contains(stderr, "pending") || !strings.Contains(stderr, "failed") {
		t.Fatalf("invalid --state error must list valid values, got: %s", stderr)
	}

	// Invalid --reason exits non-zero and lists the valid reasons.
	_, stderr, code = fx.env.RunCLI(fx.env.Home, "run", "list", "--reason", "nope")
	if code == 0 {
		t.Fatal("invalid --reason must exit non-zero")
	}
	if !strings.Contains(stderr, "session_lost") || !strings.Contains(stderr, "timed_out") {
		t.Fatalf("invalid --reason error must list valid values, got: %s", stderr)
	}
}

// TestRunGetFullProjection (AC-13): `run get <id>` is a CLI-owned projection of
// the WHOLE RunRecord with the exact key set, binding, null semantics and an
// 8 KiB output_tail ending in the path breadcrumb when truncated.
func TestRunGetFullProjection(t *testing.T) {
	fx := seedRuns(t)
	stdout, stderr, code := fx.env.RunCLI(fx.env.Home, "run", "get", itoa(fx.failedC))
	if code != 0 {
		t.Fatalf("run get exit %d, stderr: %s", code, stderr)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &obj); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, stdout)
	}
	wantKeys := []string{
		"id", "project_id", "project_path", "trigger_id", "trigger_type", "task_id",
		"task_type", "resource_key", "branch", "state", "failure_reason", "exit_code",
		"started_at", "completed_at", "deadline_at", "binding", "runtime_dir",
		"output_path", "exit_path", "worktree_path", "error_message", "output_tail",
	}
	assertExactKeys(t, obj, wantKeys)

	// Binding is the opaque (manager, target) pair.
	var binding struct{ Manager, Target string }
	if err := json.Unmarshal(obj["binding"], &binding); err != nil {
		t.Fatalf("binding is not {manager,target}: %v", err)
	}
	if binding.Manager != "tmux" || binding.Target != "autowatch-run-2--review" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
	// failed run: exit_code null (session_lost), deadline_at set, worktree set.
	if string(obj["exit_code"]) != "null" {
		t.Fatalf("session_lost run exit_code should be null, got %s", obj["exit_code"])
	}
	if string(obj["failure_reason"]) != `"session_lost"` {
		t.Fatalf("failure_reason should be session_lost, got %s", obj["failure_reason"])
	}
	if string(obj["deadline_at"]) == "null" {
		t.Fatal("deadline_at should be set for this run")
	}
	if string(obj["worktree_path"]) == "null" {
		t.Fatal("worktree_path should be set for this claude run")
	}
	// output_tail is capped and ends in the breadcrumb for the large log.
	var tail string
	if err := json.Unmarshal(obj["output_tail"], &tail); err != nil {
		t.Fatalf("output_tail not a string: %v", err)
	}
	if len(tail) > 8192 {
		t.Fatalf("output_tail must be <= 8192 bytes, got %d", len(tail))
	}
	if !strings.HasSuffix(tail, "full output: "+fx.failedLog+"]") {
		t.Fatalf("output_tail must end in the path breadcrumb, got:\n%s", tail)
	}
}

// TestRunGetNullsWhileRunning (AC-13): a running run's exit_code, completed_at
// and failure_reason are explicit null, and a bash run's worktree_path is null.
func TestRunGetNullsWhileRunning(t *testing.T) {
	fx := seedRuns(t)
	stdout, stderr, code := fx.env.RunCLI(fx.env.Home, "run", "get", itoa(fx.running))
	if code != 0 {
		t.Fatalf("run get exit %d, stderr: %s", code, stderr)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &obj); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	for _, k := range []string{"exit_code", "completed_at", "failure_reason", "worktree_path", "deadline_at"} {
		if string(obj[k]) != "null" {
			t.Fatalf("running bash run %s should be null, got %s", k, obj[k])
		}
	}
}

// TestRunGetNotFound (AC-13): an unknown id exits 1 with the recovery hint.
func TestRunGetNotFound(t *testing.T) {
	fx := seedRuns(t)
	stdout, stderr, code := fx.env.RunCLI(fx.env.Home, "run", "get", "999999")
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown id, got %d", code)
	}
	if stdout != "" {
		t.Fatalf("stdout must be empty on not-found, got: %s", stdout)
	}
	if !strings.Contains(stderr, "run 999999 not found — list runs with: auto watch run list") {
		t.Fatalf("unexpected not-found message: %s", stderr)
	}
}

// TestRunGetText (AC-13): --text prints a human-readable record.
func TestRunGetText(t *testing.T) {
	fx := seedRuns(t)
	stdout, stderr, code := fx.env.RunCLI(fx.env.Home, "run", "get", itoa(fx.failedC), "--text")
	if code != 0 {
		t.Fatalf("run get --text exit %d, stderr: %s", code, stderr)
	}
	// Not JSON.
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(stdout), &obj) == nil {
		t.Fatalf("--text output must not be JSON:\n%s", stdout)
	}
	for _, want := range []string{"state: failed", "failure_reason: session_lost", "binding: tmux/autowatch-run-2--review"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in --text record:\n%s", want, stdout)
		}
	}
}

// TestRunListText (AC-13): --text prints a table, not JSON.
func TestRunListText(t *testing.T) {
	fx := seedRuns(t)
	stdout, stderr, code := fx.env.RunCLI(fx.env.Home, "run", "list", "--text")
	if code != 0 {
		t.Fatalf("run list --text exit %d, stderr: %s", code, stderr)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout), "[") {
		t.Fatalf("--text output must not be a JSON array:\n%s", stdout)
	}
	if !strings.Contains(stdout, "STATE") || !strings.Contains(stdout, "run-etl") {
		t.Fatalf("expected a table header and run rows:\n%s", stdout)
	}
}

func assertExactKeys(t *testing.T, obj map[string]json.RawMessage, want []string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(got)
	w := append([]string(nil), want...)
	sort.Strings(w)
	if strings.Join(got, ",") != strings.Join(w, ",") {
		t.Fatalf("key set mismatch:\n got  %v\n want %v", got, w)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
