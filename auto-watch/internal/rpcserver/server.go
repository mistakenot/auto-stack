// Package rpcserver implements the RPC accept loop and HTTP hook-ingest
// endpoint for the auto-watch daemon. It accepts connections over a
// transport.Listener, wires each to an rpc.Peer with the daemon's method
// handlers, and optionally emits ctl.connect / ctl.disconnect lifecycle
// events via the bus.
package rpcserver

import (
	"context"
	"sync"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-watch/internal/rpcmethods"
)

// Default keepalive timings applied to accepted peers. Gentle, intra-VPC
// appropriate values (15s ping / 45s reap) per the auto-bus spec; aggressive
// values risk false reaps under GC pauses / CI load.
const (
	defaultKAInterval = 15 * time.Second
	defaultKATimeout  = 45 * time.Second
)

// Server accepts RPC connections over a transport.Listener and serves each
// with a dedicated rpc.Peer goroutine.
type Server struct {
	ln        transport.Listener
	handlers  *rpcmethods.Handlers
	hub       *bus.Hub
	ctlEvents bool

	// kaInterval / kaTimeout are the keepalive timings applied to accepted
	// peers. New seeds them with the daemon defaults; tests override them with
	// short durations so a dead subscriber is reaped quickly.
	kaInterval time.Duration
	kaTimeout  time.Duration

	mu    sync.Mutex
	peers map[*rpc.Peer]struct{}
}

// New creates a Server that will accept connections on ln, register method
// handlers from h on each peer, and broadcast lifecycle events to hub when
// ctlEvents is true.
func New(ln transport.Listener, h *rpcmethods.Handlers, hub *bus.Hub, ctlEvents bool) *Server {
	return &Server{
		ln:         ln,
		handlers:   h,
		hub:        hub,
		ctlEvents:  ctlEvents,
		kaInterval: defaultKAInterval,
		kaTimeout:  defaultKATimeout,
		peers:      make(map[*rpc.Peer]struct{}),
	}
}

// Serve runs the accept loop until ctx is cancelled. It blocks until all
// accepted peer goroutines have drained.
func (s *Server) Serve(ctx context.Context) error {
	var wg sync.WaitGroup

	// When ctx is done, close the listener to unblock Accept.
	go func() {
		<-ctx.Done()
		_ = s.ln.Close()
	}()

	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				// Clean shutdown: close all tracked peers, wait for goroutines.
				s.closeAllPeers()
				wg.Wait()
				return nil
			default:
				// Fatal accept error.
				s.closeAllPeers()
				wg.Wait()
				return err
			}
		}

		peer := rpc.NewPeer(conn, rpc.WithKeepAlive(s.kaInterval, s.kaTimeout))
		s.handlers.Register(peer)
		sub := &subscription{}
		registerSubscribe(peer, s.hub, sub)

		s.mu.Lock()
		s.peers[peer] = struct{}{}
		s.mu.Unlock()

		if s.ctlEvents && s.hub != nil {
			ev, err := bus.NewEvent(bus.TypeCtlConnect, "auto/watch/daemon", nil)
			if err == nil {
				s.hub.Broadcast(ev)
			}
		}

		wg.Go(func() {
			// Safety net: guarantee the hub sink is reaped on every return
			// path, including a panic in peer.Serve. teardown is idempotent, so
			// the explicit call below (which preserves the
			// teardown-before-disconnect-broadcast ordering) makes this a no-op
			// on the normal path.
			defer sub.teardown()

			_ = peer.Serve(ctx)

			sub.teardown()

			s.mu.Lock()
			delete(s.peers, peer)
			s.mu.Unlock()

			if s.ctlEvents && s.hub != nil {
				ev, err := bus.NewEvent(bus.TypeCtlDisconnect, "auto/watch/daemon", nil)
				if err == nil {
					s.hub.Broadcast(ev)
				}
			}
		})
	}
}

// closeAllPeers closes every tracked peer under the mutex.
func (s *Server) closeAllPeers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for p := range s.peers {
		_ = p.Close()
	}
}
