package daemon_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/backend"
	"github.com/mistakenot/auto-shared/backend/backendtest"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
)

func TestCancelRunningRunKillsAndFails(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	fake := &backendtest.Fake{}
	svc := daemon.New(db, backend.NewRegistry(fake), nil, func() time.Time { return now }, nil)

	runID, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "echo hi"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	state, err := svc.Cancel(context.Background(), runID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if state != model.RunFailed {
		t.Fatalf("Cancel state = %q, want failed", state)
	}
	if fake.StopCount() != 1 {
		t.Fatalf("expected exactly 1 kill, got %d", fake.StopCount())
	}

	run, err := db.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != model.RunFailed {
		t.Fatalf("run state = %q, want failed", run.State)
	}
}

func TestCancelPendingRunFailsWithoutKill(t *testing.T) {
	db := newTestStore(t)
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	fake := &backendtest.Fake{}
	svc := daemon.New(db, backend.NewRegistry(fake), nil, func() time.Time { return now }, nil)

	runID, err := db.ReserveRun(context.Background(), bashReserveInput(t.TempDir(), now))
	if err != nil {
		t.Fatalf("ReserveRun: %v", err)
	}

	state, err := svc.Cancel(context.Background(), runID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if state != model.RunFailed {
		t.Fatalf("Cancel state = %q, want failed", state)
	}
	if fake.StopCount() != 0 {
		t.Fatalf("expected no kill for pending run, got %d", fake.StopCount())
	}
}

func TestCancelTerminalRunIsNoOp(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	fake := &backendtest.Fake{}
	svc := daemon.New(db, backend.NewRegistry(fake), nil, func() time.Time { return now }, nil)

	runID, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "echo hi"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// Drive the run to completion via the exit file + Reap.
	run, err := db.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if err := os.WriteFile(run.ExitPath, []byte("0\n"), 0o644); err != nil {
		t.Fatalf("write exit file: %v", err)
	}
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap: %v", err)
	}

	killsBefore := fake.StopCount()
	state, err := svc.Cancel(context.Background(), runID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if state != model.RunCompleted {
		t.Fatalf("Cancel state = %q, want completed (no-op)", state)
	}
	if fake.StopCount() != killsBefore {
		t.Fatalf("Cancel on terminal run should not kill: before=%d after=%d", killsBefore, fake.StopCount())
	}
}

func TestReapDoesNotRetransitionCancelledRun(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	fake := &backendtest.Fake{}
	svc := daemon.New(db, backend.NewRegistry(fake), nil, func() time.Time { return now }, nil)

	runID, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "echo hi"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := svc.Cancel(context.Background(), runID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	killsAfterCancel := fake.StopCount()

	// Even if an exit file later appears, Reap must not re-transition a run
	// that Cancel already moved to a terminal state.
	run, err := db.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if err := os.WriteFile(run.ExitPath, []byte("0\n"), 0o644); err != nil {
		t.Fatalf("write exit file: %v", err)
	}
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap: %v", err)
	}

	after, err := db.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if after.State != model.RunFailed {
		t.Fatalf("run state after reap = %q, want failed (unchanged)", after.State)
	}
	if after.ExitCode != nil {
		t.Fatalf("cancelled run should have no exit code, got %d", *after.ExitCode)
	}
	if fake.StopCount() != killsAfterCancel {
		t.Fatalf("Reap should not kill an already-terminal run: before=%d after=%d", killsAfterCancel, fake.StopCount())
	}
}
