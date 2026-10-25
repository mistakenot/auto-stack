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
	if _, err := db.MarkRunTerminal(ctx, runID, model.RunCompleted, &exitCode, time.Date(2026, 3, 18, 11, 0, 0, 0, time.UTC), ""); err != nil {
		t.Fatalf("MarkRunTerminal: %v", err)
	}

	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)

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

// TestRunRetentionPruneEmptyRuntimeDir guards the startup-failure case: a run
// that failed before UpdateRunStarted persisted RuntimeDir has an empty path in
// the DB even though startWorker already created the deterministic runs/<id>
// directory. Retention must remove that directory via the derived path rather
// than letting os.RemoveAll("") "succeed" and orphan it forever.
func TestRunRetentionPruneEmptyRuntimeDir(t *testing.T) {
	env, repoRoot, db := cleanTestEnv(t)
	ctx := context.Background()
	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	runID, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: repoRoot,
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      "broken",
		TaskType:    "bash",
		ResourceKey: "cron:broken",
		StartedAt:   time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReserveRun: %v", err)
	}
	// Deterministic dir created by startWorker, but RuntimeDir never persisted
	// (no UpdateRunStarted) — mark terminal directly, as a startup failure does.
	dir := filepath.Join(env.Home, ".auto", "watch", "runs", strconv.FormatInt(runID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "output.log"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if _, err := db.MarkRunTerminal(ctx, runID, model.RunFailed, nil, time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC), "startup failed"); err != nil {
		t.Fatalf("MarkRunTerminal: %v", err)
	}

	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
	if err := service.Tick(ctx); err != nil {
		t.Fatalf("tick failed: %v", err)
	}
	service.WaitWorkers()

	if _, err := db.GetRun(ctx, runID); err == nil {
		t.Fatalf("expected terminal run %d record deleted", runID)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected orphan run dir %q removed, stat err=%v", dir, err)
	}
}

// dirtyWorktree adds a real git worktree at repoRoot and leaves an uncommitted
// file in it, so gitx.IsDirty reports it dirty. Used by the dirty-worktree guard
// tests below.
func dirtyWorktree(t *testing.T, repoRoot, name string) string {
	t.Helper()
	wt := filepath.Join(config.WorktreesDir(repoRoot), name)
	if err := gitx.AddWorktree(repoRoot, wt, "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "dirty.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatalf("dirty worktree: %v", err)
	}
	if dirty, err := gitx.IsDirty(wt); err != nil || !dirty {
		t.Fatalf("expected a dirty worktree, IsDirty=%v err=%v", dirty, err)
	}
	return wt
}

// terminalRunWithWorktree reserves a run, points it at worktreePath, and marks it
// completed at completedAt. Returns the run id.
func terminalRunWithWorktree(t *testing.T, db *store.Store, repoRoot, taskID, worktreePath string, started, completed time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: repoRoot,
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      taskID,
		TaskType:    "claude",
		ResourceKey: "cron:" + taskID,
		StartedAt:   started,
	})
	if err != nil {
		t.Fatalf("ReserveRun: %v", err)
	}
	if err := db.UpdateRunStarted(ctx, id, &store.RunStartUpdate{
		SessionName:  "sess-" + taskID,
		WorktreePath: worktreePath,
		Branch:       "main",
	}); err != nil {
		t.Fatalf("UpdateRunStarted: %v", err)
	}
	exit := 0
	if _, err := db.MarkRunTerminal(ctx, id, model.RunCompleted, &exit, completed, ""); err != nil {
		t.Fatalf("MarkRunTerminal: %v", err)
	}
	return id
}

// TestCleanSweepsOldOrphanRunDirKeepsFresh covers AC-10: a numeric run dir with
// no matching row and an mtime older than 24h is removed with a
// run_dir_orphan_removed Event, while a fresh orphan dir is left in place.
func TestCleanSweepsOldOrphanRunDirKeepsFresh(t *testing.T) {
	env, _, db := cleanTestEnv(t)
	ctx := context.Background()
	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	runsDir := filepath.Join(env.Home, ".auto", "watch", "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldOrphan := filepath.Join(runsDir, "9001")
	freshOrphan := filepath.Join(runsDir, "9002")
	nonNumeric := filepath.Join(runsDir, "scratch")
	for _, d := range []string{oldOrphan, freshOrphan, nonNumeric} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Old orphan: mtime well over 24h before the daemon clock.
	oldTime := current.Add(-48 * time.Hour)
	if err := os.Chtimes(oldOrphan, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	// Fresh orphan: mtime 30 minutes before the clock (inside the 24h window).
	freshTime := current.Add(-30 * time.Minute)
	if err := os.Chtimes(freshOrphan, freshTime, freshTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(nonNumeric, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
	if err := service.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	service.WaitWorkers()

	if _, err := os.Stat(oldOrphan); !os.IsNotExist(err) {
		t.Fatalf("expected old orphan dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(freshOrphan); err != nil {
		t.Fatalf("expected fresh orphan dir kept: %v", err)
	}
	if _, err := os.Stat(nonNumeric); err != nil {
		t.Fatalf("expected non-numeric dir kept: %v", err)
	}
	if n, err := db.CountEventsByType(ctx, "run_dir_orphan_removed"); err != nil {
		t.Fatalf("count orphan events: %v", err)
	} else if n != 1 {
		t.Fatalf("run_dir_orphan_removed events = %d, want exactly 1", n)
	}
}

// TestRemoveWorktreeKeepsDirtyOnTTL covers AC-10: a terminal run past the 24h TTL
// whose worktree has uncommitted changes is kept, not force-removed, and
// worktree_kept_dirty is logged exactly once across repeated ticks.
func TestRemoveWorktreeKeepsDirtyOnTTL(t *testing.T) {
	_, repoRoot, db := cleanTestEnv(t)
	ctx := context.Background()
	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	wt := dirtyWorktree(t, repoRoot, "dirty-ttl")
	// Completed 26h before the clock: past the 24h TTL, within 7d retention.
	terminalRunWithWorktree(t, db, repoRoot, "review", wt,
		current.Add(-27*time.Hour), current.Add(-26*time.Hour))

	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
	for i := range 5 {
		if err := service.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		service.WaitWorkers()
	}

	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("expected dirty worktree kept: %v", err)
	}
	if n, err := db.CountEventsByType(ctx, "worktree_kept_dirty"); err != nil {
		t.Fatalf("count worktree_kept_dirty: %v", err)
	} else if n != 1 {
		t.Fatalf("worktree_kept_dirty events = %d across 5 ticks, want exactly 1", n)
	}
	if n, err := db.CountEventsByType(ctx, "worktree_removed"); err != nil {
		t.Fatalf("count worktree_removed: %v", err)
	} else if n != 0 {
		t.Fatalf("a dirty worktree must not be removed, worktree_removed = %d", n)
	}
}

// TestRemoveWorktreeKeepsDirtyOnForce covers AC-10 for the clean --force path.
func TestRemoveWorktreeKeepsDirtyOnForce(t *testing.T) {
	_, repoRoot, db := cleanTestEnv(t)
	ctx := context.Background()
	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	wt := dirtyWorktree(t, repoRoot, "dirty-force")
	id, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID: "demo", ProjectPath: repoRoot, TriggerID: "daily", TriggerType: "cron",
		TaskID: "review", TaskType: "claude", ResourceKey: "cron:review", StartedAt: current,
	})
	if err != nil {
		t.Fatalf("ReserveRun: %v", err)
	}
	// No BackendTarget, so Clean --force skips Stop and goes straight to removal.
	if err := db.UpdateRunStarted(ctx, id, &store.RunStartUpdate{SessionName: "s", WorktreePath: wt, Branch: "main"}); err != nil {
		t.Fatalf("UpdateRunStarted: %v", err)
	}

	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
	if err := service.Clean(ctx, true); err != nil {
		t.Fatalf("clean --force: %v", err)
	}

	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("expected dirty worktree kept on clean --force: %v", err)
	}
	if n, err := db.CountEventsByType(ctx, "worktree_kept_dirty"); err != nil {
		t.Fatalf("count worktree_kept_dirty: %v", err)
	} else if n != 1 {
		t.Fatalf("worktree_kept_dirty events = %d, want 1", n)
	}
}

// TestRemoveWorktreeKeepsDirtyOnCancel covers AC-10 for the cancel path.
func TestRemoveWorktreeKeepsDirtyOnCancel(t *testing.T) {
	_, repoRoot, db := cleanTestEnv(t)
	ctx := context.Background()
	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	wt := dirtyWorktree(t, repoRoot, "dirty-cancel")
	id, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID: "demo", ProjectPath: repoRoot, TriggerID: "daily", TriggerType: "cron",
		TaskID: "review", TaskType: "claude", ResourceKey: "cron:review", StartedAt: current,
	})
	if err != nil {
		t.Fatalf("ReserveRun: %v", err)
	}
	if err := db.UpdateRunStarted(ctx, id, &store.RunStartUpdate{SessionName: "s", WorktreePath: wt, Branch: "main"}); err != nil {
		t.Fatalf("UpdateRunStarted: %v", err)
	}

	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
	if _, err := service.Cancel(ctx, id); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("expected dirty worktree kept on cancel: %v", err)
	}
	if n, err := db.CountEventsByType(ctx, "worktree_kept_dirty"); err != nil {
		t.Fatalf("count worktree_kept_dirty: %v", err)
	} else if n != 1 {
		t.Fatalf("worktree_kept_dirty events = %d, want 1", n)
	}
}

// TestRemoveWorktreeKeepsUninspectable covers AC-10: when IsDirty errors (the
// worktree path is not a git worktree) the worktree is kept (fail-closed).
func TestRemoveWorktreeKeepsUninspectable(t *testing.T) {
	env, repoRoot, db := cleanTestEnv(t)
	ctx := context.Background()
	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	// A real directory that is NOT a git worktree, so `git status` errors.
	badWt := filepath.Join(env.Home, "not-a-worktree")
	if err := os.MkdirAll(badWt, 0o755); err != nil {
		t.Fatal(err)
	}
	terminalRunWithWorktree(t, db, repoRoot, "review", badWt,
		current.Add(-27*time.Hour), current.Add(-26*time.Hour))

	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
	if err := service.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	service.WaitWorkers()

	if _, err := os.Stat(badWt); err != nil {
		t.Fatalf("expected uninspectable worktree kept: %v", err)
	}
	if n, err := db.CountEventsByType(ctx, "worktree_kept_dirty"); err != nil {
		t.Fatalf("count worktree_kept_dirty: %v", err)
	} else if n != 1 {
		t.Fatalf("worktree_kept_dirty events = %d, want 1 (IsDirty error -> kept)", n)
	}
}

// TestRemoveWorktreeRemovesCleanWorktree covers AC-10: a clean worktree is still
// removed on the TTL path (worktree_removed, no worktree_kept_dirty).
func TestRemoveWorktreeRemovesCleanWorktree(t *testing.T) {
	_, repoRoot, db := cleanTestEnv(t)
	ctx := context.Background()
	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	// A clean worktree (added, nothing written).
	cleanWt := filepath.Join(config.WorktreesDir(repoRoot), "clean-ttl")
	if err := gitx.AddWorktree(repoRoot, cleanWt, "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	terminalRunWithWorktree(t, db, repoRoot, "review", cleanWt,
		current.Add(-27*time.Hour), current.Add(-26*time.Hour))

	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
	if err := service.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	service.WaitWorkers()

	if _, err := os.Stat(cleanWt); !os.IsNotExist(err) {
		t.Fatalf("expected clean worktree removed, stat err=%v", err)
	}
	if n, err := db.CountEventsByType(ctx, "worktree_removed"); err != nil {
		t.Fatalf("count worktree_removed: %v", err)
	} else if n != 1 {
		t.Fatalf("worktree_removed events = %d, want 1", n)
	}
	if n, err := db.CountEventsByType(ctx, "worktree_kept_dirty"); err != nil {
		t.Fatalf("count worktree_kept_dirty: %v", err)
	} else if n != 0 {
		t.Fatalf("a clean worktree must not be kept dirty, got %d", n)
	}
}

// TestRetentionSkipsRunWithDirtyWorktreeOnDisk covers AC-10: a terminal run past
// the 7-day retention window whose dirty worktree is still on disk is NOT
// deleted — its row (and worktree_path link) outlives retention so the kept
// worktree is never orphaned from its run.
func TestRetentionSkipsRunWithDirtyWorktreeOnDisk(t *testing.T) {
	_, repoRoot, db := cleanTestEnv(t)
	ctx := context.Background()
	current := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	wt := dirtyWorktree(t, repoRoot, "dirty-retention")
	// Completed 8 days before the clock: past the 7-day retention window.
	id := terminalRunWithWorktree(t, db, repoRoot, "review", wt,
		current.Add(-8*24*time.Hour-time.Hour), current.Add(-8*24*time.Hour))

	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
	if err := service.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	service.WaitWorkers()

	if _, err := db.GetRun(ctx, id); err != nil {
		t.Fatalf("expected retention to spare the run with a kept dirty worktree: %v", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("expected dirty worktree still on disk: %v", err)
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
	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
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

	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
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
		if _, err := db.MarkRunTerminal(ctx, runID, model.RunCompleted, &exit, time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC), ""); err != nil {
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

	service := daemon.New(db, completingRegistry(), nil, func() time.Time { return current }, nil)
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
