package sessionhtml

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// sampleModel is a small hand-built tree exercising the structural markers the
// renderer must preserve: a coordinator with a bash event and a nested agent.
func sampleModel() *Node {
	return &Node{
		ID: "root-session", Title: "/execute-task 044", Model: "opus",
		DurationMs: 60000, TotalTokens: 8000, MsgCount: 9,
		Counts: Counts{Bash: 1, Agent: 1, Error: 1},
		Depth:  0,
		Events: []Event{
			{Kind: "tool", Tool: "Bash", Summary: "go test ./...", Exit: 2, IsError: true,
				Input: `{"command":"go test ./..."}`, Output: "FAIL"},
			{Kind: "agent", Summary: "explore auth", SubagentType: "Explore",
				Prompt: "Explore the auth module", Result: "Found 2 files",
				Child: &Node{ID: "child-session", SubagentName: "Explore", IsSubagent: true,
					Depth: 1, DurationMs: 10000,
					Events: []Event{{Kind: "assistant", Summary: "done", Body: "The auth module"}}},
			},
		},
	}
}

func TestRenderSelfContained(t *testing.T) {
	out, err := Render(sampleModel())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	html := string(out)

	// No external network dependencies — the file must open from file://.
	for _, re := range []string{`<script[^>]+\bsrc\s*=`, `<link[^>]+\bhref\s*=`} {
		if regexp.MustCompile(re).MatchString(html) {
			t.Errorf("rendered HTML contains external reference matching %q", re)
		}
	}
	if strings.Contains(html, "http://") || strings.Contains(html, "https://") {
		t.Error("rendered HTML contains an http(s) URL; should be fully offline")
	}

	// The injection marker must be consumed.
	if strings.Contains(html, dataMarker) {
		t.Error("injection marker still present in output")
	}
	if !strings.Contains(html, "window.__SESSION__ = ") {
		t.Error("model JSON was not injected")
	}

	// Structural markers from the model survive into the document.
	for _, want := range []string{"root-session", "child-session", "go test ./...", "Explore"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing structural marker %q", want)
		}
	}
}

// extractModelJSON pulls the JSON object out of the injected
// `window.__SESSION__ = {...};` assignment.
func extractModelJSON(t *testing.T, html string) string {
	t.Helper()
	const prefix = "window.__SESSION__ = "
	_, rest, ok := strings.Cut(html, prefix)
	if !ok {
		t.Fatal("no model assignment found")
	}
	// The assignment ends with ";\n" right before the closing </script>.
	blob, _, ok := strings.Cut(rest, ";\n")
	if !ok {
		t.Fatal("no terminator after model assignment")
	}
	return blob
}

func TestRenderScriptSafe(t *testing.T) {
	lineSep := string(rune(0x2028))
	paraSep := string(rune(0x2029))

	// Embed hostile content: a literal </script>, plus U+2028 / U+2029.
	model := &Node{
		ID: "s", Title: "t", Depth: 0,
		Events: []Event{{
			Kind:    "assistant",
			Summary: "danger",
			Body:    "before </script><script>alert(1)</script> mid" + lineSep + "line" + paraSep + "para after",
		}},
	}
	out, err := Render(model)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	html := string(out)
	jsonBlob := extractModelJSON(t, html)

	// A raw "</script>" inside the data block would prematurely close the
	// inline script; it must be escaped to <\/script> in the payload.
	if strings.Contains(jsonBlob, "</script>") {
		t.Error("escaped JSON still contains a literal </script>")
	}
	if !strings.Contains(jsonBlob, `<\/script>`) {
		t.Error("expected </ to be escaped to <\\/ in the JSON payload")
	}
	// Raw line/para separators must not survive into the JS string literal
	// (the encoder escapes them to   /  ).
	if strings.ContainsRune(jsonBlob, rune(0x2028)) || strings.ContainsRune(jsonBlob, rune(0x2029)) {
		t.Error("raw U+2028/U+2029 survived into the script payload")
	}

	// The payload must round-trip back to the original model once the only
	// non-JSON escape (<\/ -> </) is reversed;   /   are native JSON
	// escapes that json.Unmarshal handles directly.
	unescaped := strings.ReplaceAll(jsonBlob, `<\/`, "</")
	var got Node
	if err := json.Unmarshal([]byte(unescaped), &got); err != nil {
		t.Fatalf("escaped JSON is not parseable: %v", err)
	}
	if len(got.Events) != 1 || !strings.Contains(got.Events[0].Body, "alert(1)") {
		t.Errorf("round-tripped model lost content: %+v", got.Events)
	}
	if !strings.Contains(got.Events[0].Body, lineSep) || !strings.Contains(got.Events[0].Body, paraSep) {
		t.Error("round-tripped body lost the U+2028/U+2029 separators")
	}
}

func TestRenderNilModel(t *testing.T) {
	if _, err := Render(nil); err == nil {
		t.Fatal("expected error rendering nil model")
	}
}
