package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/linkscan"
)

// FolderLinkUpdate records one tag rewritten by FixedFolderLink.
type FolderLinkUpdate struct {
	Line   int    `json:"line"`
	DocID  string `json:"docId"`
	OldTag string `json:"oldTag"`
	NewTag string `json:"newTag"`
}

// FixedFolderLinkJSON is the JSON result of `auto doc fixed <dir>/.autodoc`.
type FixedFolderLinkJSON struct {
	Path    string             `json:"path"`
	Updated []FolderLinkUpdate `json:"updated"`
}

// FixedFolderLink rewrites every tag in a .autodoc folder link file to the
// current doc hash and folder scope hash. Running it is the acknowledgement
// that the linked docs were reviewed against the folder's code, mirroring
// `auto doc fixed <doc>` for doc hashes. Tags whose doc id is unknown are left
// untouched and reported as an error after the other tags are written.
func FixedFolderLink(filePath, rootDir, docsDir string, ignores []string) ([]FolderLinkUpdate, error) {
	entries, err := doctree.WalkRepo(rootDir, docsDir, ignores...)
	if err != nil {
		return nil, fmt.Errorf("walking docs: %w", err)
	}
	docsByID := make(map[string]doctree.Entry, len(entries))
	for i := range entries {
		if entries[i].Id != "" {
			docsByID[entries[i].Id] = entries[i]
		}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	scopeHash, err := linkscan.ComputeFolderScopeHash(filepath.Dir(filePath))
	if err != nil {
		return nil, err
	}

	var scan linkscan.ScanResult
	linkscan.ScanFolderLinkFile(filePath, data, &scan)

	lineEnding := "\n"
	if strings.Contains(string(data), "\r\n") {
		lineEnding = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

	updates := make([]FolderLinkUpdate, 0)
	problems := make([]string, 0)
	for _, m := range scan.Malformed {
		problems = append(problems, fmt.Sprintf("line %d: malformed tag %q; expected [autodoc"+"(<docId>@<docHash>, <scopeHash>)]", m.Line, m.RawText))
	}
	for _, tag := range scan.Tags {
		doc, ok := docsByID[tag.DocId]
		if !ok {
			problems = append(problems, fmt.Sprintf("line %d: doc id %s not found; remove the line or point it at a valid doc id", tag.Line, tag.DocId))
			continue
		}
		newTag := fmt.Sprintf("[autodoc"+"(%s@%s, %s)]", tag.DocId, doc.Hash, scopeHash)
		if newTag == tag.RawTag {
			continue
		}
		lines[tag.Line-1] = strings.Replace(lines[tag.Line-1], tag.RawTag, newTag, 1)
		updates = append(updates, FolderLinkUpdate{Line: tag.Line, DocID: tag.DocId, OldTag: tag.RawTag, NewTag: newTag})
	}

	if len(updates) > 0 {
		updated := strings.Join(lines, "\n")
		if lineEnding == "\r\n" {
			updated = strings.ReplaceAll(updated, "\n", "\r\n")
		}
		if err := os.WriteFile(filePath, []byte(updated), 0o644); err != nil {
			return updates, err
		}
	}

	if len(problems) > 0 {
		return updates, errors.New(filepath.ToSlash(filePath) + ":\n  " + strings.Join(problems, "\n  "))
	}
	return updates, nil
}
