package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-ui/internal/backend"
	uiconfig "github.com/mistakenot/auto-ui/internal/config"
	"github.com/mistakenot/auto-ui/internal/server"
)

// startAggBackend wires sConn to an rpc.Peer reporting hostID from daemon.status
// and returning the given project.list. NOTE: project.list here is a BARE array
// ([]any{...}) — the shape the aggregator merges — NOT {"projects":[...]}.
func startAggBackend(t *testing.T, hostID string, sConn net.Conn, projects []any) {
	t.Helper()
	peer := rpc.NewPeer(sConn,
		rpc.WithHandler("daemon.status", func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]any{
				"hostId":        hostID,
				"version":       "test",
				"uptimeSeconds": 1,
				"pid":           1,
				"startedAt":     "2026-01-01T00:00:00Z",
			}, nil
		}),
		rpc.WithHandler("project.list", func(_ context.Context, _ json.RawMessage) (any, error) {
			return projects, nil
		}),
		rpc.WithHandler("bus.subscribe", func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]string{"status": "subscribed"}, nil
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = peer.Serve(ctx) }()
}

// newAggServer stands up a real server.New wired to a backend.Manager that dials
// one in-process backend per host id in byHost, each serving a bare-array
// project.list. It reconciles and waits until every backend is connected.
func newAggServer(t *testing.T, byHost map[string][]any) *httptest.Server {
	t.Helper()

	uriToHost := map[string]string{}
	cfg := uiconfig.BackendsConfig{}
	i := 0
	for host := range byHost {
		uri := fmt.Sprintf("unix:///fake/agg-%d.sock", i)
		i++
		uriToHost[uri] = host
		cfg.Backends = append(cfg.Backends, uiconfig.Backend{URI: uri})
	}

	path := filepath.Join(t.TempDir(), "backends.json")
	if err := uiconfig.SaveBackends(path, cfg); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}

	dial := func(_ context.Context, u string) (net.Conn, error) {
		host, ok := uriToHost[u]
		if !ok {
			t.Errorf("unexpected dial uri: %s", u)
			return nil, fmt.Errorf("unknown uri %s", u)
		}
		sConn, cConn := net.Pipe()
		startAggBackend(t, host, sConn, byHost[host])
		return cConn, nil
	}

	mgr := backend.NewManager(path, dial, 0)
	mgr.Reconcile(context.Background())

	// Wait until every backend's host id is learned so the aggregator sees them.
	deadline := time.Now().Add(2 * time.Second)
	for host := range byHost {
		for {
			if _, err := mgr.Resolve(host); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("backend %q did not connect in time", host)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	srv := httptest.NewServer(server.New(newTestFS(), "test", server.WithBackendManager(mgr)))
	t.Cleanup(srv.Close)
	return srv
}

// TestProjectListAggregatesAcrossBackends covers Phase 1 of 046: a WS
// project.list call returns the UNION of every connected backend's projects,
// each tagged with its originating host (GR-F8). A project id that COLLIDES
// across two hosts ("shared") stays disambiguated — both entries are present
// with distinct host values.
func TestProjectListAggregatesAcrossBackends(t *testing.T) {
	srv := newAggServer(t, map[string][]any{
		"host-a": {
			map[string]any{"id": "shared", "name": "Shared", "path": "/a/shared", "remote": "r"},
			map[string]any{"id": "alpha", "name": "Alpha", "path": "/a/alpha", "remote": "r"},
		},
		"host-b": {
			map[string]any{"id": "shared", "name": "Shared", "path": "/b/shared", "remote": "r"},
			map[string]any{"id": "beta", "name": "Beta", "path": "/b/beta", "remote": "r"},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "project.list", map[string]string{})
	if resp["error"] != nil {
		t.Fatalf("project.list error: %v", resp["error"])
	}
	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("project.list result not an array: %T %v", resp["result"], resp["result"])
	}
	if len(result) != 4 {
		t.Fatalf("project.list returned %d entries, want 4 (union of both backends): %v", len(result), result)
	}

	type key struct{ host, id string }
	seen := map[key]bool{}
	var sharedHosts []string
	for _, e := range result {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("entry not a map: %v", e)
		}
		host, _ := m["host"].(string)
		id, _ := m["id"].(string)
		if host != "host-a" && host != "host-b" {
			t.Errorf("entry id=%q carries unexpected host %q", id, host)
		}
		seen[key{host, id}] = true
		if id == "shared" {
			sharedHosts = append(sharedHosts, host)
		}
	}

	for _, want := range []key{
		{"host-a", "shared"}, {"host-a", "alpha"},
		{"host-b", "shared"}, {"host-b", "beta"},
	} {
		if !seen[want] {
			t.Errorf("missing entry host=%q id=%q", want.host, want.id)
		}
	}

	// The colliding "shared" id appears once per host with distinct host values.
	slices.Sort(sharedHosts)
	if !slices.Equal(sharedHosts, []string{"host-a", "host-b"}) {
		t.Fatalf("'shared' entries hosts = %v, want [host-a host-b]", sharedHosts)
	}
}
