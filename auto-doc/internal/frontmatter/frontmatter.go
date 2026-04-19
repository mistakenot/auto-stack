// [autodoc(e8d3cf9c@34e92e15, 06d8462e)]
package frontmatter

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Dogfood marker: intentional edit to trigger scope hash mismatch.

// Doc represents parsed frontmatter and body of a markdown file.
type Doc struct {
	Id       string
	Title    string
	Summary  string
	ReadWhen string
	Hash     string
	Body     string
}

// Parse extracts YAML frontmatter from a markdown string.
// Returns a Doc with the parsed fields and the remaining body.
func Parse(content string) Doc {
	if !strings.HasPrefix(content, "---\n") {
		return Doc{Body: content}
	}

	end := strings.Index(content[4:], "\n---\n")
	if end == -1 {
		// Check for frontmatter at end of file (no trailing content)
		end = strings.Index(content[4:], "\n---")
		if end == -1 || end+4+3 != len(content) {
			return Doc{Body: content}
		}
	}

	yamlBlock := content[4 : 4+end]
	body := ""
	if 4+end+5 <= len(content) {
		body = content[4+end+5:]
	}

	doc := Doc{Body: body}
	for line := range strings.SplitSeq(yamlBlock, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = unquote(val)

		switch key {
		case "id":
			doc.Id = val
		case "title":
			doc.Title = val
		case "summary":
			doc.Summary = val
		case "read_when":
			doc.ReadWhen = val
		case "hash":
			doc.Hash = val
		}
	}
	return doc
}

// unquote removes surrounding double quotes if present.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// Serialize writes a Doc back to a markdown string with YAML frontmatter.
// Keys are sorted alphabetically; hash is included.
func Serialize(doc *Doc) string {
	fields := map[string]string{
		"title":   doc.Title,
		"summary": doc.Summary,
		"hash":    doc.Hash,
	}
	if doc.Id != "" {
		fields["id"] = doc.Id
	}
	if doc.ReadWhen != "" {
		fields["read_when"] = doc.ReadWhen
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("---\n")
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("%s: %q\n", k, fields[k]))
	}
	sb.WriteString("---\n")
	if doc.Body != "" {
		sb.WriteString(doc.Body)
	}
	return sb.String()
}

// ComputeHash computes the hash for a Doc: sort frontmatter keys alphabetically
// (excluding hash), concatenate their values, append the body content,
// MD5 digest, first 8 hex chars.
func ComputeHash(doc *Doc) string {
	fields := map[string]string{
		"summary": doc.Summary,
		"title":   doc.Title,
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(fields[k])
	}
	b.WriteString(doc.Body)

	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:8]
}

// UpdateHash reads a file, computes the correct hash, and writes it back.
func UpdateHash(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	doc := Parse(string(data))
	doc.Hash = ComputeHash(&doc)

	return os.WriteFile(path, []byte(Serialize(&doc)), 0o644)
}
