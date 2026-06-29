package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/gitx"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/testutil"
)

// cleanTestEnv registers a project (so Tick can load the global config) and
// returns the repo plus an opened store at the canonical logs path.
func cleanTestEnv(t *testing.T) (*testutil.Env, string, *store.Store) {
	t.Helper()
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	if _, stderr, code := env.RunCLI(repoRoot, "init"); code != 0 {
		t.Fatalf("init failed: %s", stderr)
	}
	dbPath := filepath.Join(env.Home, ".auto", "watch", "logs.sqlite")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	return env, repoRoot, db
}

// TestCleanWorktreeIdempotent covers AC-1: across 5 consecutive ticks a single
// expired terminal run with a worktree must emit exactly one worktree_removed
// event — proving Clean no longer re-processes already-removed worktrees.
func TestCleanWorktreeIdempotent(t *testing.T) {
	env, repoRoot, db := cleanTestEnv(t)
	ctx := context.Background()

	// A real worktree so the first Clean actually removes it.
	worktreePath := filepath.Join(config.WorktreesDir(repoRoot), "old-run")
	if err := gitx.AddWorktree(repoRoot, worktreePath, "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	runID, err := db.ReserveRun(ctx, &store.ReserveRunInput{
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
		t.Fatalf("ReserveRun: %v", err)
	}
	if err := db.UpdateRunStarted(ctx, runID, &store.RunStartUpdate{
		SessionName:  "session",
		RuntimeDir:   filepath.Join(env.Home, ".auto", "watch", "runs", "1"),
		OutputPath:   filepath.Join(env.Home, ".auto", "watch", "runs", "1", "output.log"),
		ExitPath:     filepath.Join(env.Home, ".auto", "watch", "runs", "1", "exit-code"),
		WorktreePath: worktreePath,
		Branch:       "main",
	}); err != nil {
		t.Fatalf("UpdateRunStarted: %v", err)
	}
	exitCode := 0
	// Completed well over 24h before the (fixed) daemon clock so it is an
	// expired terminal run on every tick.
	if err := db.MarkRunTerminal(ctx, runID, model.RunCompleted, &exitCode, time.Date(2026, 3, 18, 11, 0, 0, 0, time.UTC), ""); err != nil {
		t.Fatalf("MarkRunTerminal: %v", err)
	}

	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	service := daemon.New(db, fakeBackend{}, nil, func() time.Time { return current }, nil)

	for i := range 5 {
		if err := service.Tick(ctx); err != nil {
			t.Fatalf("tick %d failed: %v", i+1, err)
		}
		service.WaitWorkers()
	}

	count, err := db.CountEventsByType(ctx, "worktree_removed")
	if err != nil {
		t.Fatalf("CountEventsByType: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 worktree_removed event across 5 ticks, got %d", count)
	}

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree to be removed, stat err=%v", err)
	}

	run, err := db.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.WorktreePath != "" {
		t.Fatalf("expected run worktree_path cleared, got %q", run.WorktreePath)
	}
}

// TestWALCheckpointOnTick covers AC-4: a tick checkpoints the WAL, truncating
// accumulated WAL data without error.
func TestWALCheckpointOnTick(t *testing.T) {
	env, _, db := cleanTestEnv(t)
	ctx := context.Background()
	dbPath := filepath.Join(env.Home, ".auto", "watch", "logs.sqlite")

	now := time.Now().UTC()
	for i := range 200 {
		if _, err := db.InsertEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "info",
			EventType: "filler",
			Message:   "noise",
		}); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}

	walPath := dbPath + "-wal"
	before, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal before tick: %v", err)
	}
	if before.Size() == 0 {
		t.Fatalf("expected WAL to contain data before tick")
	}

	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	service := daemon.New(db, fakeBackend{}, nil, func() time.Time { return current }, nil)
	if err := service.Tick(ctx); err != nil {
		t.Fatalf("tick failed: %v", err)
	}

	after, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal after tick: %v", err)
	}
	if after.Size() >= before.Size() {
		t.Fatalf("expected WAL truncated by tick checkpoint: before=%d after=%d", before.Size(), after.Size())
	}
}

// TestEventRetentionPrune covers AC-2: events older than the retention window
// are pruned on tick while events within the window remain. We assert on
// purpose-seeded event types so daemon-emitted tick events don't perturb counts.
func TestEventRetentionPrune(t *testing.T) {
	_, _, db := cleanTestEnv(t)
	ctx := context.Background()

	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	// Default retention is 7 days -> cutoff is 2026-03-13 12:00.
	const oldCount = 100
	const recentCount = 10
	for i := range oldCount {
		if _, err := db.InsertEvent(ctx, &store.EventInput{
			Timestamp: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute),
			Level:     "info",
			EventType: "seeded_old",
			Message:   "old",
		}); err != nil {
			t.Fatalf("insert old event %d: %v", i, err)
		}
	}
	for i := range recentCount {
		if _, err := db.InsertEvent(ctx, &store.EventInput{
			Timestamp: time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute),
			Level:     "info",
			EventType: "seeded_recent",
			Message:   "recent",
		}); err != nil {
			t.Fatalf("insert recent event %d: %v", i, err)
		}
	}

	service := daemon.New(db, fakeBackend{}, nil, func() time.Time { return current }, nil)
	if err := service.Tick(ctx); err != nil {
		t.Fatalf("tick failed: %v", err)
	}
	service.WaitWorkers()

	if got, _ := db.CountEventsByType(ctx, "seeded_old"); got != 0 {
		t.Fatalf("expected 0 old events after prune, got %d", got)
	}
	if got, _ := db.CountEventsByType(ctx, "seeded_recent"); got != recentCount {
		t.Fatalf("expected %d recent events after prune, got %d", recentCount, got)
	}
}

// TestRunRetentionPrune covers AC-3: terminal runs older than the retention
// window have their records deleted and on-disk directories removed, while
// active runs (and their directories) are left untouched.
func TestRunRetentionPrune(t *testing.T) {
	env, repoRoot, db := cleanTestEnv(t)
	ctx := context.Background()
	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	runsDir := filepath.Join(env.Home, ".auto", "watch", "runs")

	// Helper to create an on-disk run directory with a marker file.
	makeDir := func(id int64) string {
		dir := filepath.Join(runsDir, strconv.FormatInt(id, 10))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir run dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "output.log"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		return dir
	}

	// 5 terminal runs completed well past the 7-day retention window.
	terminalDirs := map[int64]string{}
	for i := range 5 {
		runID, err := db.ReserveRun(ctx, &store.ReserveRunInput{
			ProjectID:   "demo",
			ProjectPath: repoRoot,
			TriggerID:   "daily",
			TriggerType: "cron",
			TaskID:      "review-" + strconv.Itoa(i),
			TaskType:    "bash",
			ResourceKey: "cron:review-" + strconv.Itoa(i),
			StartedAt:   time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("reserve terminal run %d: %v", i, err)
		}
		dir := makeDir(runID)
		if err := db.UpdateRunStarted(ctx, runID, &store.RunStartUpdate{SessionName: "s", RuntimeDir: dir}); err != nil {
			t.Fatalf("update started: %v", err)
		}
		exit := 0
		if err := db.MarkRunTerminal(ctx, runID, model.RunCompleted, &exit, time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC), ""); err != nil {
			t.Fatalf("mark terminal: %v", err)
		}
		terminalDirs[runID] = dir
	}

	// One active (running) run started recently — must survive.
	activeID, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: repoRoot,
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      "active",
		TaskType:    "bash",
		ResourceKey: "cron:active",
		StartedAt:   time.Date(2026, 3, 20, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("reserve active run: %v", err)
	}
	activeDir := makeDir(activeID)
	if err := db.UpdateRunStarted(ctx, activeID, &store.RunStartUpdate{SessionName: "s-active", RuntimeDir: activeDir}); err != nil {
		t.Fatalf("update active started: %v", err)
	}

	service := daemon.New(db, fakeBackend{}, nil, func() time.Time { return current }, nil)
	if err := service.Tick(ctx); err != nil {
		t.Fatalf("tick failed: %v", err)
	}
	service.WaitWorkers()

	for runID, dir := range terminalDirs {
		if _, err := db.GetRun(ctx, runID); err == nil {
			t.Fatalf("expected terminal run %d record deleted", runID)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("expected terminal run dir %q removed, stat err=%v", dir, err)
		}
	}

	if _, err := db.GetRun(ctx, activeID); err != nil {
		t.Fatalf("expected active run %d untouched: %v", activeID, err)
	}
	if _, err := os.Stat(activeDir); err != nil {
		t.Fatalf("expected active run dir %q untouched: %v", activeDir, err)
	}
}
