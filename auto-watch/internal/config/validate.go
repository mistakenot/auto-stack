package config

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/robfig/cron/v3"
)

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
)

// ValidateGlobalConfig validates the project registry. It lives in global.go,
// delegating to the shared registry validator.

func ValidateProjectConfig(cfg model.ProjectConfig) []model.ValidationError {
	errs := []model.ValidationError{}
	if !idPattern.MatchString(cfg.ID) {
		errs = append(errs, model.ValidationError{
			Code:    "invalid_project_id",
			Path:    "$.id",
			Field:   "id",
			Message: "project id must match ^[a-z0-9]+(?:-[a-z0-9]+)*$",
			Value:   cfg.ID,
		})
	}
	taskIDs := map[string]struct{}{}
	taskNames := make([]string, 0, len(cfg.Tasks))
	for id := range cfg.Tasks {
		taskNames = append(taskNames, id)
	}
	sort.Strings(taskNames)
	for _, id := range taskNames {
		taskIDs[id] = struct{}{}
		errs = append(errs, ValidateTaskEntry(id, cfg.Tasks[id])...)
	}
	triggerNames := make([]string, 0, len(cfg.Triggers))
	for id := range cfg.Triggers {
		triggerNames = append(triggerNames, id)
	}
	sort.Strings(triggerNames)
	for _, id := range triggerNames {
		trig := cfg.Triggers[id]
		errs = append(errs, ValidateTriggerEntry(id, &trig, taskIDs)...)
	}
	return errs
}

func ValidateTaskEntry(id string, def model.TaskDef) []model.ValidationError {
	errs := []model.ValidationError{}
	path := "$.tasks." + id
	if !idPattern.MatchString(id) {
		errs = append(errs, model.ValidationError{
			Code:    "invalid_task_id",
			Path:    path,
			Field:   "id",
			Message: "task id must match ^[a-z0-9]+(?:-[a-z0-9]+)*$",
			Value:   id,
		})
	}
	switch def.Type {
	case "bash":
		if strings.TrimSpace(def.Command) == "" {
			errs = append(errs, model.ValidationError{
				Code:    "missing_task_command",
				Path:    path,
				Field:   "command",
				Message: "bash tasks require command",
			})
		}
		if strings.TrimSpace(def.Prompt) != "" {
			errs = append(errs, model.ValidationError{
				Code:    "unexpected_task_prompt",
				Path:    path,
				Field:   "prompt",
				Message: "bash tasks must not set prompt",
				Value:   def.Prompt,
			})
		}
	case "claude":
		if strings.TrimSpace(def.Prompt) == "" {
			errs = append(errs, model.ValidationError{
				Code:    "missing_task_prompt",
				Path:    path,
				Field:   "prompt",
				Message: "claude tasks require prompt",
			})
		}
		if strings.TrimSpace(def.Command) != "" {
			errs = append(errs, model.ValidationError{
				Code:    "unexpected_task_command",
				Path:    path,
				Field:   "command",
				Message: "claude tasks must not set command",
				Value:   def.Command,
			})
		}
	default:
		errs = append(errs, model.ValidationError{
			Code:    "invalid_task_type",
			Path:    path,
			Field:   "type",
			Message: "task type must be bash or claude",
			Value:   def.Type,
		})
	}
	return errs
}

func ValidateTriggerEntry(id string, def *model.TriggerDef, tasks map[string]struct{}) []model.ValidationError {
	errs := []model.ValidationError{}
	path := "$.triggers." + id
	if !idPattern.MatchString(id) {
		errs = append(errs, model.ValidationError{
			Code:    "invalid_trigger_id",
			Path:    path,
			Field:   "id",
			Message: "trigger id must match ^[a-z0-9]+(?:-[a-z0-9]+)*$",
			Value:   id,
		})
	}
	switch def.Type {
	case "cron":
		if _, err := ParseCron(def.When); err != nil {
			errs = append(errs, model.ValidationError{
				Code:    "invalid_cron",
				Path:    path,
				Field:   "when",
				Message: "trigger cron must be a valid 5-field cron expression",
				Value:   def.When,
			})
		}
	case "file_created":
		if strings.TrimSpace(def.Glob) == "" {
			errs = append(errs, model.ValidationError{
				Code:    "missing_trigger_glob",
				Path:    path,
				Field:   "glob",
				Message: "file_created triggers require a glob pattern",
			})
		} else if _, err := filepath.Match(def.Glob, ""); err != nil {
			errs = append(errs, model.ValidationError{
				Code:    "invalid_trigger_glob",
				Path:    path,
				Field:   "glob",
				Message: "glob pattern is invalid",
				Value:   def.Glob,
			})
		}
	default:
		errs = append(errs, model.ValidationError{
			Code:    "invalid_trigger_type",
			Path:    path,
			Field:   "type",
			Message: "trigger type must be cron or file_created",
			Value:   def.Type,
		})
	}
	seenTasks := map[string]struct{}{}
	for i, taskID := range def.Tasks {
		taskPath := fmt.Sprintf("%s.tasks[%d]", path, i)
		if _, ok := tasks[taskID]; !ok {
			errs = append(errs, model.ValidationError{
				Code:    "unknown_task_reference",
				Path:    taskPath,
				Field:   "tasks",
				Message: "trigger references an unknown task",
				Value:   taskID,
			})
		}
		if _, ok := seenTasks[taskID]; ok {
			errs = append(errs, model.ValidationError{
				Code:    "duplicate_task_reference",
				Path:    taskPath,
				Field:   "tasks",
				Message: "trigger contains duplicate task ids",
				Value:   taskID,
			})
		}
		seenTasks[taskID] = struct{}{}
	}
	if branch := strings.TrimSpace(def.OnlyIfBranchChanged); branch != "" {
		if err := ValidateBranchName(branch); err != nil {
			errs = append(errs, model.ValidationError{
				Code:    "invalid_branch_name",
				Path:    path,
				Field:   "onlyIfBranchHasChanged",
				Message: "branch name is invalid",
				Value:   branch,
			})
		}
	}
	return errs
}

func ParseCron(expr string) (cron.Schedule, error) {
	return cronParser.Parse(strings.TrimSpace(expr))
}

func ValidateBranchName(branch string) error {
	cmd := exec.Command("git", "check-ref-format", "--branch", strings.TrimSpace(branch))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
