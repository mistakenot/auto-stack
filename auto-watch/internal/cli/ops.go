package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-shared/version"
	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/doctor"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/rpcmethods"
	"github.com/mistakenot/auto-watch/internal/rpcserver"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/textout"
	"github.com/mistakenot/auto-watch/internal/timeparse"
	"github.com/spf13/cobra"
)

func newStartCmd(application *app.App) *cobra.Command {
	var once bool
	var rpcAddr string
	var readyFile string
	var hookAddr string
	var ctlEvents bool
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
					return &ExitError{Code: 1, Err: errors.New("autowatch daemon is already running; inspect auto watch status or stop the existing daemon")}
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

			hub := bus.NewHub()
			service := daemon.New(db, application.Backend, cmd.OutOrStdout(), application.Now, hub)
			if once {
				if err := writePIDMetadata(); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
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

			// Compute default RPC address if not provided.
			if rpcAddr == "" {
				watchDir, wdErr := config.WatchDir()
				if wdErr != nil {
					return &ExitError{Code: 1, Err: wdErr}
				}
				rpcAddr = "unix://" + filepath.Join(watchDir, "rpc.sock")
			}

			// Build the RPC handlers (hub created above so the daemon service
			// can emit watch.task.* lifecycle events).
			startedAt := time.Now()
			hostID := sharedconfig.HostIDQuietly()
			regProvider := func() sharedconfig.ProjectsConfig {
				p, _ := sharedconfig.ProjectsConfigPath()
				cfg, err := sharedconfig.LoadProjects(p)
				if err != nil {
					return sharedconfig.ProjectsConfig{Projects: []sharedconfig.ProjectRef{}}
				}
				return cfg
			}
			handlers := rpcmethods.New(hostID, version.Version, startedAt, hub, ctlEvents, regProvider)

			// Bind both listeners before entering the run loop (fail fast).
			rpcLn, lnErr := transport.Listen(rpcAddr)
			if lnErr != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("bind RPC listener: %w", lnErr)}
			}
			defer func() { _ = rpcLn.Close() }()

			hookLn, hlErr := net.Listen("tcp", hookAddr)
			if hlErr != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("bind hook listener: %w", hlErr)}
			}
			defer func() { _ = hookLn.Close() }()

			// Emit ctl.health if control-plane events are enabled.
			if ctlEvents {
				ev, evErr := bus.NewEvent(bus.TypeCtlHealth, "auto/watch/daemon", nil)
				if evErr == nil {
					hub.Broadcast(ev)
				}
			}

			// Write ready-file with resolved listener addresses.
			if readyFile != "" {
				readyData := map[string]string{
					"addr":     rpcLn.Addr().String(),
					"hookAddr": hookLn.Addr().String(),
				}
				readyJSON, rjErr := json.Marshal(readyData)
				if rjErr != nil {
					return &ExitError{Code: 1, Err: rjErr}
				}
				readyJSON = append(readyJSON, '\n')
				if wfErr := os.WriteFile(readyFile, readyJSON, 0o644); wfErr != nil {
					return &ExitError{Code: 1, Err: wfErr}
				}
			}

			// Persist PID metadata with resolved listener addresses.
			if err := writePIDMetadataWithAddrs(rpcLn.Addr().String(), hookLn.Addr().String()); err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			errCh := make(chan error, 3)

			// Run initial tick.
			if err := service.Tick(ctx); err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// RPC server goroutine.
			rpcSrv := rpcserver.New(rpcLn, handlers, hub, ctlEvents)
			go func() { errCh <- rpcSrv.Serve(ctx) }()

			// HTTP hook-ingest server goroutine.
			hookSrv := &http.Server{
				Handler:           rpcserver.HookIngest(hub, ctlEvents),
				ReadHeaderTimeout: 10 * time.Second,
			}
			go func() {
				sErr := hookSrv.Serve(hookLn)
				if sErr == http.ErrServerClosed {
					errCh <- nil
				} else {
					errCh <- sErr
				}
			}()

			// Tick loop.
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_ = hookSrv.Shutdown(shutdownCtx)
					cancel()
					return nil
				case lErr := <-errCh:
					if lErr != nil {
						shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						_ = hookSrv.Shutdown(shutdownCtx)
						cancel()
						return &ExitError{Code: 1, Err: lErr}
					}
				case <-ticker.C:
					if tErr := service.Tick(ctx); tErr != nil {
						return &ExitError{Code: 1, Err: tErr}
					}
				}
			}
		},
	}
	cmd.Flags().BoolVar(&once, "once", false, "run one daemon tick then exit")
	_ = cmd.Flags().MarkHidden("once")
	cmd.Flags().StringVar(&rpcAddr, "rpc-addr", "", "RPC listener address (unix:///path or tcp://host:port; default unix socket in watch dir)")
	cmd.Flags().StringVar(&readyFile, "ready-file", "", "write {\"addr\":...,\"hookAddr\":...} to this file when listeners are bound")
	cmd.Flags().StringVar(&hookAddr, "hook-addr", "127.0.0.1:7787", "HTTP hook-ingest listener address")
	cmd.Flags().BoolVar(&ctlEvents, "ctl-events", false, "emit ctl.* control-plane events to the bus")
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
			settingsPath, err := config.ProjectsPath()
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
			service := daemon.New(db, application.Backend, nil, application.Now, nil)
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

func writePIDMetadataWithAddrs(rpcAddr, hookAddr string) error {
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
		"rpcAddr":   rpcAddr,
		"hookAddr":  hookAddr,
	}
	return textout.WriteJSONFile(pidPath, payload)
}
