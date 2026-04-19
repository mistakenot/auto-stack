package commands

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/datadyne-io/autodoc/internal/linkcheck"
)

func linkStatusString(s linkcheck.LinkStatus) string {
	switch s {
	case linkcheck.DocHashMismatch:
		return "doc_hash_mismatch"
	case linkcheck.ScopeHashMismatch:
		return "scope_hash_mismatch"
	case linkcheck.BothMismatch:
		return "both_mismatch"
	case linkcheck.OrphanedTag:
		return "orphaned_tag"
	case linkcheck.MalformedTag:
		return "malformed_tag"
	case linkcheck.SelfReferencingTag:
		return "self_referencing_tag"
	default:
		return "unknown"
	}
}

// DocJSON is the JSON representation of a doc entry.
type DocJSON struct {
	Path     string `json:"path"`
	ID       string `json:"id,omitempty"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	ReadWhen string `json:"read_when,omitempty"`
	Hash     string `json:"hash"`
}

// StaleDocJSON is the JSON representation of a stale doc.
type StaleDocJSON struct {
	Path     string   `json:"path"`
	ID       string   `json:"id,omitempty"`
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	ReadWhen string   `json:"read_when,omitempty"`
	Hash     string   `json:"hash"`
	Issues   []string `json:"issues"`
}

// FixIssueJSON is the JSON representation of a fix issue.
type FixIssueJSON struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Details string `json:"details"`
}

// FixedResultJSON is the JSON representation of a fixed result.
type FixedResultJSON struct {
	Path    string `json:"path"`
	OldHash string `json:"oldHash"`
	NewHash string `json:"newHash"`
}

// WriteJSON writes a value as indented JSON to w.
func WriteJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// TreeOutputJSON writes entries as a JSON array to w.
func TreeOutputJSON(w io.Writer, entries []doctree.Entry) error {
	docs := make([]DocJSON, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		docs = append(docs, DocJSON{
			Path:     entryDisplayPath(e),
			ID:       e.Id,
			Title:    e.Title,
			Summary:  e.Summary,
			ReadWhen: e.ReadWhen,
			Hash:     e.Hash,
		})
	}
	return WriteJSON(w, docs)
}

// StaleOutputJSON writes stale files as a JSON array to w.
func StaleOutputJSON(w io.Writer, staleFiles []doctree.Entry) error {
	docs := make([]StaleDocJSON, 0, len(staleFiles))
	for i := range staleFiles {
		e := &staleFiles[i]
		var issues []string
		if e.Title == "" || e.Summary == "" {
			issues = append(issues, "missing_frontmatter")
		}
		expected := frontmatter.ComputeHash(&frontmatter.Doc{
			Title:   e.Title,
			Summary: e.Summary,
			Body:    e.Body,
		})
		if e.Hash != expected {
			issues = append(issues, "stale_hash")
		}
		if e.Title == "" {
			issues = append(issues, "default_title")
		}
		if e.ReadWhen == "" {
			issues = append(issues, "empty_read_when")
		}
		docs = append(docs, StaleDocJSON{
			Path:     entryDisplayPath(e),
			ID:       e.Id,
			Title:    e.Title,
			Summary:  e.Summary,
			ReadWhen: e.ReadWhen,
			Hash:     e.Hash,
			Issues:   issues,
		})
	}
	return WriteJSON(w, docs)
}

// FixOutputJSON writes all fix issues as a JSON array to w.
func FixOutputJSON(w io.Writer, docIssues []docIssue, linkIssues []linkcheck.LinkIssue) error {
	issues := make([]FixIssueJSON, 0, len(docIssues)+len(linkIssues))
	for _, d := range docIssues {
		if d.MissingFM {
			issues = append(issues, FixIssueJSON{
				Type:    "missing_frontmatter",
				Path:    d.RepoRelPath,
				Details: "File has no frontmatter with title, summary, and hash fields",
			})
		}
		if d.StaleHash {
			issues = append(issues, FixIssueJSON{
				Type:    "stale_hash",
				Path:    d.RepoRelPath,
				Details: "Hash does not match content",
			})
		}
		if d.DefaultTitle {
			issues = append(issues, FixIssueJSON{
				Type:    "default_title",
				Path:    d.RepoRelPath,
				Details: "Title is empty or matches filename",
			})
		}
		if d.EmptyReadWhen {
			issues = append(issues, FixIssueJSON{
				Type:    "empty_read_when",
				Path:    d.RepoRelPath,
				Details: "read_when is empty",
			})
		}
	}
	for i := range linkIssues {
		l := &linkIssues[i]
		issues = append(issues, FixIssueJSON{
			Type:    linkStatusString(l.Status),
			Path:    l.Tag.FilePath,
			Details: fmt.Sprintf("Tag at line %d: doc=%s@%s scope=%s", l.Tag.Line, l.Tag.DocId, l.Tag.DocHash, l.Tag.ScopeHash),
		})
	}
	return WriteJSON(w, issues)
}
