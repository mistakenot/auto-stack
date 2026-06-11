package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-shared/config"
)

// docEntry is a single doc file returned by doc.list.
type docEntry struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// docListHandler returns a JSON-RPC Handler for the "doc.list" method.
// It walks docs/**/*.md under the resolved project root and returns a list of
// relative paths (no bodies — cheap rung per the resource pattern).
func docListHandler(regProvider func() config.ProjectsConfig) Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Project  string `json:"project"`
			Worktree string `json:"worktree"`
		}
		if params != nil {
			_ = json.Unmarshal(params, &p)
		}

		root, err := resolveRoot(regProvider(), p.Project, p.Worktree)
		if err != nil {
			return nil, &rpcError{Code: codeInternalError, Message: err.Error()}
		}

		entries, err := walkDocs(root)
		if err != nil {
			return nil, &rpcError{Code: codeInternalError, Message: "failed to list docs"}
		}
		return entries, nil
	}
}

// docGetHandler returns a JSON-RPC Handler for the "doc.get" method.
// It reads a single docs/**/*.md file and returns its raw markdown content.
// Path traversal and reads outside docs/**/*.md are rejected.
func docGetHandler(regProvider func() config.ProjectsConfig) Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Project  string `json:"project"`
			Path     string `json:"path"`
			Worktree string `json:"worktree"`
		}
		if params != nil {
			_ = json.Unmarshal(params, &p)
		}

		if p.Path == "" {
			return nil, &rpcError{Code: codeParseError, Message: "path is required"}
		}

		root, err := resolveRoot(regProvider(), p.Project, p.Worktree)
		if err != nil {
			return nil, &rpcError{Code: codeInternalError, Message: err.Error()}
		}

		// Clean and validate the requested path.
		cleaned := cleanDocPath(p.Path)
		if cleaned == "" {
			return nil, &rpcError{Code: codeParseError, Message: "invalid path"}
		}

		absPath := filepath.Join(root, filepath.FromSlash(cleaned))

		data, err := os.ReadFile(absPath)
		if err != nil {
			// Don't leak absolute paths in error messages.
			return nil, &rpcError{Code: codeInternalError, Message: "doc not found"}
		}

		return map[string]string{
			"path":     cleaned,
			"markdown": string(data),
		}, nil
	}
}

// resolveRoot determines the filesystem root for doc operations. It validates
// the worktree (if given) against the project registry — an arbitrary
// client-supplied path is never accepted as the read root. Resolution order:
//  1. If worktree is set, it must match a registered project's path.
//  2. If project is set, look up by ID and use its path.
//  3. Otherwise, error.
func resolveRoot(reg config.ProjectsConfig, project, worktree string) (string, error) {
	if worktree != "" {
		// Validate: the worktree must be associated with a registered project.
		// Try FindProjectByPath (handles both main and worktree paths).
		if ref := reg.FindProjectByPath(worktree); ref != nil {
			return filepath.Clean(worktree), nil
		}
		return "", fmt.Errorf("worktree not found in registry")
	}
	if project != "" {
		if ref := reg.FindProjectByID(project); ref != nil {
			return filepath.Clean(ref.Path), nil
		}
		return "", fmt.Errorf("project not found in registry")
	}
	return "", fmt.Errorf("project or worktree is required")
}

// walkDocs walks the docs/ directory under root and returns entries for all
// *.md files found there (at any depth).
func walkDocs(root string) ([]docEntry, error) {
	docsDir := filepath.Join(root, "docs")
	entries := []docEntry{}

	err := filepath.WalkDir(docsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil // skip silently
		}
		// Normalize to forward slashes for consistency.
		rel = filepath.ToSlash(rel)
		entries = append(entries, docEntry{
			ID:   rel,
			Path: rel,
		})
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil // no docs/ dir is fine — empty list
		}
		return nil, err
	}
	return entries, nil
}

// cleanDocPath validates and cleans a path for doc.get. It returns "" if the
// path is invalid (traversal, outside docs/, not .md).
func cleanDocPath(p string) string {
	// Use path.Clean (forward-slash) for the logical path.
	cleaned := path.Clean(p)

	// Reject traversal.
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/..") {
		return ""
	}

	// Strip leading "./" or "/".
	cleaned = strings.TrimPrefix(cleaned, "./")
	cleaned = strings.TrimPrefix(cleaned, "/")

	// Must be under docs/ and a .md file.
	if !strings.HasPrefix(cleaned, "docs/") || !strings.HasSuffix(cleaned, ".md") {
		return ""
	}

	return cleaned
}
