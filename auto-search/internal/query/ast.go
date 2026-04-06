package query

// NodeType represents the kind of AST node.
type NodeType int

const (
	NodeTerm   NodeType = iota // bare word
	NodePhrase                 // quoted phrase
	NodeAnd                    // binary AND
	NodeOr                     // binary OR
	NodeNot                    // unary NOT
)

// Node is a single node in the query AST.
type Node struct {
	Type  NodeType
	Value string // for Term and Phrase
	Left  *Node  // for And, Or
	Right *Node  // for And, Or, Not (right only for Not)
}
