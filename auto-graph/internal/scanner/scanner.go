package scanner

// ImportMatch represents a single import statement found by a scanner.
type ImportMatch struct {
	SourceFile string // absolute path of the file containing the import
	ImportPath string // raw import string as written in source
	Kind       string // "static", "dynamic", "require", "reexport", "side-effect", "type"
	Line       int    // 1-based line number
	// Unresolved marks an import whose specifier could not be extracted to a
	// static string literal (e.g. import(variable) or require(expr)). Such
	// imports produce no edge; ImportPath carries the raw expression text so
	// callers can report it rather than dropping it silently.
	Unresolved bool
}

// Scanner discovers import relationships in a project directory.
type Scanner interface {
	Scan(dir string) ([]ImportMatch, error)
}
