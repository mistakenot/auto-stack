package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testTimeout is the bounded timeout used for all blocking operations in tests.
// Any test that doesn't complete within this window has a bug.
const testTimeout = 5 * time.Second

// newPipePeers creates two connected peers over net.Pipe. The server peer has
// the given handlers registered; the client peer has none (callers register as
// needed). Both peers' Serve loops run in background goroutines; calling the
// returned cancel func triggers shutdown of both. The errChs carry the Serve
// return values so tests can assert on them.
func newPipePeers(t *testing.T, handlers map[string]Handler, clientOpts []Option, serverOpts ...Option) (
	client *Peer, server *Peer, cancel context.CancelFunc,
	clientErr <-chan error, serverErr <-chan error,
) {
	t.Helper()
	c1, c2 := net.Pipe()

	server = NewPeer(c1, serverOpts...)
	for m, h := range handlers {
		server.Register(m, h)
	}

	client = NewPeer(c2, clientOpts...)

	ctx, cancel := context.WithCancel(context.Background())

	sErr := make(chan error, 1)
	cErr := make(chan error, 1)

	go func() { sErr <- server.Serve(ctx) }()
	go func() { cErr <- client.Serve(ctx) }()

	return client, server, cancel, cErr, sErr
}

// ---------------------------------------------------------------------------
// AC-3: Dispatch + error mapping
// ---------------------------------------------------------------------------

func TestKnownMethodReturnsResult(t *testing.T) {
	handlers := map[string]Handler{
		"echo": func(_ context.Context, params json.RawMessage) (any, error) {
			return params, nil
		},
	}

	client, _, cancel, _, _ := newPipePeers(t, handlers, nil)
	defer cancel()

	ctx, tCancel := context.WithTimeout(context.Background(), testTimeout)
	defer tCancel()

	result, err := client.Call(ctx, "echo", map[string]string{"msg": "hello"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got["msg"] != "hello" {
		t.Errorf("result.msg = %q, want hello", got["msg"])
	}
}

func TestHandlerReturningRPCError(t *testing.T) {
	handlers := map[string]Handler{
		"validate": func(_ context.Context, _ json.RawMessage) (any, error) {
			return nil, &Error{Code: InvalidParams, Message: "bad params"}
		},
	}

	client, _, cancel, _, _ := newPipePeers(t, handlers, nil)
	defer cancel()

	ctx, tCancel := context.WithTimeout(context.Background(), testTimeout)
	defer tCancel()

	_, err := client.Call(ctx, "validate", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var rpcErr *Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error is not *Error: %T %v", err, err)
	}
	if rpcErr.Code != InvalidParams {
		t.Errorf("error code = %d, want %d (InvalidParams)", rpcErr.Code, InvalidParams)
	}
}

func TestHandlerReturningPlainError(t *testing.T) {
	handlers := map[string]Handler{
		"fail": func(_ context.Context, _ json.RawMessage) (any, error) {
			return nil, errors.New("something broke")
		},
	}

	client, _, cancel, _, _ := newPipePeers(t, handlers, nil)
	defer cancel()

	ctx, tCancel := context.WithTimeout(context.Background(), testTimeout)
	defer tCancel()

	_, err := client.Call(ctx, "fail", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var rpcErr *Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error is not *Error: %T %v", err, err)
	}
	if rpcErr.Code != InternalError {
		t.Errorf("error code = %d, want %d (InternalError)", rpcErr.Code, InternalError)
	}
}

func TestUnknownMethod(t *testing.T) {
	client, _, cancel, _, _ := newPipePeers(t, nil, nil)
	defer cancel()

	ctx, tCancel := context.WithTimeout(context.Background(), testTimeout)
	defer tCancel()

	_, err := client.Call(ctx, "nonexistent.method", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var rpcErr *Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error is not *Error: %T %v", err, err)
	}
	if rpcErr.Code != MethodNotFound {
		t.Errorf("error code = %d, want %d (MethodNotFound)", rpcErr.Code, MethodNotFound)
	}
}

func TestMalformedJSON(t *testing.T) {
	// Use raw net.Pipe to send garbage and observe the error response.
	c1, c2 := net.Pipe()

	server := NewPeer(c1, WithHandler("noop", func(_ context.Context, _ json.RawMessage) (any, error) {
		return "ok", nil
	}))

	ctx := t.Context()

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ctx) }()

	// Write malformed JSON.
	_, err := c2.Write([]byte("{not valid json}\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read the parse error response. The write pump drains before shutdown,
	// so this should succeed.
	dec := NewDecoder(c2)
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != ParseError {
		t.Errorf("error code = %d, want %d (ParseError)", resp.Error.Code, ParseError)
	}

	c2.Close()
	<-serveErr
}

func TestInvalidEnvelopeNon20(t *testing.T) {
	// Send a well-formed JSON object with jsonrpc != "2.0".
	c1, c2 := net.Pipe()

	server := NewPeer(c1)
	ctx := t.Context()

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ctx) }()

	// jsonrpc "1.0" with id and method — classify will reject it.
	_, err := c2.Write([]byte(`{"jsonrpc":"1.0","id":1,"method":"test"}` + "\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	dec := NewDecoder(c2)
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != InvalidRequest {
		t.Errorf("error code = %d, want %d (InvalidRequest)", resp.Error.Code, InvalidRequest)
	}

	c2.Close()
	<-serveErr
}

func TestInvalidEnvelopeMissingMethod(t *testing.T) {
	// Send a frame with jsonrpc "2.0" and an id but no method and no result/error.
	c1, c2 := net.Pipe()

	server := NewPeer(c1)
	ctx := t.Context()

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ctx) }()

	_, err := c2.Write([]byte(`{"jsonrpc":"2.0","id":1}` + "\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	dec := NewDecoder(c2)
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != InvalidRequest {
		t.Errorf("error code = %d, want %d (InvalidRequest)", resp.Error.Code, InvalidRequest)
	}

	c2.Close()
	<-serveErr
}

func TestNotificationNoResponse(t *testing.T) {
	// Send a notification (no id) to the server. Verify the server processes
	// it (via the OnNotify callback) but sends nothing back on the wire.
	var received atomic.Int32

	c1, c2 := net.Pipe()

	server := NewPeer(c1,
		WithHandler("ping", func(_ context.Context, _ json.RawMessage) (any, error) {
			return "pong", nil
		}),
		WithOnNotify(func(req Request) {
			received.Add(1)
		}),
	)

	ctx := t.Context()

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ctx) }()

	// Send a notification (no id).
	_, err := c2.Write([]byte(`{"jsonrpc":"2.0","method":"some.event","params":{}}` + "\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Send a request to synchronize — its response proves the notification was processed.
	_, err = c2.Write([]byte(`{"jsonrpc":"2.0","id":99,"method":"ping"}` + "\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	dec := NewDecoder(c2)
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The only frame we got back should be the response to id:99, not anything
	// for the notification.
	if string(resp.ID) != "99" {
		t.Errorf("response id = %s, want 99", resp.ID)
	}

	if received.Load() != 1 {
		t.Errorf("OnNotify called %d times, want 1", received.Load())
	}

	c2.Close()
	<-serveErr
}

// ---------------------------------------------------------------------------
// AC-4: Duplex push + terminal shutdown
// ---------------------------------------------------------------------------

func TestConcurrentNotifyAndCall(t *testing.T) {
	// Server Notify concurrently with client Calls under -race.
	handlers := map[string]Handler{
		"echo": func(_ context.Context, params json.RawMessage) (any, error) {
			return params, nil
		},
	}

	var notifyCount atomic.Int32

	// Set the client's OnNotify via clientOpts so it's wired before Serve starts.
	// Use large buffers on both sides to avoid overflow with concurrent traffic.
	clientOpts := []Option{
		WithOnNotify(func(_ Request) {
			notifyCount.Add(1)
		}),
		WithBufferSize(128),
	}

	client, server, cancel, _, _ := newPipePeers(t, handlers, clientOpts,
		WithBufferSize(128),
	)
	defer cancel()

	ctx, tCancel := context.WithTimeout(context.Background(), testTimeout)
	defer tCancel()

	const n = 50
	var wg sync.WaitGroup

	// Client sends calls sequentially (each call blocks until response).
	wg.Go(func() {
		for i := range n {
			_, _ = client.Call(ctx, "echo", map[string]int{"i": i})
		}
	})

	// Server sends notifications concurrently.
	wg.Go(func() {
		for i := range n {
			_ = server.Notify("server.push", map[string]int{"i": i})
		}
	})

	wg.Wait()

	// Give the client's read loop time to process remaining notifications.
	time.Sleep(50 * time.Millisecond)

	// We assert that at least some notifications were received (the exact count
	// depends on timing, but with a healthy pipe it should be all of them).
	if notifyCount.Load() == 0 {
		t.Error("expected at least some server notifications to arrive")
	}
}

func TestStalledReaderTriggersDropAndClose(t *testing.T) {
	// Create a peer with a tiny buffer (1). Fill the buffer by sending many
	// notifications without reading, which should trigger overflow -> shutdown.
	c1, c2 := net.Pipe()

	peer := NewPeer(c1, WithBufferSize(1))

	ctx := t.Context()

	serveErr := make(chan error, 1)
	go func() { serveErr <- peer.Serve(ctx) }()

	// Don't read from c2 — the peer's write pump will block on the pipe,
	// and the enqueue channel will overflow.

	// Hammer notifications until we get ErrClosed.
	var gotClosed bool
	for range 1000 {
		err := peer.Notify("spam", nil)
		if errors.Is(err, ErrClosed) {
			gotClosed = true
			break
		}
	}

	if !gotClosed {
		t.Error("expected ErrClosed from Notify after buffer overflow, but never got it")
	}

	c2.Close()
}

func TestPendingCallReturnsOnEOF(t *testing.T) {
	// Terminal path (a): Serve hits EOF.
	c1, c2 := net.Pipe()

	peer := NewPeer(c1)

	ctx := t.Context()

	serveErr := make(chan error, 1)
	go func() { serveErr <- peer.Serve(ctx) }()

	// Start a call in the background. The remote end has no handler and will
	// never respond — the call blocks until shutdown.
	callResult := make(chan error, 1)
	go func() {
		callCtx, callCancel := context.WithTimeout(context.Background(), testTimeout)
		defer callCancel()
		_, err := peer.Call(callCtx, "anything", nil)
		callResult <- err
	}()

	// Give the call time to register, then close the remote end -> EOF.
	time.Sleep(50 * time.Millisecond)
	c2.Close()

	select {
	case err := <-callResult:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("Call returned %v, want ErrClosed", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Call did not return promptly on EOF")
	}
}

func TestPendingCallReturnsOnContextCancel(t *testing.T) {
	// Terminal path (b): Serve's context is cancelled. The context watcher
	// goroutine closes the connection, which unblocks the readLoop's Decode
	// call, triggering shutdown. Pending calls return ErrClosed.
	c1, c2 := net.Pipe()
	defer c2.Close()

	peer := NewPeer(c1)

	ctx, cancel := context.WithCancel(context.Background())

	serveErr := make(chan error, 1)
	go func() { serveErr <- peer.Serve(ctx) }()

	callResult := make(chan error, 1)
	go func() {
		callCtx, callCancel := context.WithTimeout(context.Background(), testTimeout)
		defer callCancel()
		_, err := peer.Call(callCtx, "anything", nil)
		callResult <- err
	}()

	// Give the call time to register, then cancel the serve context.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-callResult:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("Call returned %v, want ErrClosed", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Call did not return promptly on context cancel")
	}
}

func TestPendingCallReturnsOnOverflow(t *testing.T) {
	// Terminal path (c): write-pump drops connection on overflow.
	c1, c2 := net.Pipe()

	peer := NewPeer(c1, WithBufferSize(1))

	ctx := t.Context()

	serveErr := make(chan error, 1)
	go func() { serveErr <- peer.Serve(ctx) }()

	// Start a call that will be pending (remote end never responds).
	callResult := make(chan error, 1)
	go func() {
		callCtx, callCancel := context.WithTimeout(context.Background(), testTimeout)
		defer callCancel()
		_, err := peer.Call(callCtx, "anything", nil)
		callResult <- err
	}()

	// Give the call time to register.
	time.Sleep(50 * time.Millisecond)

	// Flood notifications to trigger overflow. Don't read from c2.
	for range 1000 {
		_ = peer.Notify("spam", nil)
	}

	select {
	case err := <-callResult:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("Call returned %v, want ErrClosed", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Call did not return promptly on overflow")
	}

	c2.Close()
}

func TestPendingCallReturnsOnExplicitClose(t *testing.T) {
	// Terminal path (d): explicit Close().
	c1, c2 := net.Pipe()
	defer c2.Close()

	peer := NewPeer(c1)

	ctx := t.Context()

	go func() { peer.Serve(ctx) }()

	callResult := make(chan error, 1)
	go func() {
		callCtx, callCancel := context.WithTimeout(context.Background(), testTimeout)
		defer callCancel()
		_, err := peer.Call(callCtx, "anything", nil)
		callResult <- err
	}()

	// Give the call time to register, then close the peer explicitly.
	time.Sleep(50 * time.Millisecond)
	peer.Close()

	select {
	case err := <-callResult:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("Call returned %v, want ErrClosed", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Call did not return promptly on explicit Close")
	}
}

func TestAfterShutdownCallAndNotifyReturnErrClosed(t *testing.T) {
	client, _, cancel, _, _ := newPipePeers(t, nil, nil)
	cancel()

	// Give Serve time to wind down.
	time.Sleep(100 * time.Millisecond)

	ctx, tCancel := context.WithTimeout(context.Background(), testTimeout)
	defer tCancel()

	_, err := client.Call(ctx, "anything", nil)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("Call after shutdown returned %v, want ErrClosed", err)
	}

	err = client.Notify("anything", nil)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("Notify after shutdown returned %v, want ErrClosed", err)
	}
}

func TestPerCallCtxCancellation(t *testing.T) {
	// Per-Call ctx cancellation removes only that waiter; other pending calls
	// are unaffected.
	//
	// Setup: use raw net.Pipe so we can control exactly which responses arrive.
	// The client peer sends two calls. We cancel the first call's context,
	// then manually send a response for the second call from the raw pipe.
	c1, c2 := net.Pipe()

	client := NewPeer(c1)
	ctx := t.Context()

	go func() { client.Serve(ctx) }()

	// Call 1: short-lived context; no response will arrive.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer shortCancel()

	cancelledResult := make(chan error, 1)
	go func() {
		_, err := client.Call(shortCtx, "slow", nil)
		cancelledResult <- err
	}()

	// Read the request for call 1 from the pipe (to unblock the write pump).
	dec := NewDecoder(c2)
	var req1 Request
	if err := dec.Decode(&req1); err != nil {
		t.Fatalf("decode req1: %v", err)
	}

	// Wait for the short-context call to be cancelled.
	select {
	case err := <-cancelledResult:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("cancelled Call returned %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("cancelled Call did not return promptly")
	}

	// Call 2: should succeed because only the cancelled waiter was removed.
	call2Result := make(chan struct {
		raw json.RawMessage
		err error
	}, 1)
	go func() {
		okCtx, okCancel := context.WithTimeout(context.Background(), testTimeout)
		defer okCancel()
		raw, err := client.Call(okCtx, "echo", map[string]string{"ok": "yes"})
		call2Result <- struct {
			raw json.RawMessage
			err error
		}{raw, err}
	}()

	// Read the request for call 2 from the pipe.
	var req2 Request
	if err := dec.Decode(&req2); err != nil {
		t.Fatalf("decode req2: %v", err)
	}

	// Send a response for call 2 back through the pipe.
	resultBytes, err := json.Marshal(map[string]string{"ok": "yes"})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	resp2 := Response{
		JSONRPC: "2.0",
		ID:      req2.ID,
		Result:  json.RawMessage(resultBytes),
	}
	enc := NewEncoder(c2)
	if err := enc.Encode(resp2); err != nil {
		t.Fatalf("encode resp2: %v", err)
	}

	// Verify call 2 succeeded.
	select {
	case r := <-call2Result:
		if r.err != nil {
			t.Fatalf("subsequent Call failed: %v", r.err)
		}
		var got map[string]string
		if err := json.Unmarshal(r.raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got["ok"] != "yes" {
			t.Errorf("result.ok = %q, want yes", got["ok"])
		}
	case <-time.After(testTimeout):
		t.Fatal("subsequent Call did not return")
	}

	c2.Close()
}

func TestPendingMapEmptyAfterShutdown(t *testing.T) {
	c1, c2 := net.Pipe()

	peer := NewPeer(c1)

	ctx, cancel := context.WithCancel(context.Background())

	serveErr := make(chan error, 1)
	go func() { serveErr <- peer.Serve(ctx) }()

	// Start several pending calls. The remote end never responds.
	const n = 5
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			callCtx, callCancel := context.WithTimeout(context.Background(), testTimeout)
			defer callCancel()
			_, _ = peer.Call(callCtx, "anything", nil)
		})
	}

	// Give calls time to register. Read their requests from c2 to unblock
	// the write pump (net.Pipe is synchronous).
	dec := NewDecoder(c2)
	for i := range n {
		var req Request
		if err := dec.Decode(&req); err != nil {
			t.Fatalf("decode request %d: %v", i, err)
		}
	}

	// Shutdown.
	cancel()
	c2.Close()
	wg.Wait()
	<-serveErr

	// Assert pending map is empty.
	peer.pendingMu.Lock()
	remaining := len(peer.pending)
	peer.pendingMu.Unlock()

	if remaining != 0 {
		t.Errorf("pending map has %d entries after shutdown, want 0", remaining)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	c1, _ := net.Pipe()
	peer := NewPeer(c1)

	// Close multiple times — should not panic.
	for i := range 10 {
		if err := peer.Close(); err != nil {
			t.Errorf("Close() #%d returned error: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// AC-3 supplement: duplex — both sides can serve methods
// ---------------------------------------------------------------------------

func TestDuplexBothSidesServe(t *testing.T) {
	c1, c2 := net.Pipe()

	// Server handles "server.echo".
	server := NewPeer(c1, WithHandler("server.echo", func(_ context.Context, params json.RawMessage) (any, error) {
		return params, nil
	}))

	// Client handles "client.echo".
	client := NewPeer(c2, WithHandler("client.echo", func(_ context.Context, params json.RawMessage) (any, error) {
		return params, nil
	}))

	ctx := t.Context()

	go func() { server.Serve(ctx) }()
	go func() { client.Serve(ctx) }()

	tCtx, tCancel := context.WithTimeout(context.Background(), testTimeout)
	defer tCancel()

	// Client calls server.
	result, err := client.Call(tCtx, "server.echo", map[string]string{"from": "client"})
	if err != nil {
		t.Fatalf("client->server Call: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["from"] != "client" {
		t.Errorf("got %q, want client", got["from"])
	}

	// Server calls client.
	result, err = server.Call(tCtx, "client.echo", map[string]string{"from": "server"})
	if err != nil {
		t.Fatalf("server->client Call: %v", err)
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["from"] != "server" {
		t.Errorf("got %q, want server", got["from"])
	}
}

// ---------------------------------------------------------------------------
// Edge case: Call after Serve returns ErrClosed promptly, no hang.
// ---------------------------------------------------------------------------

func TestCallAfterServeReturnsErrClosed(t *testing.T) {
	c1, c2 := net.Pipe()
	peer := NewPeer(c1)

	// Close the other end immediately so Serve hits EOF fast.
	c2.Close()

	ctx := context.Background()
	_ = peer.Serve(ctx)

	// Now Call should return ErrClosed.
	callCtx, callCancel := context.WithTimeout(context.Background(), testTimeout)
	defer callCancel()

	_, err := peer.Call(callCtx, "anything", nil)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("Call after Serve returned %v, want ErrClosed", err)
	}
}

// ---------------------------------------------------------------------------
// Edge case: Notify sends a valid notification frame on the wire.
// ---------------------------------------------------------------------------

func TestNotifySendsValidFrame(t *testing.T) {
	c1, c2 := net.Pipe()

	peer := NewPeer(c1)
	ctx := t.Context()

	go func() { peer.Serve(ctx) }()

	err := peer.Notify("test.event", map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}

	// Read the notification from the other end.
	dec := NewDecoder(c2)
	var got Request
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", got.JSONRPC)
	}
	if got.ID != nil {
		t.Errorf("notification should have no id, got %s", got.ID)
	}
	if got.Method != "test.event" {
		t.Errorf("method = %q, want test.event", got.Method)
	}

	var params map[string]string
	if err := json.Unmarshal(got.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params["key"] != "val" {
		t.Errorf("params.key = %q, want val", params["key"])
	}

	c2.Close()
}

// ---------------------------------------------------------------------------
// Edge case: Register post-construction works.
// ---------------------------------------------------------------------------

func TestRegisterPostConstruction(t *testing.T) {
	c1, c2 := net.Pipe()

	server := NewPeer(c1)
	server.Register("late.method", func(_ context.Context, _ json.RawMessage) (any, error) {
		return "registered-late", nil
	})

	client := NewPeer(c2)

	ctx := t.Context()

	go func() { server.Serve(ctx) }()
	go func() { client.Serve(ctx) }()

	tCtx, tCancel := context.WithTimeout(context.Background(), testTimeout)
	defer tCancel()

	result, err := client.Call(tCtx, "late.method", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var got string
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != "registered-late" {
		t.Errorf("result = %q, want registered-late", got)
	}
}

// ---------------------------------------------------------------------------
// Ensure Serve exits cleanly on read error (not just EOF).
// ---------------------------------------------------------------------------

func TestServeExitsOnReadError(t *testing.T) {
	c1, c2 := net.Pipe()

	peer := NewPeer(c1)
	serveErr := make(chan error, 1)
	go func() { serveErr <- peer.Serve(context.Background()) }()

	// Write some data then force-close, causing a read error (not EOF).
	_, _ = c2.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"test"}` + "\n"))
	// Close underlying conn to produce an error on the next read.
	c2.Close()

	select {
	case err := <-serveErr:
		// Serve should return with an error (EOF or read on closed pipe).
		if err == nil {
			t.Error("Serve returned nil error on closed pipe")
		}
	case <-time.After(testTimeout):
		t.Fatal("Serve did not exit on read error")
	}
}

// ---------------------------------------------------------------------------
// Verify ErrClosed sentinel value.
// ---------------------------------------------------------------------------

func TestErrClosedSentinel(t *testing.T) {
	if ErrClosed.Error() != "rpc: peer closed" {
		t.Errorf("ErrClosed = %q, want \"rpc: peer closed\"", ErrClosed.Error())
	}
}

// ---------------------------------------------------------------------------
// Stress: multiple concurrent calls all get correct responses (no cross-talk).
// ---------------------------------------------------------------------------

func TestConcurrentCallsNoCrosstalk(t *testing.T) {
	handlers := map[string]Handler{
		"identity": func(_ context.Context, params json.RawMessage) (any, error) {
			// Return params unchanged.
			return params, nil
		},
	}

	// Use large buffers on both sides to accommodate concurrent calls.
	client, _, cancel, _, _ := newPipePeers(t, handlers,
		[]Option{WithBufferSize(64)},
		WithBufferSize(64),
	)
	defer cancel()

	const n = 20
	type result struct {
		idx int
		val int
		err error
	}

	results := make(chan result, n)

	for i := range n {
		go func(idx int) {
			ctx, c := context.WithTimeout(context.Background(), testTimeout)
			defer c()
			raw, err := client.Call(ctx, "identity", map[string]int{"idx": idx})
			if err != nil {
				results <- result{idx: idx, err: err}
				return
			}
			var got map[string]int
			_ = json.Unmarshal(raw, &got)
			results <- result{idx: idx, val: got["idx"]}
		}(i)
	}

	for range n {
		r := <-results
		if r.err != nil {
			t.Errorf("call %d: %v", r.idx, r.err)
			continue
		}
		if r.val != r.idx {
			t.Errorf("call %d: got val %d (crosstalk)", r.idx, r.val)
		}
	}
}

// ---------------------------------------------------------------------------
// AC-2 / AC-3: keepalive (opt-in liveness)
// ---------------------------------------------------------------------------

// halfOpenConn simulates a half-open connection (TCP up, peer silent): writes
// are swallowed (the local write pump succeeds, as on a real half-open socket)
// and reads block forever until Close, at which point Read returns io.EOF —
// modelling the watchdog closing the conn to unblock the stalled Decode. It is
// local to this test file by design; the conformance fault conns are Phase 4.
type halfOpenConn struct {
	closed chan struct{}
	once   sync.Once
}

func newHalfOpenConn() *halfOpenConn {
	return &halfOpenConn{closed: make(chan struct{})}
}

func (c *halfOpenConn) Read(p []byte) (int, error) {
	<-c.closed
	return 0, io.EOF
}

func (c *halfOpenConn) Write(p []byte) (int, error) {
	select {
	case <-c.closed:
		return 0, io.ErrClosedPipe
	default:
		return len(p), nil // swallow — peer is silently unresponsive
	}
}

func (c *halfOpenConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func TestKeepAliveReapsHalfOpen(t *testing.T) {
	// A peer over a half-open conn with keepalive enabled. No inbound frame
	// ever arrives, so the watchdog must reap the conn within ~timeout.
	const (
		interval = 10 * time.Millisecond
		timeout  = 40 * time.Millisecond
	)

	conn := newHalfOpenConn()
	peer := NewPeer(conn, WithKeepAlive(interval, timeout))

	serveErr := make(chan error, 1)
	go func() { serveErr <- peer.Serve(context.Background()) }()

	// An in-flight Call whose own context far outlives the keepalive bound, so
	// an ErrClosed return proves the reap released it (not the caller deadline).
	callResult := make(chan error, 1)
	go func() {
		callCtx, callCancel := context.WithTimeout(context.Background(), testTimeout)
		defer callCancel()
		_, err := peer.Call(callCtx, "anything", nil)
		callResult <- err
	}()

	// Serve must return within a bounded window (well under testTimeout).
	select {
	case <-serveErr:
		// Serve returned — the watchdog reaped the half-open conn.
	case <-time.After(testTimeout):
		t.Fatal("Serve did not return after half-open keepalive reap")
	}

	// The in-flight Call must return ErrClosed (released by shutdown), not its
	// own deadline.
	select {
	case err := <-callResult:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("in-flight Call returned %v, want ErrClosed", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("in-flight Call did not return after reap")
	}

	// Subsequent Call / Notify must also return ErrClosed.
	postCtx, postCancel := context.WithTimeout(context.Background(), testTimeout)
	defer postCancel()
	if _, err := peer.Call(postCtx, "anything", nil); !errors.Is(err, ErrClosed) {
		t.Errorf("post-reap Call returned %v, want ErrClosed", err)
	}
	if err := peer.Notify("anything", nil); !errors.Is(err, ErrClosed) {
		t.Errorf("post-reap Notify returned %v, want ErrClosed", err)
	}
}

func TestKeepAliveNoFalseReapWhenHealthy(t *testing.T) {
	// A healthy, idle pair with keepalive enabled on both ends. With no
	// application traffic, the mutual $keepalive pings keep each side's
	// lastActivity fresh, so neither watchdog should fire.
	const (
		interval = 10 * time.Millisecond
		timeout  = 40 * time.Millisecond
	)

	handlers := map[string]Handler{
		"echo": func(_ context.Context, params json.RawMessage) (any, error) {
			return params, nil
		},
	}

	client, _, cancel, clientErr, serverErr := newPipePeers(t, handlers,
		[]Option{WithKeepAlive(interval, timeout)},
		WithKeepAlive(interval, timeout),
	)
	defer cancel()

	// Bounded negative assertion: over several keepalive intervals (and past the
	// reap timeout), neither Serve returns. This is a fixed-window wait, not a
	// poll-to-settle.
	select {
	case err := <-clientErr:
		t.Fatalf("client Serve returned during healthy idle: %v", err)
	case err := <-serverErr:
		t.Fatalf("server Serve returned during healthy idle: %v", err)
	case <-time.After(8 * interval):
		// Survived multiple ping cycles past the timeout — good.
	}

	// Positive observable: a late Call still round-trips, proving the connection
	// is alive and Serve is still running.
	ctx, tCancel := context.WithTimeout(context.Background(), testTimeout)
	defer tCancel()
	result, err := client.Call(ctx, "echo", map[string]string{"msg": "alive"})
	if err != nil {
		t.Fatalf("late Call after idle failed: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["msg"] != "alive" {
		t.Errorf("late Call result = %q, want alive", got["msg"])
	}
}

// ensure io is used (net.Pipe returns net.Conn which implements io.ReadWriteCloser)
var _ io.ReadWriteCloser = (*net.UnixConn)(nil)
