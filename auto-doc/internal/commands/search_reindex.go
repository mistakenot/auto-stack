package commands

import (
	"fmt"
	"io"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/search"
)

// SearchReindex rebuilds the full search index from all doc files.
// indexPath is the path to the Bluge index directory (e.g. "/abs/path/.autodoc/index").
// rootDir is the repository root.
// docsDir is the compatibility docs directory from config.
// ignores are glob patterns to skip.
func SearchReindex(w io.Writer, indexPath string, rootDir string, docsDir string, ignores []string) error {
	entries, err := doctree.WalkRepo(rootDir, docsDir, ignores...)
	if err != nil {
		return fmt.Errorf("walk docs: %w", err)
	}

	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer func() { _ = idx.Close() }()

	desiredPaths := make(map[string]struct{}, len(entries))
	count := 0
	for i := range entries {
		entry := &entries[i]
		repoRelPath := entryDisplayPath(entry)
		desiredPaths[repoRelPath] = struct{}{}
		normalized := search.Normalize(entry.Body)
		if err := idx.UpsertDoc(repoRelPath, entry.Title, entry.Summary, normalized.Headings, normalized.Body); err != nil {
			return fmt.Errorf("upsert doc %s: %w", repoRelPath, err)
		}
		count++
	}

	existingPaths, err := idx.ListDocPaths()
	if err != nil {
		return fmt.Errorf("list existing index docs: %w", err)
	}
	for _, existingPath := range existingPaths {
		if _, ok := desiredPaths[existingPath]; ok {
			continue
		}
		if err := idx.DeleteDoc(existingPath); err != nil {
			return fmt.Errorf("delete stale doc %s: %w", existingPath, err)
		}
	}

	fmt.Fprintf(w, "Indexed %d files\n", count)
	return nil
}
