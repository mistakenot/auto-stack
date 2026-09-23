package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// --full used to call os.RemoveAll on the whole output root BEFORE --only was
// consulted, and reset only the git cursor. So `auto etl run --full --only
// sessions` deleted the git, github and hooks datasets as collateral — and
// because their cursors survived, those sources skipped everything they had
// already ingested and the data could never be rebuilt. A single command,
// unrecoverable.
func TestResetSourceLeavesOtherDatasetsAlone(t *testing.T) {
	out := t.TempDir()
	all := []string{
		"messages", "sessions",
		"commits", "commit_files", "commit_hunks", "git_refs", "git_repositories",
		"pull_requests", "pull_request_comments",
		"hooks",
	}
	for _, d := range all {
		if err := os.MkdirAll(filepath.Join(out, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		if err := os.WriteFile(filepath.Join(out, d, "data.parquet"), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", d, err)
		}
	}

	if err := resetSource("sessions", out); err != nil {
		t.Fatalf("resetSource: %v", err)
	}

	for _, d := range all {
		_, err := os.Stat(filepath.Join(out, d))
		owned := d == "messages" || d == "sessions"
		switch {
		case owned && err == nil:
			t.Errorf("%s should have been removed by a sessions rebuild", d)
		case !owned && err != nil:
			t.Errorf("%s was destroyed by a sessions rebuild; --full must only touch the selected source's datasets", d)
		}
	}
}

// Every source that keeps an ingestion cursor must have it reset alongside its
// output. Deleting one without the other is the unrecoverable half of the old
// bug: the source skips what it has already seen, so the rows never come back.
func TestEverySourceWithACursorIsCoveredByReset(t *testing.T) {
	for _, source := range []string{"sessions", "git", "github", "hooks"} {
		if _, ok := sourceDatasets[source]; !ok {
			t.Errorf("source %q has no dataset mapping, so --full would silently rebuild nothing for it", source)
		}
	}

	// sessions re-derives everything from the raw corpus, so it is the only
	// source that legitimately has no cursor.
	for _, source := range []string{"git", "github", "hooks"} {
		if sourceCursor(source) == "" {
			t.Errorf("source %q reports no cursor; if that is wrong, --full deletes its output unrecoverably", source)
		}
	}
	if sourceCursor("sessions") != "" {
		t.Error("sessions is expected to have no cursor — it re-derives its output from the raw corpus every run")
	}
}

// Datasets must not be claimed by two sources, or rebuilding one would silently
// delete another's rows.
func TestDatasetsAreOwnedByExactlyOneSource(t *testing.T) {
	owner := map[string]string{}
	for source, datasets := range sourceDatasets {
		for _, d := range datasets {
			if prev, dup := owner[d]; dup {
				t.Errorf("dataset %q is claimed by both %q and %q", d, prev, source)
			}
			owner[d] = source
		}
	}
}

// A rebuild of a source that was never run must be a no-op, not an error.
func TestResetSourceIsANoOpWhenNothingExists(t *testing.T) {
	if err := resetSource("github", t.TempDir()); err != nil {
		t.Fatalf("resetSource on an empty output dir: %v", err)
	}
}

// The dataset map is hand-maintained, so it can drift the moment a new dataset
// is added to the writer — and a dataset no source owns is one --full silently
// leaves behind, quietly making a "full rebuild" partial. This reads the writer
// package's own output paths and asserts every one is claimed.
func TestDatasetMapCoversEveryWriterOutput(t *testing.T) {
	owned := map[string]bool{}
	for _, datasets := range sourceDatasets {
		for _, d := range datasets {
			owned[d] = true
		}
	}

	entries, err := os.ReadDir(filepath.Join("..", "internal", "writer"))
	if err != nil {
		t.Fatalf("read writer package: %v", err)
	}
	re := regexp.MustCompile(`filepath\.Join\(outputDir,\s*"([a-z_]+)"`)
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join("..", "internal", "writer", e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			seen[m[1]] = true
		}
	}

	if len(seen) == 0 {
		t.Fatal("found no output datasets in the writer package — this test has stopped checking anything")
	}
	for dataset := range seen {
		if !owned[dataset] {
			t.Errorf("dataset %q is written but no source owns it, so --full would leave it behind; "+
				"add it to sourceDatasets", dataset)
		}
	}
}
