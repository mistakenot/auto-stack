package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ErrClosed is returned by Call and Notify after the peer has been shut down.
var ErrClosed = errors.New("rpc: peer closed")

// defaultBufferSize is the default outbound channel capacity. Mirrors the
// outboundBuffer constant in auto-ui/internal/server/ws.go.
const defaultBufferSize = 16

// pingMethod is the reserved notification method used by the keepalive ping
// sender. It is swallowed by handleNotification and never forwarded to the
// application's OnNotify callback.
const pingMethod = "$keepalive"

// peerConfig accumulates option values before the Peer is fully constructed.
type peerConfig struct {
	handlers map[string]Handler
	onNotify func(Request)
	bufSize  int

	// keepAlive timing; zero means keepalive disabled (the default).
	kaInterval time.Duration
	kaTimeout  time.Duration
}

// Option configures a Peer during construction.
type Option func(*peerConfig)

// WithHandler registers a method handler during construction.
func WithHandler(method string, h Handler) Option {
	return func(c *peerConfig) { c.handlers[method] = h }
}

// WithOnNotify sets the callback for inbound notifications (requests with no id).
func WithOnNotify(fn func(Request)) Option {
	return func(c *peerConfig) { c.onNotify = fn }
}

// WithBufferSize sets the outbound write-pump channel capacity.
func WithBufferSize(n int) Option {
	return func(c *peerConfig) { c.bufSize = n }
}

// WithKeepAlive enables application-level liveness detection (opt-in; default
// off). interval is the cadence at which a reserved $keepalive notification is
// pushed to the peer; timeout is the maximum inbound silence tolerated before
// the connection is reaped (via the existing shutdown path). Both must be
// positive and interval must be strictly less than timeout; on misuse the
// option is a no-op (keepalive stays disabled), mirroring the permissive style
// of the other options.
func WithKeepAlive(interval, timeout time.Duration) Option {
	return func(c *peerConfig) {
		if interval <= 0 || timeout <= 0 || interval >= timeout {
			return
		}
		c.kaInterval = interval
		c.kaTimeout = timeout
	}
}

// Peer is a symmetric duplex JSON-RPC 2.0 endpoint over an io.ReadWriteCloser.
// A single write-pump goroutine owns all Encode calls; the read loop dispatches
// inbound requests to registered handlers and delivers responses to pending Call
// waiters. Construction via NewPeer; call Serve to run.
type Peer struct {
	conn     io.ReadWriteCloser
	methods  map[string]Handler
	onNotify func(Request)

	// out is the bounded write-pump channel. Initialized once by NewPeer so
	// that Call/Notify/enqueue can send before Serve starts (no race).
	out chan any

	// stop signals the write pump to drain remaining messages and exit.
	stop chan struct{}

	// nextID assigns monotonically-increasing numeric request ids.
	nextID atomic.Int64

	// pending tracks outstanding Call waiters keyed by id.
	pendingMu sync.Mutex
	pending   map[int64]chan Response

	// closed is the terminal-shutdown sentinel. After Close returns, every
	// subsequent Call/Notify returns ErrClosed.
	closed    chan struct{}
	closeOnce sync.Once

	// pumpDone is closed when the write pump goroutine exits.
	pumpDone chan struct{}

	// kaInterval / kaTimeout configure keepalive. Both zero means keepalive is
	// disabled (the default). When enabled, Serve starts a ping sender and a
	// read watchdog.
	kaInterval time.Duration
	kaTimeout  time.Duration

	// lastActivity is the UnixNano timestamp of the last successful Decode.
	// Written by readLoop, read by the watchdog goroutine. Initialized to the
	// Serve start time so a healthy connection isn't reaped before its first
	// frame arrives.
	lastActivity atomic.Int64
}

// NewPeer creates a Peer bound to conn. Register handlers via options or
// the Register method before calling Serve.
func NewPeer(conn io.ReadWriteCloser, opts ...Option) *Peer {
	cfg := &peerConfig{
		handlers: make(map[string]Handler),
		bufSize:  defaultBufferSize,
	}
	for _, o := range opts {
		o(cfg)
	}
	return &Peer{
		conn:       conn,
		methods:    cfg.handlers,
		onNotify:   cfg.onNotify,
		out:        make(chan any, cfg.bufSize),
		stop:       make(chan struct{}),
		pending:    make(map[int64]chan Response),
		closed:     make(chan struct{}),
		kaInterval: cfg.kaInterval,
		kaTimeout:  cfg.kaTimeout,
	}
}

// Register binds a handler to a method name. Must be called before Serve;
// the method table is read-only during dispatch.
func (p *Peer) Register(method string, h Handler) {
	p.methods[method] = h
}

// Serve runs the read loop and write pump until the connection is closed,
// ctx is cancelled, or a terminal error occurs. It blocks until shutdown
// is complete.
func (p *Peer) Serve(ctx context.Context) error {
	p.pumpDone = make(chan struct{})
	go func() {
		defer close(p.pumpDone)
		p.writePump()
	}()

	// When the serve context is cancelled, close the connection to unblock
	// the read loop's blocking Decode call. Without this, net.Pipe and
	// similar blocking readers would hang indefinitely.
	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			p.shutdown()
		case <-ctxDone:
			// readLoop exited on its own (EOF, error); no action needed.
		}
	}()

	// Keepalive (opt-in): start the ping sender and read watchdog. Both exit on
	// p.closed (which shutdown closes), so the final shutdown below always
	// releases them; kaDone is joined to guarantee no goroutine leak.
	kaDone := make(chan struct{})
	if p.kaInterval > 0 && p.kaTimeout > 0 {
		// Seed lastActivity with the start time so the watchdog doesn't reap a
		// healthy connection before its first frame arrives.
		p.lastActivity.Store(time.Now().UnixNano())
		var kaWG sync.WaitGroup
		kaWG.Add(2)
		go func() { defer kaWG.Done(); p.pingLoop(ctx) }()
		go func() { defer kaWG.Done(); p.watchdogLoop(ctx) }()
		go func() { kaWG.Wait(); close(kaDone) }()
	} else {
		close(kaDone)
	}

	// Read loop — blocks until EOF, decode error, or conn closed.
	err := p.readLoop(ctx)
	close(ctxDone)

	// Signal the write pump to drain remaining messages and exit, then wait
	// for it. This order is safe: when readLoop exits on EOF the remote closed
	// (writes fail immediately); on decode error the remote is still reading
	// (pump can write); on ctx cancel the watcher already called shutdown
	// (conn closed). The pump never blocks indefinitely in Encode.
	close(p.stop)
	<-p.pumpDone

	// Ensure terminal shutdown ran (idempotent if ctx watcher triggered it).
	p.shutdown()

	// Join the keepalive goroutines (released by shutdown closing p.closed).
	<-kaDone

	return err
}

// pingLoop pushes a $keepalive notification every kaInterval so the remote's
// read watchdog stays satisfied. It mirrors the WebSocket ping model in
// auto-ui/internal/server/ws.go: a dedicated ticker goroutine using the
// drop-on-full enqueue (a stalled writer closes the conn rather than blocking).
// Exits on p.closed or ctx cancellation.
func (p *Peer) pingLoop(ctx context.Context) {
	t := time.NewTicker(p.kaInterval)
	defer t.Stop()
	for {
		select {
		case <-p.closed:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			if !p.enqueue(&Request{JSONRPC: "2.0", Method: pingMethod}) {
				return
			}
		}
	}
}

// watchdogLoop reaps a half-open connection: if no inbound frame has been
// decoded within kaTimeout, it calls shutdown (closing the conn, which unblocks
// readLoop's Decode and releases all pending callers). It ticks at ~kaTimeout/3
// so detection happens within a bounded multiple of the timeout. Exits on
// p.closed or ctx cancellation.
func (p *Peer) watchdogLoop(ctx context.Context) {
	tick := p.kaTimeout / 3
	if tick <= 0 {
		tick = p.kaTimeout
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-p.closed:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			if time.Now().UnixNano()-p.lastActivity.Load() > int64(p.kaTimeout) {
				p.shutdown()
				return
			}
		}
	}
}

// readLoop decodes inbound frames and dispatches them. Returns on the first
// terminal condition: EOF, decode error, or ctx cancellation (which closes
// the conn, unblocking Decode).
func (p *Peer) readLoop(ctx context.Context) error {
	dec := NewDecoder(p.conn)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			// ParseError for malformed JSON: enqueue error response with null id.
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				resp := &Response{
					JSONRPC: "2.0",
					ID:      json.RawMessage("null"),
					Error:   &Error{Code: ParseError, Message: "parse error"},
				}
				p.enqueue(resp)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}

		// Stamp liveness after every successful Decode. Any inbound frame —
		// including a remote $keepalive ping — proves the connection is alive
		// and resets the watchdog's silence window.
		p.lastActivity.Store(time.Now().UnixNano())

		kind, classErr := classify(raw)
		if classErr != nil {
			// Invalid envelope -> InvalidRequest with null id.
			resp := &Response{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error:   &Error{Code: InvalidRequest, Message: "invalid request"},
			}
			p.enqueue(resp)
			continue
		}

		switch kind {
		case kindRequest:
			go p.handleRequest(ctx, raw)
		case kindNotification:
			p.handleNotification(raw)
		case kindResponse:
			p.handleResponse(raw)
		}
	}
}

// handleRequest dispatches an inbound request to the matching handler and
// enqueues the response via the write pump.
func (p *Peer) handleRequest(ctx context.Context, raw json.RawMessage) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &Error{Code: ParseError, Message: "parse error"},
		}
		p.enqueue(resp)
		return
	}

	// Validate envelope: jsonrpc must be "2.0" and method must be a non-empty string.
	if req.JSONRPC != "2.0" {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: InvalidRequest, Message: "invalid request"},
		}
		p.enqueue(resp)
		return
	}
	if req.Method == "" {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: InvalidRequest, Message: "invalid request"},
		}
		p.enqueue(resp)
		return
	}

	h, ok := p.methods[req.Method]
	if !ok {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: MethodNotFound, Message: "method not found"},
		}
		p.enqueue(resp)
		return
	}

	result, err := h(ctx, req.Params)
	if err != nil {
		var rpcErr *Error
		if errors.As(err, &rpcErr) {
			resp := &Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   rpcErr,
			}
			p.enqueue(resp)
		} else {
			resp := &Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: InternalError, Message: "internal error"},
			}
			p.enqueue(resp)
		}
		return
	}

	resultRaw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: InternalError, Message: "internal error"},
		}
		p.enqueue(resp)
		return
	}

	resp := &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  json.RawMessage(resultRaw),
	}
	p.enqueue(resp)
}

// handleNotification delivers an inbound notification to the OnNotify callback.
// The reserved $keepalive method is swallowed here so it never reaches the
// application handler (its only effect is the lastActivity stamp in readLoop).
func (p *Peer) handleNotification(raw json.RawMessage) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return
	}
	if req.Method == pingMethod {
		return
	}
	if p.onNotify == nil {
		return
	}
	p.onNotify(req)
}

// handleResponse delivers a response to the matching pending Call waiter.
func (p *Peer) handleResponse(raw json.RawMessage) {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return
	}

	// Extract the numeric id.
	var id int64
	if err := json.Unmarshal(resp.ID, &id); err != nil {
		return // non-numeric id — no pending waiter will match
	}

	p.pendingMu.Lock()
	ch, ok := p.pending[id]
	if ok {
		delete(p.pending, id)
	}
	p.pendingMu.Unlock()

	if ok {
		// Non-blocking send: the channel is buffered (cap 1).
		select {
		case ch <- resp:
		default:
		}
	}
}

// Call sends a request and blocks until the correlated response arrives,
// ctx is cancelled, or the peer is shut down.
func (p *Peer) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	// Check for shutdown before doing anything.
	select {
	case <-p.closed:
		return nil, ErrClosed
	default:
	}

	id := p.nextID.Add(1)

	// Register the pending waiter.
	ch := make(chan Response, 1)
	p.pendingMu.Lock()
	p.pending[id] = ch
	p.pendingMu.Unlock()

	// Build and enqueue the request.
	var paramsRaw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			p.pendingMu.Lock()
			delete(p.pending, id)
			p.pendingMu.Unlock()
			return nil, err
		}
		paramsRaw = b
	}

	req := &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(mustMarshalInt64(id)),
		Method:  method,
		Params:  paramsRaw,
	}

	if !p.enqueue(req) {
		p.pendingMu.Lock()
		delete(p.pending, id)
		p.pendingMu.Unlock()
		return nil, ErrClosed
	}

	// Wait for the response. When shutdown closes the waiter channel, the
	// receive yields ok=false, which we treat as ErrClosed.
	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		// Per-call cancellation: remove only this waiter.
		p.pendingMu.Lock()
		delete(p.pending, id)
		p.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-p.closed:
		return nil, ErrClosed
	}
}

// Notify sends an id-less notification. Returns ErrClosed after shutdown.
func (p *Peer) Notify(method string, params any) error {
	select {
	case <-p.closed:
		return ErrClosed
	default:
	}

	var paramsRaw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		paramsRaw = b
	}

	notif := &Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsRaw,
	}

	if !p.enqueue(notif) {
		return ErrClosed
	}
	return nil
}

// enqueue offers a message to the write pump's bounded channel. If the buffer
// is full, the frame is dropped and the connection is closed (at-most-once/lossy
// per auto-bus-spec.md section 5 — never block the producer). Returns false when the
// message was not sent (buffer full or already shut down).
func (p *Peer) enqueue(msg any) bool {
	select {
	case <-p.closed:
		return false
	default:
	}

	select {
	case p.out <- msg:
		return true
	default:
		// Buffer full: drop and close (mirrors ws.go enqueue behavior).
		p.shutdown()
		return false
	}
}

// writePump is the sole writer goroutine. It drains the out channel and encodes
// each message to the connection. On write error it triggers shutdown. When
// stop is closed, the pump drains remaining buffered messages and exits.
func (p *Peer) writePump() {
	enc := NewEncoder(p.conn)
	for {
		select {
		case <-p.closed:
			return
		case <-p.stop:
			// Drain remaining buffered messages before exiting.
			p.drain(enc)
			return
		case msg, ok := <-p.out:
			if !ok {
				return
			}
			if err := enc.Encode(msg); err != nil {
				p.shutdown()
				return
			}
		}
	}
}

// drain writes any remaining buffered messages from the out channel.
// Called by writePump when stop is signalled. Best-effort: ignores write errors.
func (p *Peer) drain(enc *Encoder) {
	for {
		select {
		case msg := <-p.out:
			_ = enc.Encode(msg)
		default:
			return
		}
	}
}

// shutdown performs terminal shutdown: signal the closed channel, close the
// connection, and fail all pending Call waiters. Idempotent via sync.Once.
func (p *Peer) shutdown() {
	p.closeOnce.Do(func() {
		close(p.closed)
		_ = p.conn.Close()

		// Fail every pending Call waiter by closing their channels.
		p.pendingMu.Lock()
		waiters := p.pending
		p.pending = make(map[int64]chan Response)
		p.pendingMu.Unlock()

		for _, ch := range waiters {
			close(ch)
		}
	})
}

// Close triggers terminal shutdown. Idempotent.
func (p *Peer) Close() error {
	p.shutdown()
	return nil
}

// mustMarshalInt64 renders an int64 as JSON bytes. A JSON number is just the
// decimal representation of the integer, so strconv produces identical output
// to json.Marshal without an error path to ignore.
func mustMarshalInt64(n int64) []byte {
	return strconv.AppendInt(nil, n, 10)
}
