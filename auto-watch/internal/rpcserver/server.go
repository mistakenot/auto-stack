// Package rpcserver implements the RPC accept loop and HTTP hook-ingest
// endpoint for the auto-watch daemon. It accepts connections over a
// transport.Listener, wires each to an rpc.Peer with the daemon's method
// handlers, and optionally emits ctl.connect / ctl.disconnect lifecycle
// events via the bus.
package rpcserver

import (
	"context"
	"sync"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-watch/internal/rpcmethods"
)

// Server accepts RPC connections over a transport.Listener and serves each
// with a dedicated rpc.Peer goroutine.
type Server struct {
	ln        transport.Listener
	handlers  *rpcmethods.Handlers
	hub       *bus.Hub
	ctlEvents bool

	mu    sync.Mutex
	peers map[*rpc.Peer]struct{}
}

// New creates a Server that will accept connections on ln, register method
// handlers from h on each peer, and broadcast lifecycle events to hub when
// ctlEvents is true.
func New(ln transport.Listener, h *rpcmethods.Handlers, hub *bus.Hub, ctlEvents bool) *Server {
	return &Server{
		ln:        ln,
		handlers:  h,
		hub:       hub,
		ctlEvents: ctlEvents,
		peers:     make(map[*rpc.Peer]struct{}),
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

		peer := rpc.NewPeer(conn)
		s.handlers.Register(peer)

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
			_ = peer.Serve(ctx)

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
