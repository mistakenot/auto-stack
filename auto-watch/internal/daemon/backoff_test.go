package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/runner"
	"github.com/mistakenot/auto-watch/internal/store"
)

// startupFailBackend models a task that fails to start (e.g. backend launch
// error). startWorker returns the error and Dispatch marks the run terminal
// immediately, so the run is never seen by Reap.
type startupFailBackend struct{}

func (startupFailBackend) Start(context.Context, *runner.StartSpec) (runner.Handle, error) {
	return runner.Handle{}, errors.New("backend start failed")
}
func (startupFailBackend) Kill(context.Context, runner.Handle) error { return nil }
func (startupFailBackend) SessionExists(context.Context, string) (bool, error) {
	return false, nil
}

// failingBackend models a task that always exits non-zero, the same way
// fakeBackend models a passing task (exit 0). Reap reads the exit-code file and
// transitions the run to FAILED.
type failingBackend struct{}

func (failingBackend) Start(_ context.Context, spec *runner.StartSpec) (runner.Handle, error) {
	if err := os.WriteFile(spec.OutputPath, []byte("boom\n"), 0o644); err != nil {
		return runner.Handle{}, err
	}
	if err := os.WriteFile(spec.ExitPath, []byte("1\n"), 0o644); err != nil {
		return runner.Handle{}, err
	}
	return runner.Handle{SessionName: spec.SessionName, ExitPath: spec.ExitPath, OutputPath: spec.OutputPath}, nil
}

func (failingBackend) Kill(context.Context, runner.Handle) error { return nil }
func (failingBackend) SessionExists(context.Context, string) (bool, error) {
	return false, nil
}

// exitCodeBackend writes whatever exit code is currently set. The daemon runs
// the tick path synchronously (no worker goroutine), so the test may flip code
// between ticks without a data race.
type exitCodeBackend struct{ code int }

func (b *exitCodeBackend) Start(_ context.Context, spec *runner.StartSpec) (runner.Handle, error) {
	if err := os.WriteFile(spec.OutputPath, []byte("out\n"), 0o644); err != nil {
		return runner.Handle{}, err
	}
	if err := os.WriteFile(spec.ExitPath, fmt.Appendf(nil, "%d\n", b.code), 0o644); err != nil {
		return runner.Handle{}, err
	}
	return runner.Handle{SessionName: spec.SessionName, ExitPath: spec.ExitPath, OutputPath: spec.OutputPath}, nil
}

func (b *exitCodeBackend) Kill(context.Context, runner.Handle) error { return nil }
func (b *exitCodeBackend) SessionExists(context.Context, string) (bool, error) {
	return false, nil
}

// setupBackoffProject registers a single project with one bash task fired by an
// every-minute cron trigger, writing configs directly (no git needed for a bash
// task without a branch-change guard). Returns the opened store.
func setupBackoffProject(t *testing.T) *store.Store {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := t.TempDir()

	autoDir := filepath.Join(home, ".auto")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := fmt.Sprintf(`{"projects":[{"id":"demo","path":%q}]}`, repoRoot)
	if err := os.WriteFile(filepath.Join(autoDir, "projects.json"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(repoRoot, ".auto", "watch")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"id":"demo",` +
		`"tasks":{"failing":{"type":"bash","command":"exit 1"}},` +
		`"triggers":{"every-minute":{"type":"cron","when":"* * * * *","tasks":["failing"]}}}`
	if err := os.WriteFile(filepath.Join(projectDir, "project.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := store.Open(filepath.Join(autoDir, "watch", "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	return db
}

func countRuns(t *testing.T, db *store.Store) int {
	t.Helper()
	runs, err := db.ListRunsByStates(context.Background(),
		model.RunPending, model.RunRunning, model.RunCompleted, model.RunFailed)
	if err != nil {
		t.Fatalf("ListRunsByStates failed: %v", err)
	}
	return len(runs)
}

// TestFailureBackoffExponential (AC-5): a task whose dispatches always fail must
// be retried on an exponential schedule (1, 2, 4, 8, 16, 32 min gaps) and skipped
// within each backoff window, with the window capped at 64 minutes. With an
// every-minute cron firing from a failure at minute 0, dispatch is permitted only
// at minutes 0, 1, 3, 7, 15, 31, 63, 127, 191 — the final two gaps of 64 confirm
// the cap (an uncapped 2^7 window would have pushed the 8th dispatch to minute 255).
func TestFailureBackoffExponential(t *testing.T) {
	db := setupBackoffProject(t)
	ctx := context.Background()

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var clock time.Time
	service := daemon.New(db, failingBackend{}, nil, func() time.Time { return clock }, nil)

	var dispatched []int
	prev := 0
	for m := range 192 { // minutes 0..191 inclusive
		clock = base.Add(time.Duration(m) * time.Minute)
		if err := service.Tick(ctx); err != nil {
			t.Fatalf("tick at minute %d failed: %v", m, err)
		}
		service.WaitWorkers()
		if err := service.Reap(ctx); err != nil {
			t.Fatalf("reap at minute %d failed: %v", m, err)
		}
		if total := countRuns(t, db); total > prev {
			dispatched = append(dispatched, m)
			prev = total
		}
	}

	want := []int{0, 1, 3, 7, 15, 31, 63, 127, 191}
	if !reflect.DeepEqual(dispatched, want) {
		t.Fatalf("dispatch minutes mismatch:\n got %v\nwant %v", dispatched, want)
	}
}

// TestStartupFailureBacksOff: a task that fails during startup (Backend.Start
// errors) is marked terminal inside Dispatch and never reaches Reap. Backoff
// must still be recorded so a task that is broken before it starts backs off
// exponentially instead of being re-dispatched on every tick.
func TestStartupFailureBacksOff(t *testing.T) {
	db := setupBackoffProject(t)
	ctx := context.Background()

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var clock time.Time
	service := daemon.New(db, startupFailBackend{}, nil, func() time.Time { return clock }, nil)

	var dispatched []int
	prev := 0
	for m := range 8 { // minutes 0..7 inclusive
		clock = base.Add(time.Duration(m) * time.Minute)
		if err := service.Tick(ctx); err != nil {
			t.Fatalf("tick at minute %d failed: %v", m, err)
		}
		service.WaitWorkers()
		if total := countRuns(t, db); total > prev {
			dispatched = append(dispatched, m)
			prev = total
		}
	}

	// Without recording startup failures in the backoff state, a new run would be
	// reserved every minute (0..7). With the fix the schedule is exponential.
	want := []int{0, 1, 3, 7}
	if !reflect.DeepEqual(dispatched, want) {
		t.Fatalf("startup-failure dispatch minutes mismatch:\n got %v\nwant %v", dispatched, want)
	}
}

// TestBackoffResetsOnSuccess: after several consecutive failures, a single
// success must reset the streak so the next failure starts a fresh (short) 1-minute
// window rather than continuing the long backoff. The distinguishing observation
// is that a dispatch occurs the minute immediately after the post-success failure;
// without a reset, the stale failure count would suppress it.
func TestBackoffResetsOnSuccess(t *testing.T) {
	db := setupBackoffProject(t)
	ctx := context.Background()

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	backend := &exitCodeBackend{code: 1}
	var clock time.Time
	service := daemon.New(db, backend, nil, func() time.Time { return clock }, nil)

	step := func(m int) {
		clock = base.Add(time.Duration(m) * time.Minute)
		if err := service.Tick(ctx); err != nil {
			t.Fatalf("tick at minute %d failed: %v", m, err)
		}
		service.WaitWorkers()
		if err := service.Reap(ctx); err != nil {
			t.Fatalf("reap at minute %d failed: %v", m, err)
		}
	}

	// Three failures: dispatch at 0 (fail#1, next 1), 1 (fail#2, next 3), skip 2,
	// 3 (fail#3, next 7).
	step(0)
	step(1)
	step(2)
	step(3)

	// Switch to success and wait out the window; the run at minute 7 succeeds and
	// resets the streak.
	backend.code = 0
	step(4)
	step(5)
	step(6)
	step(7)

	// Back to failing. With the reset, minute 8 dispatches (no backoff), fails as
	// fail#1 (next eligible minute 9), so minute 9 dispatches again immediately.
	backend.code = 1
	step(8)
	runsAfter8 := countRuns(t, db)
	step(9)
	runsAfter9 := countRuns(t, db)

	if runsAfter9 != runsAfter8+1 {
		t.Fatalf("expected a fresh dispatch at minute 9 after reset (runs %d -> %d); "+
			"without reset the stale streak would have suppressed it", runsAfter8, runsAfter9)
	}

	counts, err := db.RecentRunCounts(ctx, base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("RecentRunCounts failed: %v", err)
	}
	if counts["completed"] != 1 {
		t.Fatalf("expected exactly 1 completed run, got %+v", counts)
	}
	if counts["failed"] != 5 {
		t.Fatalf("expected 5 failed runs (minutes 0,1,3,8,9), got %+v", counts)
	}
}
