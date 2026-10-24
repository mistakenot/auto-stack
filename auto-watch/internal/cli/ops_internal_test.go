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
	watchconfig "github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
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

// healthEnv sets HOME to a fresh temp dir, ensures the watch dirs, opens the
// store there and returns it plus a fixed clock. collectHealth reads the
// heartbeat and watch dir via config paths, which resolve under this HOME.
func healthEnv(t *testing.T) (*store.Store, time.Time) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := watchconfig.EnsureGlobalDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	dbPath, err := watchconfig.DBPath()
	if err != nil {
		t.Fatalf("db path: %v", err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
}

func writeHeartbeat(t *testing.T, lastOK *time.Time, lastErr string) {
	t.Helper()
	hbPath, err := watchconfig.HeartbeatPath()
	if err != nil {
		t.Fatalf("heartbeat path: %v", err)
	}
	hb := map[string]any{"pid": 1234, "last_tick_at": time.Now().UTC()}
	if lastOK != nil {
		hb["last_ok_tick_at"] = lastOK.UTC()
	}
	if lastErr != "" {
		hb["last_error"] = lastErr
	}
	data, err := json.Marshal(hb)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hbPath, data, 0o644); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
}

func hasIssue(issues []model.HealthIssue, issueType string) *model.HealthIssue {
	for i := range issues {
		if issues[i].Type == issueType {
			return &issues[i]
		}
	}
	return nil
}

// TestCollectHealthOK (AC-14): a fresh heartbeat, enough disk and no unconfirmed
// stops report ok with no issues.
func TestCollectHealthOK(t *testing.T) {
	db, now := healthEnv(t)
	ok := now
	writeHeartbeat(t, &ok, "")

	old := diskFreeBytes
	diskFreeBytes = func(string) (int64, error) { return 100 << 30, nil } // 100 GiB
	t.Cleanup(func() { diskFreeBytes = old })

	status, issues, err := collectHealth(context.Background(), db, func() time.Time { return now })
	if err != nil {
		t.Fatalf("collectHealth: %v", err)
	}
	if status != "ok" || len(issues) != 0 {
		t.Fatalf("expected ok with no issues, got status=%s issues=%+v", status, issues)
	}
}

// TestCollectHealthStaleHeartbeat (AC-14): a missing heartbeat, and one whose
// last_ok_tick_at is over 3 minutes old, both report stale_heartbeat (the old
// one carrying the age and last_error).
func TestCollectHealthStaleHeartbeat(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		db, now := healthEnv(t)
		old := diskFreeBytes
		diskFreeBytes = func(string) (int64, error) { return 100 << 30, nil }
		t.Cleanup(func() { diskFreeBytes = old })

		status, issues, err := collectHealth(context.Background(), db, func() time.Time { return now })
		if err != nil {
			t.Fatalf("collectHealth: %v", err)
		}
		if status != "warn" {
			t.Fatalf("expected warn, got %s", status)
		}
		if hasIssue(issues, "stale_heartbeat") == nil {
			t.Fatalf("expected stale_heartbeat for a missing heartbeat, got %+v", issues)
		}
	})

	t.Run("old", func(t *testing.T) {
		db, now := healthEnv(t)
		stale := now.Add(-10 * time.Minute)
		writeHeartbeat(t, &stale, "disk full")
		old := diskFreeBytes
		diskFreeBytes = func(string) (int64, error) { return 100 << 30, nil }
		t.Cleanup(func() { diskFreeBytes = old })

		_, issues, err := collectHealth(context.Background(), db, func() time.Time { return now })
		if err != nil {
			t.Fatalf("collectHealth: %v", err)
		}
		issue := hasIssue(issues, "stale_heartbeat")
		if issue == nil {
			t.Fatalf("expected stale_heartbeat for an old tick, got %+v", issues)
		}
		if !strings.Contains(issue.Message, "10m0s") || !strings.Contains(issue.Message, "disk full") {
			t.Fatalf("stale_heartbeat message must carry the age and last_error, got: %s", issue.Message)
		}
	})
}

// TestCollectHealthLowDisk (AC-14): under 1 GiB free on the watch filesystem is
// reported as low_disk with the free bytes.
func TestCollectHealthLowDisk(t *testing.T) {
	db, now := healthEnv(t)
	ok := now
	writeHeartbeat(t, &ok, "")

	old := diskFreeBytes
	diskFreeBytes = func(string) (int64, error) { return 500 << 20, nil } // 500 MiB
	t.Cleanup(func() { diskFreeBytes = old })

	_, issues, err := collectHealth(context.Background(), db, func() time.Time { return now })
	if err != nil {
		t.Fatalf("collectHealth: %v", err)
	}
	issue := hasIssue(issues, "low_disk")
	if issue == nil {
		t.Fatalf("expected low_disk, got %+v", issues)
	}
	if !strings.Contains(issue.Message, "524288000") {
		t.Fatalf("low_disk must report the free bytes, got: %s", issue.Message)
	}
}

// TestCollectHealthStopUnconfirmed (AC-4): a run still running that carries a
// task_stop_failed Event is reported as stop_unconfirmed.
func TestCollectHealthStopUnconfirmed(t *testing.T) {
	db, now := healthEnv(t)
	ok := now
	writeHeartbeat(t, &ok, "")
	old := diskFreeBytes
	diskFreeBytes = func(string) (int64, error) { return 100 << 30, nil }
	t.Cleanup(func() { diskFreeBytes = old })

	ctx := context.Background()
	id, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID: "demo", ProjectPath: "/repo", TriggerID: "cron", TriggerType: "cron",
		TaskID: "hang", TaskType: "bash", ResourceKey: "rk-hang", StartedAt: now,
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := db.UpdateRunStarted(ctx, id, &store.RunStartUpdate{
		SessionName: "autowatch-run--hang", Backend: "tmux", BackendTarget: "autowatch-run--hang",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := db.InsertEvent(ctx, &store.EventInput{
		Timestamp: now, Level: "warn", EventType: "task_stop_failed",
		ProjectID: "demo", TaskID: "hang", RunID: &id, Message: "stop not confirmed",
	}); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	_, issues, err := collectHealth(ctx, db, func() time.Time { return now })
	if err != nil {
		t.Fatalf("collectHealth: %v", err)
	}
	issue := hasIssue(issues, "stop_unconfirmed")
	if issue == nil {
		t.Fatalf("expected stop_unconfirmed, got %+v", issues)
	}
	if issue.RunID != id {
		t.Fatalf("stop_unconfirmed should name run %d, got %d", id, issue.RunID)
	}
}
