package scanner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// astGrepMatch represents a single match from ast-grep --json=stream output.
type astGrepMatch struct {
	Text     string       `json:"text"`
	Range    astGrepRange `json:"range"`
	File     string       `json:"file"`
	Language string       `json:"language"`
}

type astGrepRange struct {
	ByteOffset astGrepSpan `json:"byteOffset"`
	Start      astGrepPos  `json:"start"`
	End        astGrepPos  `json:"end"`
}

type astGrepSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type astGrepPos struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Regex patterns for extracting import paths from match text.
var (
	reFromClause    = regexp.MustCompile(`from\s+['"]([^'"]+)['"]`)
	reSideEffect    = regexp.MustCompile(`import\s+['"]([^'"]+)['"]`)
	reQuotedString  = regexp.MustCompile(`['"]([^'"]+)['"]`)
	reImportType    = regexp.MustCompile(`import\s+type\b`)
)

// patternSpec describes an ast-grep pattern and how to classify its matches.
type patternSpec struct {
	pattern string
	kind    string // default kind for matches; may be refined per-match
}

// TypeScriptScanner uses ast-grep to discover import relationships in
// TypeScript and JavaScript source files.
type TypeScriptScanner struct {
	// AstGrepBin overrides the ast-grep binary path. If empty, exec.LookPath
	// is used to find it.
	AstGrepBin string
}

// NewTypeScriptScanner returns a scanner that uses ast-grep.
func NewTypeScriptScanner() *TypeScriptScanner {
	return &TypeScriptScanner{}
}

// Scan discovers all import statements under dir by running ast-grep with
// multiple patterns. Results are deduplicated by (sourceFile, importPath).
func (s *TypeScriptScanner) Scan(dir string) ([]ImportMatch, error) {
	bin, err := s.findBinary()
	if err != nil {
		return nil, err
	}

	patterns := []patternSpec{
		{pattern: "import $$$", kind: "static"},
		{pattern: "import($$$)", kind: "dynamic"},
		{pattern: "require($$$)", kind: "require"},
		{pattern: `export { $_ } from "$_"`, kind: "reexport"},
		{pattern: `import "$_"`, kind: "side-effect"}, // fallback for side-effect imports
	}

	type seenKey struct {
		file       string
		importPath string
	}
	seen := make(map[seenKey]bool)
	var results []ImportMatch

	for _, ps := range patterns {
		matches, err := s.runPattern(bin, dir, ps.pattern)
		if err != nil {
			return nil, fmt.Errorf("ast-grep pattern %q: %w", ps.pattern, err)
		}

		for _, m := range matches {
			importPath := extractImportPath(m.Text, ps.kind)
			if importPath == "" {
				continue
			}

			key := seenKey{file: m.File, importPath: importPath}
			if seen[key] {
				continue
			}
			seen[key] = true

			kind := classifyKind(m.Text, ps.kind)

			results = append(results, ImportMatch{
				SourceFile: m.File,
				ImportPath: importPath,
				Kind:       kind,
				Line:       m.Range.Start.Line + 1, // ast-grep uses 0-based lines
			})
		}
	}

	return results, nil
}

// findBinary locates the ast-grep binary.
func (s *TypeScriptScanner) findBinary() (string, error) {
	if s.AstGrepBin != "" {
		return s.AstGrepBin, nil
	}
	bin, err := exec.LookPath("ast-grep")
	if err != nil {
		return "", fmt.Errorf("ast-grep not found: install with npm i -g @ast-grep/cli or brew install ast-grep")
	}
	return bin, nil
}

// runPattern executes a single ast-grep pattern and parses the JSON stream output.
func (s *TypeScriptScanner) runPattern(bin, dir, pattern string) ([]astGrepMatch, error) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, bin, "run", "--lang", "ts", "-p", pattern, "--json=stream", dir)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting ast-grep: %w", err)
	}

	var matches []astGrepMatch
	scanner := bufio.NewScanner(stdout)
	// Increase buffer size for lines with long match text.
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m astGrepMatch
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			// Skip malformed lines (e.g. ast-grep progress output).
			continue
		}
		if m.File != "" {
			matches = append(matches, m)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading ast-grep output: %w", err)
	}

	// ast-grep exits 0 when matches are found, 1 when no matches are found.
	// Both are valid outcomes for us.
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// No matches found — not an error.
			return matches, nil
		}
		return nil, fmt.Errorf("ast-grep exited with error: %w", err)
	}

	return matches, nil
}

// extractImportPath pulls the import specifier from an ast-grep match text.
func extractImportPath(text, patternKind string) string {
	switch patternKind {
	case "static":
		// Static imports with `from` clause: import X from "path"
		if m := reFromClause.FindStringSubmatch(text); len(m) > 1 {
			return m[1]
		}
		// Side-effect imports caught by `import $$$`: import "path"
		if m := reSideEffect.FindStringSubmatch(text); len(m) > 1 {
			return m[1]
		}
		return ""

	case "side-effect":
		// Dedicated side-effect pattern: import "path"
		if m := reSideEffect.FindStringSubmatch(text); len(m) > 1 {
			return m[1]
		}
		return ""

	case "dynamic", "require":
		// Dynamic import() or require(): extract first quoted string.
		if m := reQuotedString.FindStringSubmatch(text); len(m) > 1 {
			return m[1]
		}
		return ""

	case "reexport":
		// Re-exports: export { X } from "path"
		if m := reFromClause.FindStringSubmatch(text); len(m) > 1 {
			return m[1]
		}
		return ""

	default:
		return ""
	}
}

// classifyKind refines the import kind based on match text.
func classifyKind(text, patternKind string) string {
	switch patternKind {
	case "static":
		// Check if this is a type-only import.
		if reImportType.MatchString(text) {
			return "type"
		}
		// Check if this is a side-effect import (no `from` clause).
		if !reFromClause.MatchString(text) && reSideEffect.MatchString(text) {
			return "side-effect"
		}
		return "static"

	default:
		return patternKind
	}
}
