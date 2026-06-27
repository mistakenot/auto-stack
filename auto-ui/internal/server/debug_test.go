package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-ui/internal/server"
)

// TestDebugRecentEnabled verifies D-3: with WithDebug(true), events arriving via
// the backend relay (auto-ui's only ingest path post-047) are recorded into the
// debug ring and retrievable via GET /api/debug/recent. auto-ui no longer
// ingests or derives locally, so the ring reflects exactly what the relay
// delivers — here a single agent.tool.post the (fake) backend broadcasts.
func TestDebugRecentEnabled(t *testing.T) {
	const uri = "unix:///fake/a.sock"
	srv, fleet := newRelayServer(t, map[string]string{uri: "host-a"},
		server.WithDebug(true),
		// Registry is no longer used for local derivation, but wiring it keeps
		// the relay server shaped like production (registered project context).
		server.WithRegistryProvider(func() config.ProjectsConfig { return testRegistry() }),
	)

	// A realistic agent.tool.post (already ingested+stamped upstream by
	// autowatch) relayed from the backend. Build it with the shared fixture and
	// pin a known id so we can find it in the ring.
	ev := validToolPostEvent(t, "docs/tasks/test.md")
	ev.ID = "evt-debug"
	ev.Host = "host-a"
	fleet.backendFor("host-a").broadcast(ev)

	// The relay is asynchronous (backend → manager read loop → sink → ring), so
	// poll until the event lands in /api/debug/recent.
	waitTrue(t, "relayed event never recorded in debug ring", func() bool {
		getResp, err := http.Get(srv.URL + "/api/debug/recent")
		if err != nil {
			t.Fatalf("GET /api/debug/recent: %v", err)
		}
		defer getResp.Body.Close()
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("GET status = %d, want %d", getResp.StatusCode, http.StatusOK)
		}
		var events []bus.Event
		if err := json.NewDecoder(getResp.Body).Decode(&events); err != nil {
			t.Fatalf("decode events: %v", err)
		}
		for _, e := range events {
			if e.ID == "evt-debug" && e.Type == "agent.tool.post" {
				return true
			}
		}
		return false
	})
}

// TestDebugRecentDisabled verifies that without WithDebug, GET /api/debug/recent
// returns 404.
func TestDebugRecentDisabled(t *testing.T) {
	handler := server.New(newTestFS(), "test")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/debug/recent")
	if err != nil {
		t.Fatalf("GET /api/debug/recent: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}
