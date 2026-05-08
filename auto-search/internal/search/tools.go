package search

import (
	"fmt"
	"sort"
	"strings"
)

// KnownToolNames is the canonical set of tool_name values produced by the ETL
// pipeline, kept in PascalCase to match what is stored in the messages.tool_name
// column. Used by both message search and stats to validate --tool input and to
// surface a clear error message listing valid values.
//
// Adding a new tool: append it here and run `go test ./internal/search/...`.
var KnownToolNames = []string{
	"Agent",
	"AskUserQuestion",
	"Bash",
	"BashOutput",
	"Edit",
	"ExitPlanMode",
	"Glob",
	"Grep",
	"KillBash",
	"MultiEdit",
	"NotebookEdit",
	"NotebookRead",
	"Read",
	"Skill",
	"Task",
	"TodoWrite",
	"WebFetch",
	"WebSearch",
	"Write",
}

// knownToolNameSet is a case-insensitive lookup of KnownToolNames -> canonical PascalCase form.
var knownToolNameSet = func() map[string]string {
	m := make(map[string]string, len(KnownToolNames))
	for _, name := range KnownToolNames {
		m[strings.ToLower(name)] = name
	}
	return m
}()

// NormalizeToolNames trims, deduplicates, and validates a slice of --tool inputs.
// Tool name matching is case-insensitive; the canonical PascalCase form (matching
// the messages.tool_name column) is returned. Empty / whitespace-only values are
// dropped. Unknown values produce a fail-fast error listing every valid name.
//
// Returns nil (no predicate) when the input is empty or contains only whitespace.
func NormalizeToolNames(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	var unknown []string
	for _, item := range raw {
		// pflag's StringSlice already splits on commas, but be defensive in case
		// callers pass a single comma-separated string directly.
		for part := range strings.SplitSeq(item, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			canonical, ok := knownToolNameSet[strings.ToLower(trimmed)]
			if !ok {
				unknown = append(unknown, trimmed)
				continue
			}
			if _, dup := seen[canonical]; dup {
				continue
			}
			seen[canonical] = struct{}{}
			out = append(out, canonical)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf(
			"invalid --tool value(s) %s; valid values: %s",
			quoteAndJoin(unknown),
			strings.Join(KnownToolNames, ", "),
		)
	}
	if len(out) == 0 {
		return nil, nil
	}
	sort.Strings(out)
	return out, nil
}

func quoteAndJoin(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return strings.Join(quoted, ", ")
}
