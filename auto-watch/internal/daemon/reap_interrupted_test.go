package daemon_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
)

// The launch wrapper records a run's exit status with
//
//	printf '%s\n' "$code" > "$EXIT_FILE"
//
// The `>` redirect creates and truncates the file before printf writes into it,
// so anything that interrupts the wrapper in that window leaves the file present
// but empty. Reap runs on every Tick and the run stays RunRunning until Reap
// gets past it, so treating that as a fatal error wedges the daemon in a restart
// loop it cannot escape — it dies, restarts, reads the same byte, dies again.
//
// This happened in production: one 0-byte exit-code file took the daemon down
// for three days across 33,219 restarts.
func TestReapSurvivesUnrecordedExitCode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contents string
	}{
		{"empty", ""},
		{"whitespace only", "  \n"},
		{"partial write", "\x00"},
		{"not a number", "killed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestStore(t)
			projectPath := t.TempDir()
			now := time.Date(2026, 9, 7, 0, 1, 21, 0, time.UTC)
			svc := daemon.New(db, &recordingBackend{}, nil, func() time.Time { return now }, nil)
			ctx := context.Background()

			runID, err := svc.Dispatch(ctx, bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "auto etl run"})
			if err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			run, err := db.GetRun(ctx, runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if err := os.WriteFile(run.ExitPath, []byte(tc.contents), 0o644); err != nil {
				t.Fatalf("write exit file: %v", err)
			}

			// The regression: this must not return an error.
			if err := svc.Reap(ctx); err != nil {
				t.Fatalf("Reap returned an error on an unrecorded exit code (this is the crash-loop): %v", err)
			}

			got, err := db.GetRun(ctx, runID)
			if err != nil {
				t.Fatalf("GetRun after reap: %v", err)
			}
			if got.State != model.RunFailed {
				t.Errorf("run state = %v, want %v — an interrupted run must reach a terminal state, "+
					"otherwise it stays RunRunning and holds its resource key forever", got.State, model.RunFailed)
			}
			if got.ExitCode != nil {
				t.Errorf("exit code = %d, want nil — no code was recorded, so none should be reported", *got.ExitCode)
			}
		})
	}
}

// Reap must also be able to run again afterwards. A fix that marks the run
// terminal but still errors, or that leaves it RunRunning, would pass the first
// assertion above and still wedge the daemon on the next Tick.
func TestReapRepeatsCleanlyAfterUnrecordedExitCode(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 9, 7, 0, 1, 21, 0, time.UTC)
	svc := daemon.New(db, &recordingBackend{}, nil, func() time.Time { return now }, nil)
	ctx := context.Background()

	runID, err := svc.Dispatch(ctx, bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "auto etl run"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	run, err := db.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if err := os.WriteFile(run.ExitPath, nil, 0o644); err != nil {
		t.Fatalf("write exit file: %v", err)
	}

	for i := 1; i <= 3; i++ {
		if err := svc.Reap(ctx); err != nil {
			t.Fatalf("Reap #%d: %v", i, err)
		}
	}

	got, err := db.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.State != model.RunFailed {
		t.Errorf("run state after repeated reaps = %v, want %v", got.State, model.RunFailed)
	}
}

// A well-formed exit code must keep its existing meaning — the fix widens what
// Reap tolerates, it does not change how a recorded code is reported.
func TestReapStillReportsRecordedExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		file  string
		want  int
		state model.RunState
	}{
		{"success", "0\n", 0, model.RunCompleted},
		{"failure", "137\n", 137, model.RunFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestStore(t)
			projectPath := t.TempDir()
			now := time.Date(2026, 9, 7, 0, 1, 21, 0, time.UTC)
			svc := daemon.New(db, &recordingBackend{}, nil, func() time.Time { return now }, nil)
			ctx := context.Background()

			runID, err := svc.Dispatch(ctx, bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "echo hi"})
			if err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			run, err := db.GetRun(ctx, runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if err := os.WriteFile(run.ExitPath, []byte(tc.file), 0o644); err != nil {
				t.Fatalf("write exit file: %v", err)
			}
			if err := svc.Reap(ctx); err != nil {
				t.Fatalf("Reap: %v", err)
			}

			got, err := db.GetRun(ctx, runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if got.State != tc.state {
				t.Errorf("state = %v, want %v", got.State, tc.state)
			}
			if got.ExitCode == nil || *got.ExitCode != tc.want {
				t.Errorf("exit code = %v, want %d", got.ExitCode, tc.want)
			}
		})
	}
}
