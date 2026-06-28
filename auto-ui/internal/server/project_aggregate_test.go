package server_test

import (
	"context"
	"encoding/json"
	"errors"
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

// startAggBackendErr wires sConn to a peer that connects normally (daemon.status
// succeeds, so it joins ConnectedPeers) but whose project.list ERRORS. The
// aggregator must SKIP it and still return the healthy backend's projects —
// partial results, never a top-level failure.
func startAggBackendErr(t *testing.T, hostID string, sConn net.Conn) {
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
			return nil, errors.New("backend down")
		}),
		rpc.WithHandler("bus.subscribe", func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]string{"status": "subscribed"}, nil
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = peer.Serve(ctx) }()
}

// TestProjectListPartialResultsOnBackendError (AC-2): with two connected
// backends where ONE's project.list errors, the WS project.list returns ONLY the
// healthy backend's projects, with NO top-level error and the errored host absent
// — partial results, never a whole-list failure.
func TestProjectListPartialResultsOnBackendError(t *testing.T) {
	const (
		uriA = "unix:///fake/agg-ok.sock"
		uriB = "unix:///fake/agg-err.sock"
	)
	uriToHost := map[string]string{uriA: "host-a", uriB: "host-b"}

	path := filepath.Join(t.TempDir(), "backends.json")
	cfg := uiconfig.BackendsConfig{Backends: []uiconfig.Backend{{URI: uriA}, {URI: uriB}}}
	if err := uiconfig.SaveBackends(path, cfg); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}

	healthy := []any{
		map[string]any{"id": "alpha", "name": "Alpha", "path": "/a/alpha", "remote": "r"},
	}
	dial := func(_ context.Context, u string) (net.Conn, error) {
		host, ok := uriToHost[u]
		if !ok {
			t.Errorf("unexpected dial uri: %s", u)
			return nil, fmt.Errorf("unknown uri %s", u)
		}
		sConn, cConn := net.Pipe()
		if u == uriB {
			startAggBackendErr(t, host, sConn) // host-b: project.list errors
		} else {
			startAggBackend(t, host, sConn, healthy) // host-a: healthy
		}
		return cConn, nil
	}

	mgr := backend.NewManager(path, dial, 0)
	mgr.Reconcile(context.Background())

	// BOTH backends connect (daemon.status succeeds on both): host-b's failure is
	// confined to project.list, so it is in ConnectedPeers but contributes nothing.
	deadline := time.Now().Add(2 * time.Second)
	for _, host := range []string{"host-a", "host-b"} {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "project.list", map[string]string{})
	if resp["error"] != nil {
		t.Fatalf("project.list returned a top-level error despite a healthy backend: %v", resp["error"])
	}
	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("project.list result not an array: %T %v", resp["result"], resp["result"])
	}
	if len(result) != 1 {
		t.Fatalf("project.list returned %d entries, want 1 (healthy backend only): %v", len(result), result)
	}
	m, ok := result[0].(map[string]any)
	if !ok {
		t.Fatalf("entry not a map: %v", result[0])
	}
	if m["host"] != "host-a" || m["id"] != "alpha" {
		t.Fatalf("entry = %v, want host=host-a id=alpha", m)
	}
	for _, e := range result {
		em, _ := e.(map[string]any)
		if em["host"] == "host-b" {
			t.Fatalf("errored host-b project leaked into partial result: %v", em)
		}
	}
}

// TestProjectListZeroBackends (AC-2): a server whose Manager has no connected
// backend returns an empty array for project.list, never an error.
func TestProjectListZeroBackends(t *testing.T) {
	// An empty byHost map configures zero backends: Reconcile dials nothing, so
	// ConnectedPeers is empty and the aggregator yields an empty (non-nil) slice.
	srv := newAggServer(t, map[string][]any{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "project.list", map[string]string{})
	if resp["error"] != nil {
		t.Fatalf("project.list errored with zero backends: %v", resp["error"])
	}
	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("project.list result not an array: %T %v", resp["result"], resp["result"])
	}
	if len(result) != 0 {
		t.Fatalf("project.list with zero backends returned %d entries, want 0: %v", len(result), result)
	}
}
