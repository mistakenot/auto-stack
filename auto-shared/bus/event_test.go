package bus

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEventEnvSerialization(t *testing.T) {
	ev, err := NewEvent("agent.tool.post", "auto/hooks/claude", nil)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}

	// Omitted when empty so events without terminal context stay lean.
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"env"`) {
		t.Errorf("empty Env should be omitted, got %s", b)
	}

	// Present and round-trips when populated, and does not break validation.
	ev.Env = map[string]string{"tmux_session": "auto-stack", "tmux_pane_index": "1"}
	if errs := ev.Validate(); len(errs) != 0 {
		t.Errorf("Env should not affect validation, got %v", errs)
	}
	b, err = json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal with env: %v", err)
	}
	var got Event
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Env["tmux_session"] != "auto-stack" || got.Env["tmux_pane_index"] != "1" {
		t.Errorf("Env round-trip lost data: %v", got.Env)
	}
}

func TestNewEventSetsDefaults(t *testing.T) {
	ev, err := NewEvent("agent.tool.post", "auto/hooks/claude", map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if ev.SpecVersion != SpecVersion {
		t.Errorf("specversion = %q, want %q", ev.SpecVersion, SpecVersion)
	}
	if ev.Type != "agent.tool.post" {
		t.Errorf("type = %q, want agent.tool.post", ev.Type)
	}
	if ev.Source != "auto/hooks/claude" {
		t.Errorf("source = %q, want auto/hooks/claude", ev.Source)
	}
	if ev.ID == "" {
		t.Error("id should be set")
	}
	if ev.Time == "" {
		t.Error("time should be set")
	}
}

func TestNewEventNilData(t *testing.T) {
	ev, err := NewEvent("doc.changed", "auto/bus/derive", nil)
	if err != nil {
		t.Fatalf("NewEvent with nil data: %v", err)
	}
	if ev.Data != nil {
		t.Errorf("data should be nil, got %s", ev.Data)
	}
}

func TestValidateRequiredFields(t *testing.T) {
	tests := []struct {
		name  string
		ev    Event
		field string
	}{
		{"missing specversion", Event{Type: "agent.tool.post", Source: "s", ID: "id", Time: "2026-01-01T00:00:00Z", Host: "test-host"}, "specversion"},
		{"missing type", Event{SpecVersion: "1.0", Source: "s", ID: "id", Time: "2026-01-01T00:00:00Z", Host: "test-host"}, "type"},
		{"missing source", Event{SpecVersion: "1.0", Type: "agent.tool.post", ID: "id", Time: "2026-01-01T00:00:00Z", Host: "test-host"}, "source"},
		{"missing id", Event{SpecVersion: "1.0", Type: "agent.tool.post", Source: "s", Time: "2026-01-01T00:00:00Z", Host: "test-host"}, "id"},
		{"missing time", Event{SpecVersion: "1.0", Type: "agent.tool.post", Source: "s", ID: "id", Host: "test-host"}, "time"},
		{"missing host", Event{SpecVersion: "1.0", Type: "agent.tool.post", Source: "s", ID: "id", Time: "2026-01-01T00:00:00Z"}, "host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.ev.Validate()
			found := false
			for _, e := range errs {
				if e.Field == tt.field {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected error for field %q, got %+v", tt.field, errs)
			}
		})
	}
}

func TestValidateDottedType(t *testing.T) {
	tests := []struct {
		typ   string
		valid bool
	}{
		{"agent.tool.post", true},
		{"doc.changed", true},
		{"watch.task.started", true},
		{"agent", false},        // single segment
		{"Agent.Tool", false},   // uppercase
		{"agent_tool", false},   // underscore not dot
		{"agent..tool", false},  // empty segment
		{".agent.tool", false},  // leading dot
		{"agent.tool.", false},  // trailing dot
		{"agent.tool-x", false}, // hyphen
	}
	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			ev := Event{SpecVersion: "1.0", Type: tt.typ, Source: "s", ID: "id", Time: "2026-01-01T00:00:00Z", Host: "test-host"}
			errs := ev.Validate()
			hasTypeErr := false
			for _, e := range errs {
				if e.Field == "type" {
					hasTypeErr = true
					break
				}
			}
			if tt.valid && hasTypeErr {
				t.Errorf("type %q should be valid, got errors", tt.typ)
			}
			if !tt.valid && !hasTypeErr {
				t.Errorf("type %q should be invalid, got no error", tt.typ)
			}
		})
	}
}

func TestValidateTimeRFC3339(t *testing.T) {
	ev := Event{SpecVersion: "1.0", Type: "agent.tool.post", Source: "s", ID: "id", Time: "not-a-time", Host: "test-host"}
	errs := ev.Validate()
	found := false
	for _, e := range errs {
		if e.Field == "time" && e.Code == "format" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected format error for invalid time, got %+v", errs)
	}
}

func TestValidateValidEvent(t *testing.T) {
	ev, _ := NewEvent("agent.tool.post", "auto/hooks/claude", nil)
	if errs := ev.Validate(); len(errs) != 0 {
		t.Errorf("valid event should have no errors, got %+v", errs)
	}
}

func TestOpaqueDataRoundTrip(t *testing.T) {
	// An arbitrary nested structure should round-trip through Data.
	original := map[string]any{
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": "/repo/foo.go", "old_string": "a", "new_string": "b"},
	}
	ev, err := NewEvent("agent.tool.post", "test", original)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}

	// Marshal and unmarshal the whole event (simulates wire round-trip).
	wire, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Event
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Data should be identical.
	var got map[string]any
	if err := json.Unmarshal(decoded.Data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if got["tool_name"] != "Edit" {
		t.Errorf("tool_name = %v, want Edit", got["tool_name"])
	}
}

func TestAsNotificationShape(t *testing.T) {
	ev, _ := NewEvent("doc.changed", "auto/bus/derive", map[string]string{"path": "docs/plan.md"})
	n := ev.AsNotification()
	if n.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", n.JSONRPC)
	}
	if n.Method != "doc.changed" {
		t.Errorf("method = %q, want doc.changed", n.Method)
	}
	if n.Params.Type != "doc.changed" {
		t.Errorf("params.type = %q, want doc.changed", n.Params.Type)
	}

	// Verify JSON serialization has the expected shape.
	wire, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(wire, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"jsonrpc", "method", "params"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing key %q in notification JSON", key)
		}
	}
	// No "id" field (it's a notification, not a request).
	if _, ok := raw["id"]; ok {
		t.Error("notification should not have an id field")
	}
}

func TestTypedPayloadEncodeDecodeToolPost(t *testing.T) {
	tp := ToolPost{
		Tool:  "Edit",
		Event: "PostToolUse",
		Paths: []PathRef{{Rel: "docs/plan.md", Abs: "/repo/docs/plan.md"}},
		Raw:   json.RawMessage(`{"file_path":"/repo/docs/plan.md","old_string":"a","new_string":"b"}`),
	}
	ev, err := NewEvent("agent.tool.post", "auto/hooks/claude", tp)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}

	got, err := DecodeData[ToolPost](ev)
	if err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got.Tool != "Edit" {
		t.Errorf("tool = %q, want Edit", got.Tool)
	}
	if len(got.Paths) != 1 || got.Paths[0].Rel != "docs/plan.md" {
		t.Errorf("paths = %+v, want [{Rel:docs/plan.md Abs:/repo/docs/plan.md}]", got.Paths)
	}

	// Raw should round-trip verbatim.
	var rawData map[string]any
	if err := json.Unmarshal(got.Raw, &rawData); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if rawData["file_path"] != "/repo/docs/plan.md" {
		t.Errorf("raw file_path = %v, want /repo/docs/plan.md", rawData["file_path"])
	}
}

func TestToolPostRawNormalizedRoundTrip(t *testing.T) {
	// Simulate a wire round-trip where Raw carries agent-specific fields.
	raw := json.RawMessage(`{"tool_name":"Edit","tool_input":{"file_path":"/w/docs/x.md"}}`)
	tp := ToolPost{
		Tool:  "Edit",
		Event: "PostToolUse",
		Paths: []PathRef{{Rel: "docs/x.md", Abs: "/w/docs/x.md"}},
		Raw:   raw,
	}

	// Marshal the whole event.
	ev, _ := NewEvent("agent.tool.post", "test", tp)
	wire, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Unmarshal and decode.
	var decoded Event
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, err := DecodeData[ToolPost](decoded)
	if err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got.Tool != "Edit" || got.Paths[0].Rel != "docs/x.md" {
		t.Errorf("normalized fields mismatch: %+v", got)
	}
	// Raw should be preserved verbatim.
	if string(got.Raw) != string(raw) {
		t.Errorf("raw mismatch: got %s, want %s", got.Raw, raw)
	}
}

func TestNewEventSetsHost(t *testing.T) {
	ev, err := NewEvent("agent.tool.post", "test", nil)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if ev.Host == "" {
		t.Error("NewEvent should auto-populate Host")
	}
}

func TestHostJSONRoundTrip(t *testing.T) {
	ev, _ := NewEvent("agent.tool.post", "test", nil)
	ev.Host = "dev-box.charlie"
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"host"`) {
		t.Error("JSON should contain host key")
	}
	var got Event
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Host != "dev-box.charlie" {
		t.Errorf("Host = %q, want dev-box.charlie", got.Host)
	}
}
