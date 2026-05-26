package resolver

// ResolveResult contains the resolved path and metadata about the import.
type ResolveResult struct {
	ResolvedPath string // path relative to project root, empty if unresolved
	IsExternal   bool   // true for bare specifiers (node_modules)
	MatchedAlias bool   // true when import matched a tsconfig paths alias
}

// Resolver resolves raw import paths to actual file paths.
type Resolver interface {
	Resolve(importPath, sourceFile, projectRoot string) (ResolveResult, error)
}
