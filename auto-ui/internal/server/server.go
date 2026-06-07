package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
)

// New builds the autoui HTTP handler: a JSON /api/hello endpoint and a file
// server for the SPA assets rooted at fsys. mode is reported in /api/hello for
// diagnostics (e.g. "embed" or "disk").
func New(fsys fs.FS, mode string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "hi from go",
			"mode":    mode,
		})
	})
	mux.Handle("/", http.FileServer(http.FS(fsys))) // GET / -> index.html
	return mux
}
