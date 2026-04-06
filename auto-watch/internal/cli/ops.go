package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/doctor"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/textout"
	"github.com/mistakenot/auto-watch/internal/timeparse"
	"github.com/spf13/cobra"
)

func newStartCmd(application *app.App) *cobra.Command {
	var once bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the autowatch daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.EnsureGlobalDirs(); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			lockPath, err := config.LockPath()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			lock, err := daemon.AcquireLock(lockPath)
			if err != nil {
				if errors.Is(err, daemon.ErrDaemonAlreadyRunning) {
					return &ExitError{Code: 1, Err: errors.New("autowatch daemon is already running; inspect autowatch status or stop the existing daemon")}
				}
				return &ExitError{Code: 1, Err: err}
			}
			defer func() { _ = lock.Release() }()

			db, err := openStore(cmd.Context())
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			defer func() { _ = db.Close() }()

			status, checks := doctor.Run(cmd.Context(), application.CWD)
			if status != "ok" || doctor.HasFailures(checks) {
				for _, check := range checks {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s - %s\n", check.Name, check.Status, check.Message)
					if check.Remediation != "" {
						fmt.Fprintf(cmd.ErrOrStderr(), "  remediation: %s\n", check.Remediation)
					}
				}
				return &ExitError{Code: 1, Err: errors.New("doctor checks failed")}
			}

			if err := writePIDMetadata(); err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			service := daemon.New(db, application.Backend, cmd.OutOrStdout(), application.Now)
			if once {
				if err := service.Tick(cmd.Context()); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				service.WaitWorkers()
				time.Sleep(250 * time.Millisecond)
				if err := service.Reap(cmd.Context()); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				return nil
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if err := service.Tick(ctx); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					if err := service.Tick(ctx); err != nil {
						return &ExitError{Code: 1, Err: err}
					}
				}
			}
		},
	}
	cmd.Flags().BoolVar(&once, "once", false, "run one daemon tick then exit")
	_ = cmd.Flags().MarkHidden("once")
	return cmd
}

func newDoctorCmd(application *app.App) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check daemon prerequisites and config health",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, checks := doctor.Run(cmd.Context(), application.CWD)
			if jsonOutput {
				if err := textout.WriteJSON(cmd.OutOrStdout(), map[string]any{"status": status, "checks": checks}); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			} else {
				for _, check := range checks {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s - %s\n", check.Name, check.Status, check.Message)
					if check.Remediation != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "  remediation: %s\n", check.Remediation)
					}
				}
			}
			if doctor.HasFailures(checks) {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output JSON")
	return cmd
}

func newLogsCmd(application *app.App) *cobra.Command {
	var limit int
	var projectID string
	var taskID string
	var level string
	var since string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Query autowatch event logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openStore(cmd.Context())
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			defer func() { _ = db.Close() }()
			var sinceTime *time.Time
			if since != "" {
				parsed, err := timeparse.ParseSince(nowUTC(application.Now), since)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				sinceTime = &parsed
			}
			events, err := db.ListEvents(cmd.Context(), &store.EventFilter{
				Limit:     limit,
				ProjectID: config.NormalizeID(projectID),
				TaskID:    config.NormalizeID(taskID),
				Level:     level,
				Since:     sinceTime,
			})
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if jsonOutput {
				if err := textout.WriteJSON(cmd.OutOrStdout(), map[string]any{"events": events}); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				return nil
			}
			for i := range events {
				fmt.Fprintln(cmd.OutOrStdout(), textout.FormatEventLine(&events[i]))
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "n", "n", 50, "number of log rows to return")
	cmd.Flags().StringVar(&projectID, "project", "", "filter by project id")
	cmd.Flags().StringVar(&taskID, "task", "", "filter by task id")
	cmd.Flags().StringVar(&level, "level", "", "filter by log level")
	cmd.Flags().StringVar(&since, "since", "", "return rows since the given duration or timestamp")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output JSON")
	return cmd
}

func newStatusCmd(application *app.App) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon and project status",
		RunE: func(cmd *cobra.Command, args []string) error {
			settingsPath, err := config.SettingsPath()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			settings, err := config.LoadGlobalConfig(settingsPath)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if errs := config.ValidateGlobalConfig(settings); len(errs) > 0 {
				return &ExitError{Code: 1, Err: fmt.Errorf("global settings are invalid: %s", errs[0].Message)}
			}
			lockPath, err := config.LockPath()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			lockHeld, err := daemon.IsLockHeld(lockPath)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			db, err := openStore(cmd.Context())
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			defer func() { _ = db.Close() }()
			counts, err := db.RecentRunCounts(cmd.Context(), nowUTC(application.Now).Add(-24*time.Hour))
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			healthStatus, issues, err := collectHealth(cmd.Context(), db, application.Now)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			projectTriggers := map[string]int{}
			totalTriggers := 0
			projects := make([]model.ProjectRef, 0, len(settings.Projects))
			for _, project := range settings.Projects {
				projects = append(projects, project)
				cfg, err := config.LoadProjectConfig(project.Path)
				if err != nil {
					continue
				}
				projectTriggers[project.ID] = len(cfg.Triggers)
				totalTriggers += len(cfg.Triggers)
			}
			sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
			payload := map[string]any{
				"daemon_running": lockHeld,
				"projects":       projects,
				"trigger_counts": map[string]any{"total": totalTriggers, "by_project": projectTriggers},
				"run_counts_24h": counts,
				"health": map[string]any{
					"status":     healthStatus,
					"issueCount": len(issues),
				},
			}
			if jsonOutput {
				if err := textout.WriteJSON(cmd.OutOrStdout(), payload); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "daemon: %v\n", lockHeld)
			fmt.Fprintf(cmd.OutOrStdout(), "projects: %d\n", len(projects))
			fmt.Fprintf(cmd.OutOrStdout(), "triggers: %d\n", totalTriggers)
			fmt.Fprintf(cmd.OutOrStdout(), "runs last 24h: pending=%d running=%d completed=%d failed=%d\n", counts["pending"], counts["running"], counts["completed"], counts["failed"])
			fmt.Fprintf(cmd.OutOrStdout(), "health: %s (%d issues)\n", healthStatus, len(issues))
			for _, project := range projects {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\ttriggers=%d\n", project.ID, project.Path, projectTriggers[project.ID])
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output JSON")
	return cmd
}

func newHealthCmd(application *app.App) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Show runtime health warnings",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openStore(cmd.Context())
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			defer func() { _ = db.Close() }()
			status, issues, err := collectHealth(cmd.Context(), db, application.Now)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if jsonOutput {
				if err := textout.WriteJSON(cmd.OutOrStdout(), map[string]any{"status": status, "issues": issues}); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "status: %s\n", status)
				for _, issue := range issues {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", issue.Type, issue.ProjectID, issue.TaskID, issue.Message)
				}
			}
			if len(issues) > 0 {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output JSON")
	return cmd
}

func newCleanCmd(application *app.App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove expired autowatch worktrees",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openStore(cmd.Context())
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			defer func() { _ = db.Close() }()
			service := daemon.New(db, application.Backend, nil, application.Now)
			if err := service.Clean(cmd.Context(), force); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if force {
				fmt.Fprintln(cmd.OutOrStdout(), "Forced cleanup completed.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Expired worktree cleanup completed.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "kill active sessions and remove their worktrees too")
	return cmd
}

func collectHealth(ctx context.Context, db *store.Store, now func() time.Time) (string, []model.HealthIssue, error) {
	current := nowUTC(now)
	issues := []model.HealthIssue{}
	running, err := db.OldRunningRuns(ctx, current.Add(-2*time.Hour))
	if err != nil {
		return "", nil, err
	}
	for i := range running {
		issues = append(issues, model.HealthIssue{
			Type:      "old_running_run",
			Severity:  "warn",
			Message:   fmt.Sprintf("running for %s", current.Sub(running[i].StartedAt).Round(time.Minute)),
			ProjectID: running[i].ProjectID,
			TaskID:    running[i].TaskID,
			RunID:     running[i].ID,
		})
	}
	pending, err := db.OldPendingRuns(ctx, current.Add(-5*time.Minute))
	if err != nil {
		return "", nil, err
	}
	for i := range pending {
		issues = append(issues, model.HealthIssue{
			Type:      "old_pending_run",
			Severity:  "warn",
			Message:   fmt.Sprintf("pending for %s", current.Sub(pending[i].StartedAt).Round(time.Minute)),
			ProjectID: pending[i].ProjectID,
			TaskID:    pending[i].TaskID,
			RunID:     pending[i].ID,
		})
	}
	failedGroups, err := db.FailedRunGroups(ctx, current.Add(-24*time.Hour), 3)
	if err != nil {
		return "", nil, err
	}
	issues = append(issues, failedGroups...)
	dedupGroups, err := db.DedupSkipGroups(ctx, current.Add(-24*time.Hour), 3)
	if err != nil {
		return "", nil, err
	}
	issues = append(issues, dedupGroups...)
	status := "ok"
	if len(issues) > 0 {
		status = "warn"
	}
	return status, issues, nil
}

func writePIDMetadata() error {
	pidPath, err := config.PIDPath()
	if err != nil {
		return err
	}
	autoDir, err := config.AutoDir()
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	payload := map[string]any{
		"pid":       os.Getpid(),
		"startedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"hostId":    hostname,
		"hostPath":  filepath.Join(autoDir, "host.json"),
	}
	return textout.WriteJSONFile(pidPath, payload)
}
