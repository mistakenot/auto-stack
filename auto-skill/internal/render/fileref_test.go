package render

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codeOf extracts a stable error code from a typed render error.
func codeOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var fe *FileRefError
	if errors.As(err, &fe) {
		return fe.ErrCode
	}
	t.Fatalf("error %v (%T) is not a *FileRefError", err, err)
	return ""
}

func TestResolveWholeFileStripsFrontmatterByDefault(t *testing.T) {
	root := t.TempDir()
	content := "---\ntitle: Doc\ntag: x\n---\n# Heading\n\nBody text here.\n"
	if err := os.WriteFile(filepath.Join(root, "doc.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewFileRefResolver(root)

	// Default (nil) strips frontmatter.
	def, err := r.Resolve(FileRef{File: "doc.md"})
	if err != nil {
		t.Fatalf("default resolve: %v", err)
	}
	want := "# Heading\n\nBody text here.\n"
	if def.Content != want {
		t.Fatalf("default content = %q, want %q", def.Content, want)
	}
	if strings.Contains(def.Content, "title: Doc") {
		t.Fatalf("frontmatter not stripped by default: %q", def.Content)
	}

	// strip_frontmatter:true is identical to the default.
	stripTrue, err := r.Resolve(FileRef{File: "doc.md", StripFrontmatter: new(true)})
	if err != nil {
		t.Fatalf("strip=true resolve: %v", err)
	}
	if stripTrue.ContentHash != def.ContentHash {
		t.Fatalf("strip=true hash %s != default hash %s", stripTrue.ContentHash, def.ContentHash)
	}

	// strip_frontmatter:false keeps frontmatter and MOVES the content_hash.
	keep, err := r.Resolve(FileRef{File: "doc.md", StripFrontmatter: new(false)})
	if err != nil {
		t.Fatalf("strip=false resolve: %v", err)
	}
	if !strings.Contains(keep.Content, "title: Doc") {
		t.Fatalf("frontmatter dropped when strip=false: %q", keep.Content)
	}
	if keep.ContentHash == def.ContentHash {
		t.Fatalf("strip=false hash must differ from default; both = %s", def.ContentHash)
	}
}

func TestResolveSymlinkEscapeRejected(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// repo/link -> ../outside (a directory symlink leaving the root).
	if err := os.Symlink(filepath.Join("..", "outside"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	ref := "link/secret.txt"

	// Sanity: a LEXICAL-only containment gate (Clean/..) WOULD have allowed this
	// — the joined path is lexically inside root.
	joined := filepath.Join(root, filepath.FromSlash(ref))
	if !strings.HasPrefix(joined, root+string(filepath.Separator)) {
		t.Fatalf("precondition: joined path %q is not lexically inside root %q", joined, root)
	}
	if rel, err := filepath.Rel(root, joined); err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("precondition: lexical Rel says %q escapes (rel=%q err=%v); test would be vacuous", joined, rel, err)
	}

	// But symlink-resolved containment must reject it.
	r := NewFileRefResolver(root)
	_, err := r.Resolve(FileRef{File: ref})
	if got := codeOf(t, err); got != CodeSymlinkEscape {
		t.Fatalf("got code %q, want %q (err=%v)", got, CodeSymlinkEscape, err)
	}
}

func TestResolveDirectSymlinkFileRejected(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "target.txt")
	if err := os.WriteFile(target, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// repo/escape.md -> ../target.txt (the ref file itself is the symlink).
	if err := os.Symlink(filepath.Join("..", "target.txt"), filepath.Join(root, "escape.md")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	r := NewFileRefResolver(root)
	_, err := r.Resolve(FileRef{File: "escape.md"})
	if got := codeOf(t, err); got != CodeSymlinkEscape {
		t.Fatalf("got code %q, want %q", got, CodeSymlinkEscape)
	}
}

func TestResolveInvalidRefs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewFileRefResolver(root)

	cases := []struct {
		name string
		ref  string
		code string
	}{
		{"glob", "docs/*.md", CodeInvalidRef},
		{"bracket-glob", "docs/[ab].md", CodeInvalidRef},
		{"interpolation", "docs/{{ .other }}.md", CodeInvalidRef},
		{"empty", "   ", CodeInvalidRef},
		{"absolute", "/etc/passwd", CodeInvalidRef},
		{"missing", "nope.md", CodeRefNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Resolve(FileRef{File: tc.ref})
			if got := codeOf(t, err); got != tc.code {
				t.Fatalf("ref %q: got code %q, want %q", tc.ref, got, tc.code)
			}
		})
	}
}

func TestResolveWholeFileHashEqualsInlinedBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewFileRefResolver(root)
	res, err := r.Resolve(FileRef{File: "a.md"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ContentHash != sha256Hex([]byte(res.Content)) {
		t.Fatalf("content_hash is not the digest of the inlined bytes")
	}
	if res.MatchedHeading != "" {
		t.Fatalf("whole-file ref should have empty MatchedHeading, got %q", res.MatchedHeading)
	}
}
