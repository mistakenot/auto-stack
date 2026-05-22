package commands

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/linkcheck"
	"github.com/datadyne-io/autodoc/internal/linkscan"
)

type GraphJSON struct {
	Nodes []GraphNodeJSON  `json:"nodes"`
	Edges []GraphEdgeJSON  `json:"edges"`
	Stats GraphStatsJSON   `json:"stats"`
}

type GraphNodeJSON struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
	Hash    string `json:"hash,omitempty"`
}

type GraphEdgeJSON struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Line      int    `json:"line"`
	Status    string `json:"status"`
	LinkType  string `json:"link_type"`
	DocHash   string `json:"doc_hash"`
	ScopeHash string `json:"scope_hash"`
}

type GraphStatsJSON struct {
	TotalDocs     int `json:"total_docs"`
	ConnectedDocs int `json:"connected_docs"`
	IsolatedDocs  int `json:"isolated_docs"`
	TotalCode     int `json:"total_code_files"`
	TotalEdges    int `json:"total_edges"`
	OKEdges       int `json:"ok_edges"`
	StaleEdges    int `json:"stale_edges"`
	OrphanedEdges int `json:"orphaned_edges"`
}

type issueKey struct {
	filePath string
	line     int
}

func BuildGraph(rootDir string, docsDir string, ignores []string) (*GraphJSON, error) {
	entries, err := doctree.WalkRepo(rootDir, docsDir, ignores...)
	if err != nil {
		return nil, fmt.Errorf("walking docs: %w", err)
	}

	codeScan, err := linkscan.ScanFiles(rootDir)
	if err != nil {
		return nil, fmt.Errorf("scanning source tags: %w", err)
	}

	mdScan, err := linkscan.ScanMarkdownDocs(entries)
	if err != nil {
		return nil, fmt.Errorf("scanning markdown tags: %w", err)
	}

	allTags := make([]linkscan.Tag, 0, len(codeScan.Tags)+len(mdScan.Tags))
	allTags = append(allTags, codeScan.Tags...)
	allTags = append(allTags, mdScan.Tags...)

	allMalformed := make([]linkscan.MalformedTag, 0, len(codeScan.Malformed)+len(mdScan.Malformed))
	allMalformed = append(allMalformed, codeScan.Malformed...)
	allMalformed = append(allMalformed, mdScan.Malformed...)

	issues, err := linkcheck.Check(allTags, entries)
	if err != nil {
		return nil, fmt.Errorf("checking links: %w", err)
	}

	issueMap := make(map[issueKey]*linkcheck.LinkIssue, len(issues))
	for i := range issues {
		iss := &issues[i]
		issueMap[issueKey{filePath: iss.Tag.FilePath, line: iss.Tag.Line}] = iss
	}

	docsByID := make(map[string]*doctree.Entry, len(entries))
	for i := range entries {
		e := &entries[i]
		if e.Id != "" {
			docsByID[e.Id] = e
		}
	}

	nodes := make([]GraphNodeJSON, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		nodes = append(nodes, GraphNodeJSON{
			Path:    entryDisplayPath(e),
			Type:    "doc",
			ID:      e.Id,
			Title:   e.Title,
			Summary: e.Summary,
			Hash:    e.Hash,
		})
	}

	codeFiles := make(map[string]bool)
	edges := make([]GraphEdgeJSON, 0, len(allTags))

	for _, tag := range allTags {
		sourcePath := tag.FilePath
		if rel, err := filepath.Rel(rootDir, tag.FilePath); err == nil {
			sourcePath = rel
		}
		sourcePath = filepath.ToSlash(sourcePath)

		targetPath := ""
		if doc, ok := docsByID[tag.DocId]; ok {
			targetPath = entryDisplayPath(doc)
		}

		status := "ok"
		if iss, found := issueMap[issueKey{filePath: tag.FilePath, line: tag.Line}]; found {
			status = linkStatusString(iss.Status)
		}

		linkType := "code_to_doc"
		if tag.ScopeKind == linkscan.ScopeKindMarkdown {
			linkType = "doc_to_doc"
		}

		if linkType == "code_to_doc" {
			codeFiles[sourcePath] = true
		}

		edges = append(edges, GraphEdgeJSON{
			Source:    sourcePath,
			Target:    targetPath,
			Line:      tag.Line,
			Status:    status,
			LinkType:  linkType,
			DocHash:   tag.DocHash,
			ScopeHash: tag.ScopeHash,
		})
	}

	for _, m := range allMalformed {
		sourcePath := m.FilePath
		if rel, err := filepath.Rel(rootDir, m.FilePath); err == nil {
			sourcePath = rel
		}
		sourcePath = filepath.ToSlash(sourcePath)

		codeFiles[sourcePath] = true

		edges = append(edges, GraphEdgeJSON{
			Source:   sourcePath,
			Target:   "",
			Line:     m.Line,
			Status:   "malformed",
			LinkType: "code_to_doc",
		})
	}

	for path := range codeFiles {
		nodes = append(nodes, GraphNodeJSON{
			Path: path,
			Type: "code",
		})
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Type != nodes[j].Type {
			return nodes[i].Type < nodes[j].Type
		}
		return nodes[i].Path < nodes[j].Path
	})
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Line < edges[j].Line
	})

	connectedDocs := make(map[string]bool)
	for _, e := range edges {
		if e.Target != "" {
			connectedDocs[e.Target] = true
		}
		for _, n := range nodes {
			if n.Type == "doc" && n.Path == e.Source {
				connectedDocs[e.Source] = true
				break
			}
		}
	}

	var okEdges, staleEdges, orphanedEdges int
	for _, e := range edges {
		switch e.Status {
		case "ok":
			okEdges++
		case "orphaned_tag", "malformed":
			orphanedEdges++
		default:
			staleEdges++
		}
	}

	graph := &GraphJSON{
		Nodes: nodes,
		Edges: edges,
		Stats: GraphStatsJSON{
			TotalDocs:     len(entries),
			ConnectedDocs: len(connectedDocs),
			IsolatedDocs:  len(entries) - len(connectedDocs),
			TotalCode:     len(codeFiles),
			TotalEdges:    len(edges),
			OKEdges:       okEdges,
			StaleEdges:    staleEdges,
			OrphanedEdges: orphanedEdges,
		},
	}
	return graph, nil
}

func GraphOutputJSON(w io.Writer, graph *GraphJSON) error {
	return WriteJSON(w, graph)
}

func GraphOutput(w io.Writer, graph *GraphJSON) {
	fmt.Fprintf(w, "# Document Graph\n\n")

	staleLabel := ""
	if graph.Stats.StaleEdges > 0 {
		staleLabel = fmt.Sprintf(", %d stale", graph.Stats.StaleEdges)
	}
	orphanLabel := ""
	if graph.Stats.OrphanedEdges > 0 {
		orphanLabel = fmt.Sprintf(", %d orphaned", graph.Stats.OrphanedEdges)
	}
	fmt.Fprintf(w, "%d docs, %d code files, %d connections (%d ok%s%s)\n\n",
		graph.Stats.TotalDocs, graph.Stats.TotalCode, graph.Stats.TotalEdges,
		graph.Stats.OKEdges, staleLabel, orphanLabel)

	inbound := make(map[string][]GraphEdgeJSON)
	outbound := make(map[string][]GraphEdgeJSON)
	docNodes := make(map[string]GraphNodeJSON)

	for _, n := range graph.Nodes {
		if n.Type == "doc" {
			docNodes[n.Path] = n
		}
	}

	for _, e := range graph.Edges {
		if e.Target != "" {
			inbound[e.Target] = append(inbound[e.Target], e)
		}
		if _, isDoc := docNodes[e.Source]; isDoc {
			outbound[e.Source] = append(outbound[e.Source], e)
		}
	}

	connectedPaths := make([]string, 0)
	isolatedPaths := make([]string, 0)
	for _, n := range graph.Nodes {
		if n.Type != "doc" {
			continue
		}
		if len(inbound[n.Path]) > 0 || len(outbound[n.Path]) > 0 {
			connectedPaths = append(connectedPaths, n.Path)
		} else {
			isolatedPaths = append(isolatedPaths, n.Path)
		}
	}

	if len(connectedPaths) > 0 {
		fmt.Fprintf(w, "## Connected Documents\n\n")
		for _, docPath := range connectedPaths {
			node := docNodes[docPath]
			title := node.Title
			if title == "" {
				title = docPath
			}
			fmt.Fprintf(w, "### %s -- %q\n", docPath, title)

			if refs := inbound[docPath]; len(refs) > 0 {
				fmt.Fprintf(w, "  Referenced by:\n")
				for _, e := range refs {
					fmt.Fprintf(w, "  - %s:%d (%s)\n", e.Source, e.Line, e.Status)
				}
			}
			if refs := outbound[docPath]; len(refs) > 0 {
				fmt.Fprintf(w, "  References:\n")
				for _, e := range refs {
					target := e.Target
					if target == "" {
						target = "(unknown)"
					}
					fmt.Fprintf(w, "  - %s (%s)\n", target, e.Status)
				}
			}
			fmt.Fprintln(w)
		}
	}

	if len(isolatedPaths) > 0 {
		fmt.Fprintf(w, "## Isolated Documents\n\n")
		for _, docPath := range isolatedPaths {
			node := docNodes[docPath]
			title := node.Title
			if title == "" {
				title = docPath
			}
			fmt.Fprintf(w, "- %s -- %q\n", docPath, title)
		}
		fmt.Fprintln(w)
	}

	if len(connectedPaths) == 0 && len(isolatedPaths) == 0 {
		fmt.Fprintln(w, "No documents found.")
	}
}

