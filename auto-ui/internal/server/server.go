package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
)

// Option configures New.
type Option func(*options)

type options struct {
	regProvider func() config.ProjectsConfig
}

// WithRegistryProvider sets the function New uses to obtain the project
// registry. The provider is called per ingest/doc request so post-startup
// registrations resolve without a restart. The default (no option) returns an
// empty registry, keeping unit tests hermetic.
func WithRegistryProvider(fn func() config.ProjectsConfig) Option {
	return func(o *options) { o.regProvider = fn }
}

// New builds the autoui HTTP handler: a JSON /api/hello endpoint, a WebSocket
// JSON-RPC endpoint, a POST /api/rpc ingest endpoint, and a file server for
// the SPA assets rooted at fsys. mode is reported in /api/hello for diagnostics
// (e.g. "embed" or "disk").
//
// In disk (dev) mode the asset responses carry Cache-Control: no-store so a
// plain browser refresh always re-fetches edited files — without it, http's
// Last-Modified-only caching makes browsers serve stale ES modules, breaking
// the edit→refresh loop. Embedded assets are immutable per binary, so they are
// served with the default caching.
func New(fsys fs.FS, mode string, opts ...Option) http.Handler {
	o := &options{
		regProvider: func() config.ProjectsConfig { return config.ProjectsConfig{} },
	}
	for _, opt := range opts {
		opt(o)
	}

	hub := bus.NewHub()

	// Shared dispatcher routes client->server RPC calls over WebSocket.
	d := newDispatcher()
	d.Register("ping", func(_ context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Seq int64 `json:"seq"`
		}
		_ = json.Unmarshal(params, &p)
		return map[string]any{"pong": true, "seq": p.Seq}, nil
	})
	d.Register("doc.list", docListHandler(o.regProvider))
	d.Register("doc.get", docGetHandler(o.regProvider))
	d.Register("project.list", projectListHandler(o.regProvider))

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

	// Bidirectional JSON-RPC 2.0 over WebSocket: client RPC calls + correlated
	// responses, plus server->client push notifications via the hub.
	mux.HandleFunc("/api/ws", handleWSWithHub(hub, d))

	// POST /api/rpc: fire-and-forget ingest of bus events.
	mux.HandleFunc("/api/rpc", handleRPC(hub, o.regProvider))

	// GET /api/doc/raw: verbatim HTML doc bytes (text/html), .html only.
	mux.HandleFunc("/api/doc/raw", handleDocRaw(o.regProvider))

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
