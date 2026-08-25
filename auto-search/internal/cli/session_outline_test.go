package cli_test

import (
	"strings"
	"testing"
)

// TestSessionOutline walks the happy path end to end over the committed
// fixtures: the JSON envelope, the root node's canonical fields, and the
// sub-agent (test-session-2) nested under its dispatching parent.
func TestSessionOutline(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "outline", "test-session-1", "--request-id", "o-1")
	if code != 0 {
		t.Fatalf("session outline failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)

	meta, ok := out["_meta"].(map[string]any)
	if !ok {
		t.Fatalf("missing _meta in output:\n%s", stdout)
	}
	if meta["request_id"] != "o-1" {
		t.Fatalf("request_id = %v, want o-1", meta["request_id"])
	}

	outline, ok := out["outline"].(map[string]any)
	if !ok {
		t.Fatalf("missing outline in output:\n%s", stdout)
	}
	if outline["session_id"] != "test-session-1" {
		t.Fatalf("outline.session_id = %v, want test-session-1", outline["session_id"])
	}
	if _, ok := outline["parent_session_id"]; !ok {
		t.Fatalf("outline.parent_session_id missing:\n%s", stdout)
	}
	if outline["is_subagent"] != false {
		t.Fatalf("outline.is_subagent = %v, want false", outline["is_subagent"])
	}

	segments, ok := outline["segments"].([]any)
	if !ok || len(segments) == 0 {
		t.Fatalf("expected at least one segment:\n%s", stdout)
	}
	seg := segments[0].(map[string]any)
	segID, _ := seg["id"].(string)
	if !strings.HasPrefix(segID, "test-session-1#s") {
		t.Fatalf("segment id = %q, want a test-session-1#s<n> id", segID)
	}
	if _, ok := seg["index_range"].([]any); !ok {
		t.Fatalf("segment missing index_range:\n%s", stdout)
	}

	children, ok := outline["children"].([]any)
	if !ok || len(children) == 0 {
		t.Fatalf("expected the Explore sub-agent to nest under the root:\n%s", stdout)
	}
	child := children[0].(map[string]any)
	if child["session_id"] != "test-session-2" {
		t.Fatalf("child.session_id = %v, want test-session-2", child["session_id"])
	}
	if child["parent_session_id"] != "test-session-1" {
		t.Fatalf("child.parent_session_id = %v, want test-session-1", child["parent_session_id"])
	}
	if child["is_subagent"] != true {
		t.Fatalf("child.is_subagent = %v, want true", child["is_subagent"])
	}
	if child["subagent_name"] != "Explore" {
		t.Fatalf("child.subagent_name = %v, want Explore", child["subagent_name"])
	}
}
