package runner_test

import (
	"strings"
	"testing"

	"github.com/mistakenot/auto-watch/internal/runner"
)

func TestBuildPrompt(t *testing.T) {
	got := runner.BuildPrompt("demo", "cron", "daily", "cron:daily", "main", "/review")
	for _, fragment := range []string{
		"PROJECT_ID: demo",
		"TRIGGER_TYPE: cron",
		"TRIGGER_ID: daily",
		"RESOURCE_KEY: cron:daily",
		"BRANCH: main",
		"/review",
	} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("expected prompt to contain %q, got %q", fragment, got)
		}
	}
}

func TestInnerCommand(t *testing.T) {
	bash, err := runner.InnerCommand("bash")
	if err != nil {
		t.Fatalf("InnerCommand(bash) error: %v", err)
	}
	if !strings.Contains(bash, `bash -lc "$(cat "$RUN_DIR/command.txt")"`) {
		t.Fatalf("unexpected bash inner command: %q", bash)
	}

	claude, err := runner.InnerCommand("claude")
	if err != nil {
		t.Fatalf("InnerCommand(claude) error: %v", err)
	}
	if !strings.Contains(claude, `claude --dangerously-skip-permissions -p "$(cat "$RUN_DIR/prompt.txt")"`) {
		t.Fatalf("unexpected claude inner command: %q", claude)
	}

	if _, err := runner.InnerCommand("nope"); err == nil {
		t.Fatal("expected error for unsupported task type")
	}
}
