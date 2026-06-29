package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bmatcuk/doublestar/v4"
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

type Service struct {
	Store   *store.Store
	Backend runner.Backend
	Output  io.Writer
	Now     func() time.Time

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

func New(db *store.Store, backend runner.Backend, output io.Writer, now func() time.Time, hub *bus.Hub) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		Store:              db,
		Backend:            backend,
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
		if run.SessionName != "" {
			_ = s.Backend.Kill(ctx, runner.Handle{
				SessionName: run.SessionName,
				ExitPath:    run.ExitPath,
				OutputPath:  run.OutputPath,
			})
		}

	case model.RunPending:
		// fail without kill
	}

	now := s.Now()
	msg := "cancelled via task.cancel"
	if err := s.Store.MarkRunTerminal(ctx, run.ID, model.RunFailed, nil, now, msg); err != nil {
		return "", err
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
	s.emitWatchTask(ctx, bus.TypeWatchTaskFailed, &run, nil, msg)

	if run.WorktreePath != "" {
		_ = s.removeWorktree(ctx, &run, "cancel_cleanup")
	}

	return model.RunFailed, nil
}

// emitWatchTask broadcasts a watch.task.* data-plane event for the given run.
// These events are always-on (not gated by --ctl-events); a nil hub is a no-op.
func (s *Service) emitWatchTask(_ context.Context, typ string, run *model.RunRecord, exitCode *int, message string) {
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
			// Dedup so the same invalid-config warning is logged once, not every
			// tick (Phase 3 noise reduction). The error text distinguishes warnings.
			if s.warningSeen("config_warning:" + project.ID + ":" + err.Error()) {
				continue
			}
			if logErr := s.logEvent(ctx, &store.EventInput{
				Timestamp: now,
				Level:     "warn",
				EventType: "config_warning",
				ProjectID: project.ID,
				Message:   "project skipped due to invalid config",
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
		if strings.TrimSpace(run.ExitPath) == "" {
			continue
		}
		data, err := os.ReadFile(run.ExitPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read exit code for run %d: %w", run.ID, err)
		}
		code, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return fmt.Errorf("parse exit code for run %d: %w", run.ID, err)
		}
		if run.SessionName != "" {
			exists, err := s.Backend.SessionExists(ctx, run.SessionName)
			if err != nil {
				return err
			}
			if exists {
				if err := s.Backend.Kill(ctx, runner.Handle{SessionName: run.SessionName, ExitPath: run.ExitPath, OutputPath: run.OutputPath}); err != nil {
					return err
				}
			}
		}
		state := model.RunCompleted
		message := ""
		if code != 0 {
			state = model.RunFailed
			message = tailOutput(run.OutputPath, 200)
			if message == "" {
				message = fmt.Sprintf("run exited with code %d", code)
			}
		}
		eventType := "task_completed"
		level := "info"
		watchType := bus.TypeWatchTaskCompleted
		if state == model.RunFailed {
			eventType = "task_failed"
			level = "error"
			watchType = bus.TypeWatchTaskFailed
		}

		s.dispatchMu.Lock()
		fresh, reErr := s.Store.GetRun(ctx, run.ID)
		if reErr != nil || fresh.State != model.RunRunning {
			s.dispatchMu.Unlock()
			continue
		}
		if err := s.Store.MarkRunTerminal(ctx, run.ID, state, &code, now, message); err != nil {
			s.dispatchMu.Unlock()
			return err
		}
		// Maintain failure backoff (dispatchMu already held): a failed run extends
		// the projectID:taskID streak; a successful run clears it.
		s.recordRunOutcome(run.ProjectID, run.TaskID, state, now)
		logErr := s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     level,
			EventType: eventType,
			ProjectID: run.ProjectID,
			TriggerID: run.TriggerID,
			TaskID:    run.TaskID,
			RunID:     &run.ID,
			Message:   fmt.Sprintf("run %d finished with exit code %d", run.ID, code),
			Metadata: map[string]any{
				"exit_code":    code,
				"session_name": run.SessionName,
				"resource_key": run.ResourceKey,
			},
		})
		s.emitWatchTask(ctx, watchType, &fresh, &code, fmt.Sprintf("run %d finished with exit code %d", run.ID, code))
		s.dispatchMu.Unlock()
		if logErr != nil {
			return logErr
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
		if err := s.Store.MarkRunTerminal(ctx, run.ID, model.RunFailed, nil, now, message); err != nil {
			s.dispatchMu.Unlock()
			return err
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
		s.emitWatchTask(ctx, bus.TypeWatchTaskFailed, &fresh, nil, message)
		s.dispatchMu.Unlock()
		if logErr != nil {
			return logErr
		}
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
			if run.SessionName != "" {
				if err := s.Backend.Kill(ctx, runner.Handle{SessionName: run.SessionName, ExitPath: run.ExitPath, OutputPath: run.OutputPath}); err != nil {
					return err
				}
			}
			message := "killed by auto watch clean --force"
			if err := s.Store.MarkRunTerminal(ctx, run.ID, model.RunFailed, nil, now, message); err != nil {
				return err
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
				return err
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
			return err
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

	// Checkpoint the WAL after pruning so the checkpoint also covers the deletes,
	// bounding WAL growth.
	if err := s.Store.WALCheckpoint(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Service) tickProject(ctx context.Context, now time.Time, project *model.ProjectRef) error {
	projectCfg, err := config.LoadProjectConfig(project.Path)
	if err != nil {
		return err
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
		if errs := config.ValidateTaskEntry(taskID, task); len(errs) > 0 {
			if err := s.logValidationErrors(ctx, now, projectID, "", "", "config_warning", errs); err != nil {
				return err
			}
			continue
		}
		validTasks[taskID] = task
	}
	if errs := config.ValidateProjectConfig(model.ProjectConfig{ID: projectID, Tasks: validTasks, Triggers: map[string]model.TriggerDef{}}); len(errs) > 0 {
		return fmt.Errorf("project id is invalid: %s", errs[0].Message)
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
			return err
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

	taskIDs := append([]string(nil), trigger.Tasks...)
	sort.Strings(taskIDs)
	for _, taskID := range taskIDs {
		if skip, nextEligible := s.shouldBackoff(projectID, taskID, now); skip {
			fmt.Fprintf(os.Stderr, "task_backoff project=%s task=%s next_eligible=%s\n", projectID, taskID, nextEligible.Format(time.RFC3339))
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
				continue
			}
			return err
		}
	}

	// Trigger processing produces no event (Phase 3 noise reduction); dispatch,
	// dedup-skip, and backoff outcomes are observable via task_* events / stderr.
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

	// Fire: launch linked tasks.
	taskIDs := append([]string(nil), trigger.Tasks...)
	sort.Strings(taskIDs)
	for _, taskID := range taskIDs {
		if skip, nextEligible := s.shouldBackoff(projectID, taskID, now); skip {
			fmt.Fprintf(os.Stderr, "task_backoff project=%s task=%s next_eligible=%s\n", projectID, taskID, nextEligible.Format(time.RFC3339))
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
				continue
			}
			return err
		}
	}

	// file_created firing produces no event (Phase 3 noise reduction); dispatch,
	// dedup-skip, and backoff outcomes are observable via task_* events / stderr.
	return nil
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

	scriptPath, err := runner.WriteLaunchScript(&runner.LaunchSpec{
		RunDir:      runDir,
		WorkDir:     workDir,
		TaskType:    task.Type,
		CommandFile: commandFile,
		PromptFile:  promptFile,
		OutputPath:  outputPath,
		ExitPath:    exitPath,
	})
	if err != nil {
		return s.failRun(ctx, &run, worktreePath, err)
	}

	sessionName := runner.ScheduledRunName(run.ID, run.TaskID)
	handle, err := s.Backend.Start(ctx, &runner.StartSpec{
		SessionName: sessionName,
		WorkDir:     workDir,
		ScriptPath:  scriptPath,
		ExitPath:    exitPath,
		OutputPath:  outputPath,
	})
	if err != nil {
		return s.failRun(ctx, &run, worktreePath, err)
	}
	if err := s.Store.UpdateRunStarted(ctx, run.ID, &store.RunStartUpdate{
		SessionName:  handle.SessionName,
		RuntimeDir:   runDir,
		OutputPath:   outputPath,
		ExitPath:     exitPath,
		WorktreePath: worktreePath,
		Branch:       branch,
	}); err != nil {
		_ = s.Backend.Kill(ctx, handle)
		return s.failRun(ctx, &run, worktreePath, err)
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
			"session_name":  handle.SessionName,
			"worktree_path": worktreePath,
			"resource_key":  run.ResourceKey,
			"branch":        branch,
		},
	}); err != nil {
		return err
	}
	run.SessionName = handle.SessionName
	run.WorktreePath = worktreePath
	run.Branch = branch
	s.emitWatchTask(ctx, bus.TypeWatchTaskStarted, &run, nil, "task started")
	return nil
}

func (s *Service) failRun(ctx context.Context, run *model.RunRecord, worktreePath string, runErr error) error {
	now := s.Now()
	if err := s.Store.MarkRunTerminal(ctx, run.ID, model.RunFailed, nil, now, runErr.Error()); err != nil {
		return err
	}
	// A startup/dispatch-time failure is marked terminal here and never reaches
	// Reap, so record it for failure backoff at this single chokepoint. All
	// callers (Dispatch, startWorker) hold dispatchMu, as recordRunOutcome
	// requires.
	s.recordRunOutcome(run.ProjectID, run.TaskID, model.RunFailed, now)
	if worktreePath != "" {
		run.WorktreePath = worktreePath
	}
	s.emitWatchTask(ctx, bus.TypeWatchTaskFailed, run, nil, runErr.Error())
	if worktreePath != "" {
		if err := s.removeWorktree(ctx, &model.RunRecord{
			ID:           run.ID,
			ProjectID:    run.ProjectID,
			TriggerID:    run.TriggerID,
			TaskID:       run.TaskID,
			ProjectPath:  run.ProjectPath,
			WorktreePath: worktreePath,
		}, "worker_failure_cleanup"); err != nil {
			return err
		}
	}
	return s.logEvent(ctx, &store.EventInput{
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
		},
	})
}

func (s *Service) removeWorktree(ctx context.Context, run *model.RunRecord, reason string) error {
	if strings.TrimSpace(run.WorktreePath) == "" {
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

func tailOutput(path string, maxLines int) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
