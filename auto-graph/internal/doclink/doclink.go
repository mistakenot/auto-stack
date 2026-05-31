package doclink

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/datadyne-io/autodoc/pkg/docs"
	"github.com/datadyne-io/autodoc/pkg/scan"
	"github.com/mistakenot/auto-graph/internal/graph"
)

// Link represents a resolved connection from a source file to a documentation file.
type Link struct {
	SourceFile string // repo-relative path of the source file containing the tag
	DocFile    string // repo-relative path of the documentation file
	DocID      string // 8-char hex doc ID
	DocTitle   string // title from the doc frontmatter
}

// Scan discovers autodoc tags in the project and resolves them to doc entries,
// returning deduplicated links. Errors from scanning or doc walking are treated
// as soft failures: a warning is logged and an empty slice is returned.
func Scan(projectRoot string, warn io.Writer) ([]Link, error) {
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("abs root: %w", err)
	}

	result, err := scan.ScanFiles(absRoot)
	if err != nil {
		fmt.Fprintf(warn, "doclink: scan tags: %v\n", err)
		return nil, nil
	}

	entries, err := docs.WalkRepo(absRoot, "")
	if err != nil {
		fmt.Fprintf(warn, "doclink: walk docs: %v\n", err)
		return nil, nil
	}

	docByID := make(map[string]docs.Entry, len(entries))
	for i := range entries {
		if entries[i].Id != "" {
			docByID[entries[i].Id] = entries[i]
		}
	}

	type linkKey struct {
		source, doc string
	}
	seen := make(map[linkKey]bool)
	var links []Link

	for _, tag := range result.Tags {
		entry, ok := docByID[tag.DocId]
		if !ok {
			continue // orphaned tag — no matching doc
		}

		// tag.FilePath is absolute; convert to repo-relative.
		rel, err := filepath.Rel(absRoot, tag.FilePath)
		if err != nil {
			continue
		}
		sourceFile := filepath.ToSlash(rel)

		k := linkKey{source: sourceFile, doc: entry.RepoRelPath}
		if seen[k] {
			continue
		}
		seen[k] = true

		links = append(links, Link{
			SourceFile: sourceFile,
			DocFile:    entry.RepoRelPath,
			DocID:      tag.DocId,
			DocTitle:   entry.Title,
		})
	}

	return links, nil
}

// Enrich adds doc nodes and doc_link edges to an existing graph for every link
// whose SourceFile matches a node already present in the graph.
func Enrich(g *graph.Graph, links []Link) {
	if len(links) == 0 {
		return
	}

	// Build set of existing node IDs (repo-relative paths).
	nodeSet := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeSet[n.ID] = true
	}

	docNodeAdded := make(map[string]bool)

	type edgeKey struct {
		source, target string
	}
	edgeSeen := make(map[edgeKey]bool)

	for _, link := range links {
		if !nodeSet[link.SourceFile] {
			continue
		}

		// Add doc node if not already present.
		if !docNodeAdded[link.DocFile] && !nodeSet[link.DocFile] {
			g.Nodes = append(g.Nodes, graph.Node{
				ID:   link.DocFile,
				Kind: graph.NodeDoc,
				Path: link.DocFile,
				Attrs: map[string]string{
					"title": link.DocTitle,
				},
			})
			docNodeAdded[link.DocFile] = true
		}

		// Add edge if not already present.
		ek := edgeKey{source: link.SourceFile, target: link.DocFile}
		if edgeSeen[ek] {
			continue
		}
		edgeSeen[ek] = true

		g.Edges = append(g.Edges, graph.Edge{
			Source: link.SourceFile,
			Target: link.DocFile,
			Kind:   graph.EdgeDocLink,
		})
	}
}
