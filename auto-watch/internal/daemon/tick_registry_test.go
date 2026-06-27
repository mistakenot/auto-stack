package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/store"
)

// A registry that has picked up stale/duplicate entries (e.g. /tmp fixture rows
// left by a test run) must not take the daemon down: Tick skips the unusable
// entries and returns nil instead of erroring on validation. This is the
// regression guard for the crash-loop where the autowatch daemon kept exiting
// 1 on "global settings failed validation: project id is already registered".
func TestTickSkipsStaleRegistryEntriesInsteadOfFailing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	db, err := store.Open(filepath.Join(t.TempDir(), "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	// Duplicate id across two non-existent paths — exactly the shape that the
	// strict validator rejected and that crash-looped the daemon.
	autoDir := filepath.Join(home, ".auto")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := `{"projects":[` +
		`{"id":"001","path":"/tmp/ghost-a-2718281828/001"},` +
		`{"id":"001","path":"/tmp/ghost-b-3141592653/001"}` +
		`]}`
	if err := os.WriteFile(filepath.Join(autoDir, "projects.json"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	svc := daemon.New(db, &recordingBackend{}, nil, func() time.Time { return now }, nil)

	if err := svc.Tick(context.Background()); err != nil {
		t.Fatalf("Tick should skip stale registry entries, not fail: %v", err)
	}
}
