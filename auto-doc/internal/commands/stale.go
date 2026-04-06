package commands

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/frontmatter"
)

// StaleResult holds the result of a stale check.
type StaleResult struct {
	StaleFiles []doctree.Entry
	HasStale   bool
}

// CheckStale identifies files with incorrect or missing hashes.
func CheckStale(entries []doctree.Entry) StaleResult {
	var stale []doctree.Entry
	for i := range entries {
		e := &entries[i]
		expected := frontmatter.ComputeHash(&frontmatter.Doc{
			Title:   e.Title,
			Summary: e.Summary,
			Body:    e.Body,
		})
		if e.Hash != expected || e.Title == "" || e.Summary == "" {
			stale = append(stale, *e)
		}
	}
	return StaleResult{StaleFiles: stale, HasStale: len(stale) > 0}
}

// StaleOutput renders stale files in tree format with "Stale" in place of summary.
func StaleOutput(w io.Writer, entries []doctree.Entry, staleFiles []doctree.Entry, docsDir string) {
	rootName := docsDir
	if rootName == "" {
		rootName = "."
	}

	staleSet := make(map[string]bool)
	for i := range staleFiles {
		staleSet[entryDisplayPath(&staleFiles[i])] = true
	}

	// Build modified entries with "Stale" summary for stale files
	modified := make([]doctree.Entry, len(entries))
	for i := range entries {
		modified[i] = entries[i]
		if staleSet[entryDisplayPath(&entries[i])] {
			modified[i].Summary = "\033[31mStale\033[0m"
		}
	}

	// Reuse tree structure from TreeOutput but inline here for stale display
	type node struct {
		name     string
		entry    *doctree.Entry
		children []*node
	}

	root := &node{name: rootName}
	nodeMap := map[string]*node{"": root}

	for i := range modified {
		displayPath := entryDisplayPath(&modified[i])
		dir := path.Dir(displayPath)
		if dir == "." {
			continue
		}
		parts := strings.Split(dir, "/")
		for i := range parts {
			dirPath := strings.Join(parts[:i+1], "/")
			if _, ok := nodeMap[dirPath]; !ok {
				parentPath := ""
				if i > 0 {
					parentPath = strings.Join(parts[:i], "/")
				}
				n := &node{name: parts[i]}
				nodeMap[dirPath] = n
				nodeMap[parentPath].children = append(nodeMap[parentPath].children, n)
			}
		}
	}

	for i := range modified {
		e := &modified[i]
		displayPath := entryDisplayPath(e)
		dir := path.Dir(displayPath)
		if dir == "." {
			dir = ""
		}
		n := &node{name: path.Base(displayPath), entry: e}
		nodeMap[dir].children = append(nodeMap[dir].children, n)
	}

	var sortChildren func(n *node)
	sortChildren = func(n *node) {
		sort.Slice(n.children, func(i, j int) bool {
			iDir := n.children[i].entry == nil
			jDir := n.children[j].entry == nil
			if iDir != jDir {
				return iDir
			}
			return n.children[i].name < n.children[j].name
		})
		for _, c := range n.children {
			sortChildren(c)
		}
	}
	sortChildren(root)

	fmt.Fprintf(w, "%s/\n", root.name)
	var render func(children []*node, prefix string)
	render = func(children []*node, prefix string) {
		for i, c := range children {
			isLast := i == len(children)-1
			connector := "├── "
			if isLast {
				connector = "└── "
			}
			if c.entry == nil {
				fmt.Fprintf(w, "%s%s%s/\n", prefix, connector, c.name)
				nextPrefix := prefix + "│   "
				if isLast {
					nextPrefix = prefix + "    "
				}
				render(c.children, nextPrefix)
			} else {
				line := fmt.Sprintf("%s%s%s", prefix, connector, c.name)
				if c.entry.Title != "" {
					line += fmt.Sprintf(" — %q", c.entry.Title)
				}
				line += " — " + c.entry.Summary
				fmt.Fprintln(w, line)
			}
		}
	}
	render(root.children, "")
}
