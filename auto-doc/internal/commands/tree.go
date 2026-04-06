package commands

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/datadyne-io/autodoc/internal/doctree"
)

// TreeOutput renders entries in tree format and writes to w.
func TreeOutput(w io.Writer, entries []doctree.Entry, docsDir string) {
	rootName := docsDir
	if rootName == "" {
		rootName = "."
	}

	if len(entries) == 0 {
		fmt.Fprintf(w, "%s/\n", rootName)
		return
	}

	// Build a tree structure
	type node struct {
		name     string
		entry    *doctree.Entry // nil for directories
		children []*node
	}

	root := &node{name: rootName}
	nodeMap := map[string]*node{"": root}

	// Ensure all directories exist
	for i := range entries {
		displayPath := entryDisplayPath(&entries[i])
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

	// Add file entries
	for i := range entries {
		e := &entries[i]
		displayPath := entryDisplayPath(e)
		dir := path.Dir(displayPath)
		if dir == "." {
			dir = ""
		}
		n := &node{name: path.Base(displayPath), entry: e}
		nodeMap[dir].children = append(nodeMap[dir].children, n)
	}

	// Sort children: dirs first, then files, alphabetically within each group
	var sortChildren func(n *node)
	sortChildren = func(n *node) {
		sort.Slice(n.children, func(i, j int) bool {
			iDir := n.children[i].entry == nil
			jDir := n.children[j].entry == nil
			if iDir != jDir {
				return iDir // dirs first
			}
			return n.children[i].name < n.children[j].name
		})
		for _, c := range n.children {
			sortChildren(c)
		}
	}
	sortChildren(root)

	// Render
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
				// Directory
				fmt.Fprintf(w, "%s%s%s/\n", prefix, connector, c.name)
				nextPrefix := prefix + "│   "
				if isLast {
					nextPrefix = prefix + "    "
				}
				render(c.children, nextPrefix)
			} else {
				// File
				line := fmt.Sprintf("%s%s%s", prefix, connector, c.name)
				if c.entry.Title != "" {
					line += fmt.Sprintf(" — %q", c.entry.Title)
				}
				if c.entry.Summary != "" {
					line += " — " + c.entry.Summary
				}
				fmt.Fprintln(w, line)
			}
		}
	}
	render(root.children, "")
}
