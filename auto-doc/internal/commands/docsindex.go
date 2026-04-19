package commands

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/datadyne-io/autodoc/internal/doctree"
)

// DocsIndex renders entries in llms.txt style, grouped by parent directory.
// Links are repo-relative when available.
func DocsIndex(w io.Writer, entries []doctree.Entry, docsDir string) {
	_ = docsDir
	if len(entries) == 0 {
		return
	}

	// Group entries by parent directory (relative to docs root)
	groups := make(map[string][]doctree.Entry)
	var groupOrder []string
	for i := range entries {
		dir := path.Dir(entryDisplayPath(&entries[i]))
		if dir == "." {
			dir = ""
		}
		if _, seen := groups[dir]; !seen {
			groupOrder = append(groupOrder, dir)
		}
		groups[dir] = append(groups[dir], entries[i])
	}

	sort.Strings(groupOrder)

	// Sort entries within each group by filename
	for _, g := range groupOrder {
		sort.Slice(groups[g], func(i, j int) bool {
			return path.Base(entryDisplayPath(&groups[g][i])) < path.Base(entryDisplayPath(&groups[g][j]))
		})
	}

	for i, dir := range groupOrder {
		if i > 0 {
			fmt.Fprintln(w)
		}

		if dir != "" {
			fmt.Fprintf(w, "**%s**\n\n", dir)
		}

		for i := range groups[dir] {
			e := &groups[dir][i]
			linkPath := entryDisplayPath(e)
			title := e.Title
			if title == "" {
				title = path.Base(linkPath)
			}
			if e.Summary != "" {
				summary := strings.TrimRight(e.Summary, ".")
				line := fmt.Sprintf("- [%s](%s): %s", title, linkPath, summary)
				if e.ReadWhen != "" {
					line += fmt.Sprintf(". Read when: %s", e.ReadWhen)
				}
				fmt.Fprintln(w, line)
			} else {
				fmt.Fprintf(w, "- [%s](%s)\n", title, linkPath)
			}
		}
	}
}
