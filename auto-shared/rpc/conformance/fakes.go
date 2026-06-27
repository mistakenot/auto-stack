package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/transport/tcp"
	"github.com/mistakenot/auto-shared/transport/unix"
)

// testTimeout bounds all blocking operations in fixtures.
const testTimeout = 5 * time.Second

// ---------------------------------------------------------------------------
// FakeServer: canned handlers + dispatch counting
// ---------------------------------------------------------------------------

// FakeServer registers canned handlers on a *rpc.Peer and tracks per-method
// dispatch counts atomically. It implements Observations.
type FakeServer struct {
	peer   *rpc.Peer
	counts sync.Map // method -> *atomic.Int64
}

// NewFakeServer creates a FakeServer bound to conn. It registers the standard
// canned handlers ("echo" and "push") used by conformance scenarios.
//
// "echo" returns its params unchanged.
// "push" sends a notification back to the caller with method "server.pushed"
// and the same params, then returns "ok".
func NewFakeServer(conn net.Conn) *FakeServer {
	fs := &FakeServer{}

	fs.peer = rpc.NewPeer(conn,
		rpc.WithHandler("echo", fs.counting("echo", func(_ context.Context, params json.RawMessage) (any, error) {
			return params, nil
		})),
		rpc.WithHandler("push", fs.counting("push", func(_ context.Context, params json.RawMessage) (any, error) {
			// Send a server-push notification back to the caller.
			_ = fs.peer.Notify("server.pushed", params)
			return "ok", nil
		})),
		rpc.WithBufferSize(64),
	)

	return fs
}

// counting wraps a handler to increment the dispatch count for the method.
func (fs *FakeServer) counting(method string, h rpc.Handler) rpc.Handler {
	// Pre-create the counter.
	counter := &atomic.Int64{}
	fs.counts.Store(method, counter)
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		counter.Add(1)
		return h(ctx, params)
	}
}

// Peer returns the underlying *rpc.Peer (for wiring Serve).
func (fs *FakeServer) Peer() *rpc.Peer {
	return fs.peer
}

// DispatchCount returns the number of times the handler for method was called.
func (fs *FakeServer) DispatchCount(method string) int {
	v, ok := fs.counts.Load(method)
	if !ok {
		return 0
	}
	return int(v.(*atomic.Int64).Load())
}

// ---------------------------------------------------------------------------
// PeerClient: thin adapter wrapping *rpc.Peer to satisfy RPCClient
// ---------------------------------------------------------------------------

// PeerClient wraps a *rpc.Peer to satisfy the RPCClient interface. It exposes
// the OnNotify stream as a buffered channel via Notifications().
type PeerClient struct {
	peer    *rpc.Peer
	notifCh chan rpc.Request
}

// NewPeerClient creates a PeerClient wrapping a new *rpc.Peer bound to conn.
// Inbound notifications are pushed into a buffered channel (capacity 64).
func NewPeerClient(conn net.Conn) *PeerClient {
	ch := make(chan rpc.Request, 64)
	p := rpc.NewPeer(conn,
		rpc.WithOnNotify(func(req rpc.Request) {
			select {
			case ch <- req:
			default:
				// Drop on full — at-most-once, never block.
			}
		}),
		rpc.WithBufferSize(64),
	)
	return &PeerClient{peer: p, notifCh: ch}
}

// Peer returns the underlying *rpc.Peer (for wiring Serve).
func (pc *PeerClient) Peer() *rpc.Peer {
	return pc.peer
}

// Call delegates to the underlying Peer.
func (pc *PeerClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return pc.peer.Call(ctx, method, params)
}

// Notify delegates to the underlying Peer.
func (pc *PeerClient) Notify(method string, params any) error {
	return pc.peer.Notify(method, params)
}

// Notifications returns the channel receiving inbound notifications.
func (pc *PeerClient) Notifications() <-chan rpc.Request {
	return pc.notifCh
}

// ---------------------------------------------------------------------------
// CountingConn: wraps net.Conn, counts reads/writes atomically
// ---------------------------------------------------------------------------

// CountingConn wraps a net.Conn and counts Read/Write calls atomically.
type CountingConn struct {
	net.Conn
	Reads  atomic.Int64
	Writes atomic.Int64
}

// Read delegates to the underlying Conn and increments the read counter.
func (c *CountingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.Reads.Add(1)
	}
	return n, err
}

// Write delegates to the underlying Conn and increments the write counter.
func (c *CountingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		c.Writes.Add(1)
	}
	return n, err
}

// ---------------------------------------------------------------------------
// CountingListener: wraps a listener, counts accepts atomically
// ---------------------------------------------------------------------------

// acceptListener is the interface that both unix.Listener and tcp.Listener
// satisfy (structurally — they don't import the transport parent package).
type acceptListener interface {
	Accept() (net.Conn, error)
	Addr() net.Addr
	Close() error
}

// CountingListener wraps a listener and counts Accept calls atomically.
type CountingListener struct {
	inner   acceptListener
	Accepts atomic.Int64
}

// Accept delegates to the inner listener and increments the counter.
func (cl *CountingListener) Accept() (net.Conn, error) {
	conn, err := cl.inner.Accept()
	if err == nil {
		cl.Accepts.Add(1)
	}
	return conn, err
}

// Addr delegates to the inner listener.
func (cl *CountingListener) Addr() net.Addr {
	return cl.inner.Addr()
}

// Close delegates to the inner listener.
func (cl *CountingListener) Close() error {
	return cl.inner.Close()
}

// ---------------------------------------------------------------------------
// fakeFixture: Fixture implementation backed by FakeServer + PeerClient
// ---------------------------------------------------------------------------

type fakeFixture struct {
	client     *PeerClient
	server     *FakeServer
	cancel     context.CancelFunc
	serveErrCh chan error
	clientDone chan error
}

func (f *fakeFixture) Client() RPCClient { return f.client }
func (f *fakeFixture) Obs() Observations { return f.server }
func (f *fakeFixture) Close() error {
	f.cancel()
	<-f.serveErrCh
	<-f.clientDone
	return nil
}

// ---------------------------------------------------------------------------
// faultConn: net-new fault-injecting net.Conn wrapper
// ---------------------------------------------------------------------------

// faultConn wraps a net.Conn to inject two controllable faults used by the
// conformance fault scenarios:
//
//   - Sever()/Close() drops the connection mid-stream (closes the underlying
//     conn), simulating a connection failure.
//   - Stall()/Release() make Write block then unblock, simulating a stalled
//     reader. Per decision D-3, a blocking write is required because OS socket
//     buffers on unix/tcp absorb writes, so "just don't read" cannot
//     deterministically fill the producer's bounded out channel.
//
// All controls are thread-safe. A stalled Write always unblocks on either
// Release() or Close(), so a faulted fixture can never wedge a test.
type faultConn struct {
	net.Conn

	mu      sync.Mutex
	stalled bool
	relCh   chan struct{} // closed by Release to unblock the current stall

	closed    chan struct{} // closed by Close to unblock any stall and stop writes
	closeOnce sync.Once
}

// newFaultConn wraps conn with fault-injection controls.
func newFaultConn(conn net.Conn) *faultConn {
	return &faultConn{Conn: conn, closed: make(chan struct{})}
}

// Stall makes subsequent Write calls block until Release or Close. Idempotent.
func (c *faultConn) Stall() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stalled {
		return
	}
	c.stalled = true
	c.relCh = make(chan struct{})
}

// Release unblocks a stalled Write and lets future writes proceed. Idempotent.
func (c *faultConn) Release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.stalled {
		return
	}
	c.stalled = false
	close(c.relCh)
	c.relCh = nil
}

// Write blocks while the conn is stalled, then delegates to the underlying
// conn. A stalled Write returns net.ErrClosed if the conn is closed while
// waiting, guaranteeing the write pump can never hang past Close.
func (c *faultConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	stalled := c.stalled
	relCh := c.relCh
	c.mu.Unlock()

	if stalled {
		select {
		case <-relCh:
			// released — fall through and write
		case <-c.closed:
			return 0, net.ErrClosed
		}
	}
	return c.Conn.Write(b)
}

// Sever closes the underlying connection mid-stream.
func (c *faultConn) Sever() { _ = c.Close() }

// Close closes the underlying conn and unblocks any stalled Write. Idempotent.
func (c *faultConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.Conn.Close()
	})
	return err
}

// ---------------------------------------------------------------------------
// FakeFixtures / FaultFixtures: per-transport fixture factories
// ---------------------------------------------------------------------------

// FakeFixtures returns 3 FixtureFactory values that build connected RPC
// environments over different transports:
//  1. net.Pipe (in-process, no OS resources)
//  2. unix domain socket (transport/unix)
//  3. tcp loopback (transport/tcp)
//
// Each factory sets up a FakeServer on the accept/server side and a
// PeerClient on the dial/client side, starts Serve in goroutines, and
// returns a Fixture that tears everything down on Close.
func FakeFixtures() []FixtureFactory {
	return []FixtureFactory{
		pipeFactory,
		unixFactory,
		tcpFactory,
	}
}

// FaultFixtures returns 3 FixtureFactory values mirroring FakeFixtures over the
// same transports (pipe, unix, tcp), but each wraps the client (producer) conn
// in a faultConn so fault scenarios can Sever / StallConsumer / ReleaseConsumer
// it. Every value returned also satisfies FaultFixture.
func FaultFixtures() []FixtureFactory {
	return []FixtureFactory{
		faultPipeFactory,
		faultUnixFactory,
		faultTCPFactory,
	}
}

// connectFunc returns a connected server/client conn pair plus a cleanup that
// releases transport-level resources (closes the listener; a no-op for pipe).
type connectFunc func(t testing.TB) (serverConn, clientConn net.Conn, cleanup func())

// pipeConnect wires an in-process net.Pipe pair.
func pipeConnect(_ testing.TB) (net.Conn, net.Conn, func()) {
	c1, c2 := net.Pipe()
	return c1, c2, func() {}
}

// unixConnect creates a unix socket listener+dialer pair.
func unixConnect(t testing.TB) (net.Conn, net.Conn, func()) {
	dir := t.(*testing.T).TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	ln, err := unix.Listen(sockPath)
	if err != nil {
		t.Fatalf("unix.Listen: %v", err)
	}

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	acceptCh := make(chan acceptResult, 1)
	go func() {
		conn, err := ln.Accept()
		acceptCh <- acceptResult{conn, err}
	}()

	d := unix.NewDialer(sockPath)
	dialCtx, dialCancel := context.WithTimeout(context.Background(), testTimeout)
	defer dialCancel()
	clientConn, err := d.Dial(dialCtx)
	if err != nil {
		_ = ln.Close()
		t.Fatalf("unix.Dial: %v", err)
	}

	ar := <-acceptCh
	if ar.err != nil {
		_ = clientConn.Close()
		_ = ln.Close()
		t.Fatalf("unix.Accept: %v", ar.err)
	}

	return ar.conn, clientConn, func() { _ = ln.Close() }
}

// tcpConnect creates a TCP listener+dialer pair on loopback.
func tcpConnect(t testing.TB) (net.Conn, net.Conn, func()) {
	ln, err := tcp.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcp.Listen: %v", err)
	}

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	acceptCh := make(chan acceptResult, 1)
	go func() {
		conn, err := ln.Accept()
		acceptCh <- acceptResult{conn, err}
	}()

	addr := ln.Addr().String()
	d := tcp.NewDialer(addr)
	dialCtx, dialCancel := context.WithTimeout(context.Background(), testTimeout)
	defer dialCancel()
	clientConn, err := d.Dial(dialCtx)
	if err != nil {
		_ = ln.Close()
		t.Fatalf("tcp.Dial %s: %v", addr, err)
	}

	ar := <-acceptCh
	if ar.err != nil {
		_ = clientConn.Close()
		_ = ln.Close()
		t.Fatalf("tcp.Accept: %v", ar.err)
	}

	return ar.conn, clientConn, func() { _ = ln.Close() }
}

// startFakeFixture wires a FakeServer on serverConn and a PeerClient on
// clientConn, starts both Serve loops, and returns a fakeFixture whose Close
// cancels the serve context, runs extraCleanup, and joins both goroutines.
func startFakeFixture(serverConn, clientConn net.Conn, extraCleanup func()) *fakeFixture {
	server := NewFakeServer(serverConn)
	client := NewPeerClient(clientConn)

	ctx, cancel := context.WithCancel(context.Background())

	sErr := make(chan error, 1)
	cErr := make(chan error, 1)

	go func() { sErr <- server.Peer().Serve(ctx) }()
	go func() { cErr <- client.Peer().Serve(ctx) }()

	return &fakeFixture{
		client: client,
		server: server,
		cancel: func() {
			cancel()
			extraCleanup()
		},
		serveErrCh: sErr,
		clientDone: cErr,
	}
}

func pipeFactory(t testing.TB) Fixture {
	s, c, cleanup := pipeConnect(t)
	return startFakeFixture(s, c, cleanup)
}

func unixFactory(t testing.TB) Fixture {
	s, c, cleanup := unixConnect(t)
	return startFakeFixture(s, c, cleanup)
}

func tcpFactory(t testing.TB) Fixture {
	s, c, cleanup := tcpConnect(t)
	return startFakeFixture(s, c, cleanup)
}

// ---------------------------------------------------------------------------
// faultFixture: FaultFixture backed by a faultConn on the client side
// ---------------------------------------------------------------------------

// faultFixture extends fakeFixture with control over a faultConn wrapping the
// client (producer) conn. The client is the side scenarios drive (it issues the
// in-flight Call to drop and the notifications to overflow), so faulting its
// conn is what severs an in-flight Call and what stalls the producer's writes.
type faultFixture struct {
	*fakeFixture
	fault *faultConn
}

func (f *faultFixture) Sever()           { f.fault.Sever() }
func (f *faultFixture) StallConsumer()   { f.fault.Stall() }
func (f *faultFixture) ReleaseConsumer() { f.fault.Release() }

// newFaultFixture wraps the client conn from connect in a faultConn and returns
// a fault-capable fixture.
func newFaultFixture(t testing.TB, connect connectFunc) Fixture {
	serverConn, clientConn, cleanup := connect(t)
	fc := newFaultConn(clientConn)
	return &faultFixture{
		fakeFixture: startFakeFixture(serverConn, fc, cleanup),
		fault:       fc,
	}
}

func faultPipeFactory(t testing.TB) Fixture { return newFaultFixture(t, pipeConnect) }
func faultUnixFactory(t testing.TB) Fixture { return newFaultFixture(t, unixConnect) }
func faultTCPFactory(t testing.TB) Fixture  { return newFaultFixture(t, tcpConnect) }

// ensure compile-time interface satisfaction
var (
	_ RPCClient    = (*PeerClient)(nil)
	_ Observations = (*FakeServer)(nil)
	_ Fixture      = (*fakeFixture)(nil)
	_ Fixture      = (*faultFixture)(nil)
	_ FaultFixture = (*faultFixture)(nil)
	_ net.Conn     = (*CountingConn)(nil)
	_ net.Conn     = (*faultConn)(nil)
	_              = fmt.Sprintf // suppress unused import if needed
)
