package query

import "strings"

// CompileFTS compiles an AST into an SQLite FTS5 query string.
// All user-supplied text is quoted to prevent FTS injection.
func CompileFTS(node *Node) string {
	if node == nil {
		return ""
	}
	switch node.Type {
	case NodeTerm:
		return `"` + escapeFTS(node.Value) + `"`
	case NodePhrase:
		return `"` + escapeFTS(node.Value) + `"`
	case NodeAnd:
		left := CompileFTS(node.Left)
		right := CompileFTS(node.Right)
		return left + " " + right
	case NodeOr:
		left := CompileFTS(node.Left)
		right := CompileFTS(node.Right)
		return left + " OR " + right
	case NodeNot:
		right := CompileFTS(node.Right)
		return "NOT " + right
	default:
		return ""
	}
}

// escapeFTS escapes double quotes in user text so they cannot break
// out of the FTS5 quoted string.
func escapeFTS(s string) string {
	return strings.ReplaceAll(s, `"`, `""`)
}
