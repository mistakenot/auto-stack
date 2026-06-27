package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-ui/internal/backend"
)

// dedupWindow is the TTL the eventGate dedups event ids over. The same event
// arrives at the hub by two paths that fire within milliseconds of each other —
// the local POST /api/rpc ingest and the relayed copy from a backend's bus —
// so a small window is enough to collapse them while never suppressing a
// genuinely later event that happens to reuse an id.
const dedupWindow = 5 * time.Second

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

	// The gate fronts the hub with id-based dedup over dedupWindow. Both the
	// local /api/rpc ingest path and the relayed-backend sink broadcast through
	// it, so a single event reaching the hub by both paths is delivered once.
	gate := newEventGate(hub, dedupWindow)

	// When a backend manager is configured, route every relayed backend event
	// through the gate into the hub. The relay path broadcasts raw events only;
	// derivation happens once, on the ingesting backend (no re-derivation here).
	if o.mgr != nil {
		o.mgr.SetEventSink(gate.Broadcast)
	}

	// In debug mode, record raw + derived ingest events into a ring buffer
	// exposed via /api/debug/recent.
	var buf *debugBuffer
	if o.debug {
		buf = &debugBuffer{}
	}

	// Shared dispatcher routes client->server RPC calls over WebSocket.
	d := newDispatcher()
	// Doc/project reads are pure proxies to the resolved backend — the UI owns
	// no local copy of this data. A nil manager yields a clear error, never a
	// local-filesystem read.
	d.Register("doc.list", proxyCall(o.mgr, "doc.list"))
	d.Register("doc.get", proxyCall(o.mgr, "doc.get"))
	d.Register("project.list", fanOutProjectList(o.mgr))

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

	// POST /api/rpc: fire-and-forget ingest of bus events. Ingest broadcasts
	// through the gate (not the hub directly) so a locally-ingested event and
	// its relayed copy collapse to a single delivery.
	mux.HandleFunc("/api/rpc", handleRPC(gate.Broadcast, o.regProvider, buf))

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
