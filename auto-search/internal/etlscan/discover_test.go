package etlscan

import (
	"path/filepath"
	"runtime"
	"testing"
)

// fixtureInputDir returns the absolute path to testdata/etl-output, which holds
// the checked-in messages/ and sessions/ parquet fixtures (no git datasets).
func fixtureInputDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "etl-output")
}

// TestDiscoverOnlyMessagesAndSessions is a regression guard: Discover must NOT
// broaden to git datasets, since the FTS indexer writes index_state /
// FilesProcessed for any discovered source. Git datasets are read separately
// via DiscoverDatasets by co-change.
func TestDiscoverOnlyMessagesAndSessions(t *testing.T) {
	sources, err := Discover(fixtureInputDir(t))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("expected at least one discovered source")
	}
	allowed := map[string]bool{"messages": true, "sessions": true}
	seen := map[string]bool{}
	for _, s := range sources {
		if !allowed[s.Dataset] {
			t.Errorf("Discover returned disallowed dataset %q (path %s)", s.Dataset, s.Path)
		}
		seen[s.Dataset] = true
	}
	if !seen["messages"] || !seen["sessions"] {
		t.Errorf("expected both messages and sessions datasets, saw %v", seen)
	}
}

// TestDiscoverDatasetsSkipsAbsent confirms DiscoverDatasets silently skips
// datasets whose subdirectory is absent (the testdata fixture has no git data).
func TestDiscoverDatasetsSkipsAbsent(t *testing.T) {
	sources, err := DiscoverDatasets(fixtureInputDir(t), []string{"commits", "commit_files", "git_repositories", "git_refs"})
	if err != nil {
		t.Fatalf("DiscoverDatasets: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("expected no git sources in messages/sessions fixture, got %d", len(sources))
	}
}
