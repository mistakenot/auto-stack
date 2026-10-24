package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/backend"
	"github.com/mistakenot/auto-shared/backend/backendtest"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/store"
)

// TestRunTickContainsStoreFailure (AC-5): a store failure during the loop must
// not exit the daemon. runTick logs the error to stderr, records it in the
// heartbeat's last_error, and returns normally so the loop retries — proven by
// two consecutive failing ticks that both log and leave the loop running.
func TestRunTickContainsStoreFailure(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Close the store so every Tick fails at its first store call (Reap's
	// ListRunsByStates) — the "store itself fails" condition from AC-5.
	_ = db.Close()

	svc := daemon.New(db, backend.NewRegistry(&backendtest.Fake{}), nil, time.Now, nil)
	hbPath := filepath.Join(t.TempDir(), "heartbeat.json")
	hb := daemon.NewHeartbeatWriter(hbPath)
	now := func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) }

	var stderr bytes.Buffer
	runTick(context.Background(), svc, hb, &stderr, now)
	runTick(context.Background(), svc, hb, &stderr, now)

	if got := strings.Count(stderr.String(), "autowatch tick error"); got < 2 {
		t.Fatalf("expected both failing ticks logged to stderr, got %d:\n%s", got, stderr.String())
	}

	data, err := os.ReadFile(hbPath)
	if err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	var hbData map[string]any
	if err := json.Unmarshal(data, &hbData); err != nil {
		t.Fatalf("parse heartbeat: %v", err)
	}
	if le, _ := hbData["last_error"].(string); le == "" {
		t.Fatalf("expected heartbeat last_error set after a store failure, got: %s", data)
	}
	if _, ok := hbData["last_ok_tick_at"]; ok {
		t.Fatalf("last_ok_tick_at must be absent when no tick ever succeeded: %s", data)
	}
}
