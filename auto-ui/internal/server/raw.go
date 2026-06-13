package server

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-shared/config"
)

// handleDocRaw serves verbatim HTML doc bytes from GET /api/doc/raw.
//
// Query params: project, path, worktree (path is required; project or worktree
// identifies the root via resolveRoot). Only docs/**/*.html paths are served —
// .md and traversal are rejected. The bytes are written verbatim with
// Content-Type text/html; HTML is never routed through the markdown-inline
// path (doc.get), only this raw route.
func handleDocRaw(reg func() config.ProjectsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		project := q.Get("project")
		reqPath := q.Get("path")
		worktree := q.Get("worktree")

		if reqPath == "" {
			http.Error(w, "path is required", http.StatusBadRequest)
			return
		}

		root, err := resolveRoot(reg(), project, worktree)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Only .html docs under docs/ are served verbatim; .md and traversal rejected.
		cleaned := cleanDocPath(reqPath, ".html")
		if cleaned == "" {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		absPath := filepath.Join(root, filepath.FromSlash(cleaned))

		data, err := os.ReadFile(absPath)
		if err != nil {
			// Don't leak absolute paths in error messages.
			http.Error(w, "doc not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Serving verbatim HTML is the explicit purpose of this route (epic
		// decision: self-contained planning HTML is rendered in a sandboxed
		// iframe by the SPA, not inlined). The path is validated by cleanDocPath
		// to be a docs/**/*.html file under a registered root.
		_, _ = w.Write(data) //nolint:gosec // G705: verbatim HTML is this route's contract
	}
}
