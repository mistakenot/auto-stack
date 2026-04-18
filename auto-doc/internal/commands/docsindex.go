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
// excludeTags filters out entries that have any of the specified tags.
func DocsIndex(w io.Writer, entries []doctree.Entry, docsDir string, excludeTags ...string) {
	_ = docsDir
	if len(entries) == 0 {
		return
	}

	// Filter out entries with excluded tags
	if len(excludeTags) > 0 {
		excludeSet := make(map[string]bool, len(excludeTags))
		for _, t := range excludeTags {
			excludeSet[strings.ToLower(strings.TrimSpace(t))] = true
		}
		filtered := make([]doctree.Entry, 0, len(entries))
		for i := range entries {
			if !hasExcludedTag(&entries[i], excludeSet) {
				filtered = append(filtered, entries[i])
			}
		}
		entries = filtered
		if len(entries) == 0 {
			return
		}
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
				fmt.Fprintf(w, "- [%s](%s): %s\n", title, linkPath, e.Summary)
			} else {
				fmt.Fprintf(w, "- [%s](%s)\n", title, linkPath)
			}
		}
	}
}

// hasExcludedTag returns true if the entry has any tag in the exclude set.
func hasExcludedTag(e *doctree.Entry, excludeSet map[string]bool) bool {
	for _, t := range e.Tags {
		if excludeSet[strings.ToLower(t)] {
			return true
		}
	}
	return false
}
