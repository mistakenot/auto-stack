package daemon_test

import (
	"context"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/backend"
	"github.com/mistakenot/auto-shared/backend/backendtest"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/runner"
)

// TestDispatchUsesSelectedBackend (AC-1): a TaskDef that selects a non-default
// Backend is started through that Backend, and the run row records the Binding's
// manager (backend) and target (backend_target).
func TestDispatchUsesSelectedBackend(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)

	// A registry with the default tmux Backend plus a test-registered "fake".
	fake := &backendtest.Fake{NameVal: "fake"}
	reg := backend.NewRegistry(&backendtest.Fake{}, fake)
	svc := daemon.New(db, reg, nil, func() time.Time { return now }, nil)

	runID, err := svc.Dispatch(context.Background(),
		bashReserveInput(projectPath, now),
		model.TaskDef{Type: "bash", Command: "echo hi", Backend: "fake"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if fake.StartCount() != 1 {
		t.Fatalf("expected the fake backend to start the run once, got %d", fake.StartCount())
	}

	run, err := db.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != model.RunRunning {
		t.Fatalf("run state = %q, want running", run.State)
	}
	if run.Backend != "fake" {
		t.Fatalf("run backend = %q, want fake", run.Backend)
	}
	wantTarget := runner.ScheduledRunName(runID, "run-etl")
	if run.BackendTarget != wantTarget {
		t.Fatalf("run backend_target = %q, want %q", run.BackendTarget, wantTarget)
	}
}
