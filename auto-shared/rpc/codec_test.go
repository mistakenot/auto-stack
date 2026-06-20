package rpc

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mistakenot/auto-shared/bus"
)

// ---------------------------------------------------------------------------
// AC-2: Encode/decode round-trip for request, response, and notification
// ---------------------------------------------------------------------------

func TestRoundTripRequest(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "agent.ping",
		Params:  json.RawMessage(`{"key":"val"}`),
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	dec := NewDecoder(&buf)
	var got Request
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", got.JSONRPC)
	}
	if string(got.ID) != `1` {
		t.Errorf("id = %s, want 1", got.ID)
	}
	if got.Method != "agent.ping" {
		t.Errorf("method = %q, want agent.ping", got.Method)
	}
	if string(got.Params) != `{"key":"val"}` {
		t.Errorf("params = %s, want {\"key\":\"val\"}", got.Params)
	}
}

func TestRoundTripResponse(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`42`),
		Result:  json.RawMessage(`{"status":"ok"}`),
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(resp); err != nil {
		t.Fatalf("encode: %v", err)
	}

	dec := NewDecoder(&buf)
	var got Response
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", got.JSONRPC)
	}
	if string(got.ID) != `42` {
		t.Errorf("id = %s, want 42", got.ID)
	}
	if string(got.Result) != `{"status":"ok"}` {
		t.Errorf("result = %s, want {\"status\":\"ok\"}", got.Result)
	}
	if got.Error != nil {
		t.Errorf("error should be nil, got %+v", got.Error)
	}
}

func TestRoundTripNotification(t *testing.T) {
	notif := Request{
		JSONRPC: "2.0",
		Method:  "doc.changed",
		Params:  json.RawMessage(`{"path":"docs/plan.md"}`),
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(notif); err != nil {
		t.Fatalf("encode: %v", err)
	}

	dec := NewDecoder(&buf)
	var got Request
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", got.JSONRPC)
	}
	if got.ID != nil {
		t.Errorf("id should be nil for notification, got %s", got.ID)
	}
	if got.Method != "doc.changed" {
		t.Errorf("method = %q, want doc.changed", got.Method)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Numeric AND string id survive unchanged (json.RawMessage)
// ---------------------------------------------------------------------------

func TestNumericIDRoundTrip(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`42`),
		Method:  "test.method",
	}

	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var got Request
	if err := NewDecoder(&buf).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(got.ID) != `42` {
		t.Errorf("numeric id = %s, want 42", got.ID)
	}
}

func TestStringIDRoundTrip(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"req-abc-123"`),
		Method:  "test.method",
	}

	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var got Request
	if err := NewDecoder(&buf).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(got.ID) != `"req-abc-123"` {
		t.Errorf("string id = %s, want \"req-abc-123\"", got.ID)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Notification has no id and is exactly one newline-terminated line
// ---------------------------------------------------------------------------

func TestNotificationNoIDSingleLine(t *testing.T) {
	notif := Request{
		JSONRPC: "2.0",
		Method:  "doc.changed",
		Params:  json.RawMessage(`{"path":"docs/plan.md"}`),
	}

	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(notif); err != nil {
		t.Fatalf("encode: %v", err)
	}

	wire := buf.String()

	// Exactly one newline at the end.
	if !strings.HasSuffix(wire, "\n") {
		t.Error("NDJSON line should end with newline")
	}
	if strings.Count(wire, "\n") != 1 {
		t.Errorf("expected exactly 1 newline, got %d in %q", strings.Count(wire, "\n"), wire)
	}

	// No "id" key in the JSON.
	if strings.Contains(wire, `"id"`) {
		t.Errorf("notification should not contain \"id\" key, got %s", wire)
	}
}

// ---------------------------------------------------------------------------
// AC-2: result:null decodes to non-nil Result vs absent result -> nil Result
// ---------------------------------------------------------------------------

func TestResultNullVsAbsent(t *testing.T) {
	// result:null -> non-nil RawMessage containing the JSON literal "null".
	withNull := `{"jsonrpc":"2.0","id":1,"result":null}` + "\n"
	var resp1 Response
	if err := NewDecoder(strings.NewReader(withNull)).Decode(&resp1); err != nil {
		t.Fatalf("decode result:null: %v", err)
	}
	if resp1.Result == nil {
		t.Error("result:null should decode to non-nil json.RawMessage")
	}
	if string(resp1.Result) != "null" {
		t.Errorf("result = %s, want null", resp1.Result)
	}

	// Absent result (error response) -> nil RawMessage.
	withError := `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad"}}` + "\n"
	var resp2 Response
	if err := NewDecoder(strings.NewReader(withError)).Decode(&resp2); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp2.Result != nil {
		t.Errorf("absent result should be nil, got %s", resp2.Result)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Response with BOTH result and error -> rejected by classify
// ---------------------------------------------------------------------------

func TestClassifyRejectsBothResultAndError(t *testing.T) {
	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":"ok","error":{"code":-32600,"message":"bad"}}`)
	_, err := classify(raw)
	if err == nil {
		t.Error("classify should reject a response with both result and error")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Errorf("error should mention 'both', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Response with NEITHER result nor error -> rejected by classify
// ---------------------------------------------------------------------------

func TestClassifyRejectsNeitherResultNorError(t *testing.T) {
	// A frame with id but no method, no result, no error is not valid.
	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1}`)
	_, err := classify(raw)
	if err == nil {
		t.Error("classify should reject a frame with neither method nor result/error")
	}
}

// ---------------------------------------------------------------------------
// AC-2: Error codes match the standard set mirrored from auto-ui
// ---------------------------------------------------------------------------

func TestErrorCodeConstants(t *testing.T) {
	tests := []struct {
		name string
		code int
		want int
	}{
		{"ParseError", ParseError, -32700},
		{"InvalidRequest", InvalidRequest, -32600},
		{"MethodNotFound", MethodNotFound, -32601},
		{"InvalidParams", InvalidParams, -32602},
		{"InternalError", InternalError, -32603},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.code, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AC-2: Error implements the error interface
// ---------------------------------------------------------------------------

func TestErrorInterface(t *testing.T) {
	e := &Error{Code: InternalError, Message: "something broke"}
	var err error = e // compile-time check
	if err.Error() != "something broke" {
		t.Errorf("Error() = %q, want \"something broke\"", err.Error())
	}
}

// ---------------------------------------------------------------------------
// AC-2: classify correctly identifies request, notification, response
// ---------------------------------------------------------------------------

func TestClassifyRequest(t *testing.T) {
	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"test.ping"}`)
	kind, err := classify(raw)
	if err != nil {
		t.Fatalf("classify request: %v", err)
	}
	if kind != kindRequest {
		t.Errorf("kind = %d, want kindRequest (%d)", kind, kindRequest)
	}
}

func TestClassifyNotification(t *testing.T) {
	raw := json.RawMessage(`{"jsonrpc":"2.0","method":"doc.changed","params":{}}`)
	kind, err := classify(raw)
	if err != nil {
		t.Fatalf("classify notification: %v", err)
	}
	if kind != kindNotification {
		t.Errorf("kind = %d, want kindNotification (%d)", kind, kindNotification)
	}
}

func TestClassifyResponse(t *testing.T) {
	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":"ok"}`)
	kind, err := classify(raw)
	if err != nil {
		t.Fatalf("classify response: %v", err)
	}
	if kind != kindResponse {
		t.Errorf("kind = %d, want kindResponse (%d)", kind, kindResponse)
	}
}

func TestClassifyErrorResponse(t *testing.T) {
	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad"}}`)
	kind, err := classify(raw)
	if err != nil {
		t.Fatalf("classify error response: %v", err)
	}
	if kind != kindResponse {
		t.Errorf("kind = %d, want kindResponse (%d)", kind, kindResponse)
	}
}

func TestClassifyRejectsMissingJSONRPC(t *testing.T) {
	raw := json.RawMessage(`{"id":1,"method":"test.ping"}`)
	_, err := classify(raw)
	if err == nil {
		t.Error("classify should reject frame without jsonrpc field")
	}
}

func TestClassifyRejectsWrongVersion(t *testing.T) {
	raw := json.RawMessage(`{"jsonrpc":"1.0","id":1,"method":"test.ping"}`)
	_, err := classify(raw)
	if err == nil {
		t.Error("classify should reject jsonrpc != 2.0")
	}
}

func TestClassifyRejectsMethodWithResult(t *testing.T) {
	raw := json.RawMessage(`{"jsonrpc":"2.0","method":"test.ping","result":"ok"}`)
	_, err := classify(raw)
	if err == nil {
		t.Error("classify should reject frame with both method and result")
	}
}

func TestClassifyRejectsNonStringMethod(t *testing.T) {
	raw := json.RawMessage(`{"jsonrpc":"2.0","method":42}`)
	_, err := classify(raw)
	if err == nil {
		t.Error("classify should reject non-string method")
	}
}

// ---------------------------------------------------------------------------
// AC-2: Multiple frames in a single stream (NDJSON)
// ---------------------------------------------------------------------------

func TestMultipleFramesInStream(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	frames := []Request{
		{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "a.ping"},
		{JSONRPC: "2.0", Method: "b.notify"},
		{JSONRPC: "2.0", ID: json.RawMessage(`"x"`), Method: "c.call", Params: json.RawMessage(`{}`)},
	}
	for _, f := range frames {
		if err := enc.Encode(f); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}

	dec := NewDecoder(&buf)
	for i, want := range frames {
		var got Request
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		if got.Method != want.Method {
			t.Errorf("frame %d: method = %q, want %q", i, got.Method, want.Method)
		}
	}
}

// ---------------------------------------------------------------------------
// AC-5: bus.Event -> AsNotification() -> rpc codec round-trip
// ---------------------------------------------------------------------------

func TestBusEventNotificationRoundTrip(t *testing.T) {
	// Create a bus.Event with Host set (task 028 required field).
	ev, err := bus.NewEvent("doc.changed", "auto/bus/test", map[string]string{"path": "docs/plan.md"})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	ev.Host = "test-host-abc"

	// Convert to notification using the bus package's AsNotification().
	notif := ev.AsNotification()

	// Marshal to JSON (simulating what a transport layer would do).
	wire, err := json.Marshal(notif)
	if err != nil {
		t.Fatalf("marshal notification: %v", err)
	}

	// Decode through the rpc codec as a notification (Request with no ID).
	dec := NewDecoder(bytes.NewReader(append(wire, '\n')))
	var got Request
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Assert method == the event type.
	if got.Method != "doc.changed" {
		t.Errorf("method = %q, want doc.changed", got.Method)
	}

	// Assert this is a notification (no id).
	if got.ID != nil {
		t.Errorf("notification should have no id, got %s", got.ID)
	}

	// Assert params contains the full envelope including Host with no field loss.
	var params bus.Event
	if err := json.Unmarshal(got.Params, &params); err != nil {
		t.Fatalf("unmarshal params as bus.Event: %v", err)
	}

	if params.Host != "test-host-abc" {
		t.Errorf("params.Host = %q, want test-host-abc", params.Host)
	}
	if params.Type != "doc.changed" {
		t.Errorf("params.Type = %q, want doc.changed", params.Type)
	}
	if params.Source != "auto/bus/test" {
		t.Errorf("params.Source = %q, want auto/bus/test", params.Source)
	}
	if params.SpecVersion != bus.SpecVersion {
		t.Errorf("params.SpecVersion = %q, want %q", params.SpecVersion, bus.SpecVersion)
	}
	if params.ID == "" {
		t.Error("params.ID should be set")
	}
	if params.Time == "" {
		t.Error("params.Time should be set")
	}

	// Verify data payload survived.
	var data map[string]string
	if err := json.Unmarshal(params.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data["path"] != "docs/plan.md" {
		t.Errorf("data.path = %q, want docs/plan.md", data["path"])
	}

	// Classify the raw wire bytes as a notification.
	kind, err := classify(json.RawMessage(wire))
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if kind != kindNotification {
		t.Errorf("classify = %d, want kindNotification (%d)", kind, kindNotification)
	}
}

// TestBusEventNotificationNoFieldLoss verifies that all bus.Event fields
// (including optional ones like Project, Session, Remote, Branch, Worktree,
// Commit, Env) survive the rpc codec round-trip.
func TestBusEventNotificationNoFieldLoss(t *testing.T) {
	ev, err := bus.NewEvent("watch.task.started", "auto/watch", nil)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	ev.Host = "prod-host"
	ev.Project = "auto-stack"
	ev.Session = "sess-001"
	ev.Remote = "git@github.com:mistakenot/auto-stack.git"
	ev.Branch = "feat/transport"
	ev.Worktree = "/home/user/auto-stack-wt"
	ev.Commit = "abcdef12"
	ev.Env = map[string]string{"tmux_session": "main", "ntm_pane": "0"}

	notif := ev.AsNotification()
	wire, err := json.Marshal(notif)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	dec := NewDecoder(bytes.NewReader(append(wire, '\n')))
	var got Request
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var params bus.Event
	if err := json.Unmarshal(got.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"Host", params.Host, "prod-host"},
		{"Project", params.Project, "auto-stack"},
		{"Session", params.Session, "sess-001"},
		{"Remote", params.Remote, "git@github.com:mistakenot/auto-stack.git"},
		{"Branch", params.Branch, "feat/transport"},
		{"Worktree", params.Worktree, "/home/user/auto-stack-wt"},
		{"Commit", params.Commit, "abcdef12"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}

	if params.Env["tmux_session"] != "main" || params.Env["ntm_pane"] != "0" {
		t.Errorf("Env round-trip lost data: %v", params.Env)
	}
}
