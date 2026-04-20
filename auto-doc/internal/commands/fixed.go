package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/datadyne-io/autodoc/internal/linkscan"
	"github.com/datadyne-io/autodoc/internal/search"
)

// Fixed recalculates the hash for a single file and writes it back.
// If a search index exists at indexPath, the file is also re-indexed using indexRelPath
// as the document key (repo-relative path, e.g. "docs/auth.md").
// If indexPath is empty or the index doesn't exist, indexing is skipped silently.
func Fixed(filepath string, indexPath string, indexRelPath string) error {
	if linkscan.IsMarkdownPath(filepath) {
		if err := rewriteMarkdownTagScopeHashes(filepath); err != nil {
			return err
		}
	}

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

func rewriteMarkdownTagScopeHashes(filePath string) error {
	rootDir := repoRootForFile(filePath)
	entries, err := doctree.WalkRepo(rootDir, "docs")
	if err != nil {
		return err
	}

	scanResult, err := linkscan.ScanMarkdownDocs(entries)
	if err != nil {
		return err
	}

	tags := make([]linkscan.Tag, 0)
	for _, tag := range scanResult.Tags {
		if filepath.Clean(tag.FilePath) == filepath.Clean(filePath) {
			tags = append(tags, tag)
		}
	}
	if len(tags) == 0 {
		return nil
	}

	docsByID := make(map[string]doctree.Entry, len(entries))
	for i := range entries {
		if entries[i].Id != "" {
			docsByID[entries[i].Id] = entries[i]
		}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	lineEnding := "\n"
	if strings.Contains(string(data), "\r\n") {
		lineEnding = "\r\n"
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")

	for _, tag := range tags {
		target, ok := docsByID[tag.DocId]
		if !ok {
			fmt.Fprintf(os.Stderr, "skipping scope hash rewrite for %s:%d: doc not found\n", filePath, tag.Line)
			continue
		}
		if tag.DocHash != target.Hash {
			fmt.Fprintf(os.Stderr, "skipping scope hash rewrite for %s:%d: docHash stale, run 'autodoc fix' first\n", filePath, tag.Line)
			continue
		}

		scopeHash, err := linkscan.ComputeScopeHashFromContentForTag(content, &tag)
		if err != nil {
			return err
		}
		if scopeHash == tag.ScopeHash {
			continue
		}

		line := lines[tag.Line-1]
		lines[tag.Line-1] = strings.Replace(line, tag.ScopeHash, scopeHash, 1)
	}

	updated := strings.Join(lines, "\n")
	if lineEnding == "\r\n" {
		updated = strings.ReplaceAll(updated, "\n", "\r\n")
	}
	return os.WriteFile(filePath, []byte(updated), 0o644)
}

func repoRootForFile(filePath string) string {
	dir := filepath.Dir(filePath)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(filePath)
		}
		dir = parent
	}
}
