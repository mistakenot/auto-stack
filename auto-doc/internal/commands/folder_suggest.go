package commands

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/datadyne-io/autodoc/internal/linkscan"
)

// FolderLinkSuggestion flags a folder whose inline tags to one doc could
// collapse into a single .autodoc line: either several tagged files in the
// folder itself, or tagged files spread across two or more of its direct
// subfolders (sibling folders).
type FolderLinkSuggestion struct {
	Dir     string   // repo-relative, slash-separated: where the .autodoc goes
	DocID   string   // 8-char hex doc ID
	Folders []string // repo-relative folders holding the inline tags, sorted
	Files   []string // repo-relative files carrying the inline tag, sorted
}

// suggestFolderLinks groups uncovered inline code tags per doc and returns
// migration candidates. A folder is a candidate when, together with its
// direct subfolders, at least two tagged folders exist (sibling spread), or
// when it alone holds two or more tagged files. Candidates are taken deepest
// first so each tagged folder lands in the tightest suggestion. The repo
// root is never suggested for a sibling spread: a root .autodoc would cover
// the whole repo. Folders already covered for that doc by a .autodoc in the
// folder or an ancestor are skipped — inline and folder links may coexist.
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

	// Tagged, uncovered folders per doc.
	byDoc := make(map[string][]string)
	for k := range files {
		if !isCovered(k.dir, k.docID) {
			byDoc[k.docID] = append(byDoc[k.docID], k.dir)
		}
	}

	out := make([]FolderLinkSuggestion, 0)
	for docID, dirs := range byDoc {
		tagged := make(map[string]bool, len(dirs))
		for _, d := range dirs {
			tagged[d] = true
		}

		// Candidate folders: every tagged folder and its parent.
		candidates := make(map[string]bool)
		for _, d := range dirs {
			candidates[d] = true
			if d != rootDir {
				candidates[filepath.Dir(d)] = true
			}
		}
		ordered := make([]string, 0, len(candidates))
		for c := range candidates {
			ordered = append(ordered, c)
		}
		sort.Slice(ordered, func(i, j int) bool {
			di, dj := strings.Count(ordered[i], string(filepath.Separator)), strings.Count(ordered[j], string(filepath.Separator))
			if di != dj {
				return di > dj
			}
			return ordered[i] < ordered[j]
		})

		consumed := make(map[string]bool)
		for _, c := range ordered {
			members := make([]string, 0)
			for d := range tagged {
				if consumed[d] {
					continue
				}
				if d == c || (d != rootDir && filepath.Dir(d) == c) {
					members = append(members, d)
				}
			}
			spread := len(members) >= 2 && c != rootDir
			single := len(members) == 1 && members[0] == c && len(files[key{c, docID}]) >= 2
			if !spread && !single {
				continue
			}
			s := FolderLinkSuggestion{Dir: relSlash(rootDir, c), DocID: docID}
			for _, d := range members {
				consumed[d] = true
				s.Folders = append(s.Folders, relSlash(rootDir, d))
				for f := range files[key{d, docID}] {
					s.Files = append(s.Files, relSlash(rootDir, f))
				}
			}
			sort.Strings(s.Folders)
			sort.Strings(s.Files)
			out = append(out, s)
		}
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
