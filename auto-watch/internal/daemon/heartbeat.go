package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Heartbeat is the daemon liveness record written to heartbeat.json at the end
// of every tick. health reads it to tell a wedged or dead daemon (a stale
// last_ok_tick_at) from a healthy one.
type Heartbeat struct {
	// PID is the daemon process id that wrote the heartbeat.
	PID int `json:"pid"`
	// LastTickAt is when the most recent tick finished, success or failure.
	LastTickAt time.Time `json:"last_tick_at"`
	// LastOKTickAt is when the most recent SUCCESSFUL tick finished. It is nil
	// until the first successful tick and then only advances on success, so a
	// daemon stuck failing every tick shows a stale (not advancing) value.
	LastOKTickAt *time.Time `json:"last_ok_tick_at,omitempty"`
	// LastError is the error string of the most recent tick, empty on success.
	LastError string `json:"last_error,omitempty"`
}

// HeartbeatWriter records the daemon's liveness to a JSON file at the end of
// every tick. It carries last_ok_tick_at across ticks (advanced only on a
// successful tick) so a daemon failing every tick is distinguishable from a
// healthy one. The zero value is not usable; build one with NewHeartbeatWriter.
type HeartbeatWriter struct {
	path         string
	pid          int
	lastOKTickAt *time.Time
}

// NewHeartbeatWriter returns a writer that writes to path, stamping the current
// process id on each heartbeat.
func NewHeartbeatWriter(path string) *HeartbeatWriter {
	return &HeartbeatWriter{path: path, pid: os.Getpid()}
}

// Record writes the heartbeat for a tick that finished at `at` with tickErr (nil
// on success). last_ok_tick_at advances only when tickErr is nil; last_error
// carries tickErr's string otherwise. The write is atomic (temp file + rename),
// so a reader never sees a half-written file. A write failure is returned for
// the caller to log; it is never fatal.
func (w *HeartbeatWriter) Record(at time.Time, tickErr error) error {
	hb := Heartbeat{PID: w.pid, LastTickAt: at.UTC()}
	if tickErr == nil {
		ok := at.UTC()
		w.lastOKTickAt = &ok
	} else {
		hb.LastError = tickErr.Error()
	}
	hb.LastOKTickAt = w.lastOKTickAt
	return writeHeartbeatFile(w.path, &hb)
}

// writeHeartbeatFile marshals hb and writes it atomically: a temp file in the
// same directory, then a rename over the target path.
func writeHeartbeatFile(path string, hb *Heartbeat) error {
	data, err := json.MarshalIndent(hb, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // heartbeat is non-secret liveness data
		return fmt.Errorf("write heartbeat temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename heartbeat file: %w", err)
	}
	return nil
}

// ReadHeartbeat loads and parses the heartbeat file at path. A missing file
// returns os.ErrNotExist (wrapped), which health treats as a stale heartbeat.
func ReadHeartbeat(path string) (Heartbeat, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Heartbeat{}, err
	}
	var hb Heartbeat
	if err := json.Unmarshal(data, &hb); err != nil {
		return Heartbeat{}, fmt.Errorf("parse heartbeat file: %w", err)
	}
	return hb, nil
}
