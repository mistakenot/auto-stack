// Package render is the pure, deterministic skill render engine: it turns a
// SKILL.md template plus resolved replacement values into a canonical in-memory
// tree and a content-addressed skill_version digest.
//
// render is a leaf package. It depends only on the standard library and
// gopkg.in/yaml.v3; it must NOT import internal/skill (to avoid an import
// cycle) or any cache/network package. Where it needs 032's value shapes it
// defines its own minimal input structs and lets the orchestrator adapt.
package render

import (
	"fmt"
	"strings"
	"text/template/parse"
)

// CodeTemplateRejected is the stable error code for a template that uses any
// construct outside the accepted grammar (field access + literal-brace escape).
const CodeTemplateRejected = "template_rejected"

// TemplateRejectedError is returned at parse time when a template contains a
// construct outside the accepted grammar. It names the offending node so the
// author can find and remove it.
type TemplateRejectedError struct {
	// Node describes the offending construct, e.g. "if action" or
	// "function call (printf)".
	Node string
	// Detail is the human-readable explanation plus remediation.
	Detail string
}

func (e *TemplateRejectedError) Error() string {
	return fmt.Sprintf("%s: %s: %s", CodeTemplateRejected, e.Node, e.Detail)
}

// Code returns the stable error code.
func (e *TemplateRejectedError) Code() string { return CodeTemplateRejected }

// templateBuiltins lists the names text/template treats as built-in functions.
// We register them so that a template using them (e.g. {{ printf … }}) PARSES
// successfully and is then rejected by the AST walk with a precise typed error,
// rather than failing with a generic "function not defined" parse error.
var templateBuiltins = func() map[string]any {
	names := []string{
		"and", "call", "html", "index", "slice", "js", "len", "not", "or",
		"print", "printf", "println", "urlquery", "eq", "ge", "gt", "le",
		"lt", "ne",
	}
	m := make(map[string]any, len(names))
	for _, n := range names {
		// parse.Tree.hasFunction checks funcMap[name] != nil, so the value
		// must be non-nil. It is never invoked (we never execute).
		m[n] = func() {}
	}
	return m
}()

// Template is a parsed, validated restricted template. Its AST contains only
// literal text, field-access actions ({{ .var }} / {{ .a.b }}), and the
// literal-brace escape ({{ "{{" }}).
type Template struct {
	root *parse.ListNode
	vars []string
}

// ParseTemplate parses a SKILL.md body template with fixed {{ }} delimiters and
// walks every node of the parse tree, accepting ONLY:
//
//   - literal text (*parse.TextNode),
//   - a field-access action whose pipeline is a single command that is a single
//     field access — {{ .var }} or a chained field access {{ .a.b }}, and
//   - the literal-brace escape {{ "{{" }} (a single string-constant command),
//     which emits the literal string verbatim.
//
// Any function call, multi-stage pipeline, variable declaration, control action
// (if/range/with), nested template invocation, or built-in is rejected with a
// *TemplateRejectedError naming the offending construct. The accepted grammar
// equals the promised grammar — the AST is walked, not merely executed behind a
// restricted FuncMap (which would not block pipelines or control actions).
func ParseTemplate(body string) (*Template, error) {
	trees, err := parse.Parse("skill", body, "{{", "}}", templateBuiltins)
	if err != nil {
		// A parse failure (e.g. an undefined function like {{ .a | upper }})
		// is itself a rejection of a non-grammar construct.
		return nil, &TemplateRejectedError{
			Node:   "parse error",
			Detail: cleanParseErr(err) + "; templates may only use {{ .var }} field access and the {{ \"{{\" }} literal-brace escape",
		}
	}

	root := trees["skill"].Root
	t := &Template{root: root}
	if err := t.validate(); err != nil {
		return nil, err
	}
	return t, nil
}

// Vars returns the unique set of field-access variable keys referenced in the
// template, in first-seen order. A {{ .a.b }} chain yields the key "a.b".
func (t *Template) Vars() []string {
	return append([]string(nil), t.vars...)
}

// Render emits the template with raw, unescaped substitution: each field-access
// action is replaced by values[key] verbatim (a value containing <, &, or shell
// metacharacters passes through unchanged — skills are prose, not an injection
// sink), and each literal-brace escape emits its literal string. Missing keys
// resolve to the empty string; callers resolve values via the customize schema
// before calling Render.
func (t *Template) Render(values map[string]string) (string, error) {
	var b strings.Builder
	for _, n := range t.root.Nodes {
		switch node := n.(type) {
		case *parse.TextNode:
			b.Write(node.Text)
		case *parse.ActionNode:
			cmd := node.Pipe.Cmds[0]
			switch arg := cmd.Args[0].(type) {
			case *parse.FieldNode:
				b.WriteString(values[strings.Join(arg.Ident, ".")])
			case *parse.StringNode:
				b.WriteString(arg.Text)
			default:
				// Unreachable: validate() guarantees only the two forms above.
				return "", &TemplateRejectedError{Node: arg.String(), Detail: "unsupported action"}
			}
		default:
			return "", &TemplateRejectedError{Node: n.String(), Detail: "unsupported node"}
		}
	}
	return b.String(), nil
}

// validate walks the AST and rejects every node outside the accepted grammar,
// collecting the referenced field-access var keys.
func (t *Template) validate() error {
	seen := map[string]bool{}
	for _, n := range t.root.Nodes {
		switch node := n.(type) {
		case *parse.TextNode:
			// literal text is always fine
		case *parse.ActionNode:
			key, err := validateFieldAction(node)
			if err != nil {
				return err
			}
			if key != "" && !seen[key] {
				seen[key] = true
				t.vars = append(t.vars, key)
			}
		case *parse.IfNode:
			return &TemplateRejectedError{Node: "if action", Detail: "control actions are not allowed; render performs no code execution"}
		case *parse.RangeNode:
			return &TemplateRejectedError{Node: "range action", Detail: "control actions are not allowed; render performs no code execution"}
		case *parse.WithNode:
			return &TemplateRejectedError{Node: "with action", Detail: "control actions are not allowed; render performs no code execution"}
		case *parse.TemplateNode:
			return &TemplateRejectedError{Node: "template invocation", Detail: "nested template invocations are not allowed"}
		default:
			return &TemplateRejectedError{Node: fmt.Sprintf("%T", n), Detail: "unsupported template construct"}
		}
	}
	return nil
}

// validateFieldAction accepts an action node only when it is a single-command,
// single-argument pipeline whose argument is a field access (returns its key)
// or the literal-brace string escape (returns ""). Everything else — multi-stage
// pipelines, variable declarations, function calls, bare dot, variables — is
// rejected.
func validateFieldAction(node *parse.ActionNode) (string, error) {
	pipe := node.Pipe
	if len(pipe.Decl) > 0 {
		return "", &TemplateRejectedError{Node: "variable declaration", Detail: "variable assignments are not allowed"}
	}
	if len(pipe.Cmds) != 1 {
		return "", &TemplateRejectedError{Node: "pipeline", Detail: "multi-stage pipelines are not allowed; only a single {{ .var }} field access is permitted"}
	}
	cmd := pipe.Cmds[0]
	if len(cmd.Args) != 1 {
		// A command with more than one arg is a function call with arguments.
		name := "function call"
		if id, ok := cmd.Args[0].(*parse.IdentifierNode); ok {
			name = fmt.Sprintf("function call (%s)", id.Ident)
		}
		return "", &TemplateRejectedError{Node: name, Detail: "function calls are not allowed; only a single {{ .var }} field access is permitted"}
	}
	switch arg := cmd.Args[0].(type) {
	case *parse.FieldNode:
		return strings.Join(arg.Ident, "."), nil
	case *parse.StringNode:
		// A bare string constant command, e.g. {{ "{{" }}, emits the literal.
		return "", nil
	case *parse.IdentifierNode:
		return "", &TemplateRejectedError{Node: fmt.Sprintf("function call (%s)", arg.Ident), Detail: "function calls and built-ins are not allowed"}
	case *parse.DotNode:
		return "", &TemplateRejectedError{Node: "dot (.)", Detail: "the bare {{ . }} is not allowed; reference an explicit {{ .var }}"}
	case *parse.VariableNode:
		return "", &TemplateRejectedError{Node: "variable", Detail: "template variables are not allowed"}
	default:
		return "", &TemplateRejectedError{Node: fmt.Sprintf("%T", arg), Detail: "only {{ .var }} field access and the {{ \"{{\" }} escape are allowed"}
	}
}

// cleanParseErr strips the parser's "template: skill:" prefix for readability.
func cleanParseErr(err error) string {
	msg := err.Error()
	const prefix = "template: skill:"
	if i := strings.Index(msg, prefix); i >= 0 {
		msg = strings.TrimSpace(msg[i+len(prefix):])
		if j := strings.Index(msg, ":"); j >= 0 {
			msg = strings.TrimSpace(msg[j+1:])
		}
	}
	return msg
}
