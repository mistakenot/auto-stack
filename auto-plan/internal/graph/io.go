package graph

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

// ParseError reports malformed JSON — the only thing that fails Decode.
type ParseError struct {
	Path   string
	Line   int
	Column int
	Msg    string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s:%d:%d: malformed JSON: %s", e.Path, e.Line, e.Column, e.Msg)
}

// Decode reads graph.json tolerantly. Only malformed JSON is an error (a
// *ParseError with line and column); unknown fields, unregistered types and
// wrongly typed values are kept for Validate to report.
func Decode(path string) (*Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	g, err := Parse(data)
	var pe *ParseError
	if errors.As(err, &pe) {
		pe.Path = path
	}
	return g, err
}

// Parse decodes graph.json bytes; see Decode.
func Parse(data []byte) (*Graph, error) {
	var top any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&top); err != nil {
		return nil, parseError(data, err)
	}
	if dec.More() {
		return nil, parseError(data, errors.New("unexpected data after the top-level value"))
	}

	g := &Graph{}
	obj, ok := top.(map[string]any)
	if !ok {
		g.issue("invalid-graph", "$", "", "graph.json must hold a JSON object {version, nodes, edges}", nil)
		return g, nil
	}
	for _, key := range slices.Sorted(maps.Keys(obj)) {
		val := obj[key]
		switch key {
		case "version":
			g.Version = g.decodeVersion(val)
		case "nodes":
			g.Nodes = g.decodeNodes(val)
		case "edges":
			g.Edges = g.decodeEdges(val)
		default:
			g.Extra = addExtra(g.Extra, key, val)
		}
	}
	if g.Edges == nil {
		g.Edges = []Edge{}
	}
	return g, nil
}

func parseError(data []byte, err error) *ParseError {
	offset := int64(len(data))
	var se *json.SyntaxError
	if errors.As(err, &se) {
		offset = se.Offset
	}
	line, col := 1, 1
	for _, b := range data[:min(int(offset), len(data))] {
		if b == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return &ParseError{Line: line, Column: col, Msg: err.Error()}
}

// DecodeIssues returns the problems found while decoding: values of the wrong
// JSON type, or nodes and edges that are not objects. Decode could not keep
// those values, so rewriting the file would lose them; `fmt` refuses.
func (g *Graph) DecodeIssues() []ValidationError { return slices.Clone(g.decodeIssues) }

func (g *Graph) issue(code, path, field, msg string, value any) {
	g.decodeIssues = append(g.decodeIssues, ValidationError{Code: code, Path: path, Field: field, Message: msg, Value: value})
}

func (g *Graph) decodeVersion(val any) int {
	n, ok := val.(json.Number)
	if ok {
		if v, err := strconv.Atoi(n.String()); err == nil {
			return v
		}
	}
	g.issue("invalid-field", "$.version", "version", "version must be an integer", val)
	return -1
}

func (g *Graph) decodeNodes(val any) []Node {
	arr, ok := val.([]any)
	if !ok {
		g.issue("invalid-field", "$.nodes", "nodes", "nodes must be an array", nil)
		return nil
	}
	nodes := make([]Node, 0, len(arr))
	for i, raw := range arr {
		path := "$.nodes[" + strconv.Itoa(i) + "]"
		obj, ok := raw.(map[string]any)
		if !ok {
			g.issue("invalid-field", path, "", "a node must be a JSON object", raw)
			continue
		}
		var n Node
		for _, key := range slices.Sorted(maps.Keys(obj)) {
			v := obj[key]
			switch key {
			case "id":
				n.ID = g.str(path, key, v)
			case "type":
				n.Type = g.str(path, key, v)
			case "status":
				n.Status = g.str(path, key, v)
			case "rank":
				n.Rank = g.str(path, key, v)
			case "fields":
				m, ok := v.(map[string]any)
				if !ok {
					g.issue("invalid-field", path+".fields", "fields", "fields must be a JSON object", v)
					continue
				}
				n.Fields = m
			default:
				n.Extra = addExtra(n.Extra, key, v)
			}
		}
		if n.Fields == nil {
			n.Fields = map[string]any{}
		}
		nodes = append(nodes, n)
	}
	return nodes
}

func (g *Graph) decodeEdges(val any) []Edge {
	arr, ok := val.([]any)
	if !ok {
		g.issue("invalid-field", "$.edges", "edges", "edges must be an array", nil)
		return nil
	}
	edges := make([]Edge, 0, len(arr))
	for i, raw := range arr {
		path := "$.edges[" + strconv.Itoa(i) + "]"
		obj, ok := raw.(map[string]any)
		if !ok {
			g.issue("invalid-field", path, "", "an edge must be a JSON object", raw)
			continue
		}
		var e Edge
		for _, key := range slices.Sorted(maps.Keys(obj)) {
			v := obj[key]
			switch key {
			case "from":
				e.From = g.str(path, key, v)
			case "type":
				e.Type = g.str(path, key, v)
			case "to":
				e.To = g.str(path, key, v)
			default:
				e.Extra = addExtra(e.Extra, key, v)
			}
		}
		edges = append(edges, e)
	}
	return edges
}

func (g *Graph) str(path, key string, v any) string {
	s, ok := v.(string)
	if !ok {
		g.issue("invalid-field", path+"."+key, key, key+" must be a string", v)
	}
	return s
}

func addExtra(m map[string]json.RawMessage, key string, v any) map[string]json.RawMessage {
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	raw, err := marshal(v)
	if err != nil {
		raw = []byte("null")
	}
	m[key] = raw
	return m
}

// Canonicalize sorts nodes by (type, id) and edges by (from, type, to) in place.
func (g *Graph) Canonicalize() {
	slices.SortStableFunc(g.Nodes, func(a, b Node) int {
		return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.ID, b.ID))
	})
	slices.SortStableFunc(g.Edges, func(a, b Edge) int {
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.Type, b.Type), cmp.Compare(a.To, b.To))
	})
}

// Encode returns the canonical bytes of g: nodes sorted by (type, id), edges
// by (from, type, to), a fixed key order (field maps sorted by key), a 2-space
// indent, no HTML escaping and a trailing newline. Encode(Parse(Encode(g))) is
// byte-identical to Encode(g).
func Encode(g *Graph) ([]byte, error) {
	c := g.clone()
	c.Canonicalize()

	var buf bytes.Buffer
	buf.WriteByte('{')
	if err := writeMember(&buf, "version", c.Version, true); err != nil {
		return nil, err
	}
	buf.WriteString(`,"nodes":[`)
	for i, n := range c.Nodes {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encodeNode(&buf, n); err != nil {
			return nil, err
		}
	}
	buf.WriteString(`],"edges":[`)
	for i, e := range c.Edges {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encodeEdge(&buf, e); err != nil {
			return nil, err
		}
	}
	buf.WriteByte(']')
	if err := writeExtra(&buf, c.Extra); err != nil {
		return nil, err
	}
	buf.WriteByte('}')

	var out bytes.Buffer
	if err := json.Indent(&out, buf.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func encodeNode(buf *bytes.Buffer, n Node) error {
	buf.WriteByte('{')
	if err := writeMember(buf, "id", n.ID, true); err != nil {
		return err
	}
	if err := writeMember(buf, "type", n.Type, false); err != nil {
		return err
	}
	if err := writeMember(buf, "status", n.Status, false); err != nil {
		return err
	}
	if n.Rank != "" {
		if err := writeMember(buf, "rank", n.Rank, false); err != nil {
			return err
		}
	}
	fields := n.Fields
	if fields == nil {
		fields = map[string]any{}
	}
	if err := writeMember(buf, "fields", fields, false); err != nil {
		return err
	}
	if err := writeExtra(buf, n.Extra); err != nil {
		return err
	}
	buf.WriteByte('}')
	return nil
}

func encodeEdge(buf *bytes.Buffer, e Edge) error {
	buf.WriteByte('{')
	if err := writeMember(buf, "from", e.From, true); err != nil {
		return err
	}
	if err := writeMember(buf, "type", e.Type, false); err != nil {
		return err
	}
	if err := writeMember(buf, "to", e.To, false); err != nil {
		return err
	}
	if err := writeExtra(buf, e.Extra); err != nil {
		return err
	}
	buf.WriteByte('}')
	return nil
}

func writeExtra(buf *bytes.Buffer, extra map[string]json.RawMessage) error {
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if err := writeMember(buf, k, extra[k], false); err != nil {
			return err
		}
	}
	return nil
}

func writeMember(buf *bytes.Buffer, key string, val any, first bool) error {
	if !first {
		buf.WriteByte(',')
	}
	k, err := marshal(key)
	if err != nil {
		return err
	}
	v, err := marshal(val)
	if err != nil {
		return err
	}
	buf.Write(k)
	buf.WriteByte(':')
	buf.Write(v)
	return nil
}

// marshal encodes v as compact JSON without HTML escaping.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Save writes g's canonical bytes to path atomically: a temp file in the same
// directory, then a rename, so a crash never leaves a half-written graph.json
// (D-8: there is no lock).
func Save(path string, g *Graph) error {
	data, err := Encode(g)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeAtomic(path, data)
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod temp for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp into %s: %w", path, err)
	}
	return nil
}
