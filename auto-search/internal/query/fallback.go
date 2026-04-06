package query

// PrefixFallback rewrites positive term and phrase leaves to FTS5
// prefix form (appending "*"). Negated leaves are never rewritten.
// Returns a new AST; the original is not mutated.
func PrefixFallback(node *Node) *Node {
	if node == nil {
		return nil
	}
	return prefixRewrite(node, false)
}

func prefixRewrite(node *Node, negated bool) *Node {
	switch node.Type {
	case NodeTerm:
		out := &Node{Type: NodeTerm, Value: node.Value}
		if !negated {
			out.Value = node.Value + "*"
		}
		return out
	case NodePhrase:
		out := &Node{Type: NodePhrase, Value: node.Value}
		if !negated {
			out.Value = node.Value + "*"
		}
		return out
	case NodeAnd:
		return &Node{
			Type:  NodeAnd,
			Left:  prefixRewrite(node.Left, negated),
			Right: prefixRewrite(node.Right, negated),
		}
	case NodeOr:
		return &Node{
			Type:  NodeOr,
			Left:  prefixRewrite(node.Left, negated),
			Right: prefixRewrite(node.Right, negated),
		}
	case NodeNot:
		return &Node{
			Type:  NodeNot,
			Right: prefixRewrite(node.Right, true),
		}
	default:
		return node
	}
}
