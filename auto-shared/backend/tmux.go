package backend

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// defaultTmuxTimeout bounds every tmux invocation. A wedged tmux server once
// stalled a run for seven days; a per-call timeout turns that into a Status
// error the daemon can see, rather than an indefinite hang.
const defaultTmuxTimeout = 10 * time.Second

// Tmux is the tmux execution adapter. A run is a detached tmux session named by
// the Binding target, created with remain-on-exit so the session (and its exit
// status) survives the command for inspection. Liveness and exit are decided by
// the shared completion contract first (the exit-code file), and only then by
// asking tmux whether the session still exists.
type Tmux struct {
	// Timeout overrides the per-call tmux timeout. Zero uses defaultTmuxTimeout.
	Timeout time.Duration
}

func (t *Tmux) timeout() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}
	return defaultTmuxTimeout
}

// Name is the registry key for the tmux adapter.
func (t *Tmux) Name() string { return "tmux" }

// Start creates the run's session in a single tmux invocation: new-session,
// then remain-on-exit on, then the @auto_owner tag. Doing it in one call means
// there is no window where a configured-but-unowned session exists. On a
// failure it checks has-session: if the session exists it kills it (with a
// fresh timeout) and returns a confirmed error; if the kill or the check cannot
// be confirmed it returns the Binding with ErrStartUncertain, so the daemon
// keeps the run and lets Status own it rather than re-dispatching a possibly
// live command.
func (t *Tmux) Start(ctx context.Context, spec *StartSpec) (Binding, error) {
	binding := Binding{Manager: t.Name(), Target: spec.Name}

	args := []string{
		"new-session", "-d", "-s", spec.Name, "-c", spec.WorkDir, spec.LaunchScript,
		";", "set-option", "-t", spec.Name, "remain-on-exit", "on",
		";", "set-option", "-t", spec.Name, "@auto_owner", spec.Owner,
	}
	if out, err := t.run(ctx, args...); err != nil {
		startErr := fmt.Errorf("start tmux session %s: %w: %s", spec.Name, err, out)
		// The combined invocation may have created the session before a later
		// set-option failed. Reconcile: if the session exists, kill it so no
		// unknown live command is left behind.
		exists, checkErr := t.hasSession(context.WithoutCancel(ctx), spec.Name)
		if checkErr != nil {
			// Can't confirm whether a session is live — hand back the Binding.
			return binding, fmt.Errorf("%w: %w", ErrStartUncertain, startErr)
		}
		if !exists {
			// Nothing was left running; this is a confirmed, clean failure.
			return Binding{}, startErr
		}
		if killErr := t.kill(context.WithoutCancel(ctx), spec.Name); killErr != nil {
			// The session is live and we could not kill it — uncertain.
			return binding, fmt.Errorf("%w: %w", ErrStartUncertain, startErr)
		}
		return Binding{}, startErr
	}
	return binding, nil
}

// Status reports the run's liveness: the exit-code file first, then the session.
func (t *Tmux) Status(ctx context.Context, binding Binding, runDir string) (Status, error) {
	code, exited, err := ReadExit(runDir)
	if err != nil {
		return Status{}, err
	}
	if exited {
		return Status{State: StatusExited, ExitCode: code}, nil
	}
	exists, err := t.hasSession(ctx, binding.Target)
	if err != nil {
		return Status{}, err
	}
	if exists {
		return Status{State: StatusRunning}, nil
	}
	return Status{State: StatusLost}, nil
}

// Stop kills the run's session. An already-absent session is not an error.
func (t *Tmux) Stop(ctx context.Context, binding Binding) error {
	return t.kill(ctx, binding.Target)
}

func (t *Tmux) kill(ctx context.Context, session string) error {
	out, err := t.run(ctx, "kill-session", "-t", session)
	if err != nil {
		if sessionAbsent(out) {
			return nil
		}
		return fmt.Errorf("kill tmux session %s: %w: %s", session, err, out)
	}
	return nil
}

func (t *Tmux) hasSession(ctx context.Context, session string) (bool, error) {
	out, err := t.run(ctx, "has-session", "-t", session)
	if err != nil {
		if sessionAbsent(out) {
			return false, nil
		}
		return false, fmt.Errorf("check tmux session %s: %w: %s", session, err, out)
	}
	return true, nil
}

// run executes one tmux command with a per-call timeout, returning the trimmed
// combined output.
func (t *Tmux) run(ctx context.Context, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, t.timeout())
	defer cancel()
	out, err := exec.CommandContext(cctx, "tmux", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// sessionAbsent reports whether a failed tmux command means the session is
// simply not there. tmux says that several ways: "can't find session" when a
// server is up but the session is gone, and "no server running" (or a socket
// connect error) when there is no server at all — the stronger form of the same
// fact, since a dead server has no sessions. Every one must be a plain absent
// rather than an error: Status runs while reconciling runs left over from a
// previous daemon life, and after a reboot or a tmux teardown the server is
// routinely gone. Returning an error there crash-loops the daemon (the 33k
// restart incident) and the run it is reconciling never clears.
func sessionAbsent(msg string) bool {
	lower := strings.ToLower(msg)
	for _, s := range []string{
		"can't find session",
		"no server running",
		"no such file or directory",
		"error connecting to",
	} {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}
