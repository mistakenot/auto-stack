package scanner

// ImportMatch represents a single import statement found by a scanner.
type ImportMatch struct {
	SourceFile string // absolute path of the file containing the import
	ImportPath string // raw import string as written in source
	Kind       string // "static", "dynamic", "require", "reexport", "side-effect", "type"
	Line       int    // 1-based line number
}

// Scanner discovers import relationships in a project directory.
type Scanner interface {
	Scan(dir string) ([]ImportMatch, error)
}
