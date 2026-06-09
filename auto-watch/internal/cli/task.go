package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/gitx"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/runner"
	"github.com/mistakenot/auto-watch/internal/textout"
	"github.com/spf13/cobra"
)

func newTaskCmd(application *app.App) *cobra.Command {
	taskCmd := &cobra.Command{
		Use:   "task",
		Short: "Manage task definitions",
	}
	taskCmd.AddCommand(
		newTaskCreateCmd(application),
		newTaskListCmd(application),
		newTaskRunCmd(application),
		newTaskRemoveCmd(application),
	)
	return taskCmd
}

func newTaskCreateCmd(application *app.App) *cobra.Command {
	var id string
	var bashCommand string
	var claudePrompt string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create or update a task definition",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := config.NormalizeID(id)
			if taskID == "" {
				return &ExitError{Code: 1, Err: errors.New("--id is required")}
			}
			if (strings.TrimSpace(bashCommand) == "") == (strings.TrimSpace(claudePrompt) == "") {
				return &ExitError{Code: 1, Err: errors.New("exactly one of --bash or --claude must be provided")}
			}
			repoRoot, cfg, err := requireProjectConfig(application.CWD)
			if err != nil {
				return err
			}
			task := model.TaskDef{}
			if strings.TrimSpace(bashCommand) != "" {
				task.Type = "bash"
				task.Command = strings.TrimSpace(bashCommand)
			} else {
				task.Type = "claude"
				task.Prompt = strings.TrimSpace(claudePrompt)
			}
			cfg.Tasks[taskID] = task
			if err := saveValidatedProjectConfig(repoRoot, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved task %s\n", taskID)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&bashCommand, "bash", "", "bash command to run")
	cmd.Flags().StringVar(&claudePrompt, "claude", "", "Claude prompt to run")
	return cmd
}

func newTaskListCmd(application *app.App) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List task definitions",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := requireProjectConfig(application.CWD)
			if err != nil {
				return err
			}
			type taskRow struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Command string `json:"command,omitempty"`
				Prompt  string `json:"prompt,omitempty"`
				Preview string `json:"preview"`
			}
			rows := []taskRow{}
			errs := []model.ValidationError{}
			ids := make([]string, 0, len(cfg.Tasks))
			for id := range cfg.Tasks {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				task := cfg.Tasks[id]
				if validationErrs := config.ValidateTaskEntry(id, task); len(validationErrs) > 0 {
					errs = append(errs, validationErrs...)
					continue
				}
				row := taskRow{ID: id, Type: task.Type}
				if task.Type == "bash" {
					row.Command = task.Command
					row.Preview = textout.Preview(task.Command)
				} else {
					row.Prompt = task.Prompt
					row.Preview = textout.Preview(task.Prompt)
				}
				rows = append(rows, row)
			}
			if jsonOutput {
				payload := map[string]any{"tasks": rows, "errors": errs}
				if err := textout.WriteJSON(cmd.OutOrStdout(), payload); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			} else {
				for _, row := range rows {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", row.ID, row.Type, row.Preview)
				}
				writeValidationErrors(cmd.ErrOrStderr(), errs)
			}
			if len(errs) > 0 {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output JSON")
	return cmd
}

func newTaskRunCmd(application *app.App) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a task immediately in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, cfg, err := requireProjectConfig(application.CWD)
			if err != nil {
				return err
			}
			taskID := config.NormalizeID(id)
			task, ok := cfg.Tasks[taskID]
			if !ok {
				return &ExitError{Code: 1, Err: fmt.Errorf("task %q does not exist", taskID)}
			}
			if task.Type == "bash" {
				if err := runner.RunForeground(cmd.Context(), repoRoot, task.Type, task.Command, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) {
						return &ExitError{Code: exitErr.ExitCode()}
					}
					return &ExitError{Code: 1, Err: err}
				}
				return nil
			}

			branch, err := gitx.DefaultBranch(repoRoot)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if err := os.MkdirAll(config.WorktreesDir(repoRoot), 0o755); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			worktreePath := filepath.Join(config.WorktreesDir(repoRoot), fmt.Sprintf("manual-%d", time.Now().UnixNano()))
			if err := gitx.AddWorktree(repoRoot, worktreePath, branch); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			defer func() { _ = gitx.RemoveWorktree(repoRoot, worktreePath) }()

			prompt := runner.BuildPrompt(cfg.ID, "manual", "task-run", "manual:"+taskID, branch, task.Prompt)
			if err := runner.RunForeground(cmd.Context(), worktreePath, task.Type, prompt, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					return &ExitError{Code: exitErr.ExitCode()}
				}
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	return cmd
}

func newTaskRemoveCmd(application *app.App) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a task definition",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, cfg, err := requireProjectConfig(application.CWD)
			if err != nil {
				return err
			}
			taskID := config.NormalizeID(id)
			if _, ok := cfg.Tasks[taskID]; !ok {
				return &ExitError{Code: 1, Err: fmt.Errorf("task %q does not exist", taskID)}
			}
			delete(cfg.Tasks, taskID)
			if err := config.SaveProjectConfig(repoRoot, cfg); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed task %s\n", taskID)
			for triggerID, trigger := range cfg.Triggers {
				if slices.Contains(trigger.Tasks, taskID) {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: trigger %s still references %s. run auto watch trigger remove-task --trigger %s --task %s\n", triggerID, taskID, triggerID, taskID)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	return cmd
}
