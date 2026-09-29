package commands

import (
	"path/filepath"
	"sort"

	"github.com/datadyne-io/autodoc/internal/linkscan"
)

// FolderLinkSuggestion flags a folder where several code files carry inline
// tags to the same doc; one .autodoc line would replace all of them.
type FolderLinkSuggestion struct {
	Dir   string   // repo-relative, slash-separated ("." for the root)
	DocID string   // 8-char hex doc ID
	Files []string // repo-relative files carrying the inline tag, sorted
}

// suggestFolderLinks groups inline code tags by (folder, doc) and returns the
// groups with two or more files. Folders already covered for that doc by a
// .autodoc in the folder or an ancestor are skipped: inline and folder links
// may coexist, so the hint is only for folders that have not migrated yet.
func suggestFolderLinks(rootDir string, tags []linkscan.Tag) []FolderLinkSuggestion {
	if abs, err := filepath.Abs(rootDir); err == nil {
		rootDir = abs
	}
	type key struct{ dir, docID string }
	covered := make(map[key]bool)
	files := make(map[key]map[string]bool)

	for _, tag := range tags {
		dir := filepath.Dir(tag.FilePath)
		switch tag.ScopeKind {
		case linkscan.ScopeKindFolder:
			covered[key{dir, tag.DocId}] = true
		case linkscan.ScopeKindIndent:
			k := key{dir, tag.DocId}
			if files[k] == nil {
				files[k] = make(map[string]bool)
			}
			files[k][tag.FilePath] = true
		}
	}

	isCovered := func(dir, docID string) bool {
		for {
			if covered[key{dir, docID}] {
				return true
			}
			if dir == rootDir {
				return false
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				return false
			}
			dir = parent
		}
	}

	out := make([]FolderLinkSuggestion, 0)
	for k, set := range files {
		if len(set) < 2 || isCovered(k.dir, k.docID) {
			continue
		}
		s := FolderLinkSuggestion{Dir: relSlash(rootDir, k.dir), DocID: k.docID}
		for f := range set {
			s.Files = append(s.Files, relSlash(rootDir, f))
		}
		sort.Strings(s.Files)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir < out[j].Dir
		}
		return out[i].DocID < out[j].DocID
	})
	return out
}

func relSlash(rootDir, p string) string {
	if rel, err := filepath.Rel(rootDir, p); err == nil {
		p = rel
	}
	return filepath.ToSlash(p)
}
