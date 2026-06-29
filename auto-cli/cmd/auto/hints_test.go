package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/hooks"
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

// TestBuildHintFields (AC-3) verifies every documented field is assembled from
// the raw payload plus resolved provenance on the bus event.
func TestBuildHintFields(t *testing.T) {
	payload := map[string]any{
		"session_id":      "sess-abc123",
		"tool_name":       "Bash",
		"hook_event_name": "PostToolUse",
		"cwd":             "/home/user/repo",
	}
	ev := bus.Event{Branch: "feat/login", Remote: "github.com/org/repo", Project: "my-app"}
	got := buildHintFields("claude", payload, ev)
	want := map[string]string{
		"agent":      "claude",
		"branch":     "feat/login",
		"remote":     "github.com/org/repo",
		"project":    "my-app",
		"session_id": "sess-abc123",
		"tool":       "Bash",
		"event":      "PostToolUse",
		"cwd":        "/home/user/repo",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("field %q = %q, want %q", k, got[k], v)
		}
	}
}

// TestFireNoHintOnIsError (AC-2) verifies a failed command (is_error true)
// suppresses the hint even when the trigger pattern would otherwise match.
func TestFireNoHintOnIsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:0")
	repo := initGitRepo(t, "feat/login")
	writeHooksConfig(t, repo, validHooksConfig)

	payload := toJSON(map[string]any{
		"hook_event_name": "PostToolUse",
		"cwd":             repo,
		"tool_name":       "Bash",
		"is_error":        true,
		"tool_input":      map[string]any{"command": "git push -u origin HEAD"},
	})
	if out := runFire(t, "claude", payload); strings.TrimSpace(out) != "" {
		t.Errorf("expected no hint on is_error, got: %q", out)
	}
}

// TestFireNoHintMissingConfig (AC-4) verifies a project with no hooks.yaml emits
// nothing and exits 0.
func TestFireNoHintMissingConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:0")
	repo := initGitRepo(t, "feat/login")
	// No writeHooksConfig.
	if out := runFire(t, "claude", bashPushPayload(repo)); strings.TrimSpace(out) != "" {
		t.Errorf("expected no hint without config, got: %q", out)
	}
}

// TestFireNoHintMalformedConfig (AC-5) verifies invalid YAML degrades silently.
func TestFireNoHintMalformedConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:0")
	repo := initGitRepo(t, "feat/login")
	writeHooksConfig(t, repo, "hints:\n  - trigger: pushed-to-pr\n   hint: bad indent: [unclosed\n")
	if out := runFire(t, "claude", bashPushPayload(repo)); strings.TrimSpace(out) != "" {
		t.Errorf("expected no hint on malformed config, got: %q", out)
	}
}

// TestFireNoHintNonMatchingEvents (AC-6) verifies only PostToolUse Bash with a
// matching command emits — Edit/Read PostToolUse and SessionStart/Stop stay silent.
func TestFireNoHintNonMatchingEvents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:0")
	repo := initGitRepo(t, "feat/login")
	writeHooksConfig(t, repo, validHooksConfig)

	cases := map[string]map[string]any{
		"PostToolUse Edit": {
			"hook_event_name": "PostToolUse", "cwd": repo, "tool_name": "Edit",
			"tool_input": map[string]any{"file_path": "/x/y.go"},
		},
		"PostToolUse Read": {
			"hook_event_name": "PostToolUse", "cwd": repo, "tool_name": "Read",
			"tool_input": map[string]any{"file_path": "/x/y.go"},
		},
		"SessionStart": {"hook_event_name": "SessionStart", "cwd": repo},
		"Stop":         {"hook_event_name": "Stop", "cwd": repo},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if out := runFire(t, "claude", toJSON(payload)); strings.TrimSpace(out) != "" {
				t.Errorf("expected no hint for %s, got: %q", name, out)
			}
		})
	}
}

// TestFireEmitsHintCodex (AC-7) verifies Codex gets the same hint JSON format.
func TestFireEmitsHintCodex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:0")
	repo := initGitRepo(t, "feat/login")
	writeHooksConfig(t, repo, validHooksConfig)

	resp := decodeHint(t, runFire(t, "codex", bashPushPayload(repo)))
	if resp.HookSpecificOutput.HookEventName != "PostToolUse" {
		t.Errorf("hookEventName = %q, want PostToolUse", resp.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(resp.HookSpecificOutput.AdditionalContext, "feat/login") {
		t.Errorf("additionalContext = %q, want branch interpolated", resp.HookSpecificOutput.AdditionalContext)
	}
}

// TestFireUnknownTriggerSilent (phase 2 hardening) verifies an unknown trigger
// name in config emits no hint and writes a stderr warning.
func TestFireUnknownTriggerSilent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:0")
	repo := initGitRepo(t, "feat/login")
	writeHooksConfig(t, repo, "hints:\n  - trigger: no-such-trigger\n    hint: nope\n")

	cmd := newHooksFireCmd()
	cmd.SetArgs([]string{"--agent", "claude"})
	cmd.SetIn(strings.NewReader(bashPushPayload(repo)))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("fire returned error: %v", err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("expected no hint for unknown trigger, got: %q", out.String())
	}
	if !strings.Contains(errBuf.String(), "unknown trigger") {
		t.Errorf("expected stderr warning about unknown trigger, got: %q", errBuf.String())
	}
}

// TestFireHintAlongsideLogAndPost (AC-8) verifies hint emission is additive: the
// durable JSONL append and the autowatch POST still happen.
func TestFireHintAlongsideLogAndPost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "feat/login")
	writeHooksConfig(t, repo, validHooksConfig)

	posted := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case posted <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	t.Setenv("AUTO_WATCH_HOOK_ADDR", u.Host)

	// Hint still emitted.
	resp := decodeHint(t, runFire(t, "claude", bashPushPayload(repo)))
	if resp.HookSpecificOutput.AdditionalContext == "" {
		t.Error("expected hint emitted alongside log+post")
	}

	// POST still fired.
	select {
	case <-posted:
	default:
		t.Error("expected autowatch POST to still fire alongside hint")
	}

	// Durable log still written.
	rawDir, err := hooks.RawDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(rawDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 durable log file, got %d (err=%v)", len(entries), err)
	}
}

// validHooksConfig is a minimal valid config used across edge-case tests.
const validHooksConfig = "hints:\n  - trigger: pushed-to-pr\n    hint: >-\n      Pushed to {{.branch}} in {{.project}}. Check for review feedback.\n"

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
