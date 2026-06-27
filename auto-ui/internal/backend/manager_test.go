package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-ui/internal/config"
)

// peerSink mirrors auto-watch/internal/rpcserver/subscribe.go: it pushes hub
// events to an rpc.Peer as JSON-RPC notifications (peer.Notify(ev.Type, ev)).
type peerSink struct {
	peer *rpc.Peer
}

func (s *peerSink) Deliver(ev bus.Event) { _ = s.peer.Notify(ev.Type, ev) }

// subscribeMode controls how a fake backend answers bus.subscribe.
type subscribeMode int

const (
	// subscribeOK registers a peerSink on the hub and replies success.
	subscribeOK subscribeMode = iota
	// subscribeErr replies with an error (relay-degraded path).
	subscribeErr
	// subscribeHang never replies, so the caller must rely on its own timeout.
	subscribeHang
)

// fakeBackend is an in-process autowatch backend: an rpc.Peer serving canned
// daemon.status (returning hostID) and project.list over the server end of a
// net.Pipe, plus a bus.Hub and a bus.subscribe handler that registers a
// peerSink so broadcast events flow back to the Manager.
type fakeBackend struct {
	hostID string
	peer   *rpc.Peer
	cancel context.CancelFunc
	hub    *bus.Hub

	mu          sync.Mutex
	mode        subscribeMode // how bus.subscribe currently behaves
	subscribed  bool          // set true once bus.subscribe has succeeded
	subscribeAt int           // count of bus.subscribe calls served (any mode)
}

// setMode changes how bus.subscribe behaves for subsequent calls. Used to flip
// a degraded backend (subscribeErr) into a healthy one (subscribeOK).
func (b *fakeBackend) setMode(mode subscribeMode) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mode = mode
}

// subscribedOK reports whether bus.subscribe has succeeded at least once.
func (b *fakeBackend) subscribedOK() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.subscribed
}

// subscribeCount returns how many bus.subscribe calls have been served.
func (b *fakeBackend) subscribeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.subscribeAt
}

// broadcast publishes ev on the fake backend's hub, fanning it out to every
// registered peerSink (i.e. the Manager, once subscribed).
func (b *fakeBackend) broadcast(ev bus.Event) { b.hub.Broadcast(ev) }

// newFakeBackend wires sConn to an rpc.Peer serving canned methods and returns
// it. The peer's Serve runs until stop() is called. mode selects how
// bus.subscribe behaves.
func newFakeBackend(t *testing.T, hostID string, sConn net.Conn, mode subscribeMode) *fakeBackend {
	t.Helper()
	b := &fakeBackend{hostID: hostID, hub: bus.NewHub(), mode: mode}
	peer := rpc.NewPeer(sConn,
		rpc.WithHandler("daemon.status", func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]any{
				"hostId":        hostID,
				"version":       "test",
				"uptimeSeconds": 1,
				"pid":           1,
				"startedAt":     "2026-01-01T00:00:00Z",
			}, nil
		}),
		rpc.WithHandler("project.list", func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]any{"projects": []any{}}, nil
		}),
		rpc.WithHandler("bus.subscribe", func(ctx context.Context, _ json.RawMessage) (any, error) {
			b.mu.Lock()
			b.subscribeAt++
			cur := b.mode
			b.mu.Unlock()
			switch cur {
			case subscribeErr:
				return nil, &rpc.Error{Code: rpc.InternalError, Message: "subscribe failed"}
			case subscribeHang:
				// Never reply within the caller's timeout; honor cancellation so
				// the handler goroutine doesn't leak past the test.
				<-ctx.Done()
				return nil, ctx.Err()
			default:
				b.mu.Lock()
				b.subscribed = true
				b.mu.Unlock()
				b.hub.Subscribe(&peerSink{peer: b.peer})
				return map[string]string{"status": "subscribed"}, nil
			}
		}),
	)
	b.peer = peer
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	go func() { _ = peer.Serve(ctx) }()
	return b
}

// fakeFleet is a registry of fake backends keyed by URI, plus a set of URIs
// that should fail to dial. It supplies a DialFunc to the Manager.
type fakeFleet struct {
	mu       sync.Mutex
	hosts    map[string]string        // uri -> hostID for reachable backends
	unreach  map[string]bool          // uri -> dial should fail
	modes    map[string]subscribeMode // uri -> bus.subscribe behavior (default OK)
	backends []*fakeBackend
}

func newFakeFleet() *fakeFleet {
	return &fakeFleet{
		hosts:   map[string]string{},
		unreach: map[string]bool{},
		modes:   map[string]subscribeMode{},
	}
}

func (f *fakeFleet) addHost(uri, hostID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hosts[uri] = hostID
	delete(f.unreach, uri)
}

// setSubscribeMode sets how the next-dialed backend for uri answers
// bus.subscribe. The default (unset) is subscribeOK.
func (f *fakeFleet) setSubscribeMode(uri string, mode subscribeMode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modes[uri] = mode
}

func (f *fakeFleet) setUnreachable(uri string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unreach[uri] = true
}

// lastBackend returns the most recently dialed fake backend (or nil). Useful for
// broadcasting test events and flipping subscribe modes.
func (f *fakeFleet) lastBackend() *fakeBackend {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.backends) == 0 {
		return nil
	}
	return f.backends[len(f.backends)-1]
}

// dial returns the client end of a fresh pipe wired to a fake backend for the
// URI, or an error if the URI is marked unreachable / unknown.
func (f *fakeFleet) dial(t *testing.T) DialFunc {
	return func(_ context.Context, uri string) (net.Conn, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.unreach[uri] {
			return nil, errors.New("connection refused")
		}
		hostID, ok := f.hosts[uri]
		if !ok {
			return nil, errors.New("no such backend: " + uri)
		}
		sConn, cConn := net.Pipe()
		f.backends = append(f.backends, newFakeBackend(t, hostID, sConn, f.modes[uri]))
		return cConn, nil
	}
}

func (f *fakeFleet) stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.backends {
		b.cancel()
	}
}

// disconnectLast tears down the most recently dialed fake backend, simulating
// a daemon restart / transport drop. Cancelling its Serve ctx closes the
// net.Pipe server end, so the manager's client peer sees EOF and its Serve
// goroutine returns.
func (f *fakeFleet) disconnectLast() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.backends) > 0 {
		f.backends[len(f.backends)-1].cancel()
	}
}

// writeBackends writes a backends.json with the given URIs at path.
func writeBackends(t *testing.T, path string, uris ...string) {
	t.Helper()
	cfg := config.BackendsConfig{}
	for _, u := range uris {
		cfg.Backends = append(cfg.Backends, config.Backend{URI: u})
	}
	if err := config.SaveBackends(path, cfg); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}
}

// callOK asserts that a method call through peer succeeds.
func callOK(t *testing.T, peer *rpc.Peer, method string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := peer.Call(ctx, method, nil); err != nil {
		t.Fatalf("Call %s: %v", method, err)
	}
}

// waitConnected polls Resolve(host) until it returns a peer or the deadline
// passes. Returns the peer or fails the test.
func waitResolve(t *testing.T, m *Manager, host string) *rpc.Peer {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		peer, err := m.Resolve(host)
		if err == nil {
			return peer
		}
		if time.Now().After(deadline) {
			t.Fatalf("Resolve(%q) did not succeed in time: %v", host, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitResolveFails polls Resolve(host) until it returns an error or the
// deadline passes, asserting the backend has been marked unhealthy.
func waitResolveFails(t *testing.T, m *Manager, host string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := m.Resolve(host); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Resolve(%q) still succeeds; expected it to fail after disconnect", host)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitFor polls pred until it returns true or the deadline passes, failing the
// test with msg on timeout. Bounded and observable — never poll-to-settle.
func waitFor(t *testing.T, msg string, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if pred() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitFor timed out: %s", msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// relayDegradedFor returns the RelayDegraded health flag for hostID, or false if
// no such backend is in Health().
func relayDegradedFor(m *Manager, hostID string) bool {
	for _, h := range m.Health() {
		if h.HostID == hostID {
			return h.RelayDegraded
		}
	}
	return false
}

// TestConnectedPeers verifies ConnectedPeers returns only currently-connected
// backends, each carrying its learned host id and a live peer, sorted by host id
// — and excludes a backend that never connected (unreachable/pending).
func TestConnectedPeers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uriA = "unix:///fake/a.sock"
	const uriB = "unix:///fake/b.sock"
	const uriC = "unix:///fake/c.sock"
	writeBackends(t, path, uriA, uriB, uriC)

	fleet := newFakeFleet()
	fleet.addHost(uriA, "host-b") // intentionally out of URI order to prove host sort
	fleet.addHost(uriB, "host-a")
	fleet.setUnreachable(uriC) // never connects -> pending/errored, must be excluded
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	m.Reconcile(ctx)
	waitResolve(t, m, "host-a")
	waitResolve(t, m, "host-b")

	peers := m.ConnectedPeers()
	if len(peers) != 2 {
		t.Fatalf("ConnectedPeers returned %d, want 2 (errored backend excluded): %+v", len(peers), peers)
	}
	// Sorted by host id for stable output.
	if peers[0].HostID != "host-a" || peers[1].HostID != "host-b" {
		t.Fatalf("hostIDs = [%q, %q], want [host-a, host-b]", peers[0].HostID, peers[1].HostID)
	}
	for _, p := range peers {
		if p.HostID == "" {
			t.Fatalf("connected peer has empty hostID: %+v", p)
		}
		if p.Peer == nil {
			t.Fatalf("peer for %q is nil", p.HostID)
		}
		callOK(t, p.Peer, "daemon.status")
	}

	// The unreachable URI is present in Health() as pending but absent from
	// ConnectedPeers.
	if len(m.Health()) != 3 {
		t.Fatalf("Health() = %d backends, want 3 (incl. the pending one)", len(m.Health()))
	}
}

// TestBackendReconnectsAfterDisconnect covers AC-7's liveness edge: when a
// connected backend's transport drops, the conn is marked unhealthy (Resolve
// fails) and a later Reconcile redials it — without restarting the Manager.
func TestBackendReconnectsAfterDisconnect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uri = "unix:///fake/a.sock"
	writeBackends(t, path, uri)

	fleet := newFakeFleet()
	fleet.addHost(uri, "host-a")
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	m.Reconcile(ctx)
	waitResolve(t, m, "host-a")

	// Backend drops: the Serve goroutine must mark the conn unhealthy so it is
	// no longer resolvable and no dead peer is handed out.
	fleet.disconnectLast()
	waitResolveFails(t, m, "host-a")

	// Next tick redials and reconnects without a restart.
	m.Reconcile(ctx)
	peer := waitResolve(t, m, "host-a")
	callOK(t, peer, "daemon.status")
}

func TestReconcileDialsAndLearnsHostID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uri = "unix:///fake/a.sock"
	writeBackends(t, path, uri)

	fleet := newFakeFleet()
	fleet.addHost(uri, "host-a")
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	m.Reconcile(ctx)

	peer := waitResolve(t, m, "")
	// Calls through the proxied peer succeed.
	callOK(t, peer, "daemon.status")
	callOK(t, peer, "project.list")

	// Health reflects the connected backend.
	h := m.Health()
	if len(h) != 1 || !h[0].Connected || h[0].HostID != "host-a" {
		t.Fatalf("unexpected health: %+v", h)
	}
}

func TestLiveAddBackend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uriA = "unix:///fake/a.sock"
	const uriB = "unix:///fake/b.sock"
	writeBackends(t, path, uriA)

	fleet := newFakeFleet()
	fleet.addHost(uriA, "host-a")
	fleet.addHost(uriB, "host-b")
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	m.Reconcile(ctx)
	waitResolve(t, m, "host-a")

	// Live add: rewrite config with a second backend, reconcile again — no
	// Manager restart.
	writeBackends(t, path, uriA, uriB)
	m.Reconcile(ctx)

	peerB := waitResolve(t, m, "host-b")
	callOK(t, peerB, "daemon.status")
	// host-a is still connected.
	if _, err := m.Resolve("host-a"); err != nil {
		t.Fatalf("host-a should still resolve: %v", err)
	}
}

func TestLiveRemoveBackend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uriA = "unix:///fake/a.sock"
	const uriB = "unix:///fake/b.sock"
	writeBackends(t, path, uriA, uriB)

	fleet := newFakeFleet()
	fleet.addHost(uriA, "host-a")
	fleet.addHost(uriB, "host-b")
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	m.Reconcile(ctx)
	waitResolve(t, m, "host-a")
	waitResolve(t, m, "host-b")

	// Live remove host-b.
	writeBackends(t, path, uriA)
	m.Reconcile(ctx)

	if _, err := m.Resolve("host-b"); !errors.Is(err, ErrUnknownHost) {
		t.Fatalf("host-b should be gone, got err=%v", err)
	}
	// host-a remains and is now the sole backend.
	if _, err := m.Resolve(""); err != nil {
		t.Fatalf("host-a should resolve as sole backend: %v", err)
	}
	h := m.Health()
	if len(h) != 1 || h[0].HostID != "host-a" {
		t.Fatalf("unexpected health after remove: %+v", h)
	}
}

func TestUnreachableBackendRetries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uri = "unix:///fake/a.sock"
	writeBackends(t, path, uri)

	fleet := newFakeFleet()
	fleet.setUnreachable(uri)
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	// First reconcile: dial fails, no crash, no connection.
	m.Reconcile(ctx)
	if _, err := m.Resolve(""); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("expected ErrNoBackend, got %v", err)
	}
	h := m.Health()
	if len(h) != 1 || h[0].Connected || h[0].LastErr == "" {
		t.Fatalf("expected pending backend with lastErr, got %+v", h)
	}

	// Backend becomes reachable; a later reconcile connects it (retry).
	fleet.addHost(uri, "host-a")
	m.Reconcile(ctx)

	peer := waitResolve(t, m, "host-a")
	callOK(t, peer, "daemon.status")
	h = m.Health()
	if len(h) != 1 || !h[0].Connected || h[0].LastErr != "" {
		t.Fatalf("expected connected backend after retry, got %+v", h)
	}
}

func TestResolveMatrix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uriA = "unix:///fake/a.sock"
	const uriB = "unix:///fake/b.sock"

	fleet := newFakeFleet()
	fleet.addHost(uriA, "host-a")
	fleet.addHost(uriB, "host-b")
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	// None connected.
	if _, err := m.Resolve(""); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("empty+none: want ErrNoBackend, got %v", err)
	}

	// One connected: empty resolves to it.
	writeBackends(t, path, uriA)
	m.Reconcile(ctx)
	waitResolve(t, m, "host-a")
	if _, err := m.Resolve(""); err != nil {
		t.Fatalf("empty+one: want peer, got %v", err)
	}

	// Explicit unknown host.
	if _, err := m.Resolve("host-zzz"); !errors.Is(err, ErrUnknownHost) {
		t.Fatalf("explicit unknown: want ErrUnknownHost, got %v", err)
	}

	// Multiple connected: empty is ambiguous.
	writeBackends(t, path, uriA, uriB)
	m.Reconcile(ctx)
	waitResolve(t, m, "host-b")
	if _, err := m.Resolve(""); !errors.Is(err, ErrAmbiguousHost) {
		t.Fatalf("empty+multi: want ErrAmbiguousHost, got %v", err)
	}
	// Explicit host still works with multiple connected.
	if _, err := m.Resolve("host-a"); err != nil {
		t.Fatalf("explicit+multi: want peer, got %v", err)
	}
}

// TestSubscribeOnConnect (AC-1): after the daemon.status handshake, connect
// calls bus.subscribe so the backend records an active subscription, and the
// conn is healthy (RelayDegraded false).
func TestSubscribeOnConnect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uri = "unix:///fake/a.sock"
	writeBackends(t, path, uri)

	fleet := newFakeFleet()
	fleet.addHost(uri, "host-a")
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	m.Reconcile(ctx)
	waitResolve(t, m, "host-a")

	waitFor(t, "backend never recorded bus.subscribe", func() bool {
		b := fleet.lastBackend()
		return b != nil && b.subscribedOK()
	})
	if relayDegradedFor(m, "host-a") {
		t.Fatalf("host-a should not be relay-degraded after a successful subscribe")
	}
}

// TestResubscribeAfterRedial (AC-1): when a connected backend drops and a later
// Reconcile redials it, connect() re-subscribes the fresh peer.
func TestResubscribeAfterRedial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uri = "unix:///fake/a.sock"
	writeBackends(t, path, uri)

	fleet := newFakeFleet()
	fleet.addHost(uri, "host-a")
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	m.Reconcile(ctx)
	waitResolve(t, m, "host-a")
	waitFor(t, "initial subscribe never recorded", func() bool {
		b := fleet.lastBackend()
		return b != nil && b.subscribedOK()
	})
	first := fleet.lastBackend()

	// Drop the backend; the conn is marked unhealthy and a later Reconcile
	// redials, which must produce a NEW backend that also subscribes.
	fleet.disconnectLast()
	waitResolveFails(t, m, "host-a")
	m.Reconcile(ctx)
	waitResolve(t, m, "host-a")

	waitFor(t, "redialed backend never re-subscribed", func() bool {
		b := fleet.lastBackend()
		return b != nil && b != first && b.subscribedOK()
	})
}

// TestSubscribeRetryOnDegradedConn (AC-6): a conn that is connected but
// relay-degraded gets a bounded re-attempt in Reconcile that clears the flag on
// success — WITHOUT redialing (same backend, no new connection).
func TestSubscribeRetryOnDegradedConn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uri = "unix:///fake/a.sock"
	writeBackends(t, path, uri)

	fleet := newFakeFleet()
	fleet.addHost(uri, "host-a")
	fleet.setSubscribeMode(uri, subscribeErr)
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	// First connect: daemon.status succeeds, bus.subscribe fails -> connected
	// but relay-degraded.
	m.Reconcile(ctx)
	waitResolve(t, m, "host-a")
	waitFor(t, "conn should be relay-degraded after subscribe error", func() bool {
		return relayDegradedFor(m, "host-a")
	})
	b := fleet.lastBackend()

	// Flip the SAME backend to healthy; a later Reconcile must retry bus.subscribe
	// on the connected-degraded conn and clear the flag without redialing.
	b.setMode(subscribeOK)
	m.Reconcile(ctx)

	waitFor(t, "relayDegraded never cleared by reconcile retry", func() bool {
		return !relayDegradedFor(m, "host-a")
	})
	// No redial happened: still the same backend, and it served >1 subscribe.
	if fleet.lastBackend() != b {
		t.Fatalf("expected no redial; backend changed")
	}
	if b.subscribeCount() < 2 {
		t.Fatalf("expected a retried bus.subscribe (>=2 calls), got %d", b.subscribeCount())
	}
}

// TestOnEventReachesSink (AC-1/AC-2 seam): a capturing sink set via SetEventSink
// receives the parsed bus.Event with fields intact when the backend broadcasts.
func TestOnEventReachesSink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uri = "unix:///fake/a.sock"
	writeBackends(t, path, uri)

	fleet := newFakeFleet()
	fleet.addHost(uri, "host-a")
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	got := make(chan bus.Event, 1)
	m.SetEventSink(func(ev bus.Event) {
		select {
		case got <- ev:
		default:
		}
	})

	ctx := t.Context()
	m.Reconcile(ctx)
	waitResolve(t, m, "host-a")
	waitFor(t, "backend never subscribed", func() bool {
		b := fleet.lastBackend()
		return b != nil && b.subscribedOK()
	})

	want := bus.Event{
		SpecVersion: bus.SpecVersion,
		Type:        "doc.changed",
		Source:      "auto-watch",
		ID:          "evt-123",
		Time:        "2026-01-01T00:00:00Z",
		Host:        "host-a",
		Project:     "proj-1",
		Session:     "sess-1",
	}
	fleet.lastBackend().broadcast(want)

	select {
	case ev := <-got:
		if ev.Type != want.Type || ev.ID != want.ID || ev.Host != want.Host ||
			ev.Project != want.Project || ev.Session != want.Session || ev.Source != want.Source {
			t.Fatalf("event fields not intact: got %+v want %+v", ev, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relayed event never reached the sink")
	}
}

// TestSubscribeFailureDegradesRelay (AC-6): when bus.subscribe errors, the conn
// is marked relay-degraded yet Resolve still returns the peer and proxied RPCs
// keep working.
func TestSubscribeFailureDegradesRelay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uri = "unix:///fake/a.sock"
	writeBackends(t, path, uri)

	fleet := newFakeFleet()
	fleet.addHost(uri, "host-a")
	fleet.setSubscribeMode(uri, subscribeErr)
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	m.Reconcile(ctx)
	peer := waitResolve(t, m, "host-a")
	// RPC proxying intact despite the relay being degraded.
	callOK(t, peer, "daemon.status")
	callOK(t, peer, "project.list")

	waitFor(t, "conn should be relay-degraded after subscribe error", func() bool {
		return relayDegradedFor(m, "host-a")
	})
	h := m.Health()
	if len(h) != 1 || !h[0].Connected || !h[0].RelayDegraded {
		t.Fatalf("expected connected+relay-degraded health, got %+v", h)
	}
}

// TestNonResponsiveSubscribeDoesNotWedgeReconcile (AC-6): a backend that never
// replies to bus.subscribe is bounded by subscribeTimeout, so Reconcile still
// completes (and the conn is published connected + relay-degraded).
func TestNonResponsiveSubscribeDoesNotWedgeReconcile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backends.json")
	const uri = "unix:///fake/a.sock"
	writeBackends(t, path, uri)

	fleet := newFakeFleet()
	fleet.addHost(uri, "host-a")
	fleet.setSubscribeMode(uri, subscribeHang)
	defer fleet.stop()

	m := NewManager(path, fleet.dial(t), 0)
	ctx := t.Context()

	done := make(chan struct{})
	go func() {
		m.Reconcile(ctx)
		close(done)
	}()

	// Reconcile must return within subscribeTimeout (plus slack), proving the
	// non-responsive subscribe cannot wedge the wait group.
	select {
	case <-done:
	case <-time.After(subscribeTimeout + 3*time.Second):
		t.Fatal("Reconcile wedged on a non-responsive bus.subscribe")
	}

	// The conn is still published as connected (RPC proxying usable), just
	// relay-degraded.
	if _, err := m.Resolve("host-a"); err != nil {
		t.Fatalf("host-a should resolve despite degraded relay: %v", err)
	}
	if !relayDegradedFor(m, "host-a") {
		t.Fatalf("expected relay-degraded after subscribe timeout")
	}
}
