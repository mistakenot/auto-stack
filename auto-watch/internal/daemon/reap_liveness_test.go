package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/backend"
	"github.com/mistakenot/auto-shared/backend/backendtest"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
)

// testClock is a mutable injectable clock: daemon.New captures its now method,
// so advancing it is time travel without a sleep. Every liveness and deadline
// assertion below steps this clock rather than waiting.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time          { return c.t }
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *testClock {
	return &testClock{t: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)}
}

// dispatchBash reserves and starts a bash run through the given fake backend and
// returns its id. The run is left running (the fake writes no exit file) with a
// deadline of started_at + the TaskDef timeout (or the 2h default).
func dispatchBash(t *testing.T, svc *daemon.Service, projectPath string, clk *testClock, task model.TaskDef) int64 {
	t.Helper()
	runID, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, clk.now()), task)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	return runID
}

func getRun(t *testing.T, db *store.Store, id int64) model.RunRecord {
	t.Helper()
	run, err := db.GetRun(context.Background(), id)
	if err != nil {
		t.Fatalf("GetRun(%d): %v", id, err)
	}
	return run
}

// TestReapLostPastGraceFailsSessionLost (AC-3): a run the backend reports lost,
// at least the grace period after it started, is failed session_lost, and its
// Trigger is unblocked so the next fire dispatches again (no dedup skip).
func TestReapLostPastGraceFailsSessionLost(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	clk := newClock()
	fake := &backendtest.Fake{}
	svc := daemon.New(db, backend.NewRegistry(fake), nil, clk.now, nil)
	ctx := context.Background()

	runID := dispatchBash(t, svc, projectPath, clk, model.TaskDef{Type: "bash", Command: "sleep 1"})
	fake.SetLost()

	clk.advance(3 * time.Minute) // past the 2-minute grace, well under the 2h deadline
	if err := svc.Reap(ctx); err != nil {
		t.Fatalf("Reap: %v", err)
	}

	run := getRun(t, db, runID)
	if run.State != model.RunFailed {
		t.Fatalf("state = %q, want failed", run.State)
	}
	if run.FailureReason != "session_lost" {
		t.Fatalf("failure_reason = %q, want session_lost", run.FailureReason)
	}
	if fake.StopCount() != 0 {
		t.Fatalf("session_lost must not call Stop (there is no session), got %d", fake.StopCount())
	}

	// The next fire of that Trigger must dispatch again: with the lost run now
	// terminal, the resource key is free (no dedup skip).
	if _, err := svc.Dispatch(ctx, bashReserveInput(projectPath, clk.now()), model.TaskDef{Type: "bash", Command: "sleep 1"}); err != nil {
		t.Fatalf("re-dispatch after session_lost must not be deduped: %v", err)
	}
}

// TestReapLostWithinGraceStaysRunning is the negative twin: a lost run younger
// than the grace period is left running, so a run that just started (or a
// briefly slow manager) is not reaped by mistake.
func TestReapLostWithinGraceStaysRunning(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	clk := newClock()
	fake := &backendtest.Fake{}
	svc := daemon.New(db, backend.NewRegistry(fake), nil, clk.now, nil)

	runID := dispatchBash(t, svc, projectPath, clk, model.TaskDef{Type: "bash", Command: "sleep 1"})
	fake.SetLost()

	clk.advance(1 * time.Minute) // still inside the 2-minute grace
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if run := getRun(t, db, runID); run.State != model.RunRunning {
		t.Fatalf("state = %q, want running (lost but within grace)", run.State)
	}
}

// TestReapRunningPastGraceStaysRunning is the mandatory healthy twin: a run the
// backend still reports running survives many reaps past the grace period. A
// passing reap test can otherwise be asserting a false-reap bug.
func TestReapRunningPastGraceStaysRunning(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	clk := newClock()
	fake := &backendtest.Fake{} // running: no exit file, not lost
	svc := daemon.New(db, backend.NewRegistry(fake), nil, clk.now, nil)

	runID := dispatchBash(t, svc, projectPath, clk, model.TaskDef{Type: "bash", Command: "sleep 1"})

	for i := range 3 {
		clk.advance(5 * time.Minute) // well past the grace, still under the 2h deadline
		if err := svc.Reap(context.Background()); err != nil {
			t.Fatalf("Reap #%d: %v", i, err)
		}
		if run := getRun(t, db, runID); run.State != model.RunRunning {
			t.Fatalf("reap #%d: state = %q, want running (healthy run must not be reaped)", i, run.State)
		}
	}
	if fake.StopCount() != 0 {
		t.Fatalf("a healthy under-deadline run must not be stopped, Stop called %d times", fake.StopCount())
	}
}

// TestReapPastDeadlineStopsAndTimesOut (AC-4): a run still running past its
// deadline has Stop called once and is failed timed_out.
func TestReapPastDeadlineStopsAndTimesOut(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	clk := newClock()
	fake := &backendtest.Fake{} // stays running
	svc := daemon.New(db, backend.NewRegistry(fake), nil, clk.now, nil)

	// A 30m timeout fixes the deadline at started_at + 30m.
	runID := dispatchBash(t, svc, projectPath, clk, model.TaskDef{Type: "bash", Command: "sleep 10000", Timeout: "30m"})

	clk.advance(31 * time.Minute)
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap: %v", err)
	}

	run := getRun(t, db, runID)
	if run.State != model.RunFailed {
		t.Fatalf("state = %q, want failed", run.State)
	}
	if run.FailureReason != "timed_out" {
		t.Fatalf("failure_reason = %q, want timed_out", run.FailureReason)
	}
	if fake.StopCount() != 1 {
		t.Fatalf("Stop count = %d, want exactly 1", fake.StopCount())
	}
}

// TestReapBeforeDeadlineUntouched is the negative twin: one minute before the
// deadline the run is left alone and Stop is not called.
func TestReapBeforeDeadlineUntouched(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	clk := newClock()
	fake := &backendtest.Fake{}
	svc := daemon.New(db, backend.NewRegistry(fake), nil, clk.now, nil)

	runID := dispatchBash(t, svc, projectPath, clk, model.TaskDef{Type: "bash", Command: "sleep 10000", Timeout: "30m"})

	clk.advance(29 * time.Minute) // deadline - 1m
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if run := getRun(t, db, runID); run.State != model.RunRunning {
		t.Fatalf("state = %q, want running (still under deadline)", run.State)
	}
	if fake.StopCount() != 0 {
		t.Fatalf("Stop called %d times before the deadline, want 0", fake.StopCount())
	}
}

// startRunningRow crafts a running run row directly in the store so a test can
// control its deadline — in particular a legacy row with a NULL deadline_at
// (zero RunStartUpdate.DeadlineAt). runDir must exist so Reap does not skip it.
func startRunningRow(t *testing.T, db *store.Store, in *store.ReserveRunInput, runDir string, deadline time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := db.ReserveRun(ctx, in)
	if err != nil {
		t.Fatalf("ReserveRun: %v", err)
	}
	if err := db.UpdateRunStarted(ctx, id, &store.RunStartUpdate{
		SessionName:   "sess",
		RuntimeDir:    runDir,
		OutputPath:    filepath.Join(runDir, "output.log"),
		ExitPath:      filepath.Join(runDir, "exit-code"),
		Backend:       "tmux",
		BackendTarget: "sess",
		DeadlineAt:    deadline, // zero => NULL deadline_at (legacy row)
	}); err != nil {
		t.Fatalf("UpdateRunStarted: %v", err)
	}
	return id
}

// TestReapLegacyNullDeadlineFallsBackTo2h (AC-3/AC-4/AC-15): a pre-v3 row with a
// NULL deadline_at is enforced against started_at + the 2h default.
func TestReapLegacyNullDeadlineFallsBackTo2h(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	runDir := t.TempDir()
	clk := newClock()
	fake := &backendtest.Fake{} // stays running
	svc := daemon.New(db, backend.NewRegistry(fake), nil, clk.now, nil)

	// started_at = clk.now(), deadline_at NULL (zero time).
	runID := startRunningRow(t, db, bashReserveInput(projectPath, clk.now()), runDir, time.Time{})

	// Just under the 2h fallback: untouched.
	clk.advance(2*time.Hour - time.Minute)
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap (under fallback): %v", err)
	}
	if run := getRun(t, db, runID); run.State != model.RunRunning {
		t.Fatalf("under the 2h fallback: state = %q, want running", run.State)
	}

	// Past the 2h fallback: stopped and timed out.
	clk.advance(2 * time.Minute)
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap (past fallback): %v", err)
	}
	run := getRun(t, db, runID)
	if run.State != model.RunFailed || run.FailureReason != "timed_out" {
		t.Fatalf("past the 2h fallback: state=%q reason=%q, want failed/timed_out", run.State, run.FailureReason)
	}
	if fake.StopCount() != 1 {
		t.Fatalf("Stop count = %d, want 1", fake.StopCount())
	}
}

// TestReapStatusErrorPastDeadlineStopsEachTick (AC-4): when Status errors on
// every tick past the deadline and Stop cannot be confirmed, Stop is attempted
// on each tick and the run is kept running (never overlapped), with a single
// task_stop_failed warning.
func TestReapStatusErrorPastDeadlineStopsEachTick(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	clk := newClock()
	fake := &backendtest.Fake{}
	fake.SetStatusErr(errors.New("tmux server wedged"))
	fake.SetStopErr(errors.New("stop could not be confirmed"))
	svc := daemon.New(db, backend.NewRegistry(fake), nil, clk.now, nil)

	runID := dispatchBash(t, svc, projectPath, clk, model.TaskDef{Type: "bash", Command: "sleep 10000", Timeout: "30m"})

	clk.advance(31 * time.Minute)
	for i := range 3 {
		if err := svc.Reap(context.Background()); err != nil {
			t.Fatalf("Reap #%d: %v", i, err)
		}
		if run := getRun(t, db, runID); run.State != model.RunRunning {
			t.Fatalf("reap #%d: state = %q, want running while Stop is unconfirmed", i, run.State)
		}
	}
	if fake.StopCount() != 3 {
		t.Fatalf("Stop count = %d, want 3 (one per tick past the deadline)", fake.StopCount())
	}
	if fake.StartCount() != 1 {
		t.Fatalf("Start count = %d, want 1 (Reap must never start an overlapping run)", fake.StartCount())
	}
	if n, err := db.CountRunEventsByType(context.Background(), runID, "task_stop_failed"); err != nil {
		t.Fatalf("count task_stop_failed: %v", err)
	} else if n != 1 {
		t.Fatalf("task_stop_failed events = %d, want exactly 1 per run", n)
	}
}

// TestReapStopFailsThenSucceedsTimesOutOnce (AC-4): Stop fails twice then
// succeeds. The run stays running with no second Start while Stop is
// unconfirmed, then fails timed_out exactly once on the tick Stop succeeds.
func TestReapStopFailsThenSucceedsTimesOutOnce(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	clk := newClock()
	fake := &backendtest.Fake{} // stays running
	fake.SetStopErr(errors.New("stop unconfirmed"))
	svc := daemon.New(db, backend.NewRegistry(fake), nil, clk.now, nil)

	runID := dispatchBash(t, svc, projectPath, clk, model.TaskDef{Type: "bash", Command: "sleep 10000", Timeout: "30m"})

	clk.advance(31 * time.Minute)
	for i := range 2 {
		if err := svc.Reap(context.Background()); err != nil {
			t.Fatalf("Reap (stop failing) #%d: %v", i, err)
		}
		if run := getRun(t, db, runID); run.State != model.RunRunning {
			t.Fatalf("reap #%d: state = %q, want running while Stop fails", i, run.State)
		}
		if fake.StartCount() != 1 {
			t.Fatalf("reap #%d: Start count = %d, want 1 (no overlapping start)", i, fake.StartCount())
		}
	}

	fake.SetStopErr(nil) // the server recovers
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap (stop succeeds): %v", err)
	}
	run := getRun(t, db, runID)
	if run.State != model.RunFailed || run.FailureReason != "timed_out" {
		t.Fatalf("state=%q reason=%q, want failed/timed_out once Stop succeeds", run.State, run.FailureReason)
	}
	if fake.StartCount() != 1 {
		t.Fatalf("Start count = %d, want 1 (Reap never starts a run)", fake.StartCount())
	}
	if n, err := db.CountRunEventsByType(context.Background(), runID, "task_failed"); err != nil {
		t.Fatalf("count task_failed: %v", err)
	} else if n != 1 {
		t.Fatalf("task_failed events = %d, want exactly 1 (timed out once)", n)
	}
}

// TestReapMalformedExitFileIsExitUnrecorded (AC covering exit_unrecorded): an
// empty/malformed exit file reads as exited with an unknown code and the run is
// failed exit_unrecorded on that tick — not left running, not a Status error.
func TestReapMalformedExitFileIsExitUnrecorded(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	clk := newClock()
	fake := &backendtest.Fake{}
	svc := daemon.New(db, backend.NewRegistry(fake), nil, clk.now, nil)
	ctx := context.Background()

	runID := dispatchBash(t, svc, projectPath, clk, model.TaskDef{Type: "bash", Command: "sleep 1"})
	run := getRun(t, db, runID)
	if err := os.WriteFile(run.ExitPath, []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatalf("write malformed exit file: %v", err)
	}

	if err := svc.Reap(ctx); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	got := getRun(t, db, runID)
	if got.State != model.RunFailed {
		t.Fatalf("state = %q, want failed", got.State)
	}
	if got.ExitCode != nil {
		t.Fatalf("exit_code = %d, want nil", *got.ExitCode)
	}
	if got.FailureReason != "exit_unrecorded" {
		t.Fatalf("failure_reason = %q, want exit_unrecorded", got.FailureReason)
	}
}

// TestReapHealthyNoExitFileStaysRunning is the exit_unrecorded twin: a healthy
// run that has not written an exit file yet stays running.
func TestReapHealthyNoExitFileStaysRunning(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	clk := newClock()
	fake := &backendtest.Fake{}
	svc := daemon.New(db, backend.NewRegistry(fake), nil, clk.now, nil)

	runID := dispatchBash(t, svc, projectPath, clk, model.TaskDef{Type: "bash", Command: "sleep 1"})
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if run := getRun(t, db, runID); run.State != model.RunRunning {
		t.Fatalf("state = %q, want running (no exit file yet)", run.State)
	}
}
