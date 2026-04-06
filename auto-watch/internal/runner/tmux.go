package runner

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type TmuxBackend struct{}

func (TmuxBackend) Start(ctx context.Context, spec *StartSpec) (Handle, error) {
	cmd := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", spec.SessionName, "-c", spec.WorkDir, spec.ScriptPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return Handle{}, fmt.Errorf("start tmux session: %w: %s", err, strings.TrimSpace(string(out)))
	}
	cmd = exec.CommandContext(ctx, "tmux", "set-option", "-t", spec.SessionName, "remain-on-exit", "on")
	if out, err := cmd.CombinedOutput(); err != nil {
		return Handle{}, fmt.Errorf("configure tmux remain-on-exit: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return Handle{
		SessionName: spec.SessionName,
		ExitPath:    spec.ExitPath,
		OutputPath:  spec.OutputPath,
	}, nil
}

func (TmuxBackend) Kill(ctx context.Context, handle Handle) error {
	cmd := exec.CommandContext(ctx, "tmux", "kill-session", "-t", handle.SessionName)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "can't find session") {
			return nil
		}
		return fmt.Errorf("kill tmux session %s: %w: %s", handle.SessionName, err, msg)
	}
	return nil
}

func (TmuxBackend) SessionExists(ctx context.Context, sessionName string) (bool, error) {
	cmd := exec.CommandContext(ctx, "tmux", "has-session", "-t", sessionName)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "can't find session") {
			return false, nil
		}
		return false, fmt.Errorf("check tmux session %s: %w: %s", sessionName, err, msg)
	}
	return true, nil
}
