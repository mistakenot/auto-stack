package scanner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// GoScanner uses go/parser to discover import relationships in Go source files.
type GoScanner struct{}

// NewGoScanner returns a scanner that uses go/parser with ImportsOnly mode.
func NewGoScanner() *GoScanner {
	return &GoScanner{}
}

// Scan discovers all import statements under dir by parsing Go source files.
func (s *GoScanner) Scan(dir string) ([]ImportMatch, error) {
	var results []ImportMatch
	fset := token.NewFileSet()

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			name := d.Name()
			// Skip vendor, testdata, dot-prefixed, and underscore-prefixed directories.
			if name == "vendor" || name == "testdata" {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			// Skip files that fail to parse (e.g. generated code with syntax issues).
			return nil
		}

		absPath, absErr := filepath.Abs(path)
		if absErr != nil {
			return absErr
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			kind := classifyGoImport(imp)
			line := fset.Position(imp.Pos()).Line

			results = append(results, ImportMatch{
				SourceFile: absPath,
				ImportPath: importPath,
				Kind:       kind,
				Line:       line,
			})
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return results, nil
}

// classifyGoImport determines the import kind based on the import spec's Name field.
func classifyGoImport(imp *ast.ImportSpec) string {
	if imp.Name == nil {
		return "static"
	}
	switch imp.Name.Name {
	case "_":
		return "blank"
	case ".":
		return "dot"
	default:
		return "aliased"
	}
}
