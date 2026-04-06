// [autodoc(e8d3cf9c@34e92e15, be609cd3)]
package linkcheck

import (
	"fmt"
	"os"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/linkscan"
)

type LinkStatus int

const (
	LinkOK LinkStatus = iota
	DocHashMismatch
	ScopeHashMismatch
	BothMismatch
	OrphanedTag
	MalformedTag
)

// LinkIssue represents a single staleness finding.
type LinkIssue struct {
	Status           LinkStatus
	Tag              linkscan.Tag
	DocFile          string // relative path to doc file (empty if orphaned)
	CurrentDocHash   string // current doc hash
	CurrentScopeHash string // current scope hash
}

// Check compares code tags to docs and returns only non-OK issues.
func Check(tags []linkscan.Tag, docs []doctree.Entry) ([]LinkIssue, error) {
	docsByID := make(map[string]doctree.Entry, len(docs))
	for i := range docs {
		d := &docs[i]
		if d.Id == "" {
			continue
		}
		if _, exists := docsByID[d.Id]; !exists {
			docsByID[d.Id] = *d
		}
	}

	fileCache := make(map[string]string)
	issues := make([]LinkIssue, 0)

	for _, tag := range tags {
		doc, found := docsByID[tag.DocId]
		if !found {
			issues = append(issues, LinkIssue{
				Status: OrphanedTag,
				Tag:    tag,
			})
			continue
		}

		content, ok := fileCache[tag.FilePath]
		if !ok {
			data, err := os.ReadFile(tag.FilePath)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", tag.FilePath, err)
			}
			content = string(data)
			fileCache[tag.FilePath] = content
		}

		scopeHash, err := linkscan.ComputeScopeHashFromContent(content, tag.Line)
		if err != nil {
			return nil, fmt.Errorf("scope hash %s:%d: %w", tag.FilePath, tag.Line, err)
		}

		docMismatch := tag.DocHash != doc.Hash
		scopeMismatch := tag.ScopeHash != scopeHash
		if !docMismatch && !scopeMismatch {
			continue
		}

		status := ScopeHashMismatch
		switch {
		case docMismatch && scopeMismatch:
			status = BothMismatch
		case docMismatch:
			status = DocHashMismatch
		}

		issues = append(issues, LinkIssue{
			Status:           status,
			Tag:              tag,
			DocFile:          docPath(&doc),
			CurrentDocHash:   doc.Hash,
			CurrentScopeHash: scopeHash,
		})
	}

	return issues, nil
}

// IssuesFromMalformed converts malformed scan findings into link issues.
func IssuesFromMalformed(malformed []linkscan.MalformedTag) []LinkIssue {
	issues := make([]LinkIssue, 0, len(malformed))
	for _, m := range malformed {
		issues = append(issues, LinkIssue{
			Status: MalformedTag,
			Tag: linkscan.Tag{
				FilePath: m.FilePath,
				Line:     m.Line,
				RawTag:   m.RawText,
			},
		})
	}
	return issues
}

func docPath(doc *doctree.Entry) string {
	if doc.RepoRelPath != "" {
		return doc.RepoRelPath
	}
	return doc.RelPath
}
