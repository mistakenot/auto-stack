package config_test

import (
	"strings"
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

	errs := config.ValidateProjectConfig(cfg, []string{"tmux"})
	if len(errs) != 3 {
		t.Fatalf("expected 3 validation errors, got %d: %#v", len(errs), errs)
	}
}

func TestValidateTaskEntryBackendAndTimeout(t *testing.T) {
	backends := []string{"tmux"}

	t.Run("unknown backend", func(t *testing.T) {
		errs := config.ValidateTaskEntry("run-etl", model.TaskDef{
			Type: "bash", Command: "echo ok", Backend: "herdr",
		}, backends)
		var found *model.ValidationError
		for i := range errs {
			if errs[i].Code == "unknown_backend" {
				found = &errs[i]
			}
		}
		if found == nil {
			t.Fatalf("expected unknown_backend error, got %#v", errs)
		}
		if found.Path != "$.tasks.run-etl.backend" || found.Field != "backend" || found.Value != "herdr" {
			t.Fatalf("unexpected unknown_backend error: %#v", *found)
		}
		if !strings.Contains(found.Message, "tmux") {
			t.Fatalf("expected message to list registered backends, got %q", found.Message)
		}
	})

	t.Run("empty backend is valid (default)", func(t *testing.T) {
		errs := config.ValidateTaskEntry("run-etl", model.TaskDef{
			Type: "bash", Command: "echo ok",
		}, backends)
		for i := range errs {
			if errs[i].Code == "unknown_backend" {
				t.Fatalf("empty backend must not error: %#v", errs[i])
			}
		}
	})

	t.Run("registered backend is valid", func(t *testing.T) {
		errs := config.ValidateTaskEntry("run-etl", model.TaskDef{
			Type: "bash", Command: "echo ok", Backend: "tmux",
		}, backends)
		for i := range errs {
			if errs[i].Code == "unknown_backend" {
				t.Fatalf("tmux must be valid: %#v", errs[i])
			}
		}
	})

	for _, bad := range []string{"abc", "0s", "-5m"} {
		t.Run("invalid timeout "+bad, func(t *testing.T) {
			errs := config.ValidateTaskEntry("run-etl", model.TaskDef{
				Type: "bash", Command: "echo ok", Timeout: bad,
			}, backends)
			found := false
			for i := range errs {
				if errs[i].Code == "invalid_timeout" {
					found = true
					if errs[i].Path != "$.tasks.run-etl.timeout" || errs[i].Field != "timeout" {
						t.Fatalf("unexpected invalid_timeout error: %#v", errs[i])
					}
				}
			}
			if !found {
				t.Fatalf("expected invalid_timeout error for %q, got %#v", bad, errs)
			}
		})
	}

	t.Run("valid timeout", func(t *testing.T) {
		errs := config.ValidateTaskEntry("run-etl", model.TaskDef{
			Type: "bash", Command: "echo ok", Timeout: "30m",
		}, backends)
		for i := range errs {
			if errs[i].Code == "invalid_timeout" {
				t.Fatalf("30m must be valid: %#v", errs[i])
			}
		}
	})
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
