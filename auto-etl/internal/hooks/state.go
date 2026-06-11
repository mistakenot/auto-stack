package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// HooksSyncState persists incremental ingest progress across runs.
type HooksSyncState struct {
	SchemaVersion int                   `json:"schema_version"`
	Files         map[string]*FileState `json:"files"`
}

// FileState tracks the byte offset watermark for a single JSONL file.
type FileState struct {
	Offset int64 `json:"offset"`
}

// HooksSyncStatePath returns the default sync state file path.
func HooksSyncStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".auto", "etl", "hooks", "sync-state.json")
}

// LoadHooksSyncState reads sync state from disk. Returns empty state if file
// doesn't exist or is corrupt (with warning logged to stderr).
func LoadHooksSyncState(path string) *HooksSyncState {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: could not read hooks sync state %s: %v\n", path, err)
		}
		return newHooksSyncState()
	}

	var state HooksSyncState
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: corrupt hooks sync state %s: %v (treating as empty)\n", path, err)
		return newHooksSyncState()
	}
	if state.Files == nil {
		state.Files = make(map[string]*FileState)
	}
	return &state
}

// Save writes sync state to disk atomically.
func (s *HooksSyncState) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Write to temp file then rename for atomicity.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newHooksSyncState() *HooksSyncState {
	return &HooksSyncState{
		SchemaVersion: 1,
		Files:         make(map[string]*FileState),
	}
}
