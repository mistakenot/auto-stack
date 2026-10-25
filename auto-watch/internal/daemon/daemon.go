package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/mistakenot/auto-shared/backend"
	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/gitx"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/runner"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/textout"
)

var ErrDaemonAlreadyRunning = errors.New("daemon lock is already held")

// defaultRetentionDays is the hardcoded retention window for events and
// terminal runs (Decision D-1). The CLI overrides it via SetRetentionDays.
const defaultRetentionDays = 7

// defaultRunTimeout is the deadline window for a run whose TaskDef sets no
// timeout (Open Question: 2h, decided with the user). The deadline is fixed to
// started_at + timeout when the run starts. Legacy rows with a null deadline_at
// fall back to started_at + defaultRunTimeout.
const defaultRunTimeout = 2 * time.Hour

// lostGrace is how long a run with no recorded exit and no live session is given
// before it is reaped as session_lost, so a run that just started (or a briefly
// slow backend manager) is not reaped by mistake.
const lostGrace = 2 * time.Minute

// Failure reasons stamped on a failed run's failure_reason column and carried on
// the task_failed Event and the watch.task.failed bus payload. reasonStartFailed
// is stamped by failRun (start/dispatch failures); the others by Reap.
const (
	reasonExitNonzero    = "exit_nonzero"
	reasonExitUnrecorded = "exit_unrecorded"
	reasonSessionLost    = "session_lost"
	reasonTimedOut       = "timed_out"
	reasonStartFailed    = "start_failed"
)

type Service struct {
	Store    *store.Store
	Backends *backend.Registry
	Output   io.Writer
	Now      func() time.Time

	dispatchMu    sync.Mutex
	hub           *bus.Hub
	workerWG      sync.WaitGroup
	retentionDays int

	// backoff tracks per-(Project, TaskDef) consecutive failures so a broken
	// task does not redispatch every tick. Keyed by projectID:taskID. Guarded by
	// dispatchMu (Reap updates it; the dispatch paths read it).
	backoff map[string]*backoffState
	// configWarningsSeen dedups repeated config_warning / validation events so the
	// same warning is logged once rather than every tick. Guarded by dispatchMu.
	configWarningsSeen map[string]bool
}

// backoffState records the failure streak for a single projectID:taskID key.
type backoffState struct {
	consecutiveFailures int
	lastFailure         time.Time
}

func backoffKey(projectID, taskID string) string {
	return projectID + ":" + taskID
}

// configError marks a tickProject failure as a configuration fault (unloadable
// config or an invalid project id) rather than a system fault (git, store,
// dispatch). Tick logs the former as config_warning and the latter as
// system_warning, so an operator can tell a bad config apart from a transient
// fault instead of every failure being blamed on "invalid config".
type configError struct{ err error }

func (e *configError) Error() string { return e.err.Error() }
func (e *configError) Unwrap() error { return e.err }

// backoffWindow returns the suppression window for the given consecutive-failure
// count: min(1min * 2^(failures-1), 64min), i.e. 1, 2, 4, 8, 16, 32, 64 minutes.
// A non-positive failure count yields a zero window (dispatch allowed).
func backoffWindow(failures int) time.Duration {
	if failures < 1 {
		return 0
	}
	shift := failures - 1
	const maxShift = 6 // 2^6 = 64 minutes (the cap)
	if shift > maxShift {
		shift = maxShift
	}
	return time.Minute * time.Duration(int64(1)<<uint(shift))
}

type Lock struct {
	file *os.File
}

func New(db *store.Store, backends *backend.Registry, output io.Writer, now func() time.Time, hub *bus.Hub) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		Store:              db,
		Backends:           backends,
		Output:             output,
		Now:                now,
		hub:                hub,
		retentionDays:      defaultRetentionDays,
		backoff:            map[string]*backoffState{},
		configWarningsSeen: map[string]bool{},
	}
}

// SetRetentionDays overrides the default retention window for events and
// terminal runs. Non-positive values are ignored, keeping the default.
func (s *Service) SetRetentionDays(days int) {
	if days > 0 {
		s.retentionDays = days
	}
}

// Dispatch reserves a run for the given input and starts its worker. It is the
// shared primitive used by both the trigger tick path and the task.run RPC, so
// dedup, event logging, and worker startup behave identically. Returns
// store.ErrActiveRunExists when a duplicate active run already holds the
// resource key.
func (s *Service) Dispatch(ctx context.Context, in *store.ReserveRunInput, task model.TaskDef) (int64, error) {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()

	runID, err := s.Store.ReserveRun(ctx, in)
	if err != nil {
		return 0, err
	}
	_ = s.logEvent(ctx, &store.EventInput{
		Timestamp: s.Now(),
		Level:     "info",
		EventType: "task_reserved",
		ProjectID: in.ProjectID,
		TriggerID: in.TriggerID,
		TaskID:    in.TaskID,
		RunID:     &runID,
		Message:   fmt.Sprintf("reserved run %d", runID),
		Metadata:  map[string]any{"resource_key": in.ResourceKey},
	})
	if err := s.startWorker(ctx, runID, task); err != nil {
		run, loadErr := s.Store.GetRun(ctx, runID)
		if loadErr == nil && run.State == model.RunPending {
			_ = s.failRun(ctx, &run, "", err)
		}
		_ = s.logEvent(ctx, &store.EventInput{
			Timestamp: s.Now(),
			Level:     "error",
			EventType: "system_warning",
			ProjectID: in.ProjectID,
			TriggerID: in.TriggerID,
			TaskID:    in.TaskID,
			RunID:     &runID,
			Message:   "worker startup failed",
			Metadata:  map[string]any{"error": err.Error()},
		})
		return runID, err
	}
	return runID, nil
}

// Cancel transitions a run to failed, killing its session if running. It is
// idempotent: a run already in a terminal state returns its current state with
// no side effects.
func (s *Service) Cancel(ctx context.Context, runID int64) (model.RunState, error) {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()

	run, err := s.Store.GetRun(ctx, runID)
	if err != nil {
		return "", err
	}

	switch run.State {
	case model.RunCompleted, model.RunFailed:
		return run.State, nil

	case model.RunRunning:
		if run.BackendTarget != "" {
			if b, ok := s.Backends.Get(run.Backend); ok {
				_ = b.Stop(ctx, backend.Binding{Manager: run.Backend, Target: run.BackendTarget})
			}
		}

	case model.RunPending:
		// fail without kill
	}

	now := s.Now()
	msg := "cancelled via task.cancel"
	updated, err := s.Store.MarkRunTerminal(ctx, run.ID, model.RunFailed, nil, now, msg)
	if err != nil {
		return "", err
	}
	if !updated {
		// Another writer finished this run between our read and the conditional
		// update; report its current state without emitting a duplicate event.
		fresh, gErr := s.Store.GetRun(ctx, run.ID)
		if gErr != nil {
			return "", gErr
		}
		return fresh.State, nil
	}
	_ = s.logEvent(ctx, &store.EventInput{
		Timestamp: now,
		Level:     "error",
		EventType: "task_failed",
		ProjectID: run.ProjectID,
		TriggerID: run.TriggerID,
		TaskID:    run.TaskID,
		RunID:     &run.ID,
		Message:   msg,
		Metadata:  map[string]any{"resource_key": run.ResourceKey},
	})
	s.emitWatchTask(ctx, bus.TypeWatchTaskFailed, &run, nil, msg, "")

	if run.WorktreePath != "" {
		_ = s.removeWorktree(ctx, &run, "cancel_cleanup")
	}

	return model.RunFailed, nil
}

// emitWatchTask broadcasts a watch.task.* data-plane event for the given run.
// These events are always-on (not gated by --ctl-events); a nil hub is a no-op.
func (s *Service) emitWatchTask(_ context.Context, typ string, run *model.RunRecord, exitCode *int, message, reason string) {
	if s.hub == nil {
		return
	}
	ev, err := bus.NewWatchTask(typ, bus.RunProvenance{
		Project:  run.ProjectID,
		Branch:   run.Branch,
		Worktree: run.WorktreePath,
	}, bus.WatchTaskData{
		TaskID:      run.TaskID,
		RunID:       run.ID,
		TriggerID:   run.TriggerID,
		SessionName: run.SessionName,
		ResourceKey: run.ResourceKey,
		Message:     message,
		ExitCode:    exitCode,
		Reason:      reason,
	})
	if err != nil {
		return
	}
	s.hub.Broadcast(ev)
}

func AcquireLock(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // lock file needs owner-only permissions
	if err != nil {
		return nil, fmt.Errorf("open daemon lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrDaemonAlreadyRunning
		}
		return nil, fmt.Errorf("acquire daemon lock: %w", err)
	}
	return &Lock{file: file}, nil
}

func IsLockHeld(path string) (bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // lock file needs owner-only permissions
	if err != nil {
		return false, fmt.Errorf("open daemon lock: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return true, nil
		}
		return false, fmt.Errorf("check daemon lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		return false, fmt.Errorf("release daemon lock probe: %w", err)
	}
	return false, nil
}

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	defer func() { _ = l.file.Close() }()
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}

func (s *Service) Tick(ctx context.Context) error {
	if err := s.Reap(ctx); err != nil {
		return err
	}

	now := s.Now()
	settingsPath, err := config.ProjectsPath()
	if err != nil {
		return err
	}
	settings, err := config.LoadGlobalConfig(settingsPath)
	if err != nil {
		return err
	}
	// Operate on the usable subset (valid id, existing path, no duplicates) and
	// skip stale/malformed entries rather than failing the whole tick — a single
	// bad registry entry must not take the daemon down. Skips are surfaced by the
	// startup doctor "settings" check; we don't re-log them every tick.
	usable, _ := config.UsableGlobalConfig(settings)

	projects := append([]model.ProjectRef(nil), usable.Projects...)
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })

	for _, project := range projects {
		if err := s.tickProject(ctx, now, &project); err != nil {
			// One bad Project must not take the tick down: log it and move on so
			// every other Project still evaluates. Classify by its TRUE category
			// — config_warning only for a validation/config fault, system_warning
			// for a git/dispatch/other fault — instead of blaming every failure on
			// invalid config.
			eventType := "system_warning"
			message := "project tick failed"
			var cfgErr *configError
			if errors.As(err, &cfgErr) {
				eventType = "config_warning"
				message = "project skipped due to invalid config"
			}
			// Dedup so the same warning is logged once, not every tick (Phase 3
			// noise reduction). The event type + error text distinguish warnings.
			if s.warningSeen(eventType + ":" + project.ID + ":" + err.Error()) {
				continue
			}
			if logErr := s.logEvent(ctx, &store.EventInput{
				Timestamp: now,
				Level:     "warn",
				EventType: eventType,
				ProjectID: project.ID,
				Message:   message,
				Metadata:  map[string]any{"error": err.Error()},
			}); logErr != nil {
				return logErr
			}
		}
	}
	if err := s.Clean(ctx, false); err != nil {
		return err
	}
	return nil
}

func (s *Service) WaitWorkers() {
	s.workerWG.Wait()
}

func (s *Service) Reap(ctx context.Context) error {
	now := s.Now()
	running, err := s.Store.ListRunsByStates(ctx, model.RunRunning)
	if err != nil {
		return err
	}
	for i := range running {
		run := &running[i]
		// A run with no runtime dir never recorded a Backend target, so there is
		// nothing for Status to inspect — leave it for the pending/abandoned paths.
		if strings.TrimSpace(run.RuntimeDir) == "" {
			continue
		}
		b, ok := s.Backends.Get(run.Backend)
		if !ok {
			// No adapter registered for this run's backend. Leave the run running
			// rather than inventing an outcome; a production registry always has
			// the run's backend, so this only guards a mis-set row.
			continue
		}
		binding := backend.Binding{Manager: run.Backend, Target: run.BackendTarget}
		// Status owns the completion contract: it reads the exit-code file first
		// and only then asks the manager about liveness. An interrupted/empty
		// exit-code file reads back as StatusExited with a nil code (never an
		// error), so it can never pin a run to running or wedge the daemon in a
		// restart loop (the 33k-restart incident).
		st, statusErr := b.Status(ctx, binding, run.RuntimeDir)

		// The deadline is checked BEFORE, and independently of, Status: a wedged
		// backend that errors on Status must still be stopped at its deadline, so
		// a hung run never overlaps a fresh one for 7 days. Legacy rows with a
		// null deadline fall back to started_at + the 2h default.
		deadline := run.StartedAt.Add(defaultRunTimeout)
		if run.DeadlineAt != nil {
			deadline = *run.DeadlineAt
		}
		if now.After(deadline) {
			if err := s.reapPastDeadline(ctx, run, b, binding, st, statusErr, now); err != nil {
				return err
			}
			continue
		}

		// Under the deadline a Status error leaves the run unchanged: a hung
		// backend manager is not proof a run died. (Phase 3 broadens this
		// containment to every per-run step.)
		if statusErr != nil {
			if err := s.logEvent(ctx, &store.EventInput{
				Timestamp: now,
				Level:     "warn",
				EventType: "system_warning",
				ProjectID: run.ProjectID,
				TriggerID: run.TriggerID,
				TaskID:    run.TaskID,
				RunID:     &run.ID,
				Message:   fmt.Sprintf("run %d status check failed", run.ID),
				Metadata:  map[string]any{"error": statusErr.Error(), "resource_key": run.ResourceKey},
			}); err != nil {
				return err
			}
			continue
		}

		switch st.State {
		case backend.StatusRunning:
			continue
		case backend.StatusLost:
			// A missing run is given a short grace before being declared lost, so a
			// run that just started (or a briefly slow manager) is not reaped by
			// mistake.
			if now.Sub(run.StartedAt) < lostGrace {
				continue
			}
			if err := s.finishRun(ctx, run, now, terminalUpdate{
				state:        model.RunFailed,
				reason:       reasonSessionLost,
				journalMsg:   fmt.Sprintf("run %d lost its session before recording an exit code", run.ID),
				errorMessage: "run session was lost before recording an exit code",
				meta:         map[string]any{"session_name": run.SessionName},
			}); err != nil {
				return err
			}
		case backend.StatusExited:
			// The run has exited; clean up its (remain-on-exit) session, as today's
			// Reap did after reading the exit-code file. Stopping an absent session
			// is not an error.
			_ = b.Stop(ctx, binding)
			if err := s.finishRun(ctx, run, now, exitedUpdate(run, st.ExitCode)); err != nil {
				return err
			}
		}
	}

	pending, err := s.Store.ListPendingBefore(ctx, now.Add(-5*time.Minute))
	if err != nil {
		return err
	}
	for i := range pending {
		run := &pending[i]
		message := "worker did not start"

		s.dispatchMu.Lock()
		fresh, reErr := s.Store.GetRun(ctx, run.ID)
		if reErr != nil || fresh.State != model.RunPending {
			s.dispatchMu.Unlock()
			continue
		}
		updated, err := s.Store.MarkRunTerminal(ctx, run.ID, model.RunFailed, nil, now, message)
		if err != nil {
			s.dispatchMu.Unlock()
			return err
		}
		if !updated {
			// Another writer finished it between our re-read and the update.
			s.dispatchMu.Unlock()
			continue
		}
		// Abandoned pending runs are failures too — extend the backoff streak
		// (dispatchMu already held).
		s.recordRunOutcome(run.ProjectID, run.TaskID, model.RunFailed, now)
		logErr := s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "error",
			EventType: "task_failed",
			ProjectID: run.ProjectID,
			TriggerID: run.TriggerID,
			TaskID:    run.TaskID,
			RunID:     &run.ID,
			Message:   message,
			Metadata:  map[string]any{"resource_key": run.ResourceKey},
		})
		s.emitWatchTask(ctx, bus.TypeWatchTaskFailed, &fresh, nil, message, "")
		s.dispatchMu.Unlock()
		if logErr != nil {
			return logErr
		}
	}
	return nil
}

// terminalUpdate carries everything finishRun needs to move a running run to a
// terminal state: the target state, the (optional) exit code and failure reason,
// the Event message and the stored error_message, plus any extra Event metadata.
type terminalUpdate struct {
	state        model.RunState
	exitCode     *int
	reason       string
	journalMsg   string
	errorMessage string
	meta         map[string]any
}

// exitedUpdate builds the terminalUpdate for a run the backend reports exited,
// preserving today's completed/failed/interrupted semantics and stamping the
// exit_nonzero / exit_unrecorded reasons.
func exitedUpdate(run *model.RunRecord, codePtr *int) terminalUpdate {
	// Store exit_code in the Event metadata as a plain int (or nil), never the
	// *int itself, so a journal line reads `exit_code=3` or `exit_code=null`
	// rather than a 0x… pointer address.
	var exitMeta any
	if codePtr != nil {
		exitMeta = *codePtr
	}
	u := terminalUpdate{
		state:    model.RunCompleted,
		exitCode: codePtr,
		meta: map[string]any{
			"exit_code":    exitMeta,
			"session_name": run.SessionName,
			"interrupted":  codePtr == nil,
		},
	}
	switch {
	case codePtr == nil:
		// No code was recorded, so report none rather than inventing one.
		u.state = model.RunFailed
		u.reason = reasonExitUnrecorded
		u.journalMsg = fmt.Sprintf("run %d was interrupted before recording an exit code", run.ID)
		u.errorMessage = tailOutput(run.OutputPath, OutputTailMaxBytes)
		if u.errorMessage == "" {
			u.errorMessage = "run interrupted before recording an exit code"
		}
	case *codePtr != 0:
		u.state = model.RunFailed
		u.reason = reasonExitNonzero
		u.journalMsg = fmt.Sprintf("run %d finished with exit code %d", run.ID, *codePtr)
		u.errorMessage = tailOutput(run.OutputPath, OutputTailMaxBytes)
		if u.errorMessage == "" {
			u.errorMessage = fmt.Sprintf("run exited with code %d", *codePtr)
		}
	default:
		u.journalMsg = fmt.Sprintf("run %d finished with exit code %d", run.ID, *codePtr)
	}
	return u
}

// finishRun transitions a running run to its terminal state under dispatchMu,
// re-reading the run first so a run another writer already finished is left
// alone (the once-only guard; phase 3 folds this into a conditional
// MarkRunTerminal). It stamps failure_reason for failures, maintains failure
// backoff, logs the task_completed/task_failed Event and emits the matching
// watch.task.* bus event carrying the reason.
func (s *Service) finishRun(ctx context.Context, run *model.RunRecord, now time.Time, u terminalUpdate) error {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()

	fresh, reErr := s.Store.GetRun(ctx, run.ID)
	if reErr != nil {
		return reErr
	}

	// The conditional UPDATE (state IN ('pending','running')) is the single
	// once-only dedup mechanism: a run another writer already finished reports
	// updated=false, so we mark nothing and emit nothing. fresh is re-read only
	// for the bus event's provenance.
	var updated bool
	var err error
	if u.state == model.RunFailed && u.reason != "" {
		updated, err = s.Store.MarkRunFailedWithReason(ctx, run.ID, u.exitCode, now, u.errorMessage, u.reason)
	} else {
		updated, err = s.Store.MarkRunTerminal(ctx, run.ID, u.state, u.exitCode, now, u.errorMessage)
	}
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}

	// Maintain failure backoff (dispatchMu already held): a failed run extends
	// the projectID:taskID streak; a successful run clears it.
	s.recordRunOutcome(run.ProjectID, run.TaskID, u.state, now)

	eventType := "task_completed"
	level := "info"
	watchType := bus.TypeWatchTaskCompleted
	if u.state == model.RunFailed {
		eventType = "task_failed"
		level = "error"
		watchType = bus.TypeWatchTaskFailed
	}
	meta := u.meta
	if meta == nil {
		meta = map[string]any{}
	}
	meta["resource_key"] = run.ResourceKey
	if u.reason != "" {
		meta["reason"] = u.reason
	}
	logErr := s.logEvent(ctx, &store.EventInput{
		Timestamp: now,
		Level:     level,
		EventType: eventType,
		ProjectID: run.ProjectID,
		TriggerID: run.TriggerID,
		TaskID:    run.TaskID,
		RunID:     &run.ID,
		Message:   u.journalMsg,
		Metadata:  meta,
	})
	s.emitWatchTask(ctx, watchType, &fresh, u.exitCode, u.journalMsg, u.reason)
	return logErr
}

// reapPastDeadline enforces a run's deadline independently of Status. It always
// attempts Stop. If Stop is confirmed, or Status already reports the run
// lost/exited, the run is failed timed_out exactly once. Otherwise the run is
// kept running (so dedup stays in force and no overlapping run can start), a
// task_stop_failed warning is recorded once per run, and Stop is retried on the
// next tick.
func (s *Service) reapPastDeadline(ctx context.Context, run *model.RunRecord, b backend.Backend, binding backend.Binding, st backend.Status, statusErr error, now time.Time) error {
	stopErr := b.Stop(ctx, binding)
	statusTerminal := statusErr == nil && (st.State == backend.StatusLost || st.State == backend.StatusExited)
	if stopErr == nil || statusTerminal {
		return s.finishRun(ctx, run, now, terminalUpdate{
			state:        model.RunFailed,
			reason:       reasonTimedOut,
			journalMsg:   fmt.Sprintf("run %d timed out", run.ID),
			errorMessage: "run exceeded its deadline and was stopped",
			meta:         map[string]any{"session_name": run.SessionName},
		})
	}

	// Stop could not be confirmed and the run is not known terminal: keep it
	// running and surface the wedge once per run.
	seen, err := s.Store.CountRunEventsByType(ctx, run.ID, "task_stop_failed")
	if err != nil {
		return err
	}
	if seen == 0 {
		return s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "warn",
			EventType: "task_stop_failed",
			ProjectID: run.ProjectID,
			TriggerID: run.TriggerID,
			TaskID:    run.TaskID,
			RunID:     &run.ID,
			Message:   fmt.Sprintf("run %d past deadline but backend stop is unconfirmed", run.ID),
			Metadata:  map[string]any{"error": stopErr.Error(), "resource_key": run.ResourceKey},
		})
	}
	return nil
}

func (s *Service) Clean(ctx context.Context, force bool) error {
	now := s.Now()
	if force {
		activeRuns, err := s.Store.ListRunsByStates(ctx, model.RunPending, model.RunRunning)
		if err != nil {
			return err
		}
		for i := range activeRuns {
			run := &activeRuns[i]
			if run.BackendTarget != "" {
				if b, ok := s.Backends.Get(run.Backend); ok {
					if err := b.Stop(ctx, backend.Binding{Manager: run.Backend, Target: run.BackendTarget}); err != nil {
						// Contain a Stop failure: one wedged run must not abort the
						// whole clean. Warn, leave it active (retried next clean), move on.
						if logErr := s.systemWarning(ctx, run, fmt.Sprintf("run %d backend stop failed during clean --force", run.ID), err); logErr != nil {
							return logErr
						}
						continue
					}
				}
			}
			message := "killed by auto watch clean --force"
			updated, err := s.Store.MarkRunTerminal(ctx, run.ID, model.RunFailed, nil, now, message)
			if err != nil {
				return err
			}
			if !updated {
				continue
			}
			if err := s.logEvent(ctx, &store.EventInput{
				Timestamp: now,
				Level:     "warn",
				EventType: "task_failed",
				ProjectID: run.ProjectID,
				TriggerID: run.TriggerID,
				TaskID:    run.TaskID,
				RunID:     &run.ID,
				Message:   message,
				Metadata:  map[string]any{"resource_key": run.ResourceKey},
			}); err != nil {
				return err
			}
			if err := s.removeWorktree(ctx, run, "manual_clean_force"); err != nil {
				if logErr := s.systemWarning(ctx, run, fmt.Sprintf("run %d worktree removal failed during clean --force", run.ID), err); logErr != nil {
					return logErr
				}
				continue
			}
		}
	}

	terminalRuns, err := s.Store.ListTerminalRunsOlderThan(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	for i := range terminalRuns {
		run := &terminalRuns[i]
		if err := s.removeWorktree(ctx, run, "ttl_cleanup"); err != nil {
			// Contain a worktree-removal failure: warn naming the run and move on
			// so one un-removable worktree can't take the tick down.
			if logErr := s.systemWarning(ctx, run, fmt.Sprintf("run %d worktree removal failed during ttl cleanup", run.ID), err); logErr != nil {
				return logErr
			}
			continue
		}
	}

	// Retention pruning: delete terminal runs (and their on-disk directories)
	// and events older than the retention window. Remove directories first, then
	// delete only the rows whose directory removal succeeded — a failed removal
	// is skipped and retried next tick, never orphaning the cleanup target.
	cutoff := now.Add(-time.Duration(s.retentionDays) * 24 * time.Hour)
	expired, err := s.Store.ListTerminalRunsOlderThan(ctx, cutoff)
	if err != nil {
		return err
	}
	runsDir, err := config.RunsDir()
	if err != nil {
		return err
	}
	cleanedIDs := make([]int64, 0, len(expired))
	for i := range expired {
		run := &expired[i]
		// A kept dirty worktree outlives retention: while its worktree is still on
		// disk, keep the row so the worktree_path link survives and the worktree is
		// never orphaned from its run (the dirty-worktree guard in removeWorktree
		// left both in place).
		if wt := strings.TrimSpace(run.WorktreePath); wt != "" {
			if _, statErr := os.Stat(wt); statErr == nil {
				continue
			}
		}
		// A run that failed before UpdateRunStarted persisted RuntimeDir has an
		// empty path even though startWorker may have created the deterministic
		// runs/<id> directory. Fall back to that path so os.RemoveAll("") doesn't
		// silently "succeed" and orphan the directory past retention forever.
		dir := strings.TrimSpace(run.RuntimeDir)
		if dir == "" {
			dir = filepath.Join(runsDir, strconv.FormatInt(run.ID, 10))
		}
		if err := os.RemoveAll(dir); err != nil {
			// Leave the row in place so the directory is retried next tick.
			continue
		}
		cleanedIDs = append(cleanedIDs, run.ID)
	}
	if _, err := s.Store.DeleteRuns(ctx, cleanedIDs); err != nil {
		return err
	}
	if _, err := s.Store.PruneEventsOlderThan(ctx, cutoff); err != nil {
		return err
	}

	// Reconcile orphan run dirs: a numeric runs/<id> directory with no matching
	// row and an mtime older than 24h is leftover from a run whose row is gone
	// (e.g. a crash, or a pre-fix restart loop). A fresh orphan is left alone so a
	// run still being set up is never swept out from under itself.
	if err := s.sweepOrphanRunDirs(ctx, now, runsDir); err != nil {
		return err
	}

	// Checkpoint the WAL after pruning so the checkpoint also covers the deletes,
	// bounding WAL growth.
	if err := s.Store.WALCheckpoint(ctx); err != nil {
		return err
	}
	return nil
}

// sweepOrphanRunDirs removes numeric run directories under runsDir that have no
// matching run row and whose mtime is older than 24h, recording a
// run_dir_orphan_removed Event for each. A directory younger than 24h, or one
// that still has a row, is left in place. A per-directory removal or log failure
// is contained (skip and continue); only a store failure aborts.
func (s *Service) sweepOrphanRunDirs(ctx context.Context, now time.Time, runsDir string) error {
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := now.Add(-24 * time.Hour)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, perr := strconv.ParseInt(entry.Name(), 10, 64)
		if perr != nil {
			continue // not a numeric run dir
		}
		exists, err := s.Store.RunExists(ctx, id)
		if err != nil {
			return err
		}
		if exists {
			continue // a known run owns this dir
		}
		info, ierr := entry.Info()
		if ierr != nil {
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue // fresh orphan: a run may still be setting it up
		}
		dir := filepath.Join(runsDir, entry.Name())
		if err := os.RemoveAll(dir); err != nil {
			continue // retry next tick
		}
		if err := s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "info",
			EventType: "run_dir_orphan_removed",
			RunID:     &id,
			Message:   fmt.Sprintf("removed orphan run dir %s (no matching run row)", entry.Name()),
			Metadata: map[string]any{
				"run_dir": dir,
				"run_id":  id,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) tickProject(ctx context.Context, now time.Time, project *model.ProjectRef) error {
	projectCfg, err := config.LoadProjectConfig(project.Path)
	if err != nil {
		return &configError{err}
	}
	projectID := projectCfg.ID
	if projectID == "" {
		projectID = project.ID
	}
	validTasks := map[string]model.TaskDef{}
	taskIDs := make([]string, 0, len(projectCfg.Tasks))
	for id := range projectCfg.Tasks {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)
	for _, taskID := range taskIDs {
		task := projectCfg.Tasks[taskID]
		if errs := config.ValidateTaskEntry(taskID, task, s.Backends.Names()); len(errs) > 0 {
			if err := s.logValidationErrors(ctx, now, projectID, "", "", "config_warning", errs); err != nil {
				return err
			}
			continue
		}
		validTasks[taskID] = task
	}
	if errs := config.ValidateProjectConfig(model.ProjectConfig{ID: projectID, Tasks: validTasks, Triggers: map[string]model.TriggerDef{}}, s.Backends.Names()); len(errs) > 0 {
		return &configError{fmt.Errorf("project id is invalid: %s", errs[0].Message)}
	}

	triggerIDs := make([]string, 0, len(projectCfg.Triggers))
	for id := range projectCfg.Triggers {
		triggerIDs = append(triggerIDs, id)
	}
	sort.Strings(triggerIDs)
	taskSet := map[string]struct{}{}
	for id := range validTasks {
		taskSet[id] = struct{}{}
	}

	// Drain owed fires before evaluating this Project's Triggers, so a fire that
	// could not be dispatched on an earlier tick is redelivered exactly once as
	// soon as it is unblocked.
	if err := s.drainOwedFires(ctx, now, projectID, project.Path, projectCfg.Triggers, validTasks); err != nil {
		return err
	}

	for _, triggerID := range triggerIDs {
		trigger := projectCfg.Triggers[triggerID]
		if errs := config.ValidateTriggerEntry(triggerID, &trigger, taskSet); len(errs) > 0 {
			if err := s.logValidationErrors(ctx, now, projectID, triggerID, "", "trigger_invalid", errs); err != nil {
				return err
			}
			continue
		}
		switch trigger.Type {
		case "cron":
			if err := s.evaluateTrigger(ctx, now, projectID, project.Path, triggerID, &trigger, validTasks); err != nil {
				return err
			}
		case "file_created":
			if err := s.evaluateFileCreatedTrigger(ctx, now, projectID, project.Path, triggerID, &trigger, validTasks); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) evaluateTrigger(ctx context.Context, now time.Time, projectID, projectPath, triggerID string, trigger *model.TriggerDef, tasks map[string]model.TaskDef) error {
	tickMinuteLocal := now.In(time.Local).Truncate(time.Minute)
	tickMinute := tickMinuteLocal.UTC()
	state, err := s.Store.GetTriggerState(ctx, projectID, triggerID)
	if err != nil {
		return err
	}
	if !state.LastDueMinute.IsZero() && state.LastDueMinute.Equal(tickMinute) {
		// Already processed this minute — no event (Phase 3 noise reduction).
		return nil
	}
	schedule, err := config.ParseCron(trigger.When)
	if err != nil {
		return err
	}
	prev := tickMinuteLocal.Add(-1 * time.Minute)
	due := schedule.Next(prev).Equal(tickMinuteLocal)
	if !due {
		// Not due this minute — no event (Phase 3 noise reduction).
		return nil
	}

	currentSHA := ""
	if trigger.OnlyIfBranchChanged != "" {
		currentSHA, err = gitx.BranchHeadSHA(projectPath, trigger.OnlyIfBranchChanged)
		if err != nil {
			// A git failure resolving the branch (e.g. the branch no longer
			// exists) must neither fire the Trigger nor abort the Project's other
			// Triggers: record a system_warning naming the branch and the git
			// error, and skip this Trigger this tick. Only a store failure from
			// logging it propagates.
			return s.logEvent(ctx, &store.EventInput{
				Timestamp: now,
				Level:     "warn",
				EventType: "system_warning",
				ProjectID: projectID,
				TriggerID: triggerID,
				Message:   fmt.Sprintf("trigger %s: resolving branch %q failed; not firing", triggerID, trigger.OnlyIfBranchChanged),
				Metadata:  map[string]any{"error": err.Error(), "branch": trigger.OnlyIfBranchChanged},
			})
		}
		if state.LastBranchSHA != "" && state.LastBranchSHA == currentSHA {
			if err := s.Store.UpsertTriggerState(ctx, &model.TriggerStateRecord{
				ProjectID:     projectID,
				TriggerID:     triggerID,
				LastDueMinute: tickMinute,
				LastBranchSHA: currentSHA,
				UpdatedAt:     now,
			}); err != nil {
				return err
			}
			// Branch unchanged — no event (Phase 3 noise reduction).
			return nil
		}
	}

	// A branch-changed cron advances its last_branch_sha cursor below whether or
	// not every TaskDef dispatched, so a skipped TaskDef's fire would be lost: for
	// that Trigger we record an owed fire for any TaskDef that is deduped, backed
	// off, or whose Start fails. A plain cron advances no such cursor, so its skip
	// semantics are unchanged (no owed fire; a Start failure still returns).
	branchFilter := trigger.OnlyIfBranchChanged != ""

	taskIDs := append([]string(nil), trigger.Tasks...)
	sort.Strings(taskIDs)
	for _, taskID := range taskIDs {
		if skip, nextEligible := s.shouldBackoff(projectID, taskID, now); skip {
			if logErr := s.logBackoff(ctx, projectID, triggerID, taskID, nextEligible, now); logErr != nil {
				return logErr
			}
			if branchFilter {
				if err := s.Store.UpsertOwedFire(ctx, projectID, triggerID, taskID, "backoff", now); err != nil {
					return err
				}
			}
			continue
		}
		task := tasks[taskID]
		_, err := s.Dispatch(ctx, &store.ReserveRunInput{
			ProjectID:   projectID,
			ProjectPath: projectPath,
			TriggerID:   triggerID,
			TriggerType: trigger.Type,
			TaskID:      taskID,
			TaskType:    task.Type,
			ResourceKey: "cron:" + triggerID,
			StartedAt:   now,
		}, task)
		if err != nil {
			if errors.Is(err, store.ErrActiveRunExists) {
				if logErr := s.logEvent(ctx, &store.EventInput{
					Timestamp: now,
					Level:     "info",
					EventType: "task_skipped_dedup",
					ProjectID: projectID,
					TriggerID: triggerID,
					TaskID:    taskID,
					Message:   "skipped duplicate active run",
					Metadata: map[string]any{
						"resource_key": "cron:" + triggerID,
						"cron":         trigger.When,
					},
				}); logErr != nil {
					return logErr
				}
				if branchFilter {
					if err := s.Store.UpsertOwedFire(ctx, projectID, triggerID, taskID, "dedup", now); err != nil {
						return err
					}
				}
				continue
			}
			if branchFilter {
				// A confirmed Start failure already marked the run failed and backed
				// the task off (failRun); record an owed fire and continue so the
				// cursor upsert below still advances and the other TaskDefs still run.
				if oErr := s.Store.UpsertOwedFire(ctx, projectID, triggerID, taskID, "start_failed", now); oErr != nil {
					return oErr
				}
				continue
			}
			return err
		}
	}

	// Trigger processing produces no event (Phase 3 noise reduction); dispatch,
	// dedup-skip, and backoff outcomes are observable via task_* events.
	return s.Store.UpsertTriggerState(ctx, &model.TriggerStateRecord{
		ProjectID:     projectID,
		TriggerID:     triggerID,
		LastDueMinute: tickMinute,
		LastBranchSHA: currentSHA,
		UpdatedAt:     now,
	})
}

func (s *Service) evaluateFileCreatedTrigger(ctx context.Context, now time.Time, projectID, projectPath, triggerID string, trigger *model.TriggerDef, tasks map[string]model.TaskDef) error {
	resourceKey := "file_created:" + triggerID

	// Glob the project directory for matching files.
	fsys := os.DirFS(projectPath)
	matches, err := doublestar.Glob(fsys, trigger.Glob)
	if err != nil {
		return fmt.Errorf("glob %q in %s: %w", trigger.Glob, projectPath, err)
	}
	sort.Strings(matches)

	// Build a set of current files with their mod times.
	currentFiles := make(map[string]time.Time, len(matches))
	for _, match := range matches {
		info, err := os.Stat(filepath.Join(projectPath, match))
		if err != nil {
			continue // file vanished between glob and stat
		}
		if info.IsDir() {
			continue
		}
		currentFiles[match] = info.ModTime()
	}

	// Load the stored snapshot.
	snapshots, err := s.Store.ListFileSnapshots(ctx, projectID, triggerID)
	if err != nil {
		return err
	}
	knownFiles := make(map[string]struct{}, len(snapshots))
	for _, snap := range snapshots {
		knownFiles[snap.FilePath] = struct{}{}
	}

	// First evaluation ever: seed silently without firing.
	// We check trigger_state to distinguish "never evaluated" from "evaluated
	// but snapshot is currently empty" (e.g. all files were deleted).
	triggerState, err := s.Store.GetTriggerState(ctx, projectID, triggerID)
	if err != nil {
		return err
	}
	seeding := triggerState.UpdatedAt.IsZero() && len(currentFiles) > 0

	// Detect new files.
	var newFiles []string
	for path := range currentFiles {
		if _, known := knownFiles[path]; !known {
			newFiles = append(newFiles, path)
		}
	}
	sort.Strings(newFiles)

	// Detect deleted files for snapshot cleanup.
	var deletedFiles []string
	for _, snap := range snapshots {
		if _, exists := currentFiles[snap.FilePath]; !exists {
			deletedFiles = append(deletedFiles, snap.FilePath)
		}
	}

	// Upsert all current files into the snapshot (tracks mod_time and updated_at).
	for path, modTime := range currentFiles {
		if err := s.Store.UpsertFileSnapshot(ctx, &model.FileSnapshotRecord{
			ProjectID:   projectID,
			TriggerID:   triggerID,
			FilePath:    path,
			ModTime:     modTime,
			FirstSeenAt: now,
			UpdatedAt:   now,
		}); err != nil {
			return err
		}
	}

	// Remove deleted files from snapshot.
	if err := s.Store.DeleteFileSnapshots(ctx, projectID, triggerID, deletedFiles); err != nil {
		return err
	}

	// Record that this trigger has been evaluated (used to distinguish
	// "never evaluated" from "evaluated but snapshot empty").
	if err := s.Store.UpsertTriggerState(ctx, &model.TriggerStateRecord{
		ProjectID: projectID,
		TriggerID: triggerID,
		UpdatedAt: now,
	}); err != nil {
		return err
	}

	if seeding {
		// Baseline snapshot seeded — no event (Phase 3 noise reduction).
		return nil
	}

	if len(newFiles) == 0 {
		// No new files — no event (Phase 3 noise reduction).
		return nil
	}

	// Fire: launch linked tasks. The snapshot and trigger_state above already
	// advanced (the new file is now "known"), so a TaskDef that is deduped, backed
	// off, or fails to Start would lose this fire forever: record an owed fire for
	// it, and continue rather than abort, so every other TaskDef still runs and a
	// drain on a later tick redelivers the fire exactly once.
	taskIDs := append([]string(nil), trigger.Tasks...)
	sort.Strings(taskIDs)
	for _, taskID := range taskIDs {
		if skip, nextEligible := s.shouldBackoff(projectID, taskID, now); skip {
			if logErr := s.logBackoff(ctx, projectID, triggerID, taskID, nextEligible, now); logErr != nil {
				return logErr
			}
			if err := s.Store.UpsertOwedFire(ctx, projectID, triggerID, taskID, "backoff", now); err != nil {
				return err
			}
			continue
		}
		task := tasks[taskID]
		_, err := s.Dispatch(ctx, &store.ReserveRunInput{
			ProjectID:   projectID,
			ProjectPath: projectPath,
			TriggerID:   triggerID,
			TriggerType: trigger.Type,
			TaskID:      taskID,
			TaskType:    task.Type,
			ResourceKey: resourceKey,
			StartedAt:   now,
		}, task)
		if err != nil {
			if errors.Is(err, store.ErrActiveRunExists) {
				if logErr := s.logEvent(ctx, &store.EventInput{
					Timestamp: now,
					Level:     "info",
					EventType: "task_skipped_dedup",
					ProjectID: projectID,
					TriggerID: triggerID,
					TaskID:    taskID,
					Message:   "skipped duplicate active run",
					Metadata: map[string]any{
						"resource_key": resourceKey,
						"glob":         trigger.Glob,
					},
				}); logErr != nil {
					return logErr
				}
				if err := s.Store.UpsertOwedFire(ctx, projectID, triggerID, taskID, "dedup", now); err != nil {
					return err
				}
				continue
			}
			// A confirmed Start failure already marked the run failed and backed the
			// task off (failRun); record an owed fire and continue (never abort a
			// trigger whose snapshot already advanced).
			if oErr := s.Store.UpsertOwedFire(ctx, projectID, triggerID, taskID, "start_failed", now); oErr != nil {
				return oErr
			}
			continue
		}
	}

	// file_created firing produces no event (Phase 3 noise reduction); dispatch,
	// dedup-skip, and backoff outcomes are observable via task_* events.
	return nil
}

// resourceKeyFor returns the dedup resource key the normal dispatch path uses
// for the given Trigger, so a drained owed fire reserves the same key and dedups
// against a live run identically.
func resourceKeyFor(triggerID, triggerType string) string {
	if triggerType == "file_created" {
		return "file_created:" + triggerID
	}
	return "cron:" + triggerID
}

// drainOwedFires retries every owed fire recorded for the Project before its
// Triggers are evaluated this tick. An owed fire whose Trigger no longer exists,
// or whose TaskDef is gone or no longer linked to the Trigger, is discarded with
// an Event. One whose TaskDef is still backed off is left for a later tick.
// Otherwise it is Dispatched with the same resource_key scheme as the normal
// path: on a successful dispatch the row is deleted; a still-blocked (deduped) or
// freshly failed dispatch keeps the row so it retries once unblocked. Only a
// store failure aborts the tick.
func (s *Service) drainOwedFires(ctx context.Context, now time.Time, projectID, projectPath string, triggers map[string]model.TriggerDef, validTasks map[string]model.TaskDef) error {
	owed, err := s.Store.ListOwedFires(ctx, projectID)
	if err != nil {
		return err
	}
	for i := range owed {
		fire := owed[i]
		trigger, triggerOK := triggers[fire.TriggerID]
		if !triggerOK {
			if err := s.discardOwedFire(ctx, now, &fire, "trigger no longer exists"); err != nil {
				return err
			}
			continue
		}
		task, taskOK := validTasks[fire.TaskID]
		linked := slices.Contains(trigger.Tasks, fire.TaskID)
		if !taskOK || !linked {
			if err := s.discardOwedFire(ctx, now, &fire, "task no longer linked to trigger"); err != nil {
				return err
			}
			continue
		}
		if skip, _ := s.shouldBackoff(projectID, fire.TaskID, now); skip {
			// Still backed off — leave it for a later tick once the window clears.
			continue
		}
		_, derr := s.Dispatch(ctx, &store.ReserveRunInput{
			ProjectID:   projectID,
			ProjectPath: projectPath,
			TriggerID:   fire.TriggerID,
			TriggerType: trigger.Type,
			TaskID:      fire.TaskID,
			TaskType:    task.Type,
			ResourceKey: resourceKeyFor(fire.TriggerID, trigger.Type),
			StartedAt:   now,
		}, task)
		if derr != nil {
			// A dedup (an active run still holds the key) or a fresh Start failure
			// (already marked failed and backed off by failRun) leaves the owed fire
			// in place so it retries once unblocked. One bad TaskDef must not abort
			// the drain or the tick.
			continue
		}
		if err := s.Store.DeleteOwedFire(ctx, projectID, fire.TriggerID, fire.TaskID); err != nil {
			return err
		}
	}
	return nil
}

// discardOwedFire deletes an owed fire whose Trigger or TaskDef no longer exists
// and records why, so an operator can see a fire was dropped rather than fired.
func (s *Service) discardOwedFire(ctx context.Context, now time.Time, fire *model.OwedFireRecord, why string) error {
	if err := s.Store.DeleteOwedFire(ctx, fire.ProjectID, fire.TriggerID, fire.TaskID); err != nil {
		return err
	}
	return s.logEvent(ctx, &store.EventInput{
		Timestamp: now,
		Level:     "info",
		EventType: "owed_fire_discarded",
		ProjectID: fire.ProjectID,
		TriggerID: fire.TriggerID,
		TaskID:    fire.TaskID,
		Message:   fmt.Sprintf("discarded owed fire for trigger %s task %s: %s", fire.TriggerID, fire.TaskID, why),
		Metadata: map[string]any{
			"owed_reason":    fire.Reason,
			"discard_reason": why,
		},
	})
}

func (s *Service) startWorker(ctx context.Context, runID int64, task model.TaskDef) error {
	run, err := s.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	runsDir, err := config.RunsDir()
	if err != nil {
		return err
	}
	runDir := filepath.Join(runsDir, strconv.FormatInt(runID, 10))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return s.failRun(ctx, &run, "", fmt.Errorf("create runtime dir: %w", err))
	}

	workDir := run.ProjectPath
	worktreePath := ""
	branch := ""
	commandFile := filepath.Join(runDir, "command.txt")
	promptFile := filepath.Join(runDir, "prompt.txt")
	outputPath := filepath.Join(runDir, "output.log")
	exitPath := filepath.Join(runDir, "exit-code")

	switch task.Type {
	case "bash":
		if err := os.WriteFile(commandFile, []byte(task.Command+"\n"), 0o644); err != nil {
			return s.failRun(ctx, &run, worktreePath, fmt.Errorf("write command.txt: %w", err))
		}
	case "claude":
		branch, err = gitx.DefaultBranch(run.ProjectPath)
		if err != nil {
			return s.failRun(ctx, &run, worktreePath, err)
		}
		worktreePath = filepath.Join(config.WorktreesDir(run.ProjectPath), runner.ScheduledRunName(run.ID, run.TaskID))
		if err := gitx.AddWorktree(run.ProjectPath, worktreePath, branch); err != nil {
			return s.failRun(ctx, &run, worktreePath, err)
		}
		workDir = worktreePath
		if err := s.logEvent(ctx, &store.EventInput{
			Timestamp: s.Now(),
			Level:     "info",
			EventType: "worktree_created",
			ProjectID: run.ProjectID,
			TriggerID: run.TriggerID,
			TaskID:    run.TaskID,
			RunID:     &run.ID,
			Message:   "created worktree for claude task",
			Metadata: map[string]any{
				"branch":        branch,
				"worktree_path": worktreePath,
			},
		}); err != nil {
			return s.failRun(ctx, &run, worktreePath, err)
		}
		prompt := runner.BuildPrompt(run.ProjectID, run.TriggerType, run.TriggerID, run.ResourceKey, branch, task.Prompt)
		if err := os.WriteFile(promptFile, []byte(prompt), 0o644); err != nil {
			return s.failRun(ctx, &run, worktreePath, fmt.Errorf("write prompt.txt: %w", err))
		}
	default:
		return s.failRun(ctx, &run, worktreePath, fmt.Errorf("unsupported task type %q", task.Type))
	}

	// Compose the inner bash/claude command; the shared launch wrapper owns the
	// completion contract (tee into output.log, atomic exit-code file), so exit
	// semantics are identical across every Backend.
	innerCmd, err := runner.InnerCommand(task.Type)
	if err != nil {
		return s.failRun(ctx, &run, worktreePath, err)
	}
	launchScript, err := backend.WriteLaunchScript(&backend.LaunchSpec{
		RunDir:     runDir,
		WorkDir:    workDir,
		Command:    innerCmd,
		OutputPath: outputPath,
		ExitPath:   exitPath,
	})
	if err != nil {
		return s.failRun(ctx, &run, worktreePath, err)
	}

	sessionName := runner.ScheduledRunName(run.ID, run.TaskID)
	b, ok := s.Backends.Get(task.Backend)
	if !ok {
		return s.failRun(ctx, &run, worktreePath, fmt.Errorf("unknown backend %q", task.Backend))
	}
	binding, startErr := b.Start(ctx, &backend.StartSpec{
		Name:         sessionName,
		WorkDir:      workDir,
		LaunchScript: launchScript,
		RunDir:       runDir,
		Owner:        run.ProjectID,
	})
	// ErrStartUncertain: the adapter created a run but could not confirm whether
	// the launched command is alive. Record the Binding and keep the run running
	// so Status and the deadline own it — never re-dispatch over a possibly live
	// command. Any other error is a confirmed failure.
	uncertain := errors.Is(startErr, backend.ErrStartUncertain)
	if startErr != nil && !uncertain {
		return s.failRun(ctx, &run, worktreePath, startErr)
	}

	// Fix the deadline when the run starts (per-TaskDef timeout, else the 2h
	// default) so later config edits never move a live run's deadline.
	timeout := defaultRunTimeout
	if d, perr := time.ParseDuration(strings.TrimSpace(task.Timeout)); perr == nil && d > 0 {
		timeout = d
	}
	deadline := run.StartedAt.Add(timeout)
	if err := s.Store.UpdateRunStarted(ctx, run.ID, &store.RunStartUpdate{
		SessionName:   binding.Target,
		RuntimeDir:    runDir,
		OutputPath:    outputPath,
		ExitPath:      exitPath,
		WorktreePath:  worktreePath,
		Branch:        branch,
		Backend:       binding.Manager,
		BackendTarget: binding.Target,
		DeadlineAt:    deadline,
	}); err != nil {
		_ = b.Stop(ctx, binding)
		return s.failRun(ctx, &run, worktreePath, err)
	}
	if uncertain {
		_ = s.logEvent(ctx, &store.EventInput{
			Timestamp: s.Now(),
			Level:     "warn",
			EventType: "system_warning",
			ProjectID: run.ProjectID,
			TriggerID: run.TriggerID,
			TaskID:    run.TaskID,
			RunID:     &run.ID,
			Message:   "backend start outcome uncertain; keeping run running",
			Metadata: map[string]any{
				"error":        startErr.Error(),
				"resource_key": run.ResourceKey,
			},
		})
	}
	if err := s.logEvent(ctx, &store.EventInput{
		Timestamp: s.Now(),
		Level:     "info",
		EventType: "task_started",
		ProjectID: run.ProjectID,
		TriggerID: run.TriggerID,
		TaskID:    run.TaskID,
		RunID:     &run.ID,
		Message:   "task started",
		Metadata: map[string]any{
			"session_name":  binding.Target,
			"worktree_path": worktreePath,
			"resource_key":  run.ResourceKey,
			"branch":        branch,
		},
	}); err != nil {
		return err
	}
	run.SessionName = binding.Target
	run.WorktreePath = worktreePath
	run.Branch = branch
	s.emitWatchTask(ctx, bus.TypeWatchTaskStarted, &run, nil, "task started", "")
	return nil
}

// failRun is the single chokepoint for a start/dispatch-time failure. It stamps
// the run failed with failure_reason=start_failed, records failure backoff,
// emits watch.task.failed with the reason, cleans up any worktree and logs the
// task_failed Event. It ALWAYS returns a non-nil error — fmt.Errorf("start run
// %d: %w", id, runErr) joined (errors.Join) with any error from recording the
// failure — so Dispatch and the RPC task.run propagate it instead of reporting
// success (the swallowed-start-error bug). Callers (Dispatch, startWorker) hold
// dispatchMu, as recordRunOutcome requires.
func (s *Service) failRun(ctx context.Context, run *model.RunRecord, worktreePath string, runErr error) error {
	now := s.Now()
	wrapErr := fmt.Errorf("start run %d: %w", run.ID, runErr)
	var recErr error

	updated, err := s.Store.MarkRunFailedWithReason(ctx, run.ID, nil, now, runErr.Error(), reasonStartFailed)
	if err != nil {
		recErr = errors.Join(recErr, err)
	} else if updated {
		// A startup/dispatch-time failure is marked terminal here and never reaches
		// Reap, so record it for failure backoff at this single chokepoint.
		s.recordRunOutcome(run.ProjectID, run.TaskID, model.RunFailed, now)
		if worktreePath != "" {
			run.WorktreePath = worktreePath
		}
		s.emitWatchTask(ctx, bus.TypeWatchTaskFailed, run, nil, runErr.Error(), reasonStartFailed)
		if worktreePath != "" {
			if rmErr := s.removeWorktree(ctx, &model.RunRecord{
				ID:           run.ID,
				ProjectID:    run.ProjectID,
				TriggerID:    run.TriggerID,
				TaskID:       run.TaskID,
				ProjectPath:  run.ProjectPath,
				WorktreePath: worktreePath,
			}, "worker_failure_cleanup"); rmErr != nil {
				recErr = errors.Join(recErr, rmErr)
			}
		}
		if logErr := s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "error",
			EventType: "task_failed",
			ProjectID: run.ProjectID,
			TriggerID: run.TriggerID,
			TaskID:    run.TaskID,
			RunID:     &run.ID,
			Message:   runErr.Error(),
			Metadata: map[string]any{
				"resource_key": run.ResourceKey,
				"error":        runErr.Error(),
				"reason":       reasonStartFailed,
			},
		}); logErr != nil {
			recErr = errors.Join(recErr, logErr)
		}
	}
	return errors.Join(wrapErr, recErr)
}

func (s *Service) removeWorktree(ctx context.Context, run *model.RunRecord, reason string) error {
	if strings.TrimSpace(run.WorktreePath) == "" {
		return nil
	}
	// Dirty-worktree guard, inside the single chokepoint so EVERY caller (the 24h
	// TTL sweep, clean --force, cancel and failRun) honours it: a worktree with
	// uncommitted work — or one we cannot even inspect — is kept rather than
	// force-removed, so an in-progress agent's changes are never discarded. The
	// keep is logged once per run (Event lookup) and the worktree_path is left in
	// place so retention also spares the row. A clean worktree is still removed on
	// every path.
	dirty, dirtyErr := gitx.IsDirty(run.WorktreePath)
	if dirtyErr != nil || dirty {
		seen, err := s.Store.CountRunEventsByType(ctx, run.ID, "worktree_kept_dirty")
		if err != nil {
			return err
		}
		if seen == 0 {
			meta := map[string]any{
				"reason":        reason,
				"worktree_path": run.WorktreePath,
			}
			if dirtyErr != nil {
				meta["error"] = dirtyErr.Error()
			}
			if logErr := s.logEvent(ctx, &store.EventInput{
				Timestamp: s.Now(),
				Level:     "warn",
				EventType: "worktree_kept_dirty",
				ProjectID: run.ProjectID,
				TriggerID: run.TriggerID,
				TaskID:    run.TaskID,
				RunID:     &run.ID,
				Message:   fmt.Sprintf("kept worktree with uncommitted changes for run %d", run.ID),
				Metadata:  meta,
			}); logErr != nil {
				return logErr
			}
		}
		return nil
	}
	if err := gitx.RemoveWorktree(run.ProjectPath, run.WorktreePath); err != nil {
		return err
	}
	if err := s.logEvent(ctx, &store.EventInput{
		Timestamp: s.Now(),
		Level:     "info",
		EventType: "worktree_removed",
		ProjectID: run.ProjectID,
		TriggerID: run.TriggerID,
		TaskID:    run.TaskID,
		RunID:     &run.ID,
		Message:   "removed worktree",
		Metadata: map[string]any{
			"reason":        reason,
			"worktree_path": run.WorktreePath,
		},
	}); err != nil {
		return err
	}
	// Clear the now-stale WorktreePath so subsequent Clean ticks early-return
	// instead of re-removing an already-gone worktree and re-logging the event.
	if err := s.Store.ClearRunWorktreePath(ctx, run.ID); err != nil {
		return err
	}
	run.WorktreePath = ""
	return nil
}

// systemWarning records a per-run system_warning Event naming the run and the
// underlying error. Reap and Clean use it to surface a failure in one per-run
// step (Status, Stop, worktree removal) and then continue, so one bad run can't
// take the whole tick down. It returns only the logEvent error (a store
// failure), which the caller DOES propagate.
func (s *Service) systemWarning(ctx context.Context, run *model.RunRecord, message string, cause error) error {
	return s.logEvent(ctx, &store.EventInput{
		Timestamp: s.Now(),
		Level:     "warn",
		EventType: "system_warning",
		ProjectID: run.ProjectID,
		TriggerID: run.TriggerID,
		TaskID:    run.TaskID,
		RunID:     &run.ID,
		Message:   message,
		Metadata:  map[string]any{"error": cause.Error(), "resource_key": run.ResourceKey},
	})
}

func (s *Service) logValidationErrors(ctx context.Context, now time.Time, projectID, triggerID, taskID, eventType string, errs []model.ValidationError) error {
	for _, validationErr := range errs {
		// Dedup repeated validation warnings so the same problem is logged once
		// rather than every tick (Phase 3 noise reduction).
		if s.warningSeen(eventType + ":" + projectID + ":" + triggerID + ":" + taskID + ":" + validationErr.Message) {
			continue
		}
		if err := s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "warn",
			EventType: eventType,
			ProjectID: projectID,
			TriggerID: triggerID,
			TaskID:    taskID,
			Message:   validationErr.Message,
			Metadata: map[string]any{
				"code":  validationErr.Code,
				"path":  validationErr.Path,
				"field": validationErr.Field,
				"value": validationErr.Value,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

// shouldBackoff reports whether dispatch for projectID:taskID must be skipped
// because the task is within its failure-backoff window, and the time it next
// becomes eligible. Guarded by dispatchMu so it is consistent with Reap's
// updates.
func (s *Service) shouldBackoff(projectID, taskID string, now time.Time) (bool, time.Time) {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	st := s.backoff[backoffKey(projectID, taskID)]
	if st == nil || st.consecutiveFailures < 1 {
		return false, time.Time{}
	}
	nextEligible := st.lastFailure.Add(backoffWindow(st.consecutiveFailures))
	if now.Before(nextEligible) {
		return true, nextEligible
	}
	return false, time.Time{}
}

// recordRunOutcome updates the failure-backoff streak for a run's
// projectID:taskID when it reaches a terminal state. A failure increments the
// streak and stamps the failure time; success clears it. The caller MUST hold
// dispatchMu (Reap already does).
func (s *Service) recordRunOutcome(projectID, taskID string, state model.RunState, now time.Time) {
	key := backoffKey(projectID, taskID)
	if state == model.RunFailed {
		st := s.backoff[key]
		if st == nil {
			st = &backoffState{}
			s.backoff[key] = st
		}
		st.consecutiveFailures++
		st.lastFailure = now
		return
	}
	delete(s.backoff, key)
}

// warningSeen reports whether a warning with the given dedup key has already been
// logged this daemon lifetime, marking it seen on first sight. Guarded by
// dispatchMu. Used to suppress repeated config_warning / validation noise that
// would otherwise be re-logged every tick.
func (s *Service) warningSeen(key string) bool {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	if s.configWarningsSeen[key] {
		return true
	}
	s.configWarningsSeen[key] = true
	return false
}

// logBackoff records a task_backoff Event when a TaskDef's dispatch is skipped
// because it is inside its failure-backoff window. It replaces the raw stderr
// line the daemon used to print, so the skip is queryable through the journal
// (auto watch logs) and formatted consistently with other Events.
func (s *Service) logBackoff(ctx context.Context, projectID, triggerID, taskID string, nextEligible, now time.Time) error {
	return s.logEvent(ctx, &store.EventInput{
		Timestamp: now,
		Level:     "info",
		EventType: "task_backoff",
		ProjectID: projectID,
		TriggerID: triggerID,
		TaskID:    taskID,
		Message:   fmt.Sprintf("task %s backed off until %s", taskID, nextEligible.UTC().Format(time.RFC3339)),
		Metadata:  map[string]any{"next_eligible": nextEligible.UTC().Format(time.RFC3339)},
	})
}

func (s *Service) logEvent(ctx context.Context, input *store.EventInput) error {
	if input.Timestamp.IsZero() {
		input.Timestamp = s.Now()
	}
	id, err := s.Store.InsertEvent(ctx, input)
	if err != nil {
		return err
	}
	if s.Output == nil {
		return nil
	}
	record := model.EventRecord{
		ID:        id,
		Timestamp: input.Timestamp,
		Level:     input.Level,
		EventType: input.EventType,
		ProjectID: input.ProjectID,
		TriggerID: input.TriggerID,
		TaskID:    input.TaskID,
		Message:   input.Message,
		Metadata:  input.Metadata,
		RunID:     input.RunID,
	}
	_, err = fmt.Fprintln(s.Output, textout.FormatEventLine(&record))
	return err
}

// OutputTailMaxBytes caps the stored tail of a run's output.log: a 400 KB
// progress-redraw log must not become a 400 KB error_message.
const OutputTailMaxBytes = 8192

// TailOutput returns the tail of a run's output.log, capped at maxBytes, for the
// stored error_message and the `run get` output_tail. It reads only the last
// maxBytes (plus a small margin) of the file, collapses carriage-return progress
// redraws by keeping only the text after the LAST `\r` on each line, and trims
// the result to maxBytes. When anything was dropped (the file was larger than
// the window, or the collapsed text still overflowed) it appends a breadcrumb
// naming the full output path, so the capped tail always ends in a pointer to
// the complete log.
func TailOutput(path string, maxBytes int) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if maxBytes <= 0 {
		maxBytes = OutputTailMaxBytes
	}
	f, err := os.Open(path) //nolint:gosec // path is a daemon-owned run output file
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	size := info.Size()

	const margin = 1024
	truncated := false
	if size > int64(maxBytes)+margin {
		if _, err := f.Seek(size-int64(maxBytes)-margin, io.SeekStart); err != nil {
			return ""
		}
		truncated = true
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}

	// Collapse carriage-return progress redraws: within each newline-delimited
	// line, keep only the text after its last `\r`.
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if idx := strings.LastIndex(line, "\r"); idx >= 0 {
			lines[i] = line[idx+1:]
		}
	}
	out := strings.TrimSpace(strings.Join(lines, "\n"))

	if !truncated && len(out) <= maxBytes {
		return out
	}

	breadcrumb := fmt.Sprintf("…[truncated — full output: %s]", path)
	budget := max(maxBytes-len(breadcrumb)-1, 0)
	if len(out) > budget {
		out = out[len(out)-budget:]
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return breadcrumb
	}
	return out + "\n" + breadcrumb
}

// tailOutput is the internal alias used by the reap path.
func tailOutput(path string, maxBytes int) string {
	return TailOutput(path, maxBytes)
}
