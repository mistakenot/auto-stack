package daemon_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/daemon"
)

// TestHeartbeatRecordsFields: a successful tick records pid, last_tick_at and
// last_ok_tick_at, with no last_error.
func TestHeartbeatRecordsFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heartbeat.json")
	w := daemon.NewHeartbeatWriter(path)
	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	if err := w.Record(at, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	hb, err := daemon.ReadHeartbeat(path)
	if err != nil {
		t.Fatalf("ReadHeartbeat: %v", err)
	}
	if hb.PID != os.Getpid() {
		t.Fatalf("pid = %d, want %d", hb.PID, os.Getpid())
	}
	if !hb.LastTickAt.Equal(at) {
		t.Fatalf("last_tick_at = %v, want %v", hb.LastTickAt, at)
	}
	if hb.LastOKTickAt == nil || !hb.LastOKTickAt.Equal(at) {
		t.Fatalf("last_ok_tick_at = %v, want %v", hb.LastOKTickAt, at)
	}
	if hb.LastError != "" {
		t.Fatalf("last_error = %q, want empty", hb.LastError)
	}
}

// TestHeartbeatFailedTickKeepsLastOK: a failing tick records last_error and
// advances last_tick_at, but last_ok_tick_at stays at the previous success —
// this is the signal health uses to detect a daemon stuck failing every tick.
func TestHeartbeatFailedTickKeepsLastOK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heartbeat.json")
	w := daemon.NewHeartbeatWriter(path)
	ok := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	fail := ok.Add(time.Minute)

	if err := w.Record(ok, nil); err != nil {
		t.Fatalf("Record ok: %v", err)
	}
	if err := w.Record(fail, errors.New("store write failed")); err != nil {
		t.Fatalf("Record fail: %v", err)
	}

	hb, err := daemon.ReadHeartbeat(path)
	if err != nil {
		t.Fatalf("ReadHeartbeat: %v", err)
	}
	if !hb.LastTickAt.Equal(fail) {
		t.Fatalf("last_tick_at = %v, want %v (advances on failure)", hb.LastTickAt, fail)
	}
	if hb.LastOKTickAt == nil || !hb.LastOKTickAt.Equal(ok) {
		t.Fatalf("last_ok_tick_at = %v, want %v (frozen at last success)", hb.LastOKTickAt, ok)
	}
	if hb.LastError != "store write failed" {
		t.Fatalf("last_error = %q, want %q", hb.LastError, "store write failed")
	}
}

// TestHeartbeatAtomicWriteLeavesNoTempFile: the write is atomic (temp file +
// rename), so no .tmp file lingers and the published file is valid JSON.
func TestHeartbeatAtomicWriteLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "heartbeat.json")
	w := daemon.NewHeartbeatWriter(path)

	if err := w.Record(time.Now(), nil); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file must not linger after an atomic write: stat err=%v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("heartbeat file is not valid JSON: %v", err)
	}
}

// TestHeartbeatWriteFailureIsReported: a write into a non-existent directory
// fails, and the error is returned for the caller to log — never a panic. The
// start loop treats this as non-fatal.
func TestHeartbeatWriteFailureIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist", "heartbeat.json")
	w := daemon.NewHeartbeatWriter(path)
	if err := w.Record(time.Now(), nil); err == nil {
		t.Fatal("expected a write error for a path in a non-existent directory")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("no heartbeat file should exist after a failed write")
	}
}
