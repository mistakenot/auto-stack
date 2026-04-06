package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/gitx"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/runner"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/testutil"
)

type fakeBackend struct{}

func (fakeBackend) Start(_ context.Context, spec *runner.StartSpec) (runner.Handle, error) {
	if err := os.WriteFile(spec.OutputPath, []byte("ok\n"), 0o644); err != nil {
		return runner.Handle{}, err
	}
	if err := os.WriteFile(spec.ExitPath, []byte("0\n"), 0o644); err != nil {
		return runner.Handle{}, err
	}
	return runner.Handle{SessionName: spec.SessionName, ExitPath: spec.ExitPath, OutputPath: spec.OutputPath}, nil
}

func (fakeBackend) Kill(context.Context, runner.Handle) error { return nil }
func (fakeBackend) SessionExists(context.Context, string) (bool, error) {
	return false, nil
}

func TestDaemonHonorsOnlyIfBranchHasChanged(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	if _, _, code := env.RunCLI(repoRoot, "init"); code != 0 {
		t.Fatal("init failed")
	}
	if _, stderr, code := env.RunCLI(repoRoot, "task", "create", "--id", "run-etl", "--bash", "echo ok"); code != 0 {
		t.Fatalf("task create failed: %s", stderr)
	}
	if _, stderr, code := env.RunCLI(repoRoot, "trigger", "create", "--id", "daily", "--cron", "0 10 * * *", "--only-if-branch-changed", "main"); code != 0 {
		t.Fatalf("trigger create failed: %s", stderr)
	}
	if _, stderr, code := env.RunCLI(repoRoot, "trigger", "add-task", "--trigger", "daily", "--task", "run-etl"); code != 0 {
		t.Fatalf("trigger add-task failed: %s", stderr)
	}

	db, err := store.Open(filepath.Join(env.Home, ".auto", "watch", "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	current := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	service := daemon.New(db, fakeBackend{}, nil, func() time.Time { return current })

	if err := service.Tick(context.Background()); err != nil {
		t.Fatalf("first tick failed: %v", err)
	}
	service.WaitWorkers()
	if err := service.Reap(context.Background()); err != nil {
		t.Fatalf("first reap failed: %v", err)
	}

	current = current.Add(24 * time.Hour)
	if err := service.Tick(context.Background()); err != nil {
		t.Fatalf("second tick failed: %v", err)
	}
	service.WaitWorkers()
	if err := service.Reap(context.Background()); err != nil {
		t.Fatalf("second reap failed: %v", err)
	}

	env.CommitFile(repoRoot, "notes.txt", "changed\n", "second")
	current = current.Add(24 * time.Hour)
	if err := service.Tick(context.Background()); err != nil {
		t.Fatalf("third tick failed: %v", err)
	}
	service.WaitWorkers()
	if err := service.Reap(context.Background()); err != nil {
		t.Fatalf("third reap failed: %v", err)
	}

	counts, err := db.RecentRunCounts(context.Background(), time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("RecentRunCounts failed: %v", err)
	}
	if counts["completed"] != 2 {
		runs, _ := db.ListRunsByStates(context.Background(), model.RunPending, model.RunRunning, model.RunCompleted, model.RunFailed)
		events, _ := db.ListEvents(context.Background(), &store.EventFilter{Limit: 50})
		t.Logf("runs: %+v", runs)
		t.Logf("events: %+v", events)
		t.Fatalf("expected 2 completed runs, got %+v", counts)
	}
}

func TestReapMarksAbandonedPendingRunsFailed(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	runID, err := db.ReserveRun(context.Background(), &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: "/tmp/demo",
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      "run-etl",
		TaskType:    "bash",
		ResourceKey: "cron:daily",
		StartedAt:   time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("reserve run: %v", err)
	}

	service := daemon.New(db, fakeBackend{}, nil, func() time.Time {
		return time.Date(2026, 3, 20, 9, 10, 0, 0, time.UTC)
	})
	if err := service.Reap(context.Background()); err != nil {
		t.Fatalf("Reap failed: %v", err)
	}
	run, err := db.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun failed: %v", err)
	}
	if run.State != model.RunFailed || run.ErrorMessage != "worker did not start" {
		t.Fatalf("unexpected run state after reap: %+v", run)
	}
}

func TestTickReapsCompletedRunsBeforeDedupReservation(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	if _, _, code := env.RunCLI(repoRoot, "init"); code != 0 {
		t.Fatal("init failed")
	}
	if _, stderr, code := env.RunCLI(repoRoot, "task", "create", "--id", "run-etl", "--bash", "echo ok"); code != 0 {
		t.Fatalf("task create failed: %s", stderr)
	}
	if _, stderr, code := env.RunCLI(repoRoot, "trigger", "create", "--id", "every-minute", "--cron", "* * * * *"); code != 0 {
		t.Fatalf("trigger create failed: %s", stderr)
	}
	if _, stderr, code := env.RunCLI(repoRoot, "trigger", "add-task", "--trigger", "every-minute", "--task", "run-etl"); code != 0 {
		t.Fatalf("trigger add-task failed: %s", stderr)
	}

	db, err := store.Open(filepath.Join(env.Home, ".auto", "watch", "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	current := time.Date(2026, 3, 21, 20, 0, 0, 0, time.UTC)
	service := daemon.New(db, fakeBackend{}, nil, func() time.Time { return current })

	if err := service.Tick(context.Background()); err != nil {
		t.Fatalf("first tick failed: %v", err)
	}
	service.WaitWorkers()

	current = current.Add(time.Minute)
	if err := service.Tick(context.Background()); err != nil {
		t.Fatalf("second tick failed: %v", err)
	}
	service.WaitWorkers()

	running, err := db.ListRunsByStates(context.Background(), model.RunRunning)
	if err != nil {
		t.Fatalf("ListRunsByStates running failed: %v", err)
	}
	if len(running) != 1 {
		t.Fatalf("expected exactly one active run after second tick, got %d", len(running))
	}

	events, err := db.ListEvents(context.Background(), &store.EventFilter{
		Limit:     50,
		EventType: "task_skipped_dedup",
	})
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no dedup skip events, got %+v", events)
	}

	current = current.Add(time.Minute)
	if err := service.Tick(context.Background()); err != nil {
		t.Fatalf("third tick failed: %v", err)
	}
	service.WaitWorkers()

	counts, err := db.RecentRunCounts(context.Background(), time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("RecentRunCounts failed: %v", err)
	}
	if counts["completed"] != 2 {
		t.Fatalf("expected 2 completed runs after third tick reap, got %+v", counts)
	}
}

func TestCleanRemovesExpiredTerminalWorktrees(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	db, err := store.Open(filepath.Join(t.TempDir(), "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	worktreePath := filepath.Join(config.WorktreesDir(repoRoot), "old-run")
	if err := gitx.AddWorktree(repoRoot, worktreePath, "main"); err != nil {
		t.Fatalf("AddWorktree failed: %v", err)
	}
	runID, err := db.ReserveRun(context.Background(), &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: repoRoot,
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      "review",
		TaskType:    "claude",
		ResourceKey: "cron:daily",
		StartedAt:   time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReserveRun failed: %v", err)
	}
	if err := db.UpdateRunStarted(context.Background(), runID, &store.RunStartUpdate{
		SessionName:  "session",
		RuntimeDir:   filepath.Join(env.Home, ".auto", "watch", "runs", "1"),
		OutputPath:   filepath.Join(env.Home, ".auto", "watch", "runs", "1", "output.log"),
		ExitPath:     filepath.Join(env.Home, ".auto", "watch", "runs", "1", "exit-code"),
		WorktreePath: worktreePath,
		Branch:       "main",
	}); err != nil {
		t.Fatalf("UpdateRunStarted failed: %v", err)
	}
	exitCode := 0
	if err := db.MarkRunTerminal(context.Background(), runID, model.RunCompleted, &exitCode, time.Date(2026, 3, 18, 11, 0, 0, 0, time.UTC), ""); err != nil {
		t.Fatalf("MarkRunTerminal failed: %v", err)
	}

	service := daemon.New(db, fakeBackend{}, nil, func() time.Time {
		return time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	})
	if err := service.Clean(context.Background(), false); err != nil {
		t.Fatalf("Clean failed: %v", err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree to be removed, stat err=%v", err)
	}
}
