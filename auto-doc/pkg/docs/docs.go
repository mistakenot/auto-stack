// Package docs provides public access to autodoc's doc-tree walking functionality.
package docs

import "github.com/datadyne-io/autodoc/internal/doctree"

// Entry represents a single doc file in the tree.
type Entry = doctree.Entry

// WalkRepo discovers markdown docs recursively across the repository and returns merged entries.
// docsDir is treated as a compatibility include root in addition to directories named "docs".
func WalkRepo(rootDir, docsDir string, ignores ...string) ([]Entry, error) {
	return doctree.WalkRepo(rootDir, docsDir, ignores...)
}
