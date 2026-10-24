package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/backend"
	"github.com/mistakenot/auto-shared/backend/backendtest"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/testutil"
)

// TestTickContainsPerRunFailures (AC-5): in Project A one running run's Status
// errors and another (terminal) run's worktree removal fails. Each becomes a
// system_warning Event naming the run, Project B's cron Trigger still dispatches
// its task, and Tick returns nil — one bad run never takes the tick down.
func TestTickContainsPerRunFailures(t *testing.T) {
	env := testutil.NewEnv(t)
	ctx := context.Background()

	// Project B: a real repo with a bash task on an every-minute cron. It is the
	// only registered Project; Project A exists only as store rows.
	repoB := env.NewRepo("projectb")
	if _, _, code := env.RunCLI(repoB, "init", "--project-id", "projectb"); code != 0 {
		t.Fatal("init project B failed")
	}
	if _, stderr, code := env.RunCLI(repoB, "task", "create", "--id", "run-b", "--bash", "echo ok"); code != 0 {
		t.Fatalf("task create B: %s", stderr)
	}
	if _, stderr, code := env.RunCLI(repoB, "trigger", "create", "--id", "every-minute", "--cron", "* * * * *"); code != 0 {
		t.Fatalf("trigger create B: %s", stderr)
	}
	if _, stderr, code := env.RunCLI(repoB, "trigger", "add-task", "--trigger", "every-minute", "--task", "run-b"); code != 0 {
		t.Fatalf("trigger add-task B: %s", stderr)
	}

	db, err := store.Open(filepath.Join(env.Home, ".auto", "watch", "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// The fake errors on Status for every run — so Project A's running run hits
	// the Status-error containment path on Reap.
	fake := &backendtest.Fake{}
	fake.SetStatusErr(errors.New("backend manager wedged"))
	svc := daemon.New(db, backend.NewRegistry(fake), nil, func() time.Time { return now }, nil)

	// Project A, run 1: a running run with a runtime dir so Reap inspects it; its
	// Status errors (contained as a system_warning, under its deadline).
	runningID := reserveRunning(t, db, "proja", "task-status", repoB, now, "")

	// Project A, run 2: a terminal run older than 24h whose worktree path exists
	// but is NOT a registered git worktree, so `git worktree remove` fails on the
	// Clean TTL path (contained as a system_warning).
	badWorktree := filepath.Join(t.TempDir(), "not-a-worktree")
	if err := os.MkdirAll(badWorktree, 0o755); err != nil {
		t.Fatal(err)
	}
	terminalID := reserveRunning(t, db, "proja", "task-worktree", repoB, now.Add(-26*time.Hour), badWorktree)
	exit := 0
	if _, err := db.MarkRunTerminal(ctx, terminalID, model.RunCompleted, &exit, now.Add(-25*time.Hour), ""); err != nil {
		t.Fatalf("mark terminal: %v", err)
	}

	// Tick must contain both failures and return nil.
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("Tick returned error, want nil (failures must be contained): %v", err)
	}
	svc.WaitWorkers()

	// Each per-run failure is a system_warning naming its run.
	warnings, err := db.ListEvents(ctx, &store.EventFilter{Limit: 100, EventType: "system_warning"})
	if err != nil {
		t.Fatalf("list system_warning events: %v", err)
	}
	if !hasWarningForRun(warnings, runningID) {
		t.Fatalf("expected a system_warning naming the Status-error run %d; got %+v", runningID, warnings)
	}
	if !hasWarningForRun(warnings, terminalID) {
		t.Fatalf("expected a system_warning naming the worktree-removal run %d; got %+v", terminalID, warnings)
	}

	// Project B's Trigger still ran: a run exists for it.
	all, err := db.ListRunsByStates(ctx, model.RunPending, model.RunRunning, model.RunCompleted, model.RunFailed)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	foundB := false
	for i := range all {
		if all[i].ProjectID == "projectb" && all[i].TaskID == "run-b" {
			foundB = true
		}
	}
	if !foundB {
		t.Fatal("Project B's trigger did not dispatch despite Project A's contained failures")
	}
}

// reserveRunning reserves a run for projectID/taskID and marks it running with
// the given runtime dir and (optional) worktree path. Returns the run id.
func reserveRunning(t *testing.T, db *store.Store, projectID, taskID, projectPath string, started time.Time, worktree string) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID:   projectID,
		ProjectPath: projectPath,
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      taskID,
		TaskType:    "bash",
		ResourceKey: "cron:" + taskID,
		StartedAt:   started,
	})
	if err != nil {
		t.Fatalf("reserve run %s: %v", taskID, err)
	}
	runDir := filepath.Join(t.TempDir(), "runtime")
	if err := db.UpdateRunStarted(ctx, id, &store.RunStartUpdate{
		SessionName:   "sess-" + taskID,
		RuntimeDir:    runDir,
		Backend:       "tmux",
		BackendTarget: "sess-" + taskID,
		WorktreePath:  worktree,
	}); err != nil {
		t.Fatalf("update run started %s: %v", taskID, err)
	}
	return id
}

func hasWarningForRun(events []model.EventRecord, runID int64) bool {
	for i := range events {
		if events[i].RunID != nil && *events[i].RunID == runID {
			return true
		}
	}
	return false
}
