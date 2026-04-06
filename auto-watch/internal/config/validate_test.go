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

func TestNormalizeID(t *testing.T) {
	if got := config.NormalizeID("  My-Task "); got != "my-task" {
		t.Fatalf("expected normalized id my-task, got %q", got)
	}
}
