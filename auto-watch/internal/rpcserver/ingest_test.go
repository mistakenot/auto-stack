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
)

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
	handler := HookIngest(hub, false)

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
	handler := HookIngest(hub, false)

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
	handler := HookIngest(hub, false)

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
	handler := HookIngest(hub, false)

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
	handler := HookIngest(hub, false)

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
	handler := HookIngest(hub, false)

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
	handler := HookIngest(hub, false)

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

	handler := HookIngest(hub, true) // ctlEvents=true

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

	handler := HookIngest(hub, false) // ctlEvents=false

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

	handler := HookIngest(hub, false)

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
	handler := HookIngest(hub, false)

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
	handler := HookIngest(hub, false)

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
	handler := HookIngest(hub, false)

	body := validNotificationBody(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}
