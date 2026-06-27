package rpcserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/rpc/conformance"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-watch/internal/rpcmethods"
)

var emptyReg = func() config.ProjectsConfig {
	return config.ProjectsConfig{Projects: []config.ProjectRef{}}
}

// collectSink implements bus.Sink and collects delivered events for assertions.
type collectSink struct {
	mu     sync.Mutex
	events []bus.Event
}

func (s *collectSink) Deliver(ev bus.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *collectSink) snapshot() []bus.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]bus.Event, len(s.events))
	copy(out, s.events)
	return out
}

func dialAndCall(t *testing.T, ctx context.Context, uri string) json.RawMessage {
	t.Helper()
	conn, err := transport.Dial(ctx, uri)
	if err != nil {
		t.Fatalf("transport.Dial(%s): %v", uri, err)
	}
	client := conformance.NewPeerClient(conn)
	go client.Peer().Serve(ctx)

	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	defer callCancel()
	raw, err := client.Call(callCtx, "daemon.status", nil)
	if err != nil {
		t.Fatalf("Call daemon.status: %v", err)
	}
	return raw
}

func TestServe_TCP_DaemonStatus(t *testing.T) {
	hub := bus.NewHub()
	h := rpcmethods.New(nil, "test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	// Give accept loop a moment to start.
	time.Sleep(20 * time.Millisecond)

	addr := "tcp://" + ln.Addr().String()
	raw := dialAndCall(t, ctx, addr)

	var result rpcmethods.StatusResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if result.HostID != "test-host" {
		t.Errorf("hostId = %q, want %q", result.HostID, "test-host")
	}
	if result.Version != "0.1.0" {
		t.Errorf("version = %q, want %q", result.Version, "0.1.0")
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Errorf("Serve returned error: %v", err)
	}
}

func TestServe_Unix_DaemonStatus(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	hub := bus.NewHub()
	h := rpcmethods.New(nil, "unix-host", "2.0.0", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("unix://" + sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	time.Sleep(20 * time.Millisecond)

	raw := dialAndCall(t, ctx, "unix://"+sockPath)

	var result rpcmethods.StatusResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if result.HostID != "unix-host" {
		t.Errorf("hostId = %q, want %q", result.HostID, "unix-host")
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Errorf("Serve returned error: %v", err)
	}
}

func TestServe_ConcurrentConnections(t *testing.T) {
	hub := bus.NewHub()
	h := rpcmethods.New(nil, "test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	time.Sleep(20 * time.Millisecond)

	addr := "tcp://" + ln.Addr().String()

	var wg sync.WaitGroup
	results := make(chan string, 2)

	for range 2 {
		wg.Go(func() {
			conn, err := transport.Dial(ctx, addr)
			if err != nil {
				t.Errorf("Dial: %v", err)
				return
			}
			client := conformance.NewPeerClient(conn)
			go client.Peer().Serve(ctx)

			callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
			defer callCancel()
			raw, err := client.Call(callCtx, "daemon.status", nil)
			if err != nil {
				t.Errorf("Call: %v", err)
				return
			}
			var result rpcmethods.StatusResult
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Errorf("Unmarshal: %v", err)
				return
			}
			results <- result.HostID
		})
	}

	wg.Wait()
	close(results)

	count := 0
	for id := range results {
		count++
		if id != "test-host" {
			t.Errorf("hostId = %q, want %q", id, "test-host")
		}
	}
	if count != 2 {
		t.Errorf("got %d results, want 2", count)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Errorf("Serve returned error: %v", err)
	}
}

func TestServe_Shutdown_ClosesListener(t *testing.T) {
	hub := bus.NewHub()
	h := rpcmethods.New(nil, "test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, false)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	time.Sleep(20 * time.Millisecond)

	// Connect a client.
	addr := "tcp://" + ln.Addr().String()
	conn, err := transport.Dial(ctx, addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	client := conformance.NewPeerClient(conn)
	clientErr := make(chan error, 1)
	go func() { clientErr <- client.Peer().Serve(ctx) }()

	// Verify the connection works.
	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	_, err = client.Call(callCtx, "daemon.status", nil)
	callCancel()
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	// Cancel context — should trigger clean shutdown.
	cancel()

	// Serve should return nil (clean shutdown).
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within timeout")
	}
}

func TestServe_CtlEvents_ConnectDisconnect(t *testing.T) {
	hub := bus.NewHub()
	sink := &collectSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	h := rpcmethods.New(nil, "test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, true) // ctlEvents=true
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	time.Sleep(20 * time.Millisecond)

	// Dial and make a call.
	addr := "tcp://" + ln.Addr().String()
	clientCtx, clientCancel := context.WithCancel(ctx)
	conn, err := transport.Dial(clientCtx, addr)
	if err != nil {
		cancel()
		t.Fatalf("Dial: %v", err)
	}
	client := conformance.NewPeerClient(conn)
	clientDone := make(chan error, 1)
	go func() { clientDone <- client.Peer().Serve(clientCtx) }()

	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	_, err = client.Call(callCtx, "daemon.status", nil)
	callCancel()
	if err != nil {
		cancel()
		t.Fatalf("Call: %v", err)
	}

	// Wait for connect event to propagate.
	time.Sleep(50 * time.Millisecond)

	// Check for ctl.connect event.
	events := sink.snapshot()
	var hasConnect bool
	for _, ev := range events {
		if ev.Type == bus.TypeCtlConnect {
			hasConnect = true
		}
	}
	if !hasConnect {
		t.Error("expected ctl.connect event, got none")
	}

	// Close the client to trigger disconnect.
	clientCancel()
	<-clientDone

	// Wait for disconnect event.
	time.Sleep(100 * time.Millisecond)

	events = sink.snapshot()
	var hasDisconnect bool
	for _, ev := range events {
		if ev.Type == bus.TypeCtlDisconnect {
			hasDisconnect = true
		}
	}
	if !hasDisconnect {
		t.Error("expected ctl.disconnect event, got none")
	}

	cancel()
	<-errCh
}

// startSubServer starts a Server on a fresh TCP listener and returns the hub,
// the dial address, and the server context. The server is torn down via
// t.Cleanup. Used by the bus.subscribe bridge tests.
func startSubServer(t *testing.T, ctlEvents bool) (*bus.Hub, string, context.Context) {
	t.Helper()
	hub := bus.NewHub()
	h := rpcmethods.New(nil, "test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, ctlEvents)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()
	time.Sleep(20 * time.Millisecond)
	addr := "tcp://" + ln.Addr().String()
	t.Cleanup(func() {
		cancel()
		<-errCh
	})
	return hub, addr, ctx
}

// dialSubscribe dials the server, starts the client's Serve loop, and calls
// bus.subscribe. The returned cancel disconnects the client.
func dialSubscribe(t *testing.T, ctx context.Context, addr string) (*conformance.PeerClient, context.CancelFunc) {
	t.Helper()
	clientCtx, clientCancel := context.WithCancel(ctx)
	conn, err := transport.Dial(clientCtx, addr)
	if err != nil {
		clientCancel()
		t.Fatalf("Dial: %v", err)
	}
	client := conformance.NewPeerClient(conn)
	go client.Peer().Serve(clientCtx)

	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	defer callCancel()
	if _, err := client.Call(callCtx, "bus.subscribe", nil); err != nil {
		clientCancel()
		t.Fatalf("bus.subscribe: %v", err)
	}
	return client, clientCancel
}

// dialSubscribeHalfOpen dials the server, performs a bus.subscribe handshake via
// manual frame I/O, then stops reading while holding the TCP connection open —
// modelling a half-open peer (process wedged / host gone): the socket stays up
// (no EOF) but the client never reads the server's $keepalive pings and so never
// pongs. Only the server's read watchdog can detect this; a live client would
// auto-pong and keep its sink. The conn is closed at test end.
func dialSubscribeHalfOpen(t *testing.T, ctx context.Context, addr string) {
	t.Helper()
	conn, err := transport.Dial(ctx, addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := json.NewEncoder(conn).Encode(rpc.Request{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "bus.subscribe",
	}); err != nil {
		t.Fatalf("encode subscribe: %v", err)
	}

	// Read frames until the subscribe response (id:1) arrives, skipping any
	// interleaved notifications/pings. After this we never read again — the conn
	// stays open but silent, so the server's watchdog (not EOF) must reap it.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	dec := json.NewDecoder(conn)
	for {
		var resp rpc.Response
		if err := dec.Decode(&resp); err != nil {
			t.Fatalf("decode subscribe response: %v", err)
		}
		if string(resp.ID) == "1" {
			break
		}
	}
	_ = conn.SetReadDeadline(time.Time{})
}

// readNotif waits up to timeout for a notification on ch.
func readNotif(t *testing.T, ch <-chan rpc.Request, timeout time.Duration) (rpc.Request, bool) {
	t.Helper()
	select {
	case req := <-ch:
		return req, true
	case <-time.After(timeout):
		return rpc.Request{}, false
	}
}

// waitSinkCount polls hub.SinkCount until it equals want or the deadline passes.
func waitSinkCount(t *testing.T, hub *bus.Hub, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for hub.SinkCount() != want {
		if time.Now().After(deadline) {
			t.Fatalf("SinkCount = %d, want %d", hub.SinkCount(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// AC-1: bus.subscribe relays hub broadcasts to the connected peer as
// JSON-RPC notifications, preserving the full envelope. A second subscribe is
// idempotent (no duplicate sink, no duplicate notification).
func TestSubscribe_RelaysHubBroadcasts(t *testing.T) {
	hub, addr, ctx := startSubServer(t, false)
	client, _ := dialSubscribe(t, ctx, addr)

	ev, err := bus.NewEvent("agent.tool.post", "test/source", nil)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	ev.Host = "dev-box.charlie"
	ev.Project = "auto-stack"
	hub.Broadcast(ev)

	req, ok := readNotif(t, client.Notifications(), 2*time.Second)
	if !ok {
		t.Fatal("no notification received for broadcast")
	}
	if req.Method != ev.Type {
		t.Errorf("method = %q, want %q", req.Method, ev.Type)
	}
	var got bus.Event
	if err := json.Unmarshal(req.Params, &got); err != nil {
		t.Fatalf("Unmarshal params to bus.Event: %v", err)
	}
	if got.Host != "dev-box.charlie" {
		t.Errorf("Host = %q, want dev-box.charlie", got.Host)
	}
	if got.Project != "auto-stack" {
		t.Errorf("Project = %q, want auto-stack", got.Project)
	}
	if got.ID != ev.ID {
		t.Errorf("ID = %q, want %q", got.ID, ev.ID)
	}

	// Idempotency: a second subscribe must not register an extra sink.
	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	_, err = client.Call(callCtx, "bus.subscribe", nil)
	callCancel()
	if err != nil {
		t.Fatalf("second bus.subscribe: %v", err)
	}
	if got := hub.SinkCount(); got != 1 {
		t.Errorf("SinkCount after double subscribe = %d, want 1", got)
	}

	ev2, _ := bus.NewEvent("agent.tool.post", "test/source", nil)
	hub.Broadcast(ev2)
	if _, ok := readNotif(t, client.Notifications(), 2*time.Second); !ok {
		t.Fatal("no notification after second subscribe")
	}
	if _, ok := readNotif(t, client.Notifications(), 200*time.Millisecond); ok {
		t.Error("received a duplicate notification — second subscribe registered an extra sink")
	}
}

// AC-3: the bridge applies no ctl-specific filter — it relays whatever is on
// the hub. The ctl gate is enforced at emission (031), not in the bridge.
func TestCtlGatingThroughBridge(t *testing.T) {
	hub, addr, ctx := startSubServer(t, false)
	client, _ := dialSubscribe(t, ctx, addr)

	// A synthetic ctl.* event broadcast directly onto the hub IS relayed —
	// proving the bridge does not filter ctl events.
	ctlEv, err := bus.NewEvent(bus.TypeCtlConnect, "auto/watch/daemon", nil)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	hub.Broadcast(ctlEv)
	req, ok := readNotif(t, client.Notifications(), 2*time.Second)
	if !ok {
		t.Fatal("ctl.connect was not relayed by the bridge")
	}
	if req.Method != bus.TypeCtlConnect {
		t.Errorf("method = %q, want %q", req.Method, bus.TypeCtlConnect)
	}

	// A data-plane event also relays.
	dataEv, _ := bus.NewEvent("agent.tool.post", "test/source", nil)
	hub.Broadcast(dataEv)
	req, ok = readNotif(t, client.Notifications(), 2*time.Second)
	if !ok {
		t.Fatal("data-plane event was not relayed")
	}
	if req.Method != "agent.tool.post" {
		t.Errorf("method = %q, want agent.tool.post", req.Method)
	}
}

// AC-3 (companion): with ctlEvents=true, lifecycle events emitted by the
// accept loop reach a subscribed peer through the bridge. A second connection
// triggers a ctl.connect broadcast that the first (subscribed) client receives.
func TestCtlEventsRelayedWhenEnabled(t *testing.T) {
	_, addr, ctx := startSubServer(t, true)
	client, _ := dialSubscribe(t, ctx, addr)

	c2ctx, c2cancel := context.WithCancel(ctx)
	defer c2cancel()
	conn2, err := transport.Dial(c2ctx, addr)
	if err != nil {
		t.Fatalf("Dial second client: %v", err)
	}
	c2 := conformance.NewPeerClient(conn2)
	go c2.Peer().Serve(c2ctx)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case req := <-client.Notifications():
			if req.Method == bus.TypeCtlConnect {
				return
			}
		case <-deadline:
			t.Fatal("did not receive ctl.connect through the bridge")
		}
	}
}

// AC-4: the per-peer sink is removed from the hub when the peer disconnects,
// and subsequent broadcasts neither panic nor block.
func TestCleanupOnDisconnect(t *testing.T) {
	hub, addr, ctx := startSubServer(t, false)
	before := hub.SinkCount()

	_, clientCancel := dialSubscribe(t, ctx, addr)
	if got := hub.SinkCount(); got != before+1 {
		t.Fatalf("SinkCount after subscribe = %d, want %d", got, before+1)
	}

	clientCancel()
	waitSinkCount(t, hub, before, 2*time.Second)

	// Broadcasting after the subscriber is gone must not panic or block.
	ev, _ := bus.NewEvent("agent.tool.post", "test/source", nil)
	hub.Broadcast(ev)
}

// AC-5: a saturated subscriber (one that never reads its socket) is dropped
// when its outbound buffer fills, the hub never blocks, and a healthy
// subscriber continues to receive events.
func TestSaturatedSubscriberDropped(t *testing.T) {
	hub, addr, ctx := startSubServer(t, false)

	// Healthy client: drained continuously so its read loop never stalls.
	healthy, _ := dialSubscribe(t, ctx, addr)
	var healthyCount atomic.Int64
	drainStop := make(chan struct{})
	go func() {
		for {
			select {
			case <-healthy.Notifications():
				healthyCount.Add(1)
			case <-drainStop:
				return
			}
		}
	}()

	// Saturated client: a raw conn that subscribes but never reads its socket,
	// so its server-side outbound buffer eventually fills.
	satCtx, satCancel := context.WithCancel(ctx)
	defer satCancel()
	satConn, err := transport.Dial(satCtx, addr)
	if err != nil {
		t.Fatalf("Dial saturated client: %v", err)
	}
	defer satConn.Close()
	enc := rpc.NewEncoder(satConn)
	if err := enc.Encode(&rpc.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  "bus.subscribe",
	}); err != nil {
		t.Fatalf("encode subscribe on saturated client: %v", err)
	}

	// Both sinks registered.
	waitSinkCount(t, hub, 2, 2*time.Second)

	// Flood with large events to saturate the non-reading client by filling its
	// kernel + outbound buffers. The hub must never block: if it did, this loop
	// would hang and the test would time out. We stop as soon as the saturated
	// sink is dropped (or fail after a bounded number of events).
	blob := strings.Repeat("x", 8192)
	dropped := false
	floodStart := time.Now()
	for i := range 200000 {
		ev, _ := bus.NewEvent("agent.tool.post", "test/source", map[string]string{"seq": strconv.Itoa(i), "blob": blob})
		hub.Broadcast(ev)
		if hub.SinkCount() == 1 {
			dropped = true
			break
		}
		if time.Since(floodStart) > 8*time.Second {
			break
		}
	}
	if !dropped {
		t.Fatalf("saturated sink not dropped after flood, SinkCount = %d", hub.SinkCount())
	}

	// The healthy client is still alive and receives a fresh paced batch in full.
	base := healthyCount.Load()
	const k = 20
	for range k {
		ev, _ := bus.NewEvent("agent.tool.post", "test/source", nil)
		hub.Broadcast(ev)
		time.Sleep(2 * time.Millisecond)
	}
	deadline := time.Now().Add(2 * time.Second)
	for healthyCount.Load() < base+k {
		if time.Now().After(deadline) {
			t.Fatalf("healthy client received %d of %d post-saturation events", healthyCount.Load()-base, k)
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(drainStop)
}

// signalSink implements bus.Sink and closes/sends on a channel the first time
// it observes an event of the given type. Used to get an event-driven (not
// poll-to-settle) signal that the daemon broadcast a ctl.disconnect.
type signalSink struct {
	want string
	once sync.Once
	ch   chan struct{}
}

func (s *signalSink) Deliver(ev bus.Event) {
	if ev.Type == s.want {
		s.once.Do(func() { close(s.ch) })
	}
}

// AC-4: a dead subscriber's hub sink is reaped (no leak). A peer subscribes,
// then goes silent without closing its connection (a healthy-but-mute client
// that keeps draining server pings — so the drop-on-full path is NOT what
// reaps it). The keepalive watchdog is therefore the sole reap cause: after the
// keepalive timeout the peer's Serve returns, subscription.teardown() fires
// (now defer-guarded), and the hub sink count returns to its pre-subscribe
// baseline. Proven via the ctl.disconnect broadcast (bounded select, not a
// poll-to-settle), which the daemon emits only after teardown has run.
func TestDeadSubscriberSinkReaped(t *testing.T) {
	hub := bus.NewHub()
	h := rpcmethods.New(nil, "test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, true) // ctlEvents=true so the reap emits ctl.disconnect
	// Test seam: override the 15s/45s daemon defaults with short durations so
	// the watchdog reaps within the test's bounded wait.
	srv.kaInterval = 40 * time.Millisecond
	srv.kaTimeout = 120 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-errCh
	})
	time.Sleep(20 * time.Millisecond)
	addr := "tcp://" + ln.Addr().String()

	// Observer: an in-process hub sink that signals on the first ctl.disconnect.
	disconnected := &signalSink{want: bus.TypeCtlDisconnect, ch: make(chan struct{})}
	unsub := hub.Subscribe(disconnected)
	defer unsub()

	baseline := hub.SinkCount() // includes the observer

	// Subscribe a genuinely half-open peer: it completes bus.subscribe, then
	// holds the TCP conn open but stops reading, so it never pongs the server's
	// $keepalive pings. A live client would auto-pong and keep its sink, so only
	// the server's read watchdog can reap this one.
	dialSubscribeHalfOpen(t, ctx, addr)
	if got := hub.SinkCount(); got != baseline+1 {
		t.Fatalf("SinkCount after subscribe = %d, want %d", got, baseline+1)
	}

	// Bounded wait for the reap signal — NOT a poll-to-settle loop.
	select {
	case <-disconnected.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("dead subscriber was not reaped within timeout")
	}

	// teardown runs before the ctl.disconnect broadcast, so the dead peer's
	// sink is already gone once we observe the signal.
	if got := hub.SinkCount(); got != baseline {
		t.Fatalf("SinkCount after reap = %d, want baseline %d", got, baseline)
	}

	// A subsequent broadcast no longer targets the dead peer (its sink is
	// deregistered) and must neither panic nor block.
	ev, _ := bus.NewEvent("agent.tool.post", "test/source", nil)
	hub.Broadcast(ev)
	if got := hub.SinkCount(); got != baseline {
		t.Fatalf("SinkCount after post-reap broadcast = %d, want %d", got, baseline)
	}
}

func TestHealthySubscriberNotReaped(t *testing.T) {
	// Regression guard for server-only keepalive false-reaps: a read-only
	// subscriber (calls bus.subscribe, then only drains pushed notifications and
	// never sends application traffic) must NOT be reaped. Its peer auto-pongs
	// the server's $keepalive pings, which keeps the server's watchdog satisfied.
	hub := bus.NewHub()
	h := rpcmethods.New(nil, "test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, true)
	srv.kaInterval = 40 * time.Millisecond
	srv.kaTimeout = 120 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-errCh
	})
	time.Sleep(20 * time.Millisecond)
	addr := "tcp://" + ln.Addr().String()

	disconnected := &signalSink{want: bus.TypeCtlDisconnect, ch: make(chan struct{})}
	unsub := hub.Subscribe(disconnected)
	defer unsub()

	baseline := hub.SinkCount()

	// A healthy subscriber: dialSubscribe runs the client's Serve loop, so it
	// auto-pongs server pings even though it sends no application traffic.
	client, _ := dialSubscribe(t, ctx, addr)
	if got := hub.SinkCount(); got != baseline+1 {
		t.Fatalf("SinkCount after subscribe = %d, want %d", got, baseline+1)
	}

	// Bounded negative assertion: over several ping/pong cycles well past the
	// reap timeout, no ctl.disconnect fires. Fixed-window wait, not settle.
	select {
	case <-disconnected.ch:
		t.Fatal("healthy read-only subscriber was falsely reaped")
	case <-time.After(8 * srv.kaInterval):
	}
	if got := hub.SinkCount(); got != baseline+1 {
		t.Fatalf("SinkCount after idle = %d, want %d (healthy subscriber dropped)", got, baseline+1)
	}

	// Positive observable: a broadcast is still delivered to the live subscriber.
	ev, _ := bus.NewEvent("agent.tool.post", "test/source", nil)
	hub.Broadcast(ev)
	if _, ok := readNotif(t, client.Notifications(), 2*time.Second); !ok {
		t.Fatal("healthy subscriber did not receive broadcast after idle period")
	}
}

func TestServe_CtlEventsFalse_NoLifecycleEvents(t *testing.T) {
	hub := bus.NewHub()
	sink := &collectSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	h := rpcmethods.New(nil, "test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, false) // ctlEvents=false
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	time.Sleep(20 * time.Millisecond)

	// Dial, call, close.
	addr := "tcp://" + ln.Addr().String()
	clientCtx, clientCancel := context.WithCancel(ctx)
	conn, err := transport.Dial(clientCtx, addr)
	if err != nil {
		cancel()
		t.Fatalf("Dial: %v", err)
	}
	client := conformance.NewPeerClient(conn)
	clientDone := make(chan error, 1)
	go func() { clientDone <- client.Peer().Serve(clientCtx) }()

	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	_, err = client.Call(callCtx, "daemon.status", nil)
	callCancel()
	if err != nil {
		cancel()
		t.Fatalf("Call: %v", err)
	}

	clientCancel()
	<-clientDone

	time.Sleep(100 * time.Millisecond)

	events := sink.snapshot()
	for _, ev := range events {
		if ev.Type == bus.TypeCtlConnect || ev.Type == bus.TypeCtlDisconnect {
			t.Errorf("unexpected lifecycle event %q with ctlEvents=false", ev.Type)
		}
	}

	cancel()
	<-errCh
}
