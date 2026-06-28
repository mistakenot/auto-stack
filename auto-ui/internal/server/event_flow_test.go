package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-ui/internal/backend"
	uiconfig "github.com/mistakenot/auto-ui/internal/config"
	"github.com/mistakenot/auto-ui/internal/server"
)

// relaySink mirrors auto-watch/internal/rpcserver/subscribe.go: it pushes a fake
// backend hub's events to the connected Manager peer as JSON-RPC notifications.
type relaySink struct{ peer *rpc.Peer }

func (s *relaySink) Deliver(ev bus.Event) { _ = s.peer.Notify(ev.Type, ev) }

// relayBackend is an in-process autowatch backend: an rpc.Peer serving canned
// daemon.status / project.list plus a bus.subscribe handler that registers a
// relaySink on its hub, so events broadcast on the hub relay back to the
// Manager (and from there through the gate into the UI's hub).
type relayBackend struct {
	hostID string
	peer   *rpc.Peer
	hub    *bus.Hub

	mu         sync.Mutex
	subscribed bool
}

// broadcast publishes ev on the backend's hub, fanning it out to the registered
// relaySink (the Manager, once subscribed).
func (b *relayBackend) broadcast(ev bus.Event) { b.hub.Broadcast(ev) }

func (b *relayBackend) subscribedOK() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.subscribed
}

// startRelayBackend wires sConn to an rpc.Peer serving the canned methods and
// runs its Serve until test cleanup.
func startRelayBackend(t *testing.T, hostID string, sConn net.Conn) *relayBackend {
	t.Helper()
	b := &relayBackend{hostID: hostID, hub: bus.NewHub()}
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
		rpc.WithHandler("bus.subscribe", func(_ context.Context, _ json.RawMessage) (any, error) {
			b.hub.Subscribe(&relaySink{peer: b.peer})
			b.mu.Lock()
			b.subscribed = true
			b.mu.Unlock()
			return map[string]string{"status": "subscribed"}, nil
		}),
	)
	b.peer = peer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = peer.Serve(ctx) }()
	return b
}

// relayFleet maps backend URIs to host ids and records the fake backend dialled
// for each, supplying a DialFunc to the Manager.
type relayFleet struct {
	mu       sync.Mutex
	hosts    map[string]string // uri -> hostID
	backends []*relayBackend
}

// backendFor returns the (latest) fake backend serving hostID, or nil.
func (f *relayFleet) backendFor(hostID string) *relayBackend {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range slices.Backward(f.backends) {
		if b.hostID == hostID {
			return b
		}
	}
	return nil
}

func (f *relayFleet) dial(t *testing.T) backend.DialFunc {
	return func(_ context.Context, uri string) (net.Conn, error) {
		f.mu.Lock()
		hostID, ok := f.hosts[uri]
		f.mu.Unlock()
		if !ok {
			return nil, errors.New("relayFleet: unknown uri: " + uri)
		}
		sConn, cConn := net.Pipe()
		b := startRelayBackend(t, hostID, sConn)
		f.mu.Lock()
		f.backends = append(f.backends, b)
		f.mu.Unlock()
		return cConn, nil
	}
}

// newRelayServer stands up a real server.New wired to a backend.Manager whose
// dial resolves to in-process relayBackends for the given uri->hostID map. It
// reconciles, waits until every backend is connected and subscribed, and returns
// the running server plus the fleet (for broadcasting test events).
func newRelayServer(t *testing.T, hosts map[string]string, opts ...server.Option) (*httptest.Server, *relayFleet) {
	t.Helper()

	fleet := &relayFleet{hosts: hosts}

	path := filepath.Join(t.TempDir(), "backends.json")
	cfg := uiconfig.BackendsConfig{}
	for uri := range hosts {
		cfg.Backends = append(cfg.Backends, uiconfig.Backend{URI: uri})
	}
	if err := uiconfig.SaveBackends(path, cfg); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}

	mgr := backend.NewManager(path, fleet.dial(t), 0)

	// server.New installs the relay sink (hub.Broadcast) on the manager; build it
	// before reconciling so the sink is set before any event could relay. Extra
	// opts (e.g. WithDebug) are appended after the manager option.
	srvOpts := append([]server.Option{server.WithBackendManager(mgr)}, opts...)
	srv := httptest.NewServer(server.New(newTestFS(), "test", srvOpts...))
	t.Cleanup(srv.Close)

	mgr.Reconcile(context.Background())

	for _, hostID := range hosts {
		waitResolveHost(t, mgr, hostID)
		hostID := hostID
		waitTrue(t, "backend "+hostID+" never subscribed", func() bool {
			b := fleet.backendFor(hostID)
			return b != nil && b.subscribedOK()
		})
	}

	return srv, fleet
}

// waitResolveHost polls Resolve(host) until it succeeds or the deadline passes.
func waitResolveHost(t *testing.T, m *backend.Manager, host string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := m.Resolve(host); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Resolve(%q) did not succeed in time", host)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitTrue polls pred until true or the deadline passes — bounded and
// observable, never poll-to-settle.
func waitTrue(t *testing.T, msg string, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if pred() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitTrue timed out: %s", msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// relayEvent builds a minimal valid bus.Event for relay tests.
func relayEvent(typ, id, host string) bus.Event {
	return bus.Event{
		SpecVersion: bus.SpecVersion,
		Type:        typ,
		Source:      "auto-watch",
		ID:          id,
		Time:        "2026-01-01T00:00:00Z",
		Host:        host,
	}
}

// paramsOf returns the params object of a JSON-RPC notification message.
func paramsOf(m map[string]any) map[string]any {
	p, _ := m["params"].(map[string]any)
	return p
}

// idOf returns params.id of a notification, or "" if absent.
func idOf(m map[string]any) string {
	s, _ := paramsOf(m)["id"].(string)
	return s
}

// waitSubscribed gives the server goroutine time to reach hub.Subscribe after
// the WebSocket handshake completes. hub.Subscribe is the first thing the
// handler does after Accept, but goroutine scheduling means the client's Dial
// can return before it runs.
func waitSubscribed() {
	time.Sleep(50 * time.Millisecond)
}

// TestRelayFidelity (AC-2): a backend-broadcast event reaches the WS client as a
// JSON-RPC notification whose params is the event with every envelope field
// intact, including Host.
func TestRelayFidelity(t *testing.T) {
	const uri = "unix:///fake/a.sock"
	srv, fleet := newRelayServer(t, map[string]string{uri: "host-a"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")
	waitSubscribed()

	want := relayEvent("doc.changed", "evt-ac2", "host-a")
	want.Project = "proj-1"
	want.Session = "sess-1"
	want.Data = json.RawMessage(`{"path":"docs/x.md"}`)
	fleet.backendFor("host-a").broadcast(want)

	msg := readUntil(ctx, t, c, func(m map[string]any) bool { return idOf(m) == "evt-ac2" })
	p := paramsOf(msg)
	if p["type"] != "doc.changed" || p["source"] != "auto-watch" ||
		p["host"] != "host-a" || p["project"] != "proj-1" || p["session"] != "sess-1" ||
		p["specversion"] != bus.SpecVersion || p["time"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("envelope fields not intact: %v", p)
	}
	data, ok := p["data"].(map[string]any)
	if !ok || data["path"] != "docs/x.md" {
		t.Fatalf("data payload not intact: %v", p["data"])
	}
}

// TestRelayMultiBackendMerge (AC-3): two backends with distinct hostIds each
// broadcast; one WS client receives both, each tagged with its originating host.
func TestRelayMultiBackendMerge(t *testing.T) {
	const uriA = "unix:///fake/a.sock"
	const uriB = "unix:///fake/b.sock"
	srv, fleet := newRelayServer(t, map[string]string{uriA: "host-a", uriB: "host-b"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")
	waitSubscribed()

	fleet.backendFor("host-a").broadcast(relayEvent("doc.changed", "evt-a", "host-a"))
	fleet.backendFor("host-b").broadcast(relayEvent("doc.changed", "evt-b", "host-b"))

	// The two events come from independent backend connections, so their arrival
	// order at the client is nondeterministic — collect by id and check each
	// carries its originating host.
	hostByID := map[string]string{}
	readUntil(ctx, t, c, func(m map[string]any) bool {
		if id := idOf(m); id == "evt-a" || id == "evt-b" {
			hostByID[id], _ = paramsOf(m)["host"].(string)
		}
		return hostByID["evt-a"] != "" && hostByID["evt-b"] != ""
	})
	if hostByID["evt-a"] != "host-a" {
		t.Fatalf("evt-a host = %q, want host-a", hostByID["evt-a"])
	}
	if hostByID["evt-b"] != "host-b" {
		t.Fatalf("evt-b host = %q, want host-b", hostByID["evt-b"])
	}
}

// TestRelayNoReDerivation (AC-5): a backend relays a raw agent.tool.post AND its
// already-derived doc.changed; the client sees exactly those — auto-ui does not
// re-derive a second doc.changed off the relayed raw event.
func TestRelayNoReDerivation(t *testing.T) {
	const uri = "unix:///fake/a.sock"
	srv, fleet := newRelayServer(t, map[string]string{uri: "host-a"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")
	waitSubscribed()

	raw := relayEvent("agent.tool.post", "raw-1", "host-a")
	raw.Project = "proj-1"
	derived := relayEvent("doc.changed", "derived-1", "host-a")
	derived.Project = "proj-1"

	b := fleet.backendFor("host-a")
	b.broadcast(raw)
	b.broadcast(derived)
	b.broadcast(relayEvent("relay.test", "sentinel-1", "host-a"))

	rawCount, derivedCount := 0, 0
	readUntil(ctx, t, c, func(m map[string]any) bool {
		switch idOf(m) {
		case "raw-1":
			rawCount++
		case "derived-1":
			derivedCount++
		case "sentinel-1":
			return true
		}
		return false
	})
	if rawCount != 1 {
		t.Fatalf("agent.tool.post delivered %d times, want 1", rawCount)
	}
	if derivedCount != 1 {
		t.Fatalf("doc.changed delivered %d times, want exactly 1 (no re-derivation)", derivedCount)
	}
}
