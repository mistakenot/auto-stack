// Package backend manages the set of live RPC connections from the UI to
// autowatch backends. A single Manager owns one peer per configured backend,
// learns each backend's authoritative host id from daemon.status on connect,
// and reconciles the live set against backends.json on a periodic tick so that
// `auto ui backends add/remove` takes effect without restarting the server.
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-ui/internal/config"
)

// Sentinel errors returned by Resolve.
var (
	// ErrNoBackend is returned when no backend is connected.
	ErrNoBackend = errors.New("backend: no backend connected")
	// ErrUnknownHost is returned when an explicit host has no connected backend.
	ErrUnknownHost = errors.New("backend: unknown host")
	// ErrAmbiguousHost is returned when host is empty but multiple backends are
	// connected, so the target is ambiguous.
	ErrAmbiguousHost = errors.New("backend: ambiguous host; specify a host")
)

// defaultInterval is the reconcile tick used when interval <= 0.
const defaultInterval = 5 * time.Second

// statusTimeout bounds the daemon.status call used to learn a backend's host id
// on connect, so a half-open connection cannot wedge a reconcile tick.
const statusTimeout = 10 * time.Second

// DialFunc establishes a connection to a backend URI. It is injected so tests
// can supply a net.Pipe-backed fake; the default is transport.Dial.
type DialFunc func(ctx context.Context, uri string) (net.Conn, error)

// conn is a single live (or pending) backend connection. A conn is "connected"
// once its daemon.status call has succeeded and hostID is known; an unreachable
// backend is held as a not-connected conn carrying lastErr and is retried on a
// later tick.
type conn struct {
	uri       string
	peer      *rpc.Peer
	cancel    context.CancelFunc
	hostID    string
	connected bool
	lastErr   string
}

// Manager owns the live set of backend connections, keyed internally by URI
// (the natural backends.json key, which also lets reconcile cheaply skip an
// already-connected URI). Resolve maps the authoritative host id learned from
// daemon.status onto a peer.
type Manager struct {
	backendsPath string
	dial         DialFunc
	interval     time.Duration

	mu    sync.Mutex
	conns map[string]*conn // keyed by URI
}

// BackendHealth is a snapshot of a single backend's state for doctor.
type BackendHealth struct {
	HostID    string `json:"hostId"`
	URI       string `json:"uri"`
	Connected bool   `json:"connected"`
	LastErr   string `json:"lastErr,omitempty"`
}

// statusResult is the minimal shape decoded from daemon.status. The full type
// lives in auto-watch/internal/rpcmethods, which is internal to another module
// and cannot be imported here, so we decode only the field we need.
type statusResult struct {
	HostID string `json:"hostId"`
}

// NewManager constructs a Manager. If dial is nil, transport.Dial is used.
func NewManager(backendsPath string, dial DialFunc, interval time.Duration) *Manager {
	if dial == nil {
		dial = transport.Dial
	}
	return &Manager{
		backendsPath: backendsPath,
		dial:         dial,
		interval:     interval,
		conns:        make(map[string]*conn),
	}
}

// Run reconciles immediately, then every interval until ctx is done. On exit it
// closes all peers. It always returns ctx.Err().
func (m *Manager) Run(ctx context.Context) error {
	interval := m.interval
	if interval <= 0 {
		interval = defaultInterval
	}

	m.Reconcile(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.closeAll()
			return ctx.Err()
		case <-ticker.C:
			m.Reconcile(ctx)
		}
	}
}

// Reconcile re-reads backends.json and diffs the configured URIs against the
// live set: it closes connections whose URI is no longer configured and dials
// every configured URI that is not already connected. New dials run
// concurrently so one unreachable backend cannot block the others; a failed
// dial or daemon.status leaves the URI pending with lastErr for a later retry.
// A config that fails to load leaves the current live set untouched.
func (m *Manager) Reconcile(ctx context.Context) {
	cfg, err := config.LoadBackends(m.backendsPath)
	if err != nil {
		return
	}

	configured := make(map[string]struct{}, len(cfg.Backends))
	for _, b := range cfg.Backends {
		configured[b.URI] = struct{}{}
	}

	var (
		toClose []*conn
		toDial  []string
	)

	m.mu.Lock()
	for uri, c := range m.conns {
		if _, ok := configured[uri]; !ok {
			toClose = append(toClose, c)
			delete(m.conns, uri)
		}
	}
	for uri := range configured {
		c, ok := m.conns[uri]
		if !ok || !c.connected {
			toDial = append(toDial, uri)
		}
	}
	m.mu.Unlock()

	for _, c := range toClose {
		closeConn(c)
	}

	var wg sync.WaitGroup
	for _, uri := range toDial {
		wg.Go(func() {
			m.connect(ctx, uri)
		})
	}
	wg.Wait()
}

// connect dials uri, starts a Serve goroutine for its peer, and calls
// daemon.status to learn the authoritative host id. On any failure it records
// lastErr and leaves the URI pending (not connected). The blocking dial and
// status call run outside the lock; the lock is only held to snapshot/insert
// state.
func (m *Manager) connect(ctx context.Context, uri string) {
	// Tear down any stale, not-connected peer for this URI before redialing.
	m.mu.Lock()
	if c, ok := m.conns[uri]; ok && !c.connected {
		stale := c
		m.mu.Unlock()
		closeConn(stale)
	} else {
		m.mu.Unlock()
	}

	netConn, err := m.dial(ctx, uri)
	if err != nil {
		m.setErr(uri, fmt.Sprintf("dial %s: %v", uri, err))
		return
	}

	peer := rpc.NewPeer(netConn)
	connCtx, cancel := context.WithCancel(ctx)
	go func() { _ = peer.Serve(connCtx) }()

	callCtx, callCancel := context.WithTimeout(connCtx, statusTimeout)
	raw, err := peer.Call(callCtx, "daemon.status", nil)
	callCancel()
	if err != nil {
		cancel()
		_ = peer.Close()
		m.setErr(uri, fmt.Sprintf("daemon.status %s: %v", uri, err))
		return
	}

	var status statusResult
	if err := json.Unmarshal(raw, &status); err != nil || status.HostID == "" {
		cancel()
		_ = peer.Close()
		msg := fmt.Sprintf("daemon.status %s: empty hostId", uri)
		if err != nil {
			msg = fmt.Sprintf("daemon.status %s: decode: %v", uri, err)
		}
		m.setErr(uri, msg)
		return
	}

	m.mu.Lock()
	m.conns[uri] = &conn{
		uri:       uri,
		peer:      peer,
		cancel:    cancel,
		hostID:    status.HostID,
		connected: true,
	}
	m.mu.Unlock()
}

// setErr records a failed dial/status for uri, leaving it pending for retry.
func (m *Manager) setErr(uri, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[uri]
	if !ok {
		c = &conn{uri: uri}
		m.conns[uri] = c
	}
	c.peer = nil
	c.cancel = nil
	c.connected = false
	c.lastErr = msg
}

// Resolve returns the peer for the requested host. A non-empty host selects
// that backend by its learned host id (ErrUnknownHost if not connected). An
// empty host returns the sole connected backend, ErrAmbiguousHost if more than
// one is connected, or ErrNoBackend if none are.
func (m *Manager) Resolve(host string) (*rpc.Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var connected []*conn
	for _, c := range m.conns {
		if c.connected {
			connected = append(connected, c)
		}
	}

	if host != "" {
		for _, c := range connected {
			if c.hostID == host {
				return c.peer, nil
			}
		}
		return nil, ErrUnknownHost
	}

	switch len(connected) {
	case 0:
		return nil, ErrNoBackend
	case 1:
		return connected[0].peer, nil
	default:
		return nil, ErrAmbiguousHost
	}
}

// Health returns a snapshot of every known backend (connected or pending),
// sorted by URI for stable output.
func (m *Manager) Health() []BackendHealth {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]BackendHealth, 0, len(m.conns))
	for _, c := range m.conns {
		out = append(out, BackendHealth{
			HostID:    c.hostID,
			URI:       c.uri,
			Connected: c.connected,
			LastErr:   c.lastErr,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out
}

// closeAll tears down every connection and clears the live set.
func (m *Manager) closeAll() {
	m.mu.Lock()
	conns := m.conns
	m.conns = make(map[string]*conn)
	m.mu.Unlock()

	for _, c := range conns {
		closeConn(c)
	}
}

// closeConn cancels a connection's Serve goroutine and closes its peer. Safe on
// pending conns whose peer/cancel are nil.
func closeConn(c *conn) {
	if c == nil {
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.peer != nil {
		_ = c.peer.Close()
	}
}
