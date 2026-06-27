package rpcserver

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/rpc"
)

// peerSink implements bus.Sink by pushing events to an rpc.Peer as JSON-RPC
// notifications. Delivery is non-blocking: the peer's own bounded enqueue
// drops the frame and closes the connection when full (at-most-once).
type peerSink struct {
	peer *rpc.Peer
}

func (s *peerSink) Deliver(ev bus.Event) {
	// peer.Notify is non-blocking (drops + closes on full buffer).
	// ErrClosed means the peer is already shutting down — teardown will
	// fire the cancel.
	_ = s.peer.Notify(ev.Type, ev)
}

// subscription holds the hub cancel for a single peer connection.
// subscribe sets it once (idempotent); teardown fires it once.
type subscription struct {
	mu     sync.Mutex
	cancel func()
}

func (s *subscription) teardown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// registerSubscribe registers a peer-bound "bus.subscribe" handler on p.
// When called, the handler lazily subscribes a peerSink to the hub and stores
// the cancel in sub (idempotent — a second call is a no-op).
func registerSubscribe(p *rpc.Peer, hub *bus.Hub, sub *subscription) {
	p.Register("bus.subscribe", func(_ context.Context, _ json.RawMessage) (any, error) {
		sub.mu.Lock()
		defer sub.mu.Unlock()
		if sub.cancel == nil {
			sub.cancel = hub.Subscribe(&peerSink{peer: p})
		}
		return map[string]string{"status": "subscribed"}, nil
	})
}
