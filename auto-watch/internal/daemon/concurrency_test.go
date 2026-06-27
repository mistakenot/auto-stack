package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
)

// TestConcurrentDispatchAndReap proves the single-mutex dispatch + single-reaper
// design is race-free with exactly-once lifecycle events. It must pass under
// `go test -race`.
//
// Phase A storms the service with distinct dispatches while a Reap loop runs
// concurrently. Phase B fires a wave of colliding-resourceKey dispatches and
// asserts the SQLite unique index admits exactly one. Phase C completes every
// run and runs a single Reap, asserting each run reaches a terminal state once
// and emits exactly one started + one terminal bus event.
func TestConcurrentDispatchAndReap(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	hub := bus.NewHub()
	sink := &collectSink{}
	cancelSub := hub.Subscribe(sink)
	defer cancelSub()
	svc := daemon.New(db, &recordingBackend{}, nil, func() time.Time { return now }, hub)

	task := model.TaskDef{Type: "bash", Command: "echo hi"}

	// successIDs accumulates every run id that Dispatch reports as started, across
	// both the distinct and colliding phases.
	var successIDs sync.Map

	// --- Phase A: distinct dispatches racing an in-flight Reap loop ---
	const distinct = 50

	stop := make(chan struct{})
	var reapWG sync.WaitGroup
	reapWG.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				if err := svc.Reap(context.Background()); err != nil {
					t.Errorf("Reap in flight: %v", err)
					return
				}
			}
		}
	})

	var wgA sync.WaitGroup
	for i := range distinct {
		wgA.Go(func() {
			in := &store.ReserveRunInput{
				ProjectID:   fmt.Sprintf("proj-%d", i),
				ProjectPath: projectPath,
				TriggerID:   fmt.Sprintf("trig-%d", i),
				TriggerType: "cron",
				TaskID:      fmt.Sprintf("task-%d", i),
				TaskType:    "bash",
				ResourceKey: fmt.Sprintf("cron:%d", i),
				StartedAt:   now,
			}
			id, err := svc.Dispatch(context.Background(), in, task)
			if err != nil {
				t.Errorf("distinct dispatch %d failed: %v", i, err)
				return
			}
			successIDs.Store(id, struct{}{})
		})
	}
	wgA.Wait()
	close(stop)
	reapWG.Wait()

	// --- Phase B: colliding resourceKey, the unique index admits exactly one ---
	const collide = 20
	var collideSuccess, collideConflict int64
	collideInput := func() *store.ReserveRunInput {
		return &store.ReserveRunInput{
			ProjectID:   "collide",
			ProjectPath: projectPath,
			TriggerID:   "collide-trig",
			TriggerType: "cron",
			TaskID:      "collide-task",
			TaskType:    "bash",
			ResourceKey: "cron:collide",
			StartedAt:   now,
		}
	}
	var wgB sync.WaitGroup
	for range collide {
		wgB.Go(func() {
			id, err := svc.Dispatch(context.Background(), collideInput(), task)
			switch {
			case err == nil:
				atomic.AddInt64(&collideSuccess, 1)
				successIDs.Store(id, struct{}{})
			case errors.Is(err, store.ErrActiveRunExists):
				atomic.AddInt64(&collideConflict, 1)
			default:
				t.Errorf("colliding dispatch unexpected error: %v", err)
			}
		})
	}
	wgB.Wait()

	if collideSuccess != 1 {
		t.Fatalf("colliding dispatch: want exactly 1 success, got %d (conflicts=%d)", collideSuccess, collideConflict)
	}
	if collideConflict != collide-1 {
		t.Fatalf("colliding dispatch: want %d ErrActiveRunExists, got %d", collide-1, collideConflict)
	}

	// --- Phase C: complete every run, single Reap, assert exactly-once ---
	var runIDs []int64
	successIDs.Range(func(k, _ any) bool {
		runIDs = append(runIDs, k.(int64))
		return true
	})
	wantRuns := distinct + 1
	if len(runIDs) != wantRuns {
		t.Fatalf("expected %d successful runs, got %d", wantRuns, len(runIDs))
	}

	for _, id := range runIDs {
		run, err := db.GetRun(context.Background(), id)
		if err != nil {
			t.Fatalf("GetRun(%d): %v", id, err)
		}
		if run.State != model.RunRunning {
			t.Fatalf("run %d state = %q, want running before reap", id, run.State)
		}
		if err := os.WriteFile(run.ExitPath, []byte("0\n"), 0o644); err != nil {
			t.Fatalf("write exit file for run %d: %v", id, err)
		}
	}

	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("final Reap: %v", err)
	}

	for _, id := range runIDs {
		run, err := db.GetRun(context.Background(), id)
		if err != nil {
			t.Fatalf("GetRun(%d): %v", id, err)
		}
		if run.State != model.RunCompleted {
			t.Fatalf("run %d final state = %q, want completed", id, run.State)
		}
	}

	// Exactly one started and exactly one terminal bus event per run.
	assertExactlyOncePerRun(t, "watch.task.started", sink.byType(bus.TypeWatchTaskStarted), runIDs)
	assertExactlyOncePerRun(t, "watch.task.completed", sink.byType(bus.TypeWatchTaskCompleted), runIDs)
	if failed := sink.byType(bus.TypeWatchTaskFailed); len(failed) != 0 {
		t.Fatalf("expected 0 watch.task.failed events, got %d", len(failed))
	}
}

// assertExactlyOncePerRun fails the test unless evs contains exactly one event
// per run id in wantRuns and no events for any other run.
func assertExactlyOncePerRun(t *testing.T, label string, evs []bus.Event, wantRuns []int64) {
	t.Helper()
	seen := make(map[int64]int, len(evs))
	for i := range evs {
		var d bus.WatchTaskData
		if err := json.Unmarshal(evs[i].Data, &d); err != nil {
			t.Fatalf("%s: unmarshal watch task data: %v", label, err)
		}
		seen[d.RunID]++
	}
	for _, id := range wantRuns {
		if seen[id] != 1 {
			t.Errorf("%s: run %d emitted %d times, want exactly 1", label, id, seen[id])
		}
	}
	if len(seen) != len(wantRuns) {
		t.Errorf("%s: events for %d distinct runs, want %d", label, len(seen), len(wantRuns))
	}
}
