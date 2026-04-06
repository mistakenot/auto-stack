package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/cli"
	"github.com/mistakenot/auto-watch/internal/testutil"
)

func TestStartOnceLaunchesTmuxTaskAndLogsCompletion(t *testing.T) {
	if os.Getenv("AUTOWATCH_E2E") != "1" {
		t.Skip("set AUTOWATCH_E2E=1 to run real tmux e2e tests")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	env := testutil.NewEnv(t)
	env.WriteExecutable("claude", "#!/usr/bin/env bash\nexit 0\n")
	repoRoot := env.NewRepo("demo")
	if _, _, code := env.RunCLI(repoRoot, "init"); code != 0 {
		t.Fatal("init failed")
	}
	if _, stderr, code := env.RunCLI(repoRoot, "task", "create", "--id", "run-etl", "--bash", "printf 'hello from tmux\\n'"); code != 0 {
		t.Fatalf("task create failed: %s", stderr)
	}
	fixedNow := time.Date(2026, 3, 20, 10, 0, 0, 0, time.Local)
	cronExpr := fmt.Sprintf("%d %d * * *", fixedNow.Minute(), fixedNow.Hour())
	if _, stderr, code := env.RunCLI(repoRoot, "trigger", "create", "--id", "due-now", "--cron", cronExpr); code != 0 {
		t.Fatalf("trigger create failed: %s", stderr)
	}
	if _, stderr, code := env.RunCLI(repoRoot, "trigger", "add-task", "--trigger", "due-now", "--task", "run-etl"); code != 0 {
		t.Fatalf("trigger add-task failed: %s", stderr)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	application := app.New(&stdout, &stderr)
	application.CWD = repoRoot
	application.Now = func() time.Time { return fixedNow }
	rootCmd := cli.NewRootCmd(application)
	rootCmd.SetArgs([]string{"start", "--once"})
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("start --once failed: %v\nstderr:\n%s", err, stderr.String())
	}

	stdoutStr, stderrStr, code := env.RunCLI(repoRoot, "logs", "--json", "-n", "20")
	if code != 0 {
		t.Fatalf("logs failed: %s", stderrStr)
	}
	var payload struct {
		Events []struct {
			EventType string `json:"event_type"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(stdoutStr), &payload); err != nil {
		t.Fatalf("unmarshal logs payload: %v", err)
	}
	foundStarted := false
	foundCompleted := false
	for _, event := range payload.Events {
		if event.EventType == "task_started" {
			foundStarted = true
		}
		if event.EventType == "task_completed" {
			foundCompleted = true
		}
	}
	if !foundStarted || !foundCompleted {
		t.Fatalf("expected task_started and task_completed in logs, got %+v", payload.Events)
	}
}
