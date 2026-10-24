package runner

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

func BuildPrompt(projectID, triggerType, triggerID, resourceKey, branch, prompt string) string {
	return strings.TrimSpace(fmt.Sprintf(`<autowatch-context>
PROJECT_ID: %s
TRIGGER_TYPE: %s
TRIGGER_ID: %s
RESOURCE_KEY: %s
BRANCH: %s
</autowatch-context>

%s`, projectID, triggerType, triggerID, resourceKey, branch, strings.TrimSpace(prompt))) + "\n"
}

// InnerCommand returns the inner command line for a task type, to be wrapped by
// the shared backend.WriteLaunchScript. The wrapper exports $RUN_DIR, so the
// command reads its payload from the run directory (command.txt / prompt.txt).
func InnerCommand(taskType string) (string, error) {
	switch taskType {
	case "bash":
		return `bash -lc "$(cat "$RUN_DIR/command.txt")"`, nil
	case "claude":
		return `claude --dangerously-skip-permissions -p "$(cat "$RUN_DIR/prompt.txt")"`, nil
	default:
		return "", fmt.Errorf("unsupported task type %q", taskType)
	}
}

func RunForeground(ctx context.Context, workDir, taskType, value string, stdout, stderr io.Writer) error {
	var cmd *exec.Cmd
	switch taskType {
	case "bash":
		cmd = exec.CommandContext(ctx, "bash", "-lc", value)
	case "claude":
		cmd = exec.CommandContext(ctx, "claude", "--dangerously-skip-permissions", "-p", value)
	default:
		return fmt.Errorf("unsupported task type %q", taskType)
	}
	cmd.Dir = workDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
