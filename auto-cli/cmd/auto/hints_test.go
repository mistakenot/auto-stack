package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushedToPRDetector(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		want    bool
	}{
		{
			name:    "git push matches",
			payload: bashPayload("git push origin HEAD"),
			want:    true,
		},
		{
			name:    "git push -u matches",
			payload: bashPayload("git push -u origin feat/login"),
			want:    true,
		},
		{
			name:    "gh pr create matches",
			payload: bashPayload("gh pr create --title x"),
			want:    true,
		},
		{
			name:    "gh pr merge matches",
			payload: bashPayload("gh pr merge 42"),
			want:    true,
		},
		{
			name:    "unrelated command does not match",
			payload: bashPayload("git status"),
			want:    false,
		},
		{
			name:    "empty command does not match",
			payload: bashPayload(""),
			want:    false,
		},
		{
			name: "non-Bash tool does not match",
			payload: map[string]any{
				"hook_event_name": "PostToolUse",
				"tool_name":       "Edit",
				"tool_input":      map[string]any{"command": "git push"},
			},
			want: false,
		},
		{
			name: "non-PostToolUse event does not match",
			payload: map[string]any{
				"hook_event_name": "PreToolUse",
				"tool_name":       "Bash",
				"tool_input":      map[string]any{"command": "git push"},
			},
			want: false,
		},
		{
			name: "is_error true suppresses match",
			payload: map[string]any{
				"hook_event_name": "PostToolUse",
				"tool_name":       "Bash",
				"is_error":        true,
				"tool_input":      map[string]any{"command": "git push"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pushedToPR(tt.payload); got != tt.want {
				t.Errorf("pushedToPR() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRenderHint(t *testing.T) {
	fields := map[string]string{
		"branch":  "feat/login",
		"project": "my-app",
	}
	got, err := renderHint("pushed {{.branch}} in {{.project}}", fields)
	if err != nil {
		t.Fatalf("renderHint: %v", err)
	}
	if got != "pushed feat/login in my-app" {
		t.Errorf("renderHint = %q, want interpolated values", got)
	}

	// Unknown fields render as empty string (missingkey=zero), not <no value>.
	got, err = renderHint("a{{.nope}}b", fields)
	if err != nil {
		t.Fatalf("renderHint unknown field: %v", err)
	}
	if got != "ab" {
		t.Errorf("renderHint unknown field = %q, want %q", got, "ab")
	}
}

// TestFireEmitsHintOnGitPush is the integration test: a project with a
// hooks.yaml + a git-push PostToolUse payload produces hint JSON on stdout, with
// the branch interpolated from resolved git provenance.
func TestFireEmitsHintOnGitPush(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:0") // no daemon; post drops silently

	repo := initGitRepo(t, "feat/login")
	writeHooksConfig(t, repo, "hints:\n  - trigger: pushed-to-pr\n    hint: >-\n      Pushed to {{.branch}}. Check for review feedback.\n")

	out := runFire(t, "claude", bashPushPayload(repo))

	resp := decodeHint(t, out)
	if resp.HookSpecificOutput.HookEventName != "PostToolUse" {
		t.Errorf("hookEventName = %q, want PostToolUse", resp.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(resp.HookSpecificOutput.AdditionalContext, "feat/login") {
		t.Errorf("additionalContext = %q, want branch interpolated", resp.HookSpecificOutput.AdditionalContext)
	}
}

// --- helpers ---

func bashPayload(command string) map[string]any {
	return map[string]any{
		"hook_event_name": "PostToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": command},
	}
}

// bashPushPayload returns a raw JSON PostToolUse Bash "git push" payload with cwd
// set to repo so provenance resolves.
func bashPushPayload(repo string) string {
	return toJSON(map[string]any{
		"hook_event_name": "PostToolUse",
		"session_id":      "sess-abc123",
		"cwd":             repo,
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "git push -u origin HEAD"},
	})
}

// toJSON marshals v to a JSON string, panicking on failure (test-only input).
func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// initGitRepo creates a git repo at a temp dir on the given branch and returns
// its path. Used so buildBusEvent resolves branch provenance.
func initGitRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("checkout", "-q", "-b", branch)
	// A commit makes the branch real for rev-parse.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "init")
	return dir
}

// writeHooksConfig writes .auto/hooks/hooks.yaml under repo.
func writeHooksConfig(t *testing.T, repo, body string) {
	t.Helper()
	dir := filepath.Join(repo, ".auto", "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runFire executes `auto hooks fire --agent <agent>` with stdinJSON on stdin and
// returns captured stdout. Stderr is discarded.
func runFire(t *testing.T, agent, stdinJSON string) string {
	t.Helper()
	cmd := newHooksFireCmd()
	cmd.SetArgs([]string{"--agent", agent})
	cmd.SetIn(strings.NewReader(stdinJSON))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("fire returned error: %v", err)
	}
	return out.String()
}

// decodeHint parses stdout as a hookResponse, failing if it is empty or invalid.
func decodeHint(t *testing.T, out string) hookResponse {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected hint JSON on stdout, got empty")
	}
	var resp hookResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal hint response %q: %v", out, err)
	}
	return resp
}
