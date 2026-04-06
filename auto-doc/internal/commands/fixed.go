package commands

import (
	"os"

	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/datadyne-io/autodoc/internal/search"
)

// Fixed recalculates the hash for a single file and writes it back.
// If a search index exists at indexPath, the file is also re-indexed using indexRelPath
// as the document key (repo-relative path, e.g. "docs/auth.md").
// If indexPath is empty or the index doesn't exist, indexing is skipped silently.
func Fixed(filepath string, indexPath string, indexRelPath string) error {
	if err := frontmatter.UpdateHash(filepath); err != nil {
		return err
	}

	if indexPath == "" || !search.IndexExists(indexPath) {
		return nil
	}

	data, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}

	doc := frontmatter.Parse(string(data))
	normalized := search.Normalize(doc.Body)

	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	return idx.UpsertDoc(indexRelPath, doc.Title, doc.Summary, normalized.Headings, normalized.Body)
}
