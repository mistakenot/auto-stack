package github

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SyncState persists incremental sync progress across runs.
type SyncState struct {
	SchemaVersion int                   `json:"schema_version"`
	Repos         map[string]*RepoState `json:"repos"`
}

// RepoState tracks sync progress for a single repo.
type RepoState struct {
	HighWaterMark string                 `json:"high_water_mark,omitempty"` // RFC3339
	PRs           map[string]*PRSyncInfo `json:"prs"`
}

// PRSyncInfo tracks whether a specific PR has been fully synced.
type PRSyncInfo struct {
	Synced          bool     `json:"synced"`
	SyncedAt        string   `json:"synced_at,omitempty"` // RFC3339
	MissingFields   []string `json:"missing_fields,omitempty"`
	LastAttemptAt   string   `json:"last_attempt_at,omitempty"` // RFC3339
	FailedEndpoints []string `json:"failed_endpoints,omitempty"`
}

// SyncStatePath returns the default sync state file path.
func SyncStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".auto", "etl", "github", "sync-state.json")
}

// LoadSyncState reads sync state from disk. Returns empty state if file
// doesn't exist or is corrupt (with warning logged to stderr).
func LoadSyncState(path string) *SyncState {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: could not read sync state %s: %v\n", path, err)
		}
		return newSyncState()
	}

	var state SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: corrupt sync state %s: %v (treating as empty)\n", path, err)
		return newSyncState()
	}

	if state.Repos == nil {
		state.Repos = make(map[string]*RepoState)
	}
	for _, repo := range state.Repos {
		if repo.PRs == nil {
			repo.PRs = make(map[string]*PRSyncInfo)
		}
	}

	return &state
}

// Save writes sync state to disk atomically.
func (s *SyncState) Save(path string) error {
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

// GetRepo returns or creates the RepoState for a given owner/repo.
func (s *SyncState) GetRepo(ownerRepo string) *RepoState {
	if r, ok := s.Repos[ownerRepo]; ok {
		return r
	}
	r := &RepoState{PRs: make(map[string]*PRSyncInfo)}
	s.Repos[ownerRepo] = r
	return r
}

// HighWaterMarkTime parses the high water mark as a time.Time.
// Returns zero time if not set or unparseable.
func (r *RepoState) HighWaterMarkTime() time.Time {
	if r.HighWaterMark == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, r.HighWaterMark)
	if err != nil {
		return time.Time{}
	}
	return t
}

// MarkSynced records a PR as successfully synced.
func (r *RepoState) MarkSynced(prID string, missingFields []string) {
	r.PRs[prID] = &PRSyncInfo{
		Synced:        true,
		SyncedAt:      time.Now().UTC().Format(time.RFC3339),
		MissingFields: missingFields,
	}
}

// MarkFailed records a PR as failed (will be retried).
func (r *RepoState) MarkFailed(prID string, failedEndpoints []string) {
	r.PRs[prID] = &PRSyncInfo{
		Synced:          false,
		LastAttemptAt:   time.Now().UTC().Format(time.RFC3339),
		FailedEndpoints: failedEndpoints,
	}
}

// SyncedCount returns how many PRs have been successfully synced.
func (r *RepoState) SyncedCount() int {
	n := 0
	for _, info := range r.PRs {
		if info.Synced {
			n++
		}
	}
	return n
}

// FailedPRNumbers returns PR IDs that need retrying.
func (r *RepoState) FailedPRNumbers() []string {
	var result []string
	for id, info := range r.PRs {
		if !info.Synced {
			result = append(result, id)
		}
	}
	return result
}

func newSyncState() *SyncState {
	return &SyncState{
		SchemaVersion: 1,
		Repos:         make(map[string]*RepoState),
	}
}
