package git

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// GitSyncState persists incremental sync progress across runs.
type GitSyncState struct {
	SchemaVersion int                      `json:"schema_version"`
	Repos         map[string]*GitRepoState `json:"repos"`
}

// GitRepoState tracks sync progress for a single repo.
type GitRepoState struct {
	SeenSHAs map[string]bool `json:"seen_shas"`
}

// GitSyncStatePath returns the default sync state file path.
func GitSyncStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".auto", "etl", "git", "sync-state.json")
}

// LoadGitSyncState reads sync state from disk. Returns empty state if file
// doesn't exist or is corrupt (with warning logged to stderr).
func LoadGitSyncState(path string) *GitSyncState {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: could not read sync state %s: %v\n", path, err)
		}
		return newGitSyncState()
	}

	var state GitSyncState
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: corrupt sync state %s: %v (treating as empty)\n", path, err)
		return newGitSyncState()
	}

	if state.Repos == nil {
		state.Repos = make(map[string]*GitRepoState)
	}
	for _, repo := range state.Repos {
		if repo.SeenSHAs == nil {
			repo.SeenSHAs = make(map[string]bool)
		}
	}

	return &state
}

// Save writes sync state to disk atomically.
func (s *GitSyncState) Save(path string) error {
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

// GetRepo returns or creates the GitRepoState for a given repo ID.
func (s *GitSyncState) GetRepo(repoID string) *GitRepoState {
	if r, ok := s.Repos[repoID]; ok {
		return r
	}
	r := &GitRepoState{SeenSHAs: make(map[string]bool)}
	s.Repos[repoID] = r
	return r
}

// IsNew returns true if the SHA has NOT been seen before.
func (r *GitRepoState) IsNew(sha string) bool {
	return !r.SeenSHAs[sha]
}

// MarkSeen adds a batch of SHAs to the seen set.
func (r *GitRepoState) MarkSeen(shas []string) {
	for _, sha := range shas {
		r.SeenSHAs[sha] = true
	}
}

func newGitSyncState() *GitSyncState {
	return &GitSyncState{
		SchemaVersion: 1,
		Repos:         make(map[string]*GitRepoState),
	}
}
