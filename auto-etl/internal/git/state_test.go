package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGitSyncState_NonExistentFile(t *testing.T) {
	state := LoadGitSyncState("/tmp/does-not-exist-state.json")
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", state.SchemaVersion)
	}
	if len(state.Repos) != 0 {
		t.Errorf("repos should be empty, got %d", len(state.Repos))
	}
}

func TestLoadGitSyncState_CorruptJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "corrupt.json")
	if err := os.WriteFile(path, []byte("{not valid json!!!"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := LoadGitSyncState(path)
	if state == nil {
		t.Fatal("expected non-nil state on corrupt file")
	}
	if state.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", state.SchemaVersion)
	}
	if len(state.Repos) != 0 {
		t.Errorf("repos should be empty, got %d", len(state.Repos))
	}
}

func TestSaveAndLoadRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "state.json")

	state := newGitSyncState()
	repo := state.GetRepo("abc123")
	repo.MarkSeen([]string{"sha1", "sha2", "sha3"})

	if err := state.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded := LoadGitSyncState(path)
	if loaded.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", loaded.SchemaVersion)
	}

	r := loaded.Repos["abc123"]
	if r == nil {
		t.Fatal("expected repo abc123 to exist after reload")
	}
	for _, sha := range []string{"sha1", "sha2", "sha3"} {
		if r.IsNew(sha) {
			t.Errorf("expected sha %q to be seen after reload", sha)
		}
	}
}

func TestIsNew(t *testing.T) {
	repo := &GitRepoState{SeenSHAs: make(map[string]bool)}

	if !repo.IsNew("unknown") {
		t.Error("expected IsNew to return true for unknown SHA")
	}

	repo.SeenSHAs["known"] = true
	if repo.IsNew("known") {
		t.Error("expected IsNew to return false for known SHA")
	}
}

func TestMarkSeen(t *testing.T) {
	repo := &GitRepoState{SeenSHAs: make(map[string]bool)}
	repo.MarkSeen([]string{"a", "b", "c"})

	for _, sha := range []string{"a", "b", "c"} {
		if repo.IsNew(sha) {
			t.Errorf("expected %q to be seen after MarkSeen", sha)
		}
	}
	if !repo.IsNew("d") {
		t.Error("expected 'd' to still be new")
	}
}

func TestGetRepo(t *testing.T) {
	state := newGitSyncState()

	// Creates new repo state if not exists.
	r1 := state.GetRepo("repo1")
	if r1 == nil {
		t.Fatal("expected non-nil repo state")
	}
	if r1.SeenSHAs == nil {
		t.Fatal("expected initialized SeenSHAs map")
	}

	// Returns existing repo state on second call.
	r1.MarkSeen([]string{"x"})
	r2 := state.GetRepo("repo1")
	if r2 != r1 {
		t.Error("expected GetRepo to return same pointer")
	}
	if r2.IsNew("x") {
		t.Error("expected existing state to be preserved")
	}
}
