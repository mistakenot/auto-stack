package daemon_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/store"
)

// TestNoTriggerEvaluatedEvents (AC-6): trigger evaluation must not emit
// trigger_evaluated events any more — the bulk of historical event noise. With
// three cron triggers ticked ten times, zero such events must be stored.
func TestNoTriggerEvaluatedEvents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := t.TempDir()

	autoDir := filepath.Join(home, ".auto")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := fmt.Sprintf(`{"projects":[{"id":"demo","path":%q}]}`, repoRoot)
	if err := os.WriteFile(filepath.Join(autoDir, "projects.json"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(repoRoot, ".auto", "watch")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Three cron triggers (every minute, due first minute, and never-due) so the
	// launched / not-due / already-processed evaluation paths are all exercised.
	cfg := `{"id":"demo",` +
		`"tasks":{"noop":{"type":"bash","command":"echo ok"}},` +
		`"triggers":{` +
		`"every-minute":{"type":"cron","when":"* * * * *","tasks":["noop"]},` +
		`"hourly":{"type":"cron","when":"0 * * * *","tasks":["noop"]},` +
		`"daily":{"type":"cron","when":"0 0 * * *","tasks":["noop"]}` +
		`}}`
	if err := os.WriteFile(filepath.Join(projectDir, "project.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := store.Open(filepath.Join(autoDir, "watch", "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	var clock time.Time
	service := daemon.New(db, fakeBackend{}, nil, func() time.Time { return clock }, nil)

	for m := range 10 {
		clock = base.Add(time.Duration(m) * time.Minute)
		if err := service.Tick(ctx); err != nil {
			t.Fatalf("tick at minute %d failed: %v", m, err)
		}
		service.WaitWorkers()
		if err := service.Reap(ctx); err != nil {
			t.Fatalf("reap at minute %d failed: %v", m, err)
		}
	}

	count, err := db.CountEventsByType(ctx, "trigger_evaluated")
	if err != nil {
		t.Fatalf("CountEventsByType failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 trigger_evaluated events, got %d", count)
	}
}

// TestConfigWarningDedup (AC-7): a persistent invalid config produces a
// config_warning every tick by default; with dedup it must be logged exactly once
// across many ticks.
func TestConfigWarningDedup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := t.TempDir()

	autoDir := filepath.Join(home, ".auto")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := fmt.Sprintf(`{"projects":[{"id":"demo","path":%q}]}`, repoRoot)
	if err := os.WriteFile(filepath.Join(autoDir, "projects.json"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(repoRoot, ".auto", "watch")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// An invalid task type fails validation on every tick, producing a
	// config_warning each time before dedup.
	cfg := `{"id":"demo","tasks":{"bad":{"type":"oops"}},"triggers":{}}`
	if err := os.WriteFile(filepath.Join(projectDir, "project.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := store.Open(filepath.Join(autoDir, "watch", "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	var clock time.Time
	service := daemon.New(db, fakeBackend{}, nil, func() time.Time { return clock }, nil)

	for m := range 10 {
		clock = base.Add(time.Duration(m) * time.Minute)
		if err := service.Tick(ctx); err != nil {
			t.Fatalf("tick at minute %d failed: %v", m, err)
		}
	}

	count, err := db.CountEventsByType(ctx, "config_warning")
	if err != nil {
		t.Fatalf("CountEventsByType failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 config_warning event after dedup, got %d", count)
	}
}
