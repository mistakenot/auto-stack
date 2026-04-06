package query

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Lexer tests
// ---------------------------------------------------------------------------

func TestLexer(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []Token
	}{
		{
			name:  "single term",
			input: "hello",
			expect: []Token{
				{TokenTerm, "hello"},
				{TokenEOF, ""},
			},
		},
		{
			name:  "multiple terms",
			input: "Exit code 0",
			expect: []Token{
				{TokenTerm, "Exit"},
				{TokenTerm, "code"},
				{TokenTerm, "0"},
				{TokenEOF, ""},
			},
		},
		{
			name:  "quoted phrase",
			input: `"auth middleware"`,
			expect: []Token{
				{TokenPhrase, "auth middleware"},
				{TokenEOF, ""},
			},
		},
		{
			name:  "operators",
			input: "a AND b OR c NOT d",
			expect: []Token{
				{TokenTerm, "a"},
				{TokenAnd, "AND"},
				{TokenTerm, "b"},
				{TokenOr, "OR"},
				{TokenTerm, "c"},
				{TokenNot, "NOT"},
				{TokenTerm, "d"},
				{TokenEOF, ""},
			},
		},
		{
			name:  "empty input",
			input: "",
			expect: []Token{
				{TokenEOF, ""},
			},
		},
		{
			name:  "whitespace only",
			input: "   ",
			expect: []Token{
				{TokenEOF, ""},
			},
		},
		{
			name:  "unclosed quote",
			input: `"hello world`,
			expect: []Token{
				{TokenPhrase, "hello world"},
				{TokenEOF, ""},
			},
		},
		{
			name:  "phrase then term",
			input: `"auth middleware" retry`,
			expect: []Token{
				{TokenPhrase, "auth middleware"},
				{TokenTerm, "retry"},
				{TokenEOF, ""},
			},
		},
		{
			name:  "lowercase and or not are terms",
			input: "and or not",
			expect: []Token{
				{TokenTerm, "and"},
				{TokenTerm, "or"},
				{TokenTerm, "not"},
				{TokenEOF, ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := NewLexer(tt.input).Tokenize()
			if len(tokens) != len(tt.expect) {
				t.Fatalf("expected %d tokens, got %d: %+v", len(tt.expect), len(tokens), tokens)
			}
			for i, exp := range tt.expect {
				if tokens[i].Type != exp.Type || tokens[i].Value != exp.Value {
					t.Errorf("token[%d]: expected %+v, got %+v", i, exp, tokens[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Parser tests
// ---------------------------------------------------------------------------

func TestParser(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		checkTree func(t *testing.T, n *Node)
	}{
		{
			name:  "single term",
			input: "hello",
			checkTree: func(t *testing.T, n *Node) {
				assertNode(t, n, NodeTerm, "hello")
			},
		},
		{
			name:  "implicit AND of three terms",
			input: "Exit code 0",
			checkTree: func(t *testing.T, n *Node) {
				// AND(AND(Term("Exit"), Term("code")), Term("0"))
				assertNode(t, n, NodeAnd, "")
				assertNode(t, n.Left, NodeAnd, "")
				assertNode(t, n.Left.Left, NodeTerm, "Exit")
				assertNode(t, n.Left.Right, NodeTerm, "code")
				assertNode(t, n.Right, NodeTerm, "0")
			},
		},
		{
			name:  "phrase and term implicit AND",
			input: `"auth middleware" retry`,
			checkTree: func(t *testing.T, n *Node) {
				assertNode(t, n, NodeAnd, "")
				assertNode(t, n.Left, NodePhrase, "auth middleware")
				assertNode(t, n.Right, NodeTerm, "retry")
			},
		},
		{
			name:  "explicit AND with NOT",
			input: "flaky AND retry NOT passed",
			checkTree: func(t *testing.T, n *Node) {
				// AND(AND(Term("flaky"), Term("retry")), NOT(Term("passed")))
				assertNode(t, n, NodeAnd, "")
				assertNode(t, n.Left, NodeAnd, "")
				assertNode(t, n.Left.Left, NodeTerm, "flaky")
				assertNode(t, n.Left.Right, NodeTerm, "retry")
				assertNode(t, n.Right, NodeNot, "")
				assertNode(t, n.Right.Right, NodeTerm, "passed")
			},
		},
		{
			name:  "OR operator",
			input: "error OR warning",
			checkTree: func(t *testing.T, n *Node) {
				assertNode(t, n, NodeOr, "")
				assertNode(t, n.Left, NodeTerm, "error")
				assertNode(t, n.Right, NodeTerm, "warning")
			},
		},
		{
			name:  "OR has lower precedence than AND",
			input: "a b OR c d",
			checkTree: func(t *testing.T, n *Node) {
				// OR(AND(a, b), AND(c, d))
				assertNode(t, n, NodeOr, "")
				assertNode(t, n.Left, NodeAnd, "")
				assertNode(t, n.Left.Left, NodeTerm, "a")
				assertNode(t, n.Left.Right, NodeTerm, "b")
				assertNode(t, n.Right, NodeAnd, "")
				assertNode(t, n.Right.Left, NodeTerm, "c")
				assertNode(t, n.Right.Right, NodeTerm, "d")
			},
		},
		{
			name:  "NOT has highest precedence",
			input: "a NOT b c",
			checkTree: func(t *testing.T, n *Node) {
				// AND(AND(a, NOT(b)), c)
				assertNode(t, n, NodeAnd, "")
				assertNode(t, n.Left, NodeAnd, "")
				assertNode(t, n.Left.Left, NodeTerm, "a")
				assertNode(t, n.Left.Right, NodeNot, "")
				assertNode(t, n.Left.Right.Right, NodeTerm, "b")
				assertNode(t, n.Right, NodeTerm, "c")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.checkTree(t, node)
		})
	}
}

func TestParserError(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

// ---------------------------------------------------------------------------
// CompileFTS tests
// ---------------------------------------------------------------------------

func TestCompileFTS(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "single term",
			input:  "hello",
			expect: `"hello"`,
		},
		{
			name:   "implicit AND",
			input:  "Exit code",
			expect: `"Exit" "code"`,
		},
		{
			name:   "phrase",
			input:  `"auth middleware"`,
			expect: `"auth middleware"`,
		},
		{
			name:   "OR",
			input:  "error OR warning",
			expect: `"error" OR "warning"`,
		},
		{
			name:   "NOT",
			input:  "retry NOT passed",
			expect: `"retry" NOT "passed"`,
		},
		{
			name:   "complex query",
			input:  "flaky AND retry NOT passed",
			expect: `"flaky" "retry" NOT "passed"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got := CompileFTS(node)
			if got != tt.expect {
				t.Errorf("expected %q, got %q", tt.expect, got)
			}
		})
	}
}

func TestCompileFTSEscaping(t *testing.T) {
	// Build an AST node directly with a value containing double quotes,
	// which cannot be produced by the lexer but tests the escaping path.
	node := &Node{Type: NodePhrase, Value: `he said "hi"`}
	got := CompileFTS(node)
	expect := `"he said ""hi"""`
	if got != expect {
		t.Errorf("expected %q, got %q", expect, got)
	}
}

// ---------------------------------------------------------------------------
// PrefixFallback tests
// ---------------------------------------------------------------------------

func TestPrefixFallback(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "single term gets prefix",
			input:  "hello",
			expect: `"hello*"`,
		},
		{
			name:   "phrase gets prefix",
			input:  `"auth middleware"`,
			expect: `"auth middleware*"`,
		},
		{
			name:   "negated term is not rewritten",
			input:  "retry NOT passed",
			expect: `"retry*" NOT "passed"`,
		},
		{
			name:   "complex with NOT",
			input:  "a b NOT c",
			expect: `"a*" "b*" NOT "c"`,
		},
		{
			name:   "OR branches both get prefix",
			input:  "error OR warning",
			expect: `"error*" OR "warning*"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			rewritten := PrefixFallback(node)
			got := CompileFTS(rewritten)
			if got != tt.expect {
				t.Errorf("expected %q, got %q", tt.expect, got)
			}
		})
	}
}

func TestPrefixFallbackDoesNotMutateOriginal(t *testing.T) {
	node, err := Parse("hello")
	if err != nil {
		t.Fatal(err)
	}
	original := CompileFTS(node)
	_ = PrefixFallback(node)
	after := CompileFTS(node)
	if original != after {
		t.Errorf("original tree was mutated: before=%q after=%q", original, after)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertNode(t *testing.T, n *Node, expectedType NodeType, expectedValue string) {
	t.Helper()
	if n == nil {
		t.Fatal("expected non-nil node")
	}
	if n.Type != expectedType {
		t.Errorf("expected type %d, got %d", expectedType, n.Type)
	}
	if expectedValue != "" && n.Value != expectedValue {
		t.Errorf("expected value %q, got %q", expectedValue, n.Value)
	}
}
