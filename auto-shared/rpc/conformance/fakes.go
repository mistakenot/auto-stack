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
			return json.RawMessage(params), nil
		})),
		rpc.WithHandler("push", fs.counting("push", func(_ context.Context, params json.RawMessage) (any, error) {
			// Send a server-push notification back to the caller.
			_ = fs.peer.Notify("server.pushed", json.RawMessage(params))
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
// FakeFixtures: returns 3 fixture factories (pipe, unix, tcp)
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

// pipeFactory wires two peers via net.Pipe.
func pipeFactory(t testing.TB) Fixture {
	c1, c2 := net.Pipe()

	server := NewFakeServer(c1)
	client := NewPeerClient(c2)

	ctx, cancel := context.WithCancel(context.Background())

	sErr := make(chan error, 1)
	cErr := make(chan error, 1)

	go func() { sErr <- server.Peer().Serve(ctx) }()
	go func() { cErr <- client.Peer().Serve(ctx) }()

	return &fakeFixture{
		client:     client,
		server:     server,
		cancel:     cancel,
		serveErrCh: sErr,
		clientDone: cErr,
	}
}

// unixFactory creates a unix socket listener+dialer pair.
func unixFactory(t testing.TB) Fixture {
	dir := t.(*testing.T).TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	ln, err := unix.Listen(sockPath)
	if err != nil {
		t.Fatalf("unix.Listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Accept one connection in a goroutine.
	type acceptResult struct {
		conn net.Conn
		err  error
	}
	acceptCh := make(chan acceptResult, 1)
	go func() {
		conn, err := ln.Accept()
		acceptCh <- acceptResult{conn, err}
	}()

	// Dial the server.
	d := unix.NewDialer(sockPath)
	dialCtx, dialCancel := context.WithTimeout(ctx, testTimeout)
	defer dialCancel()
	clientConn, err := d.Dial(dialCtx)
	if err != nil {
		cancel()
		ln.Close()
		t.Fatalf("unix.Dial: %v", err)
	}

	// Wait for accept.
	ar := <-acceptCh
	if ar.err != nil {
		cancel()
		clientConn.Close()
		ln.Close()
		t.Fatalf("unix.Accept: %v", ar.err)
	}

	server := NewFakeServer(ar.conn)
	client := NewPeerClient(clientConn)

	sErr := make(chan error, 1)
	cErr := make(chan error, 1)

	go func() { sErr <- server.Peer().Serve(ctx) }()
	go func() { cErr <- client.Peer().Serve(ctx) }()

	return &fakeFixture{
		client: client,
		server: server,
		cancel: func() {
			cancel()
			ln.Close()
		},
		serveErrCh: sErr,
		clientDone: cErr,
	}
}

// tcpFactory creates a TCP listener+dialer pair on loopback.
func tcpFactory(t testing.TB) Fixture {
	ln, err := tcp.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcp.Listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Accept one connection in a goroutine.
	type acceptResult struct {
		conn net.Conn
		err  error
	}
	acceptCh := make(chan acceptResult, 1)
	go func() {
		conn, err := ln.Accept()
		acceptCh <- acceptResult{conn, err}
	}()

	// Dial the server using the resolved address.
	addr := ln.Addr().String()
	d := tcp.NewDialer(addr)
	dialCtx, dialCancel := context.WithTimeout(ctx, testTimeout)
	defer dialCancel()
	clientConn, err := d.Dial(dialCtx)
	if err != nil {
		cancel()
		ln.Close()
		t.Fatalf("tcp.Dial %s: %v", addr, err)
	}

	// Wait for accept.
	ar := <-acceptCh
	if ar.err != nil {
		cancel()
		clientConn.Close()
		ln.Close()
		t.Fatalf("tcp.Accept: %v", ar.err)
	}

	server := NewFakeServer(ar.conn)
	client := NewPeerClient(clientConn)

	sErr := make(chan error, 1)
	cErr := make(chan error, 1)

	go func() { sErr <- server.Peer().Serve(ctx) }()
	go func() { cErr <- client.Peer().Serve(ctx) }()

	return &fakeFixture{
		client: client,
		server: server,
		cancel: func() {
			cancel()
			ln.Close()
		},
		serveErrCh: sErr,
		clientDone: cErr,
	}
}

// ensure compile-time interface satisfaction
var (
	_ RPCClient    = (*PeerClient)(nil)
	_ Observations = (*FakeServer)(nil)
	_ Fixture      = (*fakeFixture)(nil)
	_ net.Conn     = (*CountingConn)(nil)
	_              = fmt.Sprintf // suppress unused import if needed
)
