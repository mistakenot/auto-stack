package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/backend"
	"github.com/mistakenot/auto-shared/backend/backendtest"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/runner"
	"github.com/mistakenot/auto-watch/internal/store"
)

// runningRegistry returns a Backend registry whose (default, "tmux") Backend
// leaves every run running — it writes no exit-code file, so a run stays
// RunRunning until a test writes one and Status reads it. It replaces the old
// ad-hoc recordingBackend.
func runningRegistry() *backend.Registry {
	return backend.NewRegistry(&backendtest.Fake{})
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	db, err := store.Open(filepath.Join(t.TempDir(), "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	return db
}

func bashReserveInput(projectPath string, now time.Time) *store.ReserveRunInput {
	return &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: projectPath,
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      "run-etl",
		TaskType:    "bash",
		ResourceKey: "cron:daily",
		StartedAt:   now,
	}
}

func TestDispatchReservesAndStartsRun(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	svc := daemon.New(db, runningRegistry(), nil, func() time.Time { return now }, nil)

	runID, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "echo hi"})
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if runID <= 0 {
		t.Fatalf("expected positive run id, got %d", runID)
	}

	run, err := db.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != model.RunRunning {
		t.Fatalf("expected run state running, got %q", run.State)
	}
	wantSession := runner.ScheduledRunName(runID, "run-etl")
	if run.SessionName != wantSession {
		t.Fatalf("session name = %q, want %q", run.SessionName, wantSession)
	}
	if run.ProjectID != "demo" || run.TriggerID != "daily" || run.TaskID != "run-etl" {
		t.Fatalf("unexpected run identity columns: %+v", run)
	}
	if run.ResourceKey != "cron:daily" {
		t.Fatalf("resource key = %q, want cron:daily", run.ResourceKey)
	}
}

func TestDispatchDuplicateActiveRunReturnsErrActiveRunExists(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	svc := daemon.New(db, runningRegistry(), nil, func() time.Time { return now }, nil)

	if _, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "echo hi"}); err != nil {
		t.Fatalf("first Dispatch failed: %v", err)
	}
	_, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "echo hi"})
	if !errors.Is(err, store.ErrActiveRunExists) {
		t.Fatalf("expected ErrActiveRunExists, got %v", err)
	}
}
