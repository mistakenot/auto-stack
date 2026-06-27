package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Error codes for file-ref resolution failures.
const (
	// CodeSymlinkEscape is returned when a file-ref is, or traverses, a symlink
	// whose fully resolved real path lands outside the repo root — even when the
	// lexical path looks contained.
	CodeSymlinkEscape = "symlink_escape"
	// CodeInvalidRef is returned for a malformed ref: empty, absolute, a glob, or
	// a value-to-value interpolation (none of which name a single repo file).
	CodeInvalidRef = "invalid_ref"
	// CodeRefNotFound is returned when the referenced file does not exist.
	CodeRefNotFound = "ref_not_found"
	// CodeSectionNotFound is returned when a section selector matches no heading.
	// A skill never renders with missing content, so this is a hard error.
	CodeSectionNotFound = "section_not_found"
)

// FileRefError is a typed, structured file-ref resolution error carrying a
// stable code, the offending file, and a remediation hint.
type FileRefError struct {
	ErrCode string
	File    string
	Message string
}

func (e *FileRefError) Error() string { return e.Message }

// Code returns the stable error code.
func (e *FileRefError) Code() string { return e.ErrCode }

// fileRefResolver resolves file-refs against a single repo root, enforcing
// containment on the fully symlink-resolved real path. It is a pure leaf: it
// touches only the filesystem under root and imports no cache/network package.
type fileRefResolver struct {
	root string
}

// NewFileRefResolver returns a FileRefResolver scoped to root. Refs are
// repo-relative paths under root; resolution rejects any ref whose real
// (symlink-resolved) path escapes root. root itself is symlink-resolved per
// call so a moved/relinked root is handled at use time, not construction time.
func NewFileRefResolver(root string) FileRefResolver {
	return &fileRefResolver{root: root}
}

// Resolve reads the referenced file (or a section of it), strips leading YAML
// frontmatter by default, and returns the canonical inlined bytes plus their
// sha256. Containment is enforced on the EvalSymlinks-resolved real path of both
// root and target: a ref that is or traverses a symlink leaving root is rejected
// with CodeSymlinkEscape, even when lexical `..`/Clean cleaning alone would have
// allowed it. Content is inserted raw — it is never re-run through the template.
func (r *fileRefResolver) Resolve(ref FileRef) (ResolvedRef, error) {
	file := ref.File
	if strings.TrimSpace(file) == "" {
		return ResolvedRef{}, &FileRefError{
			ErrCode: CodeInvalidRef,
			File:    file,
			Message: CodeInvalidRef + ": file-ref has an empty file path; set replacements.<var>.file to a repo-relative path",
		}
	}
	if strings.ContainsAny(file, "*?[") {
		return ResolvedRef{}, &FileRefError{
			ErrCode: CodeInvalidRef,
			File:    file,
			Message: fmt.Sprintf("%s: file-ref %q is a glob; a file-ref must name exactly one file", CodeInvalidRef, file),
		}
	}
	if strings.Contains(file, "{{") || strings.Contains(file, "}}") {
		return ResolvedRef{}, &FileRefError{
			ErrCode: CodeInvalidRef,
			File:    file,
			Message: fmt.Sprintf("%s: file-ref %q looks like a value-to-value interpolation; a file-ref must name a literal repo path", CodeInvalidRef, file),
		}
	}
	if filepath.IsAbs(file) {
		return ResolvedRef{}, &FileRefError{
			ErrCode: CodeInvalidRef,
			File:    file,
			Message: fmt.Sprintf("%s: file-ref %q is absolute; refs must be repo-relative to the resolver root", CodeInvalidRef, file),
		}
	}

	// Resolve the real root (follows any symlinks in root itself).
	realRoot, err := filepath.EvalSymlinks(r.root)
	if err != nil {
		return ResolvedRef{}, &FileRefError{
			ErrCode: CodeRefNotFound,
			File:    file,
			Message: fmt.Sprintf("%s: cannot resolve resolver root %q: %v", CodeRefNotFound, r.root, err),
		}
	}

	// Lexical join, then fully resolve symlinks on the TARGET. This is the gate:
	// lexical Clean/.. cleaning alone is deliberately NOT trusted, because a
	// symlinked path can look lexically contained yet resolve outside root.
	joined := filepath.Join(r.root, filepath.FromSlash(file))
	realTarget, err := filepath.EvalSymlinks(joined)
	if err != nil {
		if os.IsNotExist(err) {
			return ResolvedRef{}, &FileRefError{
				ErrCode: CodeRefNotFound,
				File:    file,
				Message: fmt.Sprintf("%s: referenced file %q does not exist under the repo root", CodeRefNotFound, file),
			}
		}
		return ResolvedRef{}, &FileRefError{
			ErrCode: CodeRefNotFound,
			File:    file,
			Message: fmt.Sprintf("%s: cannot resolve referenced file %q: %v", CodeRefNotFound, file, err),
		}
	}

	if !withinRoot(realRoot, realTarget) {
		return ResolvedRef{}, &FileRefError{
			ErrCode: CodeSymlinkEscape,
			File:    file,
			Message: fmt.Sprintf("%s: file-ref %q resolves to %q which is outside the repo root %q (a symlink escapes the root)", CodeSymlinkEscape, file, realTarget, realRoot),
		}
	}

	// realTarget is fully symlink-resolved, so reading it cannot traverse a new
	// symlink — TOCTOU-safe in practice for the containment we enforced.
	data, err := os.ReadFile(realTarget)
	if err != nil {
		return ResolvedRef{}, &FileRefError{
			ErrCode: CodeRefNotFound,
			File:    file,
			Message: fmt.Sprintf("%s: cannot read referenced file %q: %v", CodeRefNotFound, file, err),
		}
	}

	text := normalizeLF(string(data))

	var content, matchedHeading string
	var warnings []string
	if len(ref.Section) > 0 {
		// Sections always operate on the frontmatter-stripped body: frontmatter
		// is never a heading-bounded section.
		body := stripLeadingFrontmatter(text)
		content, matchedHeading, warnings, err = extractSection(body, ref.Section, ref.IncludeHeading)
		if err != nil {
			return ResolvedRef{}, err
		}
	} else {
		strip := ref.StripFrontmatter == nil || *ref.StripFrontmatter
		if strip {
			content = stripLeadingFrontmatter(text)
		} else {
			content = text
		}
	}

	// content_hash is taken over exactly the inlined bytes, using the same text
	// canonicalization phase 1 uses for emitted text (hash == inlined bytes).
	canonical := canonicalizeText([]byte(content))
	return ResolvedRef{
		Content:        string(canonical),
		ContentHash:    sha256Hex(canonical),
		MatchedHeading: matchedHeading,
		Warnings:       warnings,
	}, nil
}

// withinRoot reports whether target is root itself or lies beneath it. Both
// paths must already be symlink-resolved real paths.
func withinRoot(root, target string) bool {
	if root == target {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// normalizeLF converts CRLF and lone CR to LF.
func normalizeLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// stripLeadingFrontmatter removes a leading `---`-fenced YAML frontmatter block
// from LF-normalized text. Text without a leading frontmatter fence, or with an
// unterminated one, is returned unchanged.
func stripLeadingFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	rest := s[len("---\n"):]
	if _, after, ok := strings.Cut(rest, "\n---\n"); ok {
		return after
	}
	if rest == "---" || strings.HasSuffix(rest, "\n---") {
		return ""
	}
	return s
}
