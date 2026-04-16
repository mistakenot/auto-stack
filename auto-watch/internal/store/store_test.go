package store_test

import (
	"context"
	"errors"
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
