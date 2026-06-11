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
	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/gitx"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/runner"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/textout"
)

var ErrDaemonAlreadyRunning = errors.New("daemon lock is already held")

type Service struct {
	Store   *store.Store
	Backend runner.Backend
	Output  io.Writer
	Now     func() time.Time

	workerWG sync.WaitGroup
}

type Lock struct {
	file *os.File
}

func New(db *store.Store, backend runner.Backend, output io.Writer, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		Store:   db,
		Backend: backend,
		Output:  output,
		Now:     now,
	}
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
	if errs := config.ValidateGlobalConfig(settings); len(errs) > 0 {
		return fmt.Errorf("global settings failed validation: %s", errs[0].Message)
	}

	projects := append([]model.ProjectRef(nil), settings.Projects...)
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })

	for _, project := range projects {
		if err := s.tickProject(ctx, now, &project); err != nil {
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
		if err := s.Store.MarkRunTerminal(ctx, run.ID, state, &code, now, message); err != nil {
			return err
		}
		eventType := "task_completed"
		level := "info"
		if state == model.RunFailed {
			eventType = "task_failed"
			level = "error"
		}
		if err := s.logEvent(ctx, &store.EventInput{
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
		}); err != nil {
			return err
		}
	}

	pending, err := s.Store.ListPendingBefore(ctx, now.Add(-5*time.Minute))
	if err != nil {
		return err
	}
	for i := range pending {
		run := &pending[i]
		message := "worker did not start"
		if err := s.Store.MarkRunTerminal(ctx, run.ID, model.RunFailed, nil, now, message); err != nil {
			return err
		}
		if err := s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "error",
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
		return s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "info",
			EventType: "trigger_evaluated",
			ProjectID: projectID,
			TriggerID: triggerID,
			Message:   "trigger already processed for this minute",
			Metadata: map[string]any{
				"outcome":      "already_processed",
				"cron":         trigger.When,
				"resource_key": "cron:" + triggerID,
			},
		})
	}
	schedule, err := config.ParseCron(trigger.When)
	if err != nil {
		return err
	}
	prev := tickMinuteLocal.Add(-1 * time.Minute)
	due := schedule.Next(prev).Equal(tickMinuteLocal)
	if !due {
		return s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "info",
			EventType: "trigger_evaluated",
			ProjectID: projectID,
			TriggerID: triggerID,
			Message:   "trigger not due",
			Metadata: map[string]any{
				"outcome":      "not_due",
				"cron":         trigger.When,
				"resource_key": "cron:" + triggerID,
			},
		})
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
			return s.logEvent(ctx, &store.EventInput{
				Timestamp: now,
				Level:     "info",
				EventType: "trigger_evaluated",
				ProjectID: projectID,
				TriggerID: triggerID,
				Message:   "branch unchanged",
				Metadata: map[string]any{
					"outcome":      "branch_unchanged",
					"branch":       trigger.OnlyIfBranchChanged,
					"cron":         trigger.When,
					"resource_key": "cron:" + triggerID,
				},
			})
		}
	}

	launched := 0
	taskIDs := append([]string(nil), trigger.Tasks...)
	sort.Strings(taskIDs)
	for _, taskID := range taskIDs {
		task := tasks[taskID]
		runID, err := s.Store.ReserveRun(ctx, &store.ReserveRunInput{
			ProjectID:   projectID,
			ProjectPath: projectPath,
			TriggerID:   triggerID,
			TriggerType: trigger.Type,
			TaskID:      taskID,
			TaskType:    task.Type,
			ResourceKey: "cron:" + triggerID,
			StartedAt:   now,
		})
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
		launched++
		if err := s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "info",
			EventType: "task_reserved",
			ProjectID: projectID,
			TriggerID: triggerID,
			TaskID:    taskID,
			RunID:     &runID,
			Message:   fmt.Sprintf("reserved run %d", runID),
			Metadata: map[string]any{
				"resource_key": "cron:" + triggerID,
				"cron":         trigger.When,
			},
		}); err != nil {
			return err
		}
		if err := s.startWorker(ctx, runID, task); err != nil {
			run, loadErr := s.Store.GetRun(ctx, runID)
			if loadErr == nil && run.State == model.RunPending {
				_ = s.failRun(ctx, &run, "", err)
			}
			_ = s.logEvent(ctx, &store.EventInput{
				Timestamp: s.Now(),
				Level:     "error",
				EventType: "system_warning",
				ProjectID: projectID,
				TriggerID: triggerID,
				TaskID:    taskID,
				RunID:     &runID,
				Message:   "worker startup failed",
				Metadata:  map[string]any{"error": err.Error()},
			})
		}
	}

	if err := s.Store.UpsertTriggerState(ctx, &model.TriggerStateRecord{
		ProjectID:     projectID,
		TriggerID:     triggerID,
		LastDueMinute: tickMinute,
		LastBranchSHA: currentSHA,
		UpdatedAt:     now,
	}); err != nil {
		return err
	}
	outcome := "launched"
	if launched == 0 {
		outcome = "dedup"
	}
	return s.logEvent(ctx, &store.EventInput{
		Timestamp: now,
		Level:     "info",
		EventType: "trigger_evaluated",
		ProjectID: projectID,
		TriggerID: triggerID,
		Message:   "trigger processed with outcome " + outcome,
		Metadata: map[string]any{
			"outcome":      outcome,
			"cron":         trigger.When,
			"resource_key": "cron:" + triggerID,
			"launched":     launched,
		},
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
		return s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "info",
			EventType: "trigger_evaluated",
			ProjectID: projectID,
			TriggerID: triggerID,
			Message:   "file_created trigger seeded baseline snapshot",
			Metadata: map[string]any{
				"outcome":      "seeded",
				"glob":         trigger.Glob,
				"resource_key": resourceKey,
				"file_count":   len(currentFiles),
			},
		})
	}

	if len(newFiles) == 0 {
		return s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "info",
			EventType: "trigger_evaluated",
			ProjectID: projectID,
			TriggerID: triggerID,
			Message:   "no new files detected",
			Metadata: map[string]any{
				"outcome":      "no_new_files",
				"glob":         trigger.Glob,
				"resource_key": resourceKey,
			},
		})
	}

	// Fire: launch linked tasks.
	launched := 0
	taskIDs := append([]string(nil), trigger.Tasks...)
	sort.Strings(taskIDs)
	for _, taskID := range taskIDs {
		task := tasks[taskID]
		runID, err := s.Store.ReserveRun(ctx, &store.ReserveRunInput{
			ProjectID:   projectID,
			ProjectPath: projectPath,
			TriggerID:   triggerID,
			TriggerType: trigger.Type,
			TaskID:      taskID,
			TaskType:    task.Type,
			ResourceKey: resourceKey,
			StartedAt:   now,
		})
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
		launched++
		if err := s.logEvent(ctx, &store.EventInput{
			Timestamp: now,
			Level:     "info",
			EventType: "task_reserved",
			ProjectID: projectID,
			TriggerID: triggerID,
			TaskID:    taskID,
			RunID:     &runID,
			Message:   fmt.Sprintf("reserved run %d for %d new file(s)", runID, len(newFiles)),
			Metadata: map[string]any{
				"resource_key": resourceKey,
				"glob":         trigger.Glob,
				"new_files":    newFiles,
			},
		}); err != nil {
			return err
		}
		if err := s.startWorker(ctx, runID, task); err != nil {
			run, loadErr := s.Store.GetRun(ctx, runID)
			if loadErr == nil && run.State == model.RunPending {
				_ = s.failRun(ctx, &run, "", err)
			}
			_ = s.logEvent(ctx, &store.EventInput{
				Timestamp: s.Now(),
				Level:     "error",
				EventType: "system_warning",
				ProjectID: projectID,
				TriggerID: triggerID,
				TaskID:    taskID,
				RunID:     &runID,
				Message:   "worker startup failed",
				Metadata:  map[string]any{"error": err.Error()},
			})
		}
	}

	outcome := "launched"
	if launched == 0 {
		outcome = "dedup"
	}
	return s.logEvent(ctx, &store.EventInput{
		Timestamp: now,
		Level:     "info",
		EventType: "trigger_evaluated",
		ProjectID: projectID,
		TriggerID: triggerID,
		Message:   fmt.Sprintf("file_created trigger fired for %d new file(s)", len(newFiles)),
		Metadata: map[string]any{
			"outcome":      outcome,
			"glob":         trigger.Glob,
			"resource_key": resourceKey,
			"new_files":    newFiles,
			"launched":     launched,
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
	return s.logEvent(ctx, &store.EventInput{
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
	})
}

func (s *Service) failRun(ctx context.Context, run *model.RunRecord, worktreePath string, runErr error) error {
	now := s.Now()
	if err := s.Store.MarkRunTerminal(ctx, run.ID, model.RunFailed, nil, now, runErr.Error()); err != nil {
		return err
	}
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
	return s.logEvent(ctx, &store.EventInput{
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
	})
}

func (s *Service) logValidationErrors(ctx context.Context, now time.Time, projectID, triggerID, taskID, eventType string, errs []model.ValidationError) error {
	for _, validationErr := range errs {
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
