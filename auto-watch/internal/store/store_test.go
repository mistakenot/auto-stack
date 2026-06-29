package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
)

func TestReserveRunDedupUsesSQLiteUniqueIndex(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	now := time.Now().UTC()
	_, err = db.ReserveRun(context.Background(), &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: "/tmp/demo",
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      "task-a",
		TaskType:    "bash",
		ResourceKey: "cron:daily",
		StartedAt:   now,
	})
	if err != nil {
		t.Fatalf("first reserve run failed: %v", err)
	}
	_, err = db.ReserveRun(context.Background(), &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: "/tmp/demo",
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      "task-a",
		TaskType:    "bash",
		ResourceKey: "cron:daily",
		StartedAt:   now,
	})
	if !errors.Is(err, store.ErrActiveRunExists) {
		t.Fatalf("expected ErrActiveRunExists, got %v", err)
	}
	runs, err := db.ListRunsByStates(context.Background(), model.RunPending)
	if err != nil {
		t.Fatalf("ListRunsByStates failed: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 pending run, got %d", len(runs))
	}
}

func openTestDB(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	return db
}

func TestClearRunWorktreePath(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	runID, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: "/tmp/demo",
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      "review",
		TaskType:    "claude",
		ResourceKey: "cron:daily",
		StartedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("reserve run: %v", err)
	}
	if err := db.UpdateRunStarted(ctx, runID, &store.RunStartUpdate{
		SessionName:  "session",
		WorktreePath: "/tmp/demo/.worktrees/old-run",
		Branch:       "main",
	}); err != nil {
		t.Fatalf("update run started: %v", err)
	}

	run, err := db.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.WorktreePath == "" {
		t.Fatalf("expected worktree_path set before clear")
	}

	if err := db.ClearRunWorktreePath(ctx, runID); err != nil {
		t.Fatalf("clear run worktree path: %v", err)
	}

	run, err = db.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("get run after clear: %v", err)
	}
	if run.WorktreePath != "" {
		t.Fatalf("expected worktree_path cleared, got %q", run.WorktreePath)
	}
}

func TestCountEventsByType(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insert := func(eventType string) {
		if _, err := db.InsertEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "info",
			EventType: eventType,
			Message:   "x",
		}); err != nil {
			t.Fatalf("insert %s: %v", eventType, err)
		}
	}
	insert("worktree_removed")
	insert("worktree_removed")
	insert("worktree_removed")
	insert("task_completed")

	count, err := db.CountEventsByType(ctx, "worktree_removed")
	if err != nil {
		t.Fatalf("count worktree_removed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 worktree_removed, got %d", count)
	}
	count, err = db.CountEventsByType(ctx, "task_completed")
	if err != nil {
		t.Fatalf("count task_completed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 task_completed, got %d", count)
	}
	count, err = db.CountEventsByType(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("count nonexistent: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 nonexistent, got %d", count)
	}
}

func TestWALCheckpoint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "logs.sqlite")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

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
		t.Fatalf("stat wal before checkpoint: %v", err)
	}
	if before.Size() == 0 {
		t.Fatalf("expected WAL to contain data before checkpoint")
	}

	if err := db.WALCheckpoint(ctx); err != nil {
		t.Fatalf("wal checkpoint: %v", err)
	}

	after, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal after checkpoint: %v", err)
	}
	if after.Size() >= before.Size() {
		t.Fatalf("expected WAL truncated by checkpoint: before=%d after=%d", before.Size(), after.Size())
	}
}

func TestFileSnapshotCRUD(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)

	// Empty initially.
	snaps, err := db.ListFileSnapshots(ctx, "proj", "watch-docs")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("expected 0 snapshots, got %d", len(snaps))
	}

	// Upsert two files.
	for _, path := range []string{"docs/a.md", "docs/b.md"} {
		if err := db.UpsertFileSnapshot(ctx, &model.FileSnapshotRecord{
			ProjectID:   "proj",
			TriggerID:   "watch-docs",
			FilePath:    path,
			ModTime:     now,
			FirstSeenAt: now,
			UpdatedAt:   now,
		}); err != nil {
			t.Fatalf("upsert %s: %v", path, err)
		}
	}

	snaps, err = db.ListFileSnapshots(ctx, "proj", "watch-docs")
	if err != nil {
		t.Fatalf("list after upsert: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}
	if snaps[0].FilePath != "docs/a.md" || snaps[1].FilePath != "docs/b.md" {
		t.Fatalf("unexpected paths: %v, %v", snaps[0].FilePath, snaps[1].FilePath)
	}

	// Re-upsert preserves first_seen_at, updates mod_time and updated_at.
	later := now.Add(time.Hour)
	if err := db.UpsertFileSnapshot(ctx, &model.FileSnapshotRecord{
		ProjectID:   "proj",
		TriggerID:   "watch-docs",
		FilePath:    "docs/a.md",
		ModTime:     later,
		FirstSeenAt: later, // passed but should NOT overwrite original
		UpdatedAt:   later,
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	snaps, err = db.ListFileSnapshots(ctx, "proj", "watch-docs")
	if err != nil {
		t.Fatalf("list after re-upsert: %v", err)
	}
	a := snaps[0]
	if !a.FirstSeenAt.Equal(now) {
		t.Fatalf("expected first_seen_at preserved as %v, got %v", now, a.FirstSeenAt)
	}
	if !a.ModTime.Equal(later) {
		t.Fatalf("expected mod_time updated to %v, got %v", later, a.ModTime)
	}
	if !a.UpdatedAt.Equal(later) {
		t.Fatalf("expected updated_at updated to %v, got %v", later, a.UpdatedAt)
	}

	// Delete one file.
	if err := db.DeleteFileSnapshots(ctx, "proj", "watch-docs", []string{"docs/a.md"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	snaps, err = db.ListFileSnapshots(ctx, "proj", "watch-docs")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(snaps) != 1 || snaps[0].FilePath != "docs/b.md" {
		t.Fatalf("expected only docs/b.md remaining, got %+v", snaps)
	}

	// Delete with empty slice is a no-op.
	if err := db.DeleteFileSnapshots(ctx, "proj", "watch-docs", nil); err != nil {
		t.Fatalf("delete empty: %v", err)
	}
}

func TestFileSnapshotIsolatedByTrigger(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)

	// Two triggers in the same project should have independent snapshots.
	for _, triggerID := range []string{"trigger-a", "trigger-b"} {
		if err := db.UpsertFileSnapshot(ctx, &model.FileSnapshotRecord{
			ProjectID:   "proj",
			TriggerID:   triggerID,
			FilePath:    "shared.txt",
			ModTime:     now,
			FirstSeenAt: now,
			UpdatedAt:   now,
		}); err != nil {
			t.Fatalf("upsert for %s: %v", triggerID, err)
		}
	}

	snapsA, _ := db.ListFileSnapshots(ctx, "proj", "trigger-a")
	snapsB, _ := db.ListFileSnapshots(ctx, "proj", "trigger-b")
	if len(snapsA) != 1 || len(snapsB) != 1 {
		t.Fatalf("expected 1 snapshot per trigger, got %d and %d", len(snapsA), len(snapsB))
	}

	// Deleting from one trigger doesn't affect the other.
	if err := db.DeleteFileSnapshots(ctx, "proj", "trigger-a", []string{"shared.txt"}); err != nil {
		t.Fatalf("delete from trigger-a: %v", err)
	}
	snapsB, _ = db.ListFileSnapshots(ctx, "proj", "trigger-b")
	if len(snapsB) != 1 {
		t.Fatalf("expected trigger-b snapshot untouched, got %d", len(snapsB))
	}
}

// reserveTerminalRun reserves a run, marks it started and terminal, and returns
// its id. Used by the retention pruning tests.
func reserveTerminalRun(t *testing.T, db *store.Store, taskID string, started, completed time.Time, state model.RunState) int64 {
	t.Helper()
	ctx := context.Background()
	runID, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: "/tmp/demo",
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
	if err := db.UpdateRunStarted(ctx, runID, &store.RunStartUpdate{SessionName: "s-" + taskID}); err != nil {
		t.Fatalf("update run started %s: %v", taskID, err)
	}
	exit := 0
	if err := db.MarkRunTerminal(ctx, runID, state, &exit, completed, ""); err != nil {
		t.Fatalf("mark run terminal %s: %v", taskID, err)
	}
	return runID
}

func TestPruneEventsOlderThan(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	cutoff := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)

	const oldCount = 100
	const recentCount = 10
	for i := range oldCount {
		if _, err := db.InsertEvent(ctx, &store.EventInput{
			Timestamp: cutoff.Add(-time.Duration(i+1) * time.Hour),
			Level:     "info",
			EventType: "old",
			Message:   "old",
		}); err != nil {
			t.Fatalf("insert old event %d: %v", i, err)
		}
	}
	for i := range recentCount {
		if _, err := db.InsertEvent(ctx, &store.EventInput{
			Timestamp: cutoff.Add(time.Duration(i+1) * time.Hour),
			Level:     "info",
			EventType: "recent",
			Message:   "recent",
		}); err != nil {
			t.Fatalf("insert recent event %d: %v", i, err)
		}
	}

	deleted, err := db.PruneEventsOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("prune events: %v", err)
	}
	if deleted != oldCount {
		t.Fatalf("expected %d events deleted, got %d", oldCount, deleted)
	}

	if got, _ := db.CountEventsByType(ctx, "old"); got != 0 {
		t.Fatalf("expected 0 old events remaining, got %d", got)
	}
	if got, _ := db.CountEventsByType(ctx, "recent"); got != recentCount {
		t.Fatalf("expected %d recent events remaining, got %d", recentCount, got)
	}
}

func TestDeleteRuns(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	old := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)

	id1 := reserveTerminalRun(t, db, "task-a", old, old.Add(time.Hour), model.RunCompleted)
	id2 := reserveTerminalRun(t, db, "task-b", old, old.Add(time.Hour), model.RunFailed)
	keep := reserveTerminalRun(t, db, "task-c", old, old.Add(time.Hour), model.RunCompleted)

	// An active run that must never be deleted by an id-targeted delete.
	activeID, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: "/tmp/demo",
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      "task-active",
		TaskType:    "bash",
		ResourceKey: "cron:task-active",
		StartedAt:   old,
	})
	if err != nil {
		t.Fatalf("reserve active run: %v", err)
	}

	// Empty slice is a no-op.
	if n, err := db.DeleteRuns(ctx, nil); err != nil || n != 0 {
		t.Fatalf("expected no-op for empty ids, got n=%d err=%v", n, err)
	}

	deleted, err := db.DeleteRuns(ctx, []int64{id1, id2})
	if err != nil {
		t.Fatalf("delete runs: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 runs deleted, got %d", deleted)
	}

	if _, err := db.GetRun(ctx, id1); err == nil {
		t.Fatalf("expected run %d deleted", id1)
	}
	if _, err := db.GetRun(ctx, id2); err == nil {
		t.Fatalf("expected run %d deleted", id2)
	}
	if _, err := db.GetRun(ctx, keep); err != nil {
		t.Fatalf("expected run %d (not in id list) untouched: %v", keep, err)
	}
	if _, err := db.GetRun(ctx, activeID); err != nil {
		t.Fatalf("expected active run %d untouched: %v", activeID, err)
	}
}
