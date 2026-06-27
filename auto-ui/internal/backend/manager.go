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
	"sync/atomic"
	"time"

	"github.com/mistakenot/auto-shared/bus"
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

// subscribeTimeout bounds the bus.subscribe call made after the daemon.status
// handshake, so a backend that never replies to bus.subscribe cannot wedge a
// reconcile tick (mirrors statusTimeout). A timeout degrades the relay only.
const subscribeTimeout = 10 * time.Second

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
	// down is set under m.mu by the Serve goroutine when the transport drops,
	// so the handshake path won't publish a dead peer as connected and so the
	// conn is marked unhealthy for redial. peer/cancel are written once at
	// creation (read locklessly by closeConn), so the goroutine never mutates
	// them.
	down    bool
	lastErr string
	// relayDegraded is set under m.mu when bus.subscribe fails (or times out):
	// the peer stays usable for proxied RPCs, but relayed events are not flowing
	// for this backend. Reconcile re-attempts bus.subscribe on connected-degraded
	// conns and clears the flag on success.
	relayDegraded bool
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

	// eventSink holds the relay callback set by SetEventSink. It is stored in an
	// atomic.Value (NOT guarded by m.mu) so the read-loop read in onNotify can't
	// race the setter write, and so the callback is invoked outside the conns
	// lock (D-1). It always holds an eventSinkHolder (possibly with a nil fn).
	eventSink atomic.Value
}

// eventSinkHolder wraps the relay callback so atomic.Value always stores a
// single concrete type even when the callback is nil.
type eventSinkHolder struct {
	fn func(bus.Event)
}

// BackendHealth is a snapshot of a single backend's state for doctor.
type BackendHealth struct {
	HostID        string `json:"hostId"`
	URI           string `json:"uri"`
	Connected     bool   `json:"connected"`
	RelayDegraded bool   `json:"relayDegraded,omitempty"`
	LastErr       string `json:"lastErr,omitempty"`
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
	m := &Manager{
		backendsPath: backendsPath,
		dial:         dial,
		interval:     interval,
		conns:        make(map[string]*conn),
	}
	m.eventSink.Store(eventSinkHolder{})
	return m
}

// SetEventSink stores the relay callback invoked for every relayed bus event.
// It is stored in an atomic.Value (not guarded by m.mu) so the setter write
// cannot race the read-loop read in onNotify, and so the callback is invoked
// outside the conns lock (D-1). Passing nil disables relaying.
func (m *Manager) SetEventSink(fn func(bus.Event)) {
	m.eventSink.Store(eventSinkHolder{fn: fn})
}

// onNotify is the OnNotify callback registered on every peer. It decodes the
// inbound notification params into a bus.Event and forwards it to the event
// sink. The sink is loaded + copied and invoked OUTSIDE m.mu (D-1); an unset
// sink drops the event silently (harmless — events are lossy invalidations).
func (m *Manager) onNotify(req rpc.Request) {
	var ev bus.Event
	if err := json.Unmarshal(req.Params, &ev); err != nil {
		return
	}
	holder, _ := m.eventSink.Load().(eventSinkHolder)
	fn := holder.fn
	if fn == nil {
		return
	}
	fn(ev)
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
		toClose       []*conn
		toDial        []string
		toResubscribe []*conn
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
		switch {
		case !ok || !c.connected:
			// Missing or disconnected: (re)dial. connect() re-subscribes.
			toDial = append(toDial, uri)
		case c.relayDegraded:
			// Connected but relay-degraded: re-attempt bus.subscribe without
			// redialing — the peer is still usable for proxied RPCs (AC-6).
			toResubscribe = append(toResubscribe, c)
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
	for _, c := range toResubscribe {
		wg.Go(func() {
			m.resubscribe(ctx, c)
		})
	}
	wg.Wait()
}

// resubscribe re-attempts bus.subscribe on a connected-but-degraded conn,
// clearing relayDegraded on success. The call is bounded by subscribeTimeout so
// a non-responsive backend cannot wedge Reconcile's wait group (AC-6). It does
// not redial; a full disconnect+redial re-subscribes via connect() instead.
func (m *Manager) resubscribe(ctx context.Context, c *conn) {
	if !m.subscribe(ctx, c.peer) {
		return
	}
	m.mu.Lock()
	c.relayDegraded = false
	m.mu.Unlock()
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

	peer := rpc.NewPeer(netConn, rpc.WithOnNotify(m.onNotify))
	connCtx, cancel := context.WithCancel(ctx)
	// Build the conn up front so the Serve goroutine can mark this exact
	// instance unhealthy when the transport drops (backend restart / network
	// loss). Without this the conn would stay connected=true forever, Reconcile
	// would skip redialing it, and Resolve would keep handing out a dead peer
	// until the service is restarted.
	c := &conn{uri: uri, peer: peer, cancel: cancel}
	go func() {
		_ = peer.Serve(connCtx)
		m.mu.Lock()
		c.connected = false
		c.down = true
		if c.lastErr == "" {
			c.lastErr = "backend disconnected: " + uri
		}
		m.mu.Unlock()
	}()

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

	// Subscribe to the backend's event bus so relayed events flow into the UI.
	// This is bounded by its own subscribeTimeout (mirroring statusTimeout) so a
	// backend that never replies cannot wedge Reconcile's wait group. A failure
	// degrades the relay only (D-3): the peer stays usable for proxied RPCs, the
	// conn is published as connected, and relayDegraded is set for retry.
	relayDegraded := !m.subscribe(connCtx, peer)

	m.mu.Lock()
	// If the transport already dropped during the handshake, the Serve
	// goroutine set c.down — leave it pending for the next tick rather than
	// publishing a dead peer as connected.
	if c.down {
		m.mu.Unlock()
		cancel()
		_ = peer.Close()
		m.setErr(uri, "backend disconnected during handshake: "+uri)
		return
	}
	c.hostID = status.HostID
	c.connected = true
	c.relayDegraded = relayDegraded
	m.conns[uri] = c
	m.mu.Unlock()
}

// subscribe calls bus.subscribe on peer, bounded by subscribeTimeout. It returns
// true on success. A false result degrades the relay only — the peer remains
// usable for proxied RPCs.
func (m *Manager) subscribe(connCtx context.Context, peer *rpc.Peer) bool {
	callCtx, callCancel := context.WithTimeout(connCtx, subscribeTimeout)
	defer callCancel()
	_, err := peer.Call(callCtx, "bus.subscribe", nil)
	return err == nil
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

// ResolveByProject finds the backend that owns projectID by calling
// project.list on each connected peer concurrently. Returns ErrNoBackend if
// none are connected, ErrUnknownHost if no backend claims the project.
func (m *Manager) ResolveByProject(ctx context.Context, projectID string) (*rpc.Peer, error) {
	peers := m.ConnectedPeers()
	if len(peers) == 0 {
		return nil, ErrNoBackend
	}

	type hit struct {
		peer *rpc.Peer
	}
	ch := make(chan *hit, len(peers))

	for _, p := range peers {
		go func() {
			raw, err := p.Peer.Call(ctx, "project.list", nil)
			if err != nil {
				ch <- nil
				return
			}
			var projects []struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &projects); err != nil {
				ch <- nil
				return
			}
			for _, proj := range projects {
				if proj.ID == projectID {
					ch <- &hit{peer: p.Peer}
					return
				}
			}
			ch <- nil
		}()
	}

	for range peers {
		if h := <-ch; h != nil {
			return h.peer, nil
		}
	}
	return nil, ErrUnknownHost
}

// PeerInfo is a connected peer with its host id, returned by ConnectedPeers.
type PeerInfo struct {
	HostID string
	Peer   *rpc.Peer
}

// ConnectedPeers returns every currently connected peer. The caller can fan out
// RPCs across all backends (e.g. project.list aggregation).
func (m *Manager) ConnectedPeers() []PeerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []PeerInfo
	for _, c := range m.conns {
		if c.connected {
			out = append(out, PeerInfo{HostID: c.hostID, Peer: c.peer})
		}
	}
	return out
}

// Health returns a snapshot of every known backend (connected or pending),
// sorted by URI for stable output.
func (m *Manager) Health() []BackendHealth {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]BackendHealth, 0, len(m.conns))
	for _, c := range m.conns {
		out = append(out, BackendHealth{
			HostID:        c.hostID,
			URI:           c.uri,
			Connected:     c.connected,
			RelayDegraded: c.relayDegraded,
			LastErr:       c.lastErr,
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
