package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/textout"
	"github.com/mistakenot/auto-watch/internal/timeparse"
	"github.com/spf13/cobra"
)

// validRunStates and validFailureReasons are the closed vocabularies the `run
// list` --state and --reason filters validate against. A filter value outside
// the set is a usage error that lists the valid values.
var validRunStates = []string{
	string(model.RunPending),
	string(model.RunRunning),
	string(model.RunCompleted),
	string(model.RunFailed),
}

var validFailureReasons = []string{
	"exit_nonzero",
	"exit_unrecorded",
	"session_lost",
	"timed_out",
	"start_failed",
}

// runTimeLayout is the UTC, second-precision timestamp format used throughout
// the run resource's JSON and text output.
const runTimeLayout = "2006-01-02T15:04:05Z"

// runListRow is the metadata rung returned by `run list` — ids plus the fields
// an operator scans first. exit_code, failure_reason and completed_at are
// pointers so a running run serialises them as explicit null rather than
// omitting them.
type runListRow struct {
	ID            int64   `json:"id"`
	Project       string  `json:"project"`
	Trigger       string  `json:"trigger"`
	Task          string  `json:"task"`
	State         string  `json:"state"`
	FailureReason *string `json:"failure_reason"`
	ExitCode      *int    `json:"exit_code"`
	Backend       string  `json:"backend"`
	StartedAt     string  `json:"started_at"`
	CompletedAt   *string `json:"completed_at"`
}

// runBinding mirrors the opaque (manager, target) pair the run executed on.
type runBinding struct {
	Manager string `json:"manager"`
	Target  string `json:"target"`
}

// runJSON is the CLI-owned, full-fidelity projection of a RunRecord returned by
// `run get`. It is deliberately NOT the RPC rpcmethods.RunView: it exposes every
// stored field plus a byte-capped output_tail, with explicit null semantics.
type runJSON struct {
	ID            int64      `json:"id"`
	ProjectID     string     `json:"project_id"`
	ProjectPath   string     `json:"project_path"`
	TriggerID     string     `json:"trigger_id"`
	TriggerType   string     `json:"trigger_type"`
	TaskID        string     `json:"task_id"`
	TaskType      string     `json:"task_type"`
	ResourceKey   string     `json:"resource_key"`
	Branch        string     `json:"branch"`
	State         string     `json:"state"`
	FailureReason *string    `json:"failure_reason"`
	ExitCode      *int       `json:"exit_code"`
	StartedAt     string     `json:"started_at"`
	CompletedAt   *string    `json:"completed_at"`
	DeadlineAt    *string    `json:"deadline_at"`
	Binding       runBinding `json:"binding"`
	RuntimeDir    string     `json:"runtime_dir"`
	OutputPath    string     `json:"output_path"`
	ExitPath      string     `json:"exit_path"`
	WorktreePath  *string    `json:"worktree_path"`
	ErrorMessage  string     `json:"error_message"`
	OutputTail    string     `json:"output_tail"`
}

func newRunCmd(application *app.App) *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Inspect runs (JSON by default; use --text for a table/record)",
	}
	runCmd.AddCommand(
		newRunListCmd(application),
		newRunGetCmd(),
	)
	return runCmd
}

func newRunListCmd(application *app.App) *cobra.Command {
	var (
		textOutput bool
		stateF     string
		taskF      string
		triggerF   string
		projectF   string
		reasonF    string
		sinceF     string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List runs, newest first (JSON by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := &store.RunListFilter{
				State:     config.NormalizeID(stateF),
				TaskID:    config.NormalizeID(taskF),
				TriggerID: config.NormalizeID(triggerF),
				ProjectID: config.NormalizeID(projectF),
				Reason:    config.NormalizeID(reasonF),
			}
			if filter.State != "" && !slices.Contains(validRunStates, filter.State) {
				return &ExitError{Code: 1, Err: fmt.Errorf("invalid --state %q; valid values: %s", filter.State, strings.Join(validRunStates, ", "))}
			}
			if filter.Reason != "" && !slices.Contains(validFailureReasons, filter.Reason) {
				return &ExitError{Code: 1, Err: fmt.Errorf("invalid --reason %q; valid values: %s", filter.Reason, strings.Join(validFailureReasons, ", "))}
			}
			if strings.TrimSpace(sinceF) != "" {
				parsed, err := timeparse.ParseSince(nowUTC(application.Now), sinceF)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				filter.Since = &parsed
			}

			db, err := openStore(cmd.Context())
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			defer func() { _ = db.Close() }()

			runs, err := db.ListRunsFiltered(cmd.Context(), filter)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			rows := make([]runListRow, 0, len(runs))
			for i := range runs {
				rows = append(rows, toRunListRow(&runs[i]))
			}

			if textOutput {
				writeRunListText(cmd.OutOrStdout(), rows)
				return nil
			}
			if err := textout.WriteJSON(cmd.OutOrStdout(), rows); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&textOutput, "text", false, "print a human-readable table instead of JSON")
	cmd.Flags().StringVar(&stateF, "state", "", "filter by run state")
	cmd.Flags().StringVar(&taskF, "task", "", "filter by task id")
	cmd.Flags().StringVar(&triggerF, "trigger", "", "filter by trigger id")
	cmd.Flags().StringVar(&projectF, "project", "", "filter by project id")
	cmd.Flags().StringVar(&reasonF, "reason", "", "filter by failure reason")
	cmd.Flags().StringVar(&sinceF, "since", "", "only runs started since the given duration or timestamp")
	return cmd
}

func newRunGetCmd() *cobra.Command {
	var textOutput bool
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show a single run in full (JSON by default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(strings.TrimSpace(args[0]), 10, 64)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("invalid run id %q", args[0])}
			}
			db, err := openStore(cmd.Context())
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			defer func() { _ = db.Close() }()

			run, err := db.GetRun(cmd.Context(), id)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return &ExitError{Code: 1, Err: fmt.Errorf("run %d not found — list runs with: auto watch run list", id)}
				}
				return &ExitError{Code: 1, Err: err}
			}

			projection := toRunJSON(&run)
			if textOutput {
				writeRunGetText(cmd.OutOrStdout(), &projection)
				return nil
			}
			if err := textout.WriteJSON(cmd.OutOrStdout(), &projection); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&textOutput, "text", false, "print a human-readable record instead of JSON")
	return cmd
}

func toRunListRow(run *model.RunRecord) runListRow {
	row := runListRow{
		ID:        run.ID,
		Project:   run.ProjectID,
		Trigger:   run.TriggerID,
		Task:      run.TaskID,
		State:     string(run.State),
		Backend:   run.Backend,
		StartedAt: run.StartedAt.UTC().Format(runTimeLayout),
	}
	if run.FailureReason != "" {
		reason := run.FailureReason
		row.FailureReason = &reason
	}
	if run.ExitCode != nil {
		code := *run.ExitCode
		row.ExitCode = &code
	}
	if !run.CompletedAt.IsZero() {
		completed := run.CompletedAt.UTC().Format(runTimeLayout)
		row.CompletedAt = &completed
	}
	return row
}

func toRunJSON(run *model.RunRecord) runJSON {
	out := runJSON{
		ID:           run.ID,
		ProjectID:    run.ProjectID,
		ProjectPath:  run.ProjectPath,
		TriggerID:    run.TriggerID,
		TriggerType:  run.TriggerType,
		TaskID:       run.TaskID,
		TaskType:     run.TaskType,
		ResourceKey:  run.ResourceKey,
		Branch:       run.Branch,
		State:        string(run.State),
		Binding:      runBinding{Manager: run.Backend, Target: run.BackendTarget},
		RuntimeDir:   run.RuntimeDir,
		OutputPath:   run.OutputPath,
		ExitPath:     run.ExitPath,
		ErrorMessage: run.ErrorMessage,
		StartedAt:    run.StartedAt.UTC().Format(runTimeLayout),
		OutputTail:   daemon.TailOutput(run.OutputPath, daemon.OutputTailMaxBytes),
	}
	if run.FailureReason != "" {
		reason := run.FailureReason
		out.FailureReason = &reason
	}
	if run.ExitCode != nil {
		code := *run.ExitCode
		out.ExitCode = &code
	}
	if !run.CompletedAt.IsZero() {
		completed := run.CompletedAt.UTC().Format(runTimeLayout)
		out.CompletedAt = &completed
	}
	if run.DeadlineAt != nil {
		deadline := run.DeadlineAt.UTC().Format(runTimeLayout)
		out.DeadlineAt = &deadline
	}
	if run.WorktreePath != "" {
		worktree := run.WorktreePath
		out.WorktreePath = &worktree
	}
	return out
}

func writeRunListText(w interface{ Write([]byte) (int, error) }, rows []runListRow) {
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", "ID", "STATE", "TASK", "TRIGGER", "PROJECT", "BACKEND", "REASON", "STARTED")
	for i := range rows {
		row := rows[i]
		reason := "-"
		if row.FailureReason != nil {
			reason = *row.FailureReason
		}
		exit := ""
		if row.ExitCode != nil {
			exit = " exit=" + strconv.Itoa(*row.ExitCode)
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s%s\n",
			row.ID, row.State, row.Task, row.Trigger, row.Project, row.Backend, reason, row.StartedAt, exit)
	}
}

func writeRunGetText(w interface{ Write([]byte) (int, error) }, run *runJSON) {
	fields := [][2]string{
		{"id", strconv.FormatInt(run.ID, 10)},
		{"project_id", run.ProjectID},
		{"project_path", run.ProjectPath},
		{"trigger_id", run.TriggerID},
		{"trigger_type", run.TriggerType},
		{"task_id", run.TaskID},
		{"task_type", run.TaskType},
		{"resource_key", run.ResourceKey},
		{"branch", run.Branch},
		{"state", run.State},
		{"failure_reason", strOrNull(run.FailureReason)},
		{"exit_code", intPtrOrNull(run.ExitCode)},
		{"started_at", run.StartedAt},
		{"completed_at", strOrNull(run.CompletedAt)},
		{"deadline_at", strOrNull(run.DeadlineAt)},
		{"binding", fmt.Sprintf("%s/%s", run.Binding.Manager, run.Binding.Target)},
		{"runtime_dir", run.RuntimeDir},
		{"output_path", run.OutputPath},
		{"exit_path", run.ExitPath},
		{"worktree_path", strOrNull(run.WorktreePath)},
		{"error_message", run.ErrorMessage},
		{"output_tail", run.OutputTail},
	}
	for _, f := range fields {
		fmt.Fprintf(w, "%s: %s\n", f[0], f[1])
	}
}

func strOrNull(s *string) string {
	if s == nil {
		return "null"
	}
	return *s
}

func intPtrOrNull(v *int) string {
	if v == nil {
		return "null"
	}
	return strconv.Itoa(*v)
}
