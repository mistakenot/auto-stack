package server

import (
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-ui/internal/backend"
)

// Option configures New.
type Option func(*options)

type options struct {
	regProvider func() config.ProjectsConfig
	mgr         *backend.Manager
	debug       bool
}

// WithRegistryProvider sets the function New uses to obtain the project
// registry. The provider is called per ingest/doc request so post-startup
// registrations resolve without a restart. The default (no option) returns an
// empty registry, keeping unit tests hermetic.
func WithRegistryProvider(fn func() config.ProjectsConfig) Option {
	return func(o *options) { o.regProvider = fn }
}

// WithBackendManager sets the backend.Manager the server proxies doc/project
// RPCs to. The UI holds no local doc/project data: doc.list/doc.get/project.list
// and GET /api/doc/raw forward to the resolved autowatch backend. When no
// manager is set those routes return a clear "no backend configured" error
// rather than touching the local filesystem.
func WithBackendManager(mgr *backend.Manager) Option {
	return func(o *options) { o.mgr = mgr }
}

// WithDebug enables the in-memory debug event buffer and the gated
// GET /api/debug/recent route, which exposes the last N raw and derived events
// that passed through the ingest handler. It is off by default; serve.go enables
// it from AUTO_UI_DEBUG so server tests stay hermetic.
func WithDebug(enabled bool) Option {
	return func(o *options) { o.debug = enabled }
}

// New builds the autoui HTTP handler: a JSON /api/hello endpoint, a WebSocket
// JSON-RPC endpoint, and a file server for the SPA assets rooted at fsys. Live
// events reach the browser only via the backend relay (045) — auto-ui has no
// local ingest endpoint. mode is reported in /api/hello for diagnostics
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

	// In debug mode, record relayed events into a ring buffer exposed via
	// /api/debug/recent.
	var buf *debugBuffer
	if o.debug {
		buf = &debugBuffer{}
	}

	// When a backend manager is configured, broadcast every relayed backend
	// event straight to the hub — the relay (045) is auto-ui's only ingest path,
	// so there is no second route an id could arrive by and no dedup is needed.
	// Derivation happens once, on the ingesting autowatch backend (no
	// re-derivation here). The same sink also records into the debug ring so
	// /api/debug/recent keeps showing events now that the local ingest is gone
	// (D-3).
	if o.mgr != nil {
		o.mgr.SetEventSink(func(ev bus.Event) {
			hub.Broadcast(ev)
			if buf != nil {
				buf.record(ev)
			}
		})
	}

	// Shared dispatcher routes client->server RPC calls over WebSocket.
	d := newDispatcher()
	// Doc reads are pure proxies to the resolved backend — the UI owns no local
	// copy of this data. A nil manager yields a clear error, never a
	// local-filesystem read. project.list is the exception: it AGGREGATES across
	// all connected backends (fan-out + host-tag + merge — see
	// project_aggregate.go), so a multi-host fleet renders one flat list; doc.list
	// and doc.get stay per-backend proxies routed by host.
	d.Register("doc.list", proxyCall(o.mgr, "doc.list"))
	d.Register("doc.get", proxyCall(o.mgr, "doc.get"))
	d.Register("project.list", aggregateProjectList(o.mgr))
	// backends.list surfaces Manager.Health() so the SPA renders a per-backend status UI.
	d.Register("backends.list", backendsList(o.mgr))

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

	// GET /api/doc/raw: verbatim doc bytes proxied from the backend's doc.raw.
	mux.HandleFunc("/api/doc/raw", handleDocRawProxy(o.mgr))

	// GET /api/debug/recent: last N ingest events (gated by WithDebug).
	mux.HandleFunc("/api/debug/recent", handleDebugRecent(buf, o.debug))

	assets := http.FileServer(http.FS(fsys))
	if mode == "disk" {
		assets = noStore(assets)
	}
	mux.Handle("/", assets) // GET / -> index.html

	// Defense-in-depth: refuse any peer that is not loopback, even though we
	// also bind to 127.0.0.1. See loopbackOnly for why this is safe behind
	// `tailscale serve`.
	return loopbackOnly(mux)
}

// noStore wraps h to disable browser caching of static assets (dev only).
func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}
