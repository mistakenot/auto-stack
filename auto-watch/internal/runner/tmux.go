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
		if sessionAbsent(msg) {
			return false, nil
		}
		return false, fmt.Errorf("check tmux session %s: %w: %s", sessionName, err, msg)
	}
	return true, nil
}

// sessionAbsent reports whether a failed `tmux has-session` means the session is
// simply not there. tmux says that two different ways: "can't find session" when
// a server is up but the session is gone, and "no server running" (or a socket
// connect error) when there is no server at all — which is the stronger form of
// the same fact, since a dead server has no sessions. Both must be a plain
// false, not an error: Reap calls this while reconciling runs left over from a
// previous daemon life, and after a reboot or a tmux teardown the server is
// routinely gone. Returning an error there kills the daemon on every Tick, and
// the run it is reconciling stays RunRunning until it gets past this, so the
// restart never clears it.
func sessionAbsent(msg string) bool {
	for _, s := range []string{
		"can't find session",
		"no server running",
		"no such file or directory",
		"error connecting to",
	} {
		if strings.Contains(strings.ToLower(msg), s) {
			return true
		}
	}
	return false
}
