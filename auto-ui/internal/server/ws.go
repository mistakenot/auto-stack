package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/mistakenot/auto-shared/bus"
)

// pingInterval is how often the server pushes an unsolicited `ping` notification
// to each connected client (the POC's server->client push primitive).
const pingInterval = time.Second

// outboundBuffer bounds the per-connection write queue. If a client stalls and
// the buffer fills, the connection is dropped rather than letting the ticker and
// RPC responses block server-side.
const outboundBuffer = 16

// handleWSWithHub returns an http.HandlerFunc that upgrades an HTTP request to a
// WebSocket and runs the JSON-RPC session over it, using the shared dispatcher
// and hub for broadcast delivery.
//
// Same-origin is the default Accept policy in coder/websocket; the SPA is served
// from the same origin both on localhost and behind `tailscale serve`, so no
// OriginPatterns override is needed.
func handleWSWithHub(hub *bus.Hub, d *Dispatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			log.Printf("ws: accept: %v", err)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		s := newSession(c, ctx, cancel)
		unsub := hub.Subscribe(s)
		defer unsub()

		go s.writePump(ctx)
		go s.pingLoop(ctx)

		s.readLoop(ctx, d)
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
}

// session owns a single WebSocket connection. coder/websocket connections are
// not safe for concurrent writers, so EVERY outbound message — RPC responses
// and server-push notifications alike — is funnelled through `out` and written
// by the single writePump goroutine. cancel is the connection's context cancel:
// calling it unwinds writePump, pingLoop, and readLoop, then handleWS closes.
//
// session implements bus.Sink so the hub can deliver broadcast events.
type session struct {
	c      *websocket.Conn
	ctx    context.Context
	out    chan any
	cancel context.CancelFunc
	seq    atomic.Int64 // monotonic ping sequence
}

func newSession(c *websocket.Conn, ctx context.Context, cancel context.CancelFunc) *session {
	return &session{c: c, ctx: ctx, out: make(chan any, outboundBuffer), cancel: cancel}
}

// Deliver implements bus.Sink. It enqueues the event as a JSON-RPC notification
// on the session's outbound channel. If the client is too slow, the message is
// dropped (and the connection cancelled) by enqueue's non-blocking default.
func (s *session) Deliver(ev bus.Event) {
	s.enqueue(s.ctx, ev.AsNotification())
}

// enqueue offers a message to the write pump. If the buffer is full the client
// is too slow to keep up, so the connection is dropped (cancel) rather than
// letting the ticker and RPC responses block server-side. Returns false when the
// message was not — and will not be — sent (buffer full or ctx already done).
func (s *session) enqueue(ctx context.Context, msg any) bool {
	select {
	case s.out <- msg:
		return true
	case <-ctx.Done():
		return false
	default:
		s.cancel()
		return false
	}
}

// notify enqueues an id-less JSON-RPC notification (server push).
func (s *session) notify(ctx context.Context, method string, params any) bool {
	return s.enqueue(ctx, rpcRequest{JSONRPC: "2.0", Method: method, Params: mustRaw(params)})
}

// writePump is the sole writer for the connection.
func (s *session) writePump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.out:
			if err := wsjson.Write(ctx, s.c, msg); err != nil {
				s.cancel() // connection gone; unwind pingLoop/readLoop promptly
				return
			}
		}
	}
}

// pingLoop pushes a `ping` notification every pingInterval — the server->client
// push primitive the POC demonstrates.
func (s *session) pingLoop(ctx context.Context) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n := s.seq.Add(1)
			if !s.notify(ctx, "ping", map[string]any{
				"seq": n,
				"ts":  time.Now().UnixMilli(),
			}) {
				return
			}
		}
	}
}

// readLoop reads inbound JSON-RPC messages, dispatches them, and queues any
// response. It returns (ending the connection) on the first read error, which
// includes normal client close.
func (s *session) readLoop(ctx context.Context, d *Dispatcher) {
	for {
		_, data, err := s.c.Read(ctx)
		if err != nil {
			if !isCloseErr(err) {
				log.Printf("ws: read: %v", err)
			}
			return
		}
		if resp, ok := d.dispatch(ctx, data); ok {
			if !s.enqueue(ctx, resp) {
				return
			}
		}
	}
}

// mustRaw marshals params for embedding in an rpcRequest. Inputs are server-built
// maps that always marshal cleanly; on the impossible error, send null params.
func mustRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// isCloseErr reports whether err is an ordinary connection close (normal or
// going-away), which is not worth logging as an error.
func isCloseErr(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	status := websocket.CloseStatus(err)
	return status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway
}
