package rpcserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
)

const testHostID = "test-daemon-host"

func emptyRegProvider() config.ProjectsConfig {
	return config.ProjectsConfig{Projects: []config.ProjectRef{}}
}

func registeredRegProvider(projectID, projectPath string) func() config.ProjectsConfig {
	return func() config.ProjectsConfig {
		return config.ProjectsConfig{Projects: []config.ProjectRef{
			{ID: projectID, Path: projectPath},
		}}
	}
}

// ingestSink collects events for ingest test assertions.
type ingestSink struct {
	mu     sync.Mutex
	events []bus.Event
}

func (s *ingestSink) Deliver(ev bus.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *ingestSink) snapshot() []bus.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]bus.Event, len(s.events))
	copy(out, s.events)
	return out
}

func validNotificationBody(t *testing.T) string {
	t.Helper()
	ev, err := bus.NewEvent("agent.tool.post", "test/source", nil)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	frame := bus.Notification{JSONRPC: "2.0", Method: ev.Type, Params: ev}
	b, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(b)
}

func TestHookIngest_ValidFrame_204(t *testing.T) {
	hub := bus.NewHub()
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	body := validNotificationBody(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestHookIngest_InvalidJSON_400(t *testing.T) {
	hub := bus.NewHub()
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var resp struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error body: %v", err)
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700", resp.Error.Code)
	}
}

func TestHookIngest_MissingJSONRPC_400(t *testing.T) {
	hub := bus.NewHub()
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	ev, _ := bus.NewEvent("agent.tool.post", "test/source", nil)
	frame := map[string]any{
		"method": ev.Type,
		"params": ev,
	}
	body, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHookIngest_ValidationFailure_400(t *testing.T) {
	hub := bus.NewHub()
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	// Event with missing required fields (empty type will fail Validate).
	frame := map[string]any{
		"jsonrpc": "2.0",
		"method":  "test.method",
		"params": map[string]any{
			"specversion": "1.0",
			"type":        "", // empty type
			"source":      "test",
			"id":          "abc123",
			"time":        time.Now().UTC().Format(time.RFC3339),
			"host":        "test-host",
		},
	}
	body, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var resp struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", resp.Error.Code)
	}
}

func TestHookIngest_GET_405(t *testing.T) {
	hub := bus.NewHub()
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	req := httptest.NewRequest(http.MethodGet, "/api/rpc", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHookIngest_NonLoopback_403(t *testing.T) {
	hub := bus.NewHub()
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	body := validNotificationBody(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHookIngest_IPv6Loopback_204(t *testing.T) {
	hub := bus.NewHub()
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	body := validNotificationBody(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "[::1]:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestHookIngest_CtlEventsTrue_EmitsEvent(t *testing.T) {
	hub := bus.NewHub()
	sink := &ingestSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	handler := HookIngest(hub, testHostID, emptyRegProvider, true) // ctlEvents=true

	body := validNotificationBody(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	// The hub should have received the broadcast event + the ctl event.
	events := sink.snapshot()
	var hasCtlLog bool
	for _, ev := range events {
		if ev.Type == bus.TypeCtlLogInfo {
			var data bus.CtlLogEvent
			if err := json.Unmarshal(ev.Data, &data); err == nil && data.Op == "hook.ingested" {
				hasCtlLog = true
			}
		}
	}
	if !hasCtlLog {
		t.Error("expected ctl.log.info event with op=hook.ingested, got none")
	}
}

func TestHookIngest_CtlEventsFalse_NoEvents(t *testing.T) {
	hub := bus.NewHub()
	sink := &ingestSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	handler := HookIngest(hub, testHostID, emptyRegProvider, false) // ctlEvents=false

	body := validNotificationBody(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	events := sink.snapshot()
	for _, ev := range events {
		if ev.Type == bus.TypeCtlLogInfo {
			t.Errorf("unexpected ctl.log.info event with ctlEvents=false")
		}
	}
}

func TestHookIngest_BroadcastsEvent(t *testing.T) {
	hub := bus.NewHub()
	sink := &ingestSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	body := validNotificationBody(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	events := sink.snapshot()
	var hasAgentEvent bool
	for _, ev := range events {
		if ev.Type == "agent.tool.post" {
			hasAgentEvent = true
		}
	}
	if !hasAgentEvent {
		t.Error("expected broadcast of agent.tool.post event, got none")
	}
}

func TestHookIngest_OriginHeader_403(t *testing.T) {
	hub := bus.NewHub()
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	body := validNotificationBody(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example.com")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHookIngest_WrongContentType_415(t *testing.T) {
	hub := bus.NewHub()
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	body := validNotificationBody(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHookIngest_NoContentType_415(t *testing.T) {
	hub := bus.NewHub()
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	body := validNotificationBody(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHookIngest_DeriveDocChanged_RegisteredProject(t *testing.T) {
	hub := bus.NewHub()
	sink := &ingestSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	regProvider := registeredRegProvider("my-project", "/home/user/my-project")
	handler := HookIngest(hub, testHostID, regProvider, false)

	// Build an agent.tool.post event with paths in docs/
	tp := bus.ToolPost{
		Tool:  "Write",
		Event: "write",
		Paths: []bus.PathRef{
			{Rel: "docs/foo.md", Abs: "/home/user/my-project/docs/foo.md"},
		},
	}
	ev, err := bus.NewEvent("agent.tool.post", "test/source", tp)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	ev.Project = "my-project"

	frame := bus.Notification{JSONRPC: "2.0", Method: ev.Type, Params: ev}
	body, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	// Should have 2 events: the ingested agent.tool.post + derived doc.changed
	events := sink.snapshot()
	var ingested, derived []bus.Event
	for _, e := range events {
		switch e.Type {
		case "agent.tool.post":
			ingested = append(ingested, e)
		case "doc.changed":
			derived = append(derived, e)
		}
	}

	if len(ingested) != 1 {
		t.Fatalf("expected 1 ingested event, got %d", len(ingested))
	}
	if len(derived) != 1 {
		t.Fatalf("expected 1 derived doc.changed event, got %d", len(derived))
	}

	// Both must carry the daemon hostId (overwrite-always)
	if ingested[0].Host != testHostID {
		t.Errorf("ingested Host = %q, want %q", ingested[0].Host, testHostID)
	}
	if derived[0].Host != testHostID {
		t.Errorf("derived Host = %q, want %q", derived[0].Host, testHostID)
	}

	// Derived event should carry project
	if derived[0].Project != "my-project" {
		t.Errorf("derived Project = %q, want %q", derived[0].Project, "my-project")
	}

	// Derived doc.changed must carry the cleaned doc path in its data payload.
	// This is the params.data.path shape pin that auto-ui's deleted
	// rpc_ingest_test.go (TestRPCIngestBroadcastAndDerive) used to guard; with
	// the local ingest gone (047), autowatch is the sole derive site, so the
	// pin lives here.
	dc, err := bus.DecodeData[bus.DocChanged](derived[0])
	if err != nil {
		t.Fatalf("decode derived DocChanged: %v", err)
	}
	if dc.Path != "docs/foo.md" {
		t.Errorf("derived data.path = %q, want %q", dc.Path, "docs/foo.md")
	}
}

func TestHookIngest_UnregisteredProject_NoDerived(t *testing.T) {
	hub := bus.NewHub()
	sink := &ingestSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	// Empty registry — no projects registered
	handler := HookIngest(hub, testHostID, emptyRegProvider, false)

	tp := bus.ToolPost{
		Tool:  "Write",
		Event: "write",
		Paths: []bus.PathRef{
			{Rel: "docs/foo.md", Abs: "/tmp/docs/foo.md"},
		},
	}
	ev, err := bus.NewEvent("agent.tool.post", "test/source", tp)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	ev.Project = "unknown-project"

	frame := bus.Notification{JSONRPC: "2.0", Method: ev.Type, Params: ev}
	body, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	events := sink.snapshot()
	for _, e := range events {
		if e.Type == "doc.changed" {
			t.Error("unexpected doc.changed for unregistered project")
		}
	}

	// But the ingested event should still be there with the daemon hostId
	var hasIngested bool
	for _, e := range events {
		if e.Type == "agent.tool.post" {
			hasIngested = true
			if e.Host != testHostID {
				t.Errorf("ingested Host = %q, want %q", e.Host, testHostID)
			}
		}
	}
	if !hasIngested {
		t.Error("expected ingested agent.tool.post event")
	}
}

func TestHookIngest_NonDocPath_NoDerived(t *testing.T) {
	hub := bus.NewHub()
	sink := &ingestSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	regProvider := registeredRegProvider("my-project", "/home/user/my-project")
	handler := HookIngest(hub, testHostID, regProvider, false)

	tp := bus.ToolPost{
		Tool:  "Write",
		Event: "write",
		Paths: []bus.PathRef{
			{Rel: "src/main.go", Abs: "/home/user/my-project/src/main.go"},
		},
	}
	ev, err := bus.NewEvent("agent.tool.post", "test/source", tp)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	ev.Project = "my-project"

	frame := bus.Notification{JSONRPC: "2.0", Method: ev.Type, Params: ev}
	body, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	events := sink.snapshot()
	for _, e := range events {
		if e.Type == "doc.changed" {
			t.Error("unexpected doc.changed for non-doc path")
		}
	}
}
