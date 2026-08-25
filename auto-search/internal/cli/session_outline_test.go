package cli_test

import (
	"regexp"
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

// TestSessionOutlineIsBodiesFree pins AC-3: the default view carries structure,
// Message ids and breadcrumbs — never a body.
func TestSessionOutlineIsBodiesFree(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "outline", "test-session-1")
	if code != 0 {
		t.Fatalf("session outline failed: code=%d\nstderr:\n%s", code, stderr)
	}

	// Leaves carry a bounded one-line summary, never a body. msg-003's body
	// is ~5 KB and ends with a distinctive marker that must not appear.
	if strings.Contains(stdout, "End of long content.") {
		t.Fatalf("outline leaked a message body:\n%s", stdout)
	}
	full, _, code := runCLI(t, "message", "get", "msg-003")
	if code != 0 {
		t.Fatal("message get msg-003 failed")
	}
	if strings.Contains(stdout, full) {
		t.Fatalf("outline embedded msg-003's full body")
	}
	if strings.Contains(stdout, "\"content\"") {
		t.Fatalf("outline emitted a content field without --expand:\n%s", stdout)
	}

	outline := decodeJSON(t, stdout)["outline"].(map[string]any)
	seg := outline["segments"].([]any)[0].(map[string]any)

	ids, ok := seg["message_ids"].([]any)
	if !ok || len(ids) == 0 {
		t.Fatalf("segment missing message_ids:\n%s", stdout)
	}

	wantExpand := "auto search session outline test-session-1 --expand " + seg["id"].(string)
	if seg["expand"] != wantExpand {
		t.Fatalf("segment expand = %v, want %q", seg["expand"], wantExpand)
	}

	// Leaf tier of the breadcrumb contract: a leaf points at `message get`.
	leaves := seg["leaves"].([]any)
	if len(leaves) == 0 {
		t.Fatalf("segment missing leaves:\n%s", stdout)
	}
	get, _ := leaves[0].(map[string]any)["get"].(string)
	if !strings.HasPrefix(get, "auto search message get ") {
		t.Fatalf("leaf get = %q, want a `message get` breadcrumb", get)
	}
}

// TestSessionOutlineDepth pins AC-4: sub-agents collapse at the default depth
// and open up at --depth 2, with no persisted state between the two runs.
func TestSessionOutlineDepth(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "outline", "test-session-1")
	if code != 0 {
		t.Fatalf("session outline failed: code=%d\nstderr:\n%s", code, stderr)
	}
	child := decodeJSON(t, stdout)["outline"].(map[string]any)["children"].([]any)[0].(map[string]any)
	if child["collapsed"] != true {
		t.Fatalf("child should be collapsed at the default depth:\n%s", stdout)
	}
	if _, ok := child["segments"]; ok {
		t.Fatalf("collapsed child should carry no segments:\n%s", stdout)
	}
	if child["expand"] != "auto search session outline test-session-1 --depth 2" {
		t.Fatalf("child expand = %v, want the --depth 2 breadcrumb", child["expand"])
	}

	stdout, stderr, code = runCLI(t, "session", "outline", "test-session-1", "--depth", "2")
	if code != 0 {
		t.Fatalf("session outline --depth 2 failed: code=%d\nstderr:\n%s", code, stderr)
	}
	child = decodeJSON(t, stdout)["outline"].(map[string]any)["children"].([]any)[0].(map[string]any)
	if _, ok := child["collapsed"]; ok {
		t.Fatalf("child should be expanded at --depth 2:\n%s", stdout)
	}
	segs, ok := child["segments"].([]any)
	if !ok || len(segs) == 0 {
		t.Fatalf("--depth 2 should reveal the sub-agent's segments:\n%s", stdout)
	}
	segID := segs[0].(map[string]any)["id"].(string)
	if !strings.HasPrefix(segID, "test-session-2#s") {
		t.Fatalf("sub-agent segment id = %q, want a test-session-2#s<n> id", segID)
	}
}

// TestSessionOutlineExpandSegment pins AC-4's full-fidelity contract: an
// expanded body is byte-identical to what `message get` prints.
func TestSessionOutlineExpandSegment(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "outline", "test-session-1")
	if code != 0 {
		t.Fatalf("session outline failed: code=%d\nstderr:\n%s", code, stderr)
	}
	outline := decodeJSON(t, stdout)["outline"].(map[string]any)
	segments := outline["segments"].([]any)

	// Find the segment holding msg-003, the long assistant body.
	var segID, wantMsgID string
	for _, s := range segments {
		seg := s.(map[string]any)
		for _, id := range seg["message_ids"].([]any) {
			if id.(string) == "msg-003" {
				segID, wantMsgID = seg["id"].(string), "msg-003"
			}
		}
	}
	if segID == "" {
		t.Fatalf("no segment carries msg-003:\n%s", stdout)
	}

	stdout, stderr, code = runCLI(t, "session", "outline", "test-session-1", "--expand", segID)
	if code != 0 {
		t.Fatalf("session outline --expand failed: code=%d\nstderr:\n%s", code, stderr)
	}
	expanded, ok := decodeJSON(t, stdout)["outline"].(map[string]any)["expanded"].(map[string]any)
	if !ok {
		t.Fatalf("--expand produced no expanded block:\n%s", stdout)
	}
	if expanded["kind"] != "segment" || expanded["id"] != segID {
		t.Fatalf("expanded = %v, want kind=segment id=%s", expanded, segID)
	}

	var body string
	for _, m := range expanded["messages"].([]any) {
		msg := m.(map[string]any)
		if msg["id"] == wantMsgID {
			body = msg["content"].(string)
		}
	}
	if body == "" {
		t.Fatalf("expanded segment missing %s:\n%s", wantMsgID, stdout)
	}

	canonical, _, code := runCLI(t, "message", "get", wantMsgID)
	if code != 0 {
		t.Fatalf("message get %s failed", wantMsgID)
	}
	if body != canonical {
		t.Fatalf("expanded body differs from `message get %s`", wantMsgID)
	}
}

// TestSessionOutlineExpandMessage checks the second --expand form: a bare
// Message id.
func TestSessionOutlineExpandMessage(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "outline", "test-session-1", "--expand", "msg-006")
	if code != 0 {
		t.Fatalf("session outline --expand msg-006 failed: code=%d\nstderr:\n%s", code, stderr)
	}
	expanded := decodeJSON(t, stdout)["outline"].(map[string]any)["expanded"].(map[string]any)
	if expanded["kind"] != "message" {
		t.Fatalf("expanded.kind = %v, want message", expanded["kind"])
	}
	msgs := expanded["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["id"] != "msg-006" {
		t.Fatalf("expected exactly msg-006, got:\n%s", stdout)
	}
}

func TestSessionOutlineIsDeterministic(t *testing.T) {
	setupIndexedFixtures(t)

	first, _, code := runCLI(t, "session", "outline", "test-session-1", "--request-id", "det")
	if code != 0 {
		t.Fatal("session outline failed")
	}
	second, _, code := runCLI(t, "session", "outline", "test-session-1", "--request-id", "det")
	if code != 0 {
		t.Fatal("session outline failed")
	}
	// elapsed_ms is the one legitimately varying field; strip it before
	// comparing so the rest must match byte for byte.
	if stripElapsed(first) != stripElapsed(second) {
		t.Fatalf("outline output is not stable across runs:\n%s\n%s", first, second)
	}
}

var elapsedRE = regexp.MustCompile(`"elapsed_ms": \d+`)

func stripElapsed(s string) string {
	return elapsedRE.ReplaceAllString(s, `"elapsed_ms": 0`)
}

func TestSessionOutlineUnknownSession(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "outline", "no-such-session")
	if code == 0 {
		t.Fatal("expected non-zero exit for an unknown session")
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "session not found") {
		t.Errorf("expected 'session not found' remediation, got:\n%s", stderr)
	}
}

func TestSessionOutlineMissingIndex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No init/index run — the index database does not exist.
	stdout, stderr, code := runCLI(t, "session", "outline", "test-session-1")
	if code == 0 {
		t.Fatal("expected non-zero exit when index is missing")
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "auto search index") {
		t.Errorf("expected index remediation hint, got:\n%s", stderr)
	}
}

func TestSessionOutlineBadExpandID(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "outline", "test-session-1", "--expand", "test-session-1#s99")
	if code == 0 {
		t.Fatal("expected non-zero exit for an unknown --expand id")
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "auto search session outline") {
		t.Errorf("expected a remediation hint naming the outline command, got:\n%s", stderr)
	}
}

func TestSessionOutlineBadDepth(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "outline", "test-session-1", "--depth", "0")
	if code == 0 {
		t.Fatal("expected non-zero exit for --depth 0")
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "--depth") {
		t.Errorf("expected a --depth usage error, got:\n%s", stderr)
	}
}
