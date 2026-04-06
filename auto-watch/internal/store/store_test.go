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
