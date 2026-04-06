package commands

import (
	"path/filepath"

	"github.com/datadyne-io/autodoc/internal/doctree"
)

func entryDisplayPath(entry *doctree.Entry) string {
	if entry.RepoRelPath != "" {
		return filepath.ToSlash(entry.RepoRelPath)
	}
	return filepath.ToSlash(entry.RelPath)
}
