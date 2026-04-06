package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/textout"
	"github.com/spf13/cobra"
)

func newTriggerCmd(application *app.App) *cobra.Command {
	triggerCmd := &cobra.Command{
		Use:   "trigger",
		Short: "Manage trigger definitions",
	}
	triggerCmd.AddCommand(
		newTriggerCreateCmd(application),
		newTriggerAddTaskCmd(application),
		newTriggerRemoveTaskCmd(application),
		newTriggerListCmd(application),
		newTriggerRemoveCmd(application),
	)
	return triggerCmd
}

func newTriggerCreateCmd(application *app.App) *cobra.Command {
	var id string
	var cronExpr string
	var onlyIfBranchChanged string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create or update a trigger definition",
		RunE: func(cmd *cobra.Command, args []string) error {
			triggerID := config.NormalizeID(id)
			if triggerID == "" {
				return &ExitError{Code: 1, Err: errors.New("--id is required")}
			}
			if strings.TrimSpace(cronExpr) == "" {
				return &ExitError{Code: 1, Err: errors.New("--cron is required")}
			}
			if _, err := config.ParseCron(cronExpr); err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("invalid cron expression: %w", err)}
			}
			repoRoot, cfg, err := requireProjectConfig(application.CWD)
			if err != nil {
				return err
			}
			cfg.Triggers[triggerID] = model.TriggerDef{
				Type:                "cron",
				When:                strings.TrimSpace(cronExpr),
				Tasks:               []string{},
				OnlyIfBranchChanged: strings.TrimSpace(onlyIfBranchChanged),
			}
			if err := saveValidatedProjectConfig(repoRoot, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved trigger %s\n", triggerID)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "trigger id")
	cmd.Flags().StringVar(&cronExpr, "cron", "", "5-field cron expression")
	cmd.Flags().StringVar(&onlyIfBranchChanged, "only-if-branch-changed", "", "only fire when this branch head changes")
	return cmd
}

func newTriggerAddTaskCmd(application *app.App) *cobra.Command {
	var triggerID string
	var taskID string
	cmd := &cobra.Command{
		Use:   "add-task",
		Short: "Link a task to a trigger",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, cfg, err := requireProjectConfig(application.CWD)
			if err != nil {
				return err
			}
			triggerID = config.NormalizeID(triggerID)
			taskID = config.NormalizeID(taskID)
			trigger, ok := cfg.Triggers[triggerID]
			if !ok {
				return &ExitError{Code: 1, Err: fmt.Errorf("trigger %q does not exist", triggerID)}
			}
			if _, ok := cfg.Tasks[taskID]; !ok {
				return &ExitError{Code: 1, Err: fmt.Errorf("task %q does not exist", taskID)}
			}
			trigger.Tasks = append(trigger.Tasks, taskID)
			trigger.Tasks = normalizeTaskSet(trigger.Tasks)
			cfg.Triggers[triggerID] = trigger
			if err := saveValidatedProjectConfig(repoRoot, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Linked task %s to trigger %s\n", taskID, triggerID)
			return nil
		},
	}
	cmd.Flags().StringVar(&triggerID, "trigger", "", "trigger id")
	cmd.Flags().StringVar(&taskID, "task", "", "task id")
	return cmd
}

func newTriggerRemoveTaskCmd(application *app.App) *cobra.Command {
	var triggerID string
	var taskID string
	cmd := &cobra.Command{
		Use:   "remove-task",
		Short: "Unlink a task from a trigger",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, cfg, err := requireProjectConfig(application.CWD)
			if err != nil {
				return err
			}
			triggerID = config.NormalizeID(triggerID)
			taskID = config.NormalizeID(taskID)
			trigger, ok := cfg.Triggers[triggerID]
			if !ok {
				return &ExitError{Code: 1, Err: fmt.Errorf("trigger %q does not exist", triggerID)}
			}
			filtered := make([]string, 0, len(trigger.Tasks))
			for _, linkedTask := range trigger.Tasks {
				if linkedTask != taskID {
					filtered = append(filtered, linkedTask)
				}
			}
			trigger.Tasks = filtered
			cfg.Triggers[triggerID] = trigger
			if err := saveValidatedProjectConfig(repoRoot, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Unlinked task %s from trigger %s\n", taskID, triggerID)
			return nil
		},
	}
	cmd.Flags().StringVar(&triggerID, "trigger", "", "trigger id")
	cmd.Flags().StringVar(&taskID, "task", "", "task id")
	return cmd
}

func newTriggerListCmd(application *app.App) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List trigger definitions",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := requireProjectConfig(application.CWD)
			if err != nil {
				return err
			}
			type triggerRow struct {
				ID                  string   `json:"id"`
				Type                string   `json:"type"`
				When                string   `json:"when"`
				OnlyIfBranchChanged string   `json:"onlyIfBranchChanged,omitempty"`
				Tasks               []string `json:"tasks"`
			}
			rows := []triggerRow{}
			errs := []model.ValidationError{}
			taskSet := map[string]struct{}{}
			for id, task := range cfg.Tasks {
				if len(config.ValidateTaskEntry(id, task)) == 0 {
					taskSet[id] = struct{}{}
				}
			}
			ids := make([]string, 0, len(cfg.Triggers))
			for id := range cfg.Triggers {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				trigger := cfg.Triggers[id]
				if validationErrs := config.ValidateTriggerEntry(id, trigger, taskSet); len(validationErrs) > 0 {
					errs = append(errs, validationErrs...)
					continue
				}
				rows = append(rows, triggerRow{
					ID:                  id,
					Type:                trigger.Type,
					When:                trigger.When,
					OnlyIfBranchChanged: trigger.OnlyIfBranchChanged,
					Tasks:               append([]string(nil), trigger.Tasks...),
				})
			}
			if jsonOutput {
				payload := map[string]any{"triggers": rows, "errors": errs}
				if err := textout.WriteJSON(cmd.OutOrStdout(), payload); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			} else {
				for _, row := range rows {
					branchGuard := ""
					if row.OnlyIfBranchChanged != "" {
						branchGuard = " branch=" + row.OnlyIfBranchChanged
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s%s\ttasks=%s\n", row.ID, row.Type, row.When, branchGuard, strings.Join(row.Tasks, ","))
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

func newTriggerRemoveCmd(application *app.App) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a trigger definition",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, cfg, err := requireProjectConfig(application.CWD)
			if err != nil {
				return err
			}
			triggerID := config.NormalizeID(id)
			if _, ok := cfg.Triggers[triggerID]; !ok {
				return &ExitError{Code: 1, Err: fmt.Errorf("trigger %q does not exist", triggerID)}
			}
			delete(cfg.Triggers, triggerID)
			if err := config.SaveProjectConfig(repoRoot, cfg); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed trigger %s\n", triggerID)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "trigger id")
	return cmd
}
