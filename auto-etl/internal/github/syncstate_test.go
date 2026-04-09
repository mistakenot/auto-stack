package github

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncState_ReadWriteRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync-state.json")

	state := newSyncState()
	repo := state.GetRepo("owner/repo")
	repo.HighWaterMark = "2026-04-06T12:00:00Z"
	repo.MarkSynced("owner/repo#123", nil)
	repo.MarkFailed("owner/repo#124", []string{"reviews"})

	if err := state.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded := LoadSyncState(path)
	r := loaded.GetRepo("owner/repo")

	if r.HighWaterMark != "2026-04-06T12:00:00Z" {
		t.Errorf("high water mark = %q, want 2026-04-06T12:00:00Z", r.HighWaterMark)
	}
	if info, ok := r.PRs["owner/repo#123"]; !ok || !info.Synced {
		t.Error("PR 123 should be synced")
	}
	if info, ok := r.PRs["owner/repo#124"]; !ok || info.Synced {
		t.Error("PR 124 should be failed")
	}
}

func TestSyncState_MissingFile(t *testing.T) {
	state := LoadSyncState("/nonexistent/path/sync-state.json")
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if len(state.Repos) != 0 {
		t.Errorf("expected empty repos, got %d", len(state.Repos))
	}
}

func TestSyncState_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync-state.json")
	os.WriteFile(path, []byte("{invalid json"), 0o644)

	state := LoadSyncState(path)
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if len(state.Repos) != 0 {
		t.Errorf("expected empty repos, got %d", len(state.Repos))
	}
}

func TestRepoState_FailedPRNumbers(t *testing.T) {
	repo := &RepoState{PRs: map[string]*PRSyncInfo{
		"owner/repo#1": {Synced: true},
		"owner/repo#2": {Synced: false},
		"owner/repo#3": {Synced: false},
		"owner/repo#4": {Synced: true},
	}}

	failed := repo.FailedPRNumbers()
	if len(failed) != 2 {
		t.Errorf("got %d failed, want 2", len(failed))
	}
}

func TestRepoState_HighWaterMarkTime(t *testing.T) {
	repo := &RepoState{HighWaterMark: "2026-04-06T12:00:00Z"}
	hwm := repo.HighWaterMarkTime()
	if hwm.IsZero() {
		t.Error("expected non-zero time")
	}
	if hwm.Year() != 2026 || hwm.Month() != 4 || hwm.Day() != 6 {
		t.Errorf("unexpected time: %v", hwm)
	}
}

func TestRepoState_HighWaterMarkTimeEmpty(t *testing.T) {
	repo := &RepoState{}
	hwm := repo.HighWaterMarkTime()
	if !hwm.IsZero() {
		t.Error("expected zero time for empty high water mark")
	}
}
