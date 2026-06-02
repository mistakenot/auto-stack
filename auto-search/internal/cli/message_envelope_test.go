package cli_test

import (
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-search/internal/testutil"
)

// TestMessageDescribe_ToolUseResult verifies that `message describe` surfaces a
// structured toolUseResult object when the row carries the envelope, and omits
// the key entirely when it does not.
func TestMessageDescribe_ToolUseResult(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, _, code := runCLI(t, "init"); code != 0 {
		t.Fatal("init failed")
	}

	inputDir := filepath.Join(home, "etl-output")
	if err := testutil.GenerateAUQFixtures(inputDir); err != nil {
		t.Fatalf("GenerateAUQFixtures: %v", err)
	}
	if _, stderr, code := runCLI(t, "index", "--input", inputDir); code != 0 {
		t.Fatalf("index failed: %s", stderr)
	}

	// Envelope-bearing tool_result row: toolUseResult is present and parsed.
	stdout, stderr, code := runCLI(t, "message", "describe", "auq-msg-result")
	if code != 0 {
		t.Fatalf("message describe (result) failed: code=%d\nstderr:\n%s", code, stderr)
	}
	out := decodeJSON(t, stdout)
	msg := out["message"].(map[string]any)

	raw, ok := msg["toolUseResult"]
	if !ok {
		t.Fatalf("expected toolUseResult key on envelope-bearing row, got: %s", stdout)
	}
	// Must be a parsed JSON object, not a quoted string.
	envelope, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("toolUseResult should be a structured object, got %T: %v", raw, raw)
	}
	answers, ok := envelope["answers"].(map[string]any)
	if !ok {
		t.Fatalf("expected answers object in toolUseResult, got: %v", envelope["answers"])
	}
	if answers["Which database should we use?"] != "Postgres (Recommended)" {
		t.Errorf("answer = %v, want \"Postgres (Recommended)\"", answers["Which database should we use?"])
	}
	annotations, ok := envelope["annotations"].(map[string]any)
	if !ok {
		t.Fatalf("expected annotations object in toolUseResult, got: %v", envelope["annotations"])
	}
	q := annotations["Which database should we use?"].(map[string]any)
	if q["notes"] != "prefer managed instance" {
		t.Errorf("notes = %v, want \"prefer managed instance\"", q["notes"])
	}

	// Envelope-empty assistant tool_use row: key is omitted entirely.
	stdout, stderr, code = runCLI(t, "message", "describe", "auq-msg-use")
	if code != 0 {
		t.Fatalf("message describe (use) failed: code=%d\nstderr:\n%s", code, stderr)
	}
	out = decodeJSON(t, stdout)
	msg = out["message"].(map[string]any)
	if _, ok := msg["toolUseResult"]; ok {
		t.Errorf("toolUseResult key should be omitted on envelope-empty row, got: %s", stdout)
	}
}
