package runner_test

import (
	"os"
	"path/filepath"
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

func TestWriteLaunchScript(t *testing.T) {
	runDir := t.TempDir()
	path, err := runner.WriteLaunchScript(&runner.LaunchSpec{
		RunDir:      runDir,
		WorkDir:     "/tmp/work",
		TaskType:    "bash",
		CommandFile: filepath.Join(runDir, "command.txt"),
		OutputPath:  filepath.Join(runDir, "output.log"),
		ExitPath:    filepath.Join(runDir, "exit-code"),
	})
	if err != nil {
		t.Fatalf("WriteLaunchScript returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read launch script: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `bash -lc "$(cat "$RUN_DIR/command.txt")"`) {
		t.Fatalf("expected bash launch command in script, got %q", content)
	}
	if !strings.Contains(content, `printf '%s\n' "$code" > "$EXIT_FILE"`) {
		t.Fatalf("expected exit-code write in script, got %q", content)
	}
}
