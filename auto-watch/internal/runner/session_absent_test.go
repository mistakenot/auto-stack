package runner

import "testing"

// `tmux has-session` reports "not there" in more than one way. Every one of them
// must be a plain false rather than an error: the daemon's Reap calls
// SessionExists while reconciling runs left over from a previous daemon life,
// and after a reboot or a tmux teardown the server is routinely gone. An error
// there kills the daemon on every Tick, and the run being reconciled stays
// RunRunning until Reap gets past it — so the restart can never clear it.
//
// Observed in production: "no server running on /tmp/tmux-1002/default" wedged
// the daemon immediately after the exit-code parse fix let Reap get this far.
func TestSessionAbsent(t *testing.T) {
	absent := []string{
		"can't find session: autowatch-run-2958--run-etl",
		"no server running on /tmp/tmux-1002/default",
		"error connecting to /tmp/tmux-1002/default (No such file or directory)",
		"No server running on /tmp/tmux-0/default", // capitalised variant
	}
	for _, msg := range absent {
		if !sessionAbsent(msg) {
			t.Errorf("sessionAbsent(%q) = false, want true — this message means the session is gone, "+
				"and treating it as an error crash-loops the daemon", msg)
		}
	}

	present := []string{
		"",
		"permission denied",
		"lost server",
	}
	for _, msg := range present {
		if sessionAbsent(msg) {
			t.Errorf("sessionAbsent(%q) = true, want false — a genuine failure must stay an error", msg)
		}
	}
}
