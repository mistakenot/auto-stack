package contextpack

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-graph/internal/graph"
)

// NormalizeAndValidateSeeds normalizes seed file paths and validates them against
// the project root and the graph node set. It returns the deduplicated list of
// normalized paths in input order, or a ValidationErrors if any paths are invalid.
//
// Normalization steps per path:
//   - Trim leading/trailing whitespace
//   - filepath.Clean
//   - Convert absolute paths inside the project root to relative
//   - Convert to forward-slash separators
//
// Validation checks (in order):
//   - Reject paths that resolve outside the project root
//   - Reject paths that do not exist on disk
//   - Reject paths that are not present in the graph node set
func NormalizeAndValidateSeeds(seeds []string, projectRoot string, g *graph.Graph) ([]string, error) {
	nodeSet := buildNodeSet(g)

	var errs []ValidationError
	seen := make(map[string]bool)
	var result []string

	for _, raw := range seeds {
		// Trim whitespace
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}

		// Clean path
		s = filepath.Clean(s)

		// Convert absolute paths inside project root to relative
		if filepath.IsAbs(s) {
			rel, err := filepath.Rel(projectRoot, s)
			if err != nil || strings.HasPrefix(rel, "..") {
				errs = append(errs, ValidationError{
					Code:    "outside_project",
					Path:    raw,
					Field:   "file",
					Message: "path is outside the project root",
					Value:   raw,
				})
				continue
			}
			s = rel
		}

		// Convert to slash paths
		s = filepath.ToSlash(s)

		// Reject paths that escape the project root
		if strings.HasPrefix(s, "../") || s == ".." {
			errs = append(errs, ValidationError{
				Code:    "outside_project",
				Path:    raw,
				Field:   "file",
				Message: "path is outside the project root",
				Value:   raw,
			})
			continue
		}

		// Dedupe in input order
		if seen[s] {
			continue
		}
		seen[s] = true

		// Check file exists on disk
		absPath := filepath.Join(projectRoot, filepath.FromSlash(s))
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			errs = append(errs, ValidationError{
				Code:    "missing_file",
				Path:    s,
				Field:   "file",
				Message: "file does not exist: " + s,
				Value:   raw,
			})
			continue
		}

		// Check file is in the graph node set
		if !nodeSet[s] {
			errs = append(errs, ValidationError{
				Code:    "not_in_graph",
				Path:    s,
				Field:   "file",
				Message: "file is not in the import graph: " + s,
				Value:   raw,
			})
			continue
		}

		result = append(result, s)
	}

	if len(errs) > 0 {
		return nil, &ValidationErrors{Errors: errs}
	}

	return result, nil
}

// buildNodeSet creates a set of node paths from the graph for O(1) lookup.
func buildNodeSet(g *graph.Graph) map[string]bool {
	set := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		set[n.Path] = true
	}
	return set
}
