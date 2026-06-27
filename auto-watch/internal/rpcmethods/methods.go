// Package rpcmethods registers transport-free JSON-RPC method handlers for the
// auto-watch daemon. It depends on auto-shared/rpc and auto-shared/bus but
// NEVER on auto-shared/transport — layering_test.go enforces this.
package rpcmethods

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/rpc"
)

// StatusResult is the response payload for the "daemon.status" method.
type StatusResult struct {
	HostID        string `json:"hostId"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	PID           int    `json:"pid"`
	StartedAt     string `json:"startedAt"`
}

// Handlers owns the registered RPC method handlers and their shared state.
// It implements the conformance.Observations interface via DispatchCount.
type Handlers struct {
	hostID    string
	version   string
	startedAt time.Time
	hub       *bus.Hub
	ctlEvents bool
	reg       func() config.ProjectsConfig
	counts    sync.Map // method → *atomic.Int64
}

// New creates a Handlers instance. Pass ctlEvents=true to emit ctl.log.info
// events on each dispatch; false suppresses them. The reg provider is called
// per request to get a fresh project-registry snapshot.
func New(hostID, version string, startedAt time.Time, hub *bus.Hub, ctlEvents bool, reg func() config.ProjectsConfig) *Handlers {
	return &Handlers{
		hostID:    hostID,
		version:   version,
		startedAt: startedAt,
		hub:       hub,
		ctlEvents: ctlEvents,
		reg:       reg,
	}
}

// Register binds all method handlers onto the peer. Must be called before
// Peer.Serve.
func (h *Handlers) Register(p *rpc.Peer) {
	p.Register("daemon.status", h.counted("daemon.status", h.handleStatus))
	p.Register("doc.list", h.counted("doc.list", h.handleDocList))
	p.Register("doc.get", h.counted("doc.get", h.handleDocGet))
	p.Register("doc.raw", h.counted("doc.raw", h.handleDocRaw))
	p.Register("project.list", h.counted("project.list", h.handleProjectList))
}

// DispatchCount returns the number of times the handler for method was
// dispatched. Returns 0 for untracked methods. This satisfies the
// conformance.Observations interface.
func (h *Handlers) DispatchCount(method string) int {
	v, ok := h.counts.Load(method)
	if !ok {
		return 0
	}
	return int(v.(*atomic.Int64).Load())
}

// counted wraps a handler with dispatch-counting and optional ctl event
// emission.
func (h *Handlers) counted(method string, inner rpc.Handler) rpc.Handler {
	counter := &atomic.Int64{}
	h.counts.Store(method, counter)
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		counter.Add(1)
		if h.ctlEvents && h.hub != nil {
			ev, err := bus.NewCtlLog("info", "rpc.served", "dispatched "+method, map[string]string{
				"method": method,
			})
			if err == nil {
				h.hub.Broadcast(ev)
			}
		}
		return inner(ctx, params)
	}
}

// handleStatus implements the "daemon.status" method.
func (h *Handlers) handleStatus(_ context.Context, _ json.RawMessage) (any, error) {
	return StatusResult{
		HostID:        h.hostID,
		Version:       h.version,
		UptimeSeconds: int64(time.Since(h.startedAt).Seconds()),
		PID:           os.Getpid(),
		StartedAt:     h.startedAt.UTC().Format(time.RFC3339),
	}, nil
}
