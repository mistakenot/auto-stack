package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"unicode/utf8"
)

// RenderVersion is the engine's behavior version. It participates in the
// skill_version digest, so a bump invalidates every previously rendered tree
// and triggers a one-time lazy re-render on the next sync. Phase 4 serializes
// this int into the manifest's string render_version field.
const RenderVersion = 1

// Standard git file modes for the emitted tree.
const (
	ModeFile       = "100644"
	ModeExecutable = "100755"
)

// TreeFile is a single emitted file in a rendered skill tree. Data is the exact
// bytes to be written (canonicalized for text, verbatim for binary), and the
// digest is taken over exactly these bytes.
type TreeFile struct {
	// Path is the skill-relative slash-separated path, e.g. "SKILL.md" or
	// "references/api.md".
	Path string
	// Mode is the git file mode, "100644" or "100755".
	Mode string
	// Data is the exact emitted bytes.
	Data []byte
	// Binary reports whether the file was classified as binary (copied verbatim)
	// rather than text (canonicalized).
	Binary bool
}

// isText classifies bytes as text iff they are valid UTF-8 AND contain no NUL
// byte. Everything else (a NUL, or invalid UTF-8) is binary.
func isText(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	return utf8.Valid(data)
}

// canonicalizeText normalizes a text file so the emitted bytes are stable: CRLF
// and lone CR become LF, trailing whitespace is stripped per line, and the file
// ends with exactly one trailing newline (an all-whitespace/empty file emits no
// bytes). The returned bytes are what get hashed, so hash == emitted bytes.
func canonicalizeText(data []byte) []byte {
	s := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	s = bytes.ReplaceAll(s, []byte("\r"), []byte("\n"))
	lines := bytes.Split(s, []byte("\n"))
	for i := range lines {
		lines[i] = bytes.TrimRight(lines[i], " \t\v\f")
	}
	joined := bytes.Join(lines, []byte("\n"))
	joined = bytes.TrimRight(joined, "\n")
	if len(joined) == 0 {
		return []byte{}
	}
	return append(joined, '\n')
}

// newTreeFile classifies and canonicalizes a single file for emission.
func newTreeFile(path, mode string, raw []byte) TreeFile {
	if mode != ModeExecutable {
		mode = ModeFile
	}
	if isText(raw) {
		return TreeFile{Path: path, Mode: mode, Data: canonicalizeText(raw), Binary: false}
	}
	// Binary: copy verbatim, hash byte-for-byte.
	data := append([]byte(nil), raw...)
	return TreeFile{Path: path, Mode: mode, Data: data, Binary: true}
}

// digestFileEntry is the per-file unit of the skill_version digest.
type digestFileEntry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}

// digestDoc is the canonical JSON document hashed to produce skill_version.
type digestDoc struct {
	RenderVersion int               `json:"render_version"`
	Files         []digestFileEntry `json:"files"`
}

// ComputeSkillVersion returns the full rendered-tree digest:
//
//	sha256(canonical_json({render_version, files:[{path, mode, sha256-of-emitted-bytes}] sorted by path}))
//
// over every emitted file. Mode is part of the digest. The metadata.auto_skill
// provenance stamp must NOT be present in the files passed here — it is added to
// SKILL.md after the digest and excluded from it.
func ComputeSkillVersion(files []TreeFile) string {
	entries := make([]digestFileEntry, len(files))
	for i, f := range files {
		sum := sha256.Sum256(f.Data)
		entries[i] = digestFileEntry{
			Path:   f.Path,
			Mode:   f.Mode,
			SHA256: hex.EncodeToString(sum[:]),
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	doc := digestDoc{RenderVersion: RenderVersion, Files: entries}
	// json.Marshal is deterministic: struct field order is fixed and files are
	// pre-sorted by path. It cannot fail for this fixed string/int schema.
	canonical, err := json.Marshal(doc)
	if err != nil {
		panic("render: marshal digest doc: " + err.Error())
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// sha256Hex returns the lowercase hex sha256 of data (used for template_hash
// and file-ref content_hash).
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Provenance carries the fields written into the metadata.auto_skill stamp.
type Provenance struct {
	// Source is the canonical skill source (e.g. repo URL or "authored").
	Source string
	// Commit is the resolved upstream commit (empty for authored skills).
	Commit string
}

// ProvenanceStamp builds the metadata.auto_skill block stamped into the emitted
// SKILL.md AFTER the skill_version digest and EXCLUDED from it. It is
// informational only and never a deletion authority (receipts + the on-disk
// digest are the source of truth). skillVersion is the already-computed digest.
//
// Map keys are emitted in sorted order by yaml.v3, keeping the stamp
// deterministic.
func ProvenanceStamp(p Provenance, skillVersion string) map[string]any {
	return map[string]any{
		"source":         p.Source,
		"commit":         p.Commit,
		"skill_version":  skillVersion,
		"render_version": RenderVersion,
		"managed":        true,
	}
}
