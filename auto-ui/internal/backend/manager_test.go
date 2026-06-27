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

	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-ui/internal/config"
)

// fakeBackend is an in-process autowatch backend: an rpc.Peer serving canned
// daemon.status (returning hostID) and project.list over the server end of a
// net.Pipe.
type fakeBackend struct {
	hostID string
	peer   *rpc.Peer
	cancel context.CancelFunc
}

// newFakeBackend wires sConn to an rpc.Peer serving canned methods and returns
// it. The peer's Serve runs until stop() is called.
func newFakeBackend(t *testing.T, hostID string, sConn net.Conn) *fakeBackend {
	t.Helper()
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
	)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = peer.Serve(ctx) }()
	return &fakeBackend{hostID: hostID, peer: peer, cancel: cancel}
}

// fakeFleet is a registry of fake backends keyed by URI, plus a set of URIs
// that should fail to dial. It supplies a DialFunc to the Manager.
type fakeFleet struct {
	mu       sync.Mutex
	hosts    map[string]string // uri -> hostID for reachable backends
	unreach  map[string]bool   // uri -> dial should fail
	backends []*fakeBackend
}

func newFakeFleet() *fakeFleet {
	return &fakeFleet{hosts: map[string]string{}, unreach: map[string]bool{}}
}

func (f *fakeFleet) addHost(uri, hostID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hosts[uri] = hostID
	delete(f.unreach, uri)
}

func (f *fakeFleet) setUnreachable(uri string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unreach[uri] = true
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
		f.backends = append(f.backends, newFakeBackend(t, hostID, sConn))
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
