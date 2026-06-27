package sessionhtml

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// dataMarker is the placeholder in template.html replaced with the model JSON.
const dataMarker = "/*__DATA__*/"

// Render marshals the work-graph model and injects it into the embedded viewer
// template, returning one self-contained HTML document.
//
// The JSON is script-safe-escaped before injection so message content can never
// break out of the surrounding <script> tag: "</" sequences (e.g. a literal
// "</script>" inside a transcript) and the U+2028 / U+2029 line separators are
// escaped.
func Render(root *Node) ([]byte, error) {
	if root == nil {
		return nil, errors.New("render: nil model")
	}
	// Encode with HTML escaping OFF so "</script>" survives as literal text;
	// scriptSafe then handles script-safety explicitly (matching the
	// prototype). The encoder still escapes U+2028/U+2029 unconditionally.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("marshal model: %w", err)
	}
	data := bytes.TrimRight(buf.Bytes(), "\n")
	safe := scriptSafe(string(data))

	if !strings.Contains(templateHTML, dataMarker) {
		return nil, fmt.Errorf("template missing %s injection marker", dataMarker)
	}
	doc := strings.Replace(templateHTML, dataMarker, "window.__SESSION__ = "+safe+";", 1)
	return []byte(doc), nil
}

// scriptSafe escapes a JSON string so it can be embedded inside an inline
// <script> without breaking the page. "</" can't form a closing tag, and the
// U+2028 / U+2029 line separators — valid in JSON but illegal inside a JS
// string literal — are rewritten to their \u escapes. Search runes are built
// from code points so this source stays plain ASCII.
func scriptSafe(s string) string {
	s = strings.ReplaceAll(s, "</", "<\\/")
	s = strings.ReplaceAll(s, string(rune(0x2028)), "\\u2028")
	s = strings.ReplaceAll(s, string(rune(0x2029)), "\\u2029")
	return s
}
