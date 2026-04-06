package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type StartSpec struct {
	SessionName string
	WorkDir     string
	ScriptPath  string
	ExitPath    string
	OutputPath  string
}

type Handle struct {
	SessionName string
	ExitPath    string
	OutputPath  string
}

type Backend interface {
	Start(ctx context.Context, spec *StartSpec) (Handle, error)
	Kill(ctx context.Context, handle Handle) error
	SessionExists(ctx context.Context, sessionName string) (bool, error)
}

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

type LaunchSpec struct {
	RunDir      string
	WorkDir     string
	TaskType    string
	CommandFile string
	PromptFile  string
	OutputPath  string
	ExitPath    string
}

func WriteLaunchScript(spec *LaunchSpec) (string, error) {
	body := `#!/usr/bin/env bash
set -u

RUN_DIR=%q
WORK_DIR=%q
OUTPUT_FILE=%q
EXIT_FILE=%q

exec > >(tee -a "$OUTPUT_FILE") 2>&1
cd "$WORK_DIR"

`
	command := ""
	switch spec.TaskType {
	case "bash":
		command = "bash -lc \"$(cat \"$RUN_DIR/command.txt\")\"\n"
	case "claude":
		command = "claude --dangerously-skip-permissions -p \"$(cat \"$RUN_DIR/prompt.txt\")\"\n"
	default:
		return "", fmt.Errorf("unsupported task type %q", spec.TaskType)
	}
	body += command + `code=$?
printf '%%s\n' "$code" > "$EXIT_FILE"
exit "$code"
`
	script := fmt.Sprintf(body, spec.RunDir, spec.WorkDir, spec.OutputPath, spec.ExitPath)
	path := filepath.Join(spec.RunDir, "launch.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("write launch script: %w", err)
	}
	return path, nil
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
