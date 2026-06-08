package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
)

// New builds the autoui HTTP handler: a JSON /api/hello endpoint and a file
// server for the SPA assets rooted at fsys. mode is reported in /api/hello for
// diagnostics (e.g. "embed" or "disk").
//
// In disk (dev) mode the asset responses carry Cache-Control: no-store so a
// plain browser refresh always re-fetches edited files — without it, http's
// Last-Modified-only caching makes browsers serve stale ES modules, breaking
// the edit→refresh loop. Embedded assets are immutable per binary, so they are
// served with the default caching.
func New(fsys fs.FS, mode string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "hi from go",
			"mode":    mode,
		})
	})

	assets := http.FileServer(http.FS(fsys))
	if mode == "disk" {
		assets = noStore(assets)
	}
	mux.Handle("/", assets) // GET / -> index.html
	return mux
}

// noStore wraps h to disable browser caching of static assets (dev only).
func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}
