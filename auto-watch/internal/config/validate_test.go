package config_test

import (
	"testing"

	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/model"
)

func TestValidateProjectConfig(t *testing.T) {
	cfg := model.ProjectConfig{
		ID: "demo-project",
		Tasks: map[string]model.TaskDef{
			"valid-task": {
				Type:    "bash",
				Command: "echo ok",
			},
			"Bad Task": {
				Type:   "claude",
				Prompt: "review this",
			},
		},
		Triggers: map[string]model.TriggerDef{
			"daily": {
				Type:  "cron",
				When:  "0 9 * * 1",
				Tasks: []string{"valid-task", "missing-task", "valid-task"},
			},
		},
	}

	errs := config.ValidateProjectConfig(cfg)
	if len(errs) != 3 {
		t.Fatalf("expected 3 validation errors, got %d: %#v", len(errs), errs)
	}
}

func TestValidateFileCreatedTrigger(t *testing.T) {
	tasks := map[string]struct{}{"my-task": {}}

	t.Run("valid file_created trigger", func(t *testing.T) {
		def := model.TriggerDef{
			Type:  "file_created",
			Glob:  "docs/**/*.md",
			Tasks: []string{"my-task"},
		}
		errs := config.ValidateTriggerEntry("watch-docs", &def, tasks)
		if len(errs) != 0 {
			t.Fatalf("expected 0 errors, got %d: %#v", len(errs), errs)
		}
	})

	t.Run("missing glob", func(t *testing.T) {
		def := model.TriggerDef{
			Type:  "file_created",
			Glob:  "",
			Tasks: []string{"my-task"},
		}
		errs := config.ValidateTriggerEntry("watch-docs", &def, tasks)
		found := false
		for _, e := range errs {
			if e.Code == "missing_trigger_glob" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected missing_trigger_glob error, got %#v", errs)
		}
	})

	t.Run("invalid trigger type", func(t *testing.T) {
		def := model.TriggerDef{
			Type:  "webhook",
			Tasks: []string{"my-task"},
		}
		errs := config.ValidateTriggerEntry("bad-type", &def, tasks)
		found := false
		for _, e := range errs {
			if e.Code == "invalid_trigger_type" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected invalid_trigger_type error, got %#v", errs)
		}
	})

	t.Run("cron trigger still validates", func(t *testing.T) {
		def := model.TriggerDef{
			Type:  "cron",
			When:  "0 9 * * *",
			Tasks: []string{"my-task"},
		}
		errs := config.ValidateTriggerEntry("daily", &def, tasks)
		if len(errs) != 0 {
			t.Fatalf("expected 0 errors for valid cron, got %d: %#v", len(errs), errs)
		}
	})
}

func TestNormalizeID(t *testing.T) {
	if got := config.NormalizeID("  My-Task "); got != "my-task" {
		t.Fatalf("expected normalized id my-task, got %q", got)
	}
}
