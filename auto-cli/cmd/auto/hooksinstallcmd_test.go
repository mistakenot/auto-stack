package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readJSONTree reads a JSON file from disk into a generic map[string]any tree.
func readJSONTree(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

// fireHandlerCount counts handlers with type=="command" and command==target
// across every group registered on the given event in a parsed config tree.
func fireHandlerCount(t *testing.T, doc map[string]any, event, target string) int {
	t.Helper()
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		return 0
	}
	groups, ok := hooks[event].([]any)
	if !ok {
		return 0
	}
	count := 0
	for _, g := range groups {
		group, ok := g.(map[string]any)
		if !ok {
			continue
		}
		handlers, ok := group["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range handlers {
			handler, ok := h.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := handler["type"].(string)
			cmd, _ := handler["command"].(string)
			if typ == "command" && cmd == target {
				count++
			}
		}
	}
	return count
}

// runInstall executes `auto hooks install` and returns its captured stdout.
func runInstall(t *testing.T) string {
	t.Helper()
	cmd := newHooksInstallCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hooks install: %v", err)
	}
	return buf.String()
}

// TestInstallWritesBothAgents covers AC-1 and AC-4: a single install wires the
// fire command onto every documented event of both agents in their respective
// project-local config files.
func TestInstallWritesBothAgents(t *testing.T) {
	repo := t.TempDir()
	gitInTest(t, repo, "init")
	t.Chdir(repo)

	runInstall(t)

	claudePath := filepath.Join(repo, ".claude", "settings.json")
	if _, err := os.Stat(claudePath); err != nil {
		t.Fatalf("expected %s to exist: %v", claudePath, err)
	}
	claudeDoc := readJSONTree(t, claudePath)
	for _, event := range claudeHookEvents {
		if got := fireHandlerCount(t, claudeDoc, event, "auto hooks fire --agent claude"); got != 1 {
			t.Errorf("claude event %q: fire handler count = %d, want 1", event, got)
		}
	}

	codexPath := filepath.Join(repo, ".codex", "hooks.json")
	if _, err := os.Stat(codexPath); err != nil {
		t.Fatalf("expected %s to exist: %v", codexPath, err)
	}
	codexDoc := readJSONTree(t, codexPath)
	for _, event := range codexHookEvents {
		if got := fireHandlerCount(t, codexDoc, event, "auto hooks fire --agent codex"); got != 1 {
			t.Errorf("codex event %q: fire handler count = %d, want 1", event, got)
		}
	}
}

// TestInstallPreservesExistingKeysAndHooks covers AC-2: pre-existing top-level
// keys and pre-existing hook handlers (including extra handler fields) survive
// the merge, and the fire handler is added alongside them.
func TestInstallPreservesExistingKeysAndHooks(t *testing.T) {
	repo := t.TempDir()
	gitInTest(t, repo, "init")
	t.Chdir(repo)

	claudeDir := filepath.Join(repo, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	claudePath := filepath.Join(claudeDir, "settings.json")
	seed := `{"env":{"GOMEMLIMIT":"1GiB"},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo existing","statusMessage":"hi","timeout":30,"args":["x"],"if":"Bash(git *)"}]}]}}`
	if err := os.WriteFile(claudePath, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed settings.json: %v", err)
	}

	runInstall(t)

	doc := readJSONTree(t, claudePath)

	// (a) env survives.
	env, ok := doc["env"].(map[string]any)
	if !ok {
		t.Fatalf("env key did not survive merge: %#v", doc["env"])
	}
	if env["GOMEMLIMIT"] != "1GiB" {
		t.Errorf("env.GOMEMLIMIT = %#v, want \"1GiB\"", env["GOMEMLIMIT"])
	}

	// (b) the echo existing handler is retained with all four extra fields.
	hooks := doc["hooks"].(map[string]any)
	groups, ok := hooks["Stop"].([]any)
	if !ok {
		t.Fatalf("Stop event missing or wrong type: %#v", hooks["Stop"])
	}
	// (c) Stop ends up with 2 groups: the original + the fire group.
	if len(groups) != 2 {
		t.Fatalf("Stop group count = %d, want 2 (existing + fire)", len(groups))
	}

	var existingHandler map[string]any
	for _, g := range groups {
		group := g.(map[string]any)
		handlers, _ := group["hooks"].([]any)
		for _, h := range handlers {
			handler := h.(map[string]any)
			if handler["command"] == "echo existing" {
				existingHandler = handler
			}
		}
	}
	if existingHandler == nil {
		t.Fatal("echo existing handler was dropped by the merge")
	}
	if existingHandler["statusMessage"] != "hi" {
		t.Errorf("statusMessage = %#v, want \"hi\"", existingHandler["statusMessage"])
	}
	// JSON numbers decode to float64.
	if existingHandler["timeout"] != float64(30) {
		t.Errorf("timeout = %#v, want 30", existingHandler["timeout"])
	}
	if existingHandler["if"] != "Bash(git *)" {
		t.Errorf("if = %#v, want \"Bash(git *)\"", existingHandler["if"])
	}
	args, ok := existingHandler["args"].([]any)
	if !ok || len(args) != 1 || args[0] != "x" {
		t.Errorf("args = %#v, want [\"x\"]", existingHandler["args"])
	}

	// (c) the fire handler is added alongside the existing one on Stop.
	if got := fireHandlerCount(t, doc, "Stop", "auto hooks fire --agent claude"); got != 1 {
		t.Errorf("Stop fire handler count = %d, want 1", got)
	}
}

// TestInstallIdempotent covers AC-3: running install twice produces no
// duplicate fire handlers.
func TestInstallIdempotent(t *testing.T) {
	repo := t.TempDir()
	gitInTest(t, repo, "init")
	t.Chdir(repo)

	runInstall(t)
	runInstall(t)

	claudeDoc := readJSONTree(t, filepath.Join(repo, ".claude", "settings.json"))
	for _, event := range claudeHookEvents {
		if got := fireHandlerCount(t, claudeDoc, event, "auto hooks fire --agent claude"); got != 1 {
			t.Errorf("claude event %q after 2 installs: fire handler count = %d, want 1", event, got)
		}
	}

	codexDoc := readJSONTree(t, filepath.Join(repo, ".codex", "hooks.json"))
	for _, event := range codexHookEvents {
		if got := fireHandlerCount(t, codexDoc, event, "auto hooks fire --agent codex"); got != 1 {
			t.Errorf("codex event %q after 2 installs: fire handler count = %d, want 1", event, got)
		}
	}
}

// TestInstallRejectsNonRepo verifies the command fails fast outside a git repo
// with a remediation-style message mentioning the git repository requirement.
func TestInstallRejectsNonRepo(t *testing.T) {
	dir := t.TempDir() // deliberately NOT a git repo
	t.Chdir(dir)

	cmd := newHooksInstallCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error outside a git repository, got nil")
	}
	if !strings.Contains(err.Error(), "git repository") {
		t.Errorf("error = %q, want it to mention \"git repository\"", err.Error())
	}
}

// TestInstallSummaryHasCodexTrustHint covers AC-6: the summary tells the user
// they must trust .codex/hooks.json via /hooks.
func TestInstallSummaryHasCodexTrustHint(t *testing.T) {
	repo := t.TempDir()
	gitInTest(t, repo, "init")
	t.Chdir(repo)

	out := runInstall(t)

	if !strings.Contains(out, ".codex/hooks.json") {
		t.Errorf("summary did not mention .codex/hooks.json:\n%s", out)
	}
	if !strings.Contains(out, "/hooks") {
		t.Errorf("summary did not mention the /hooks trust step:\n%s", out)
	}
}

// TestInstallAgentHooksCounts unit-tests the merge helper directly: the first
// call adds every event and reports the file created; a second call on the same
// path adds nothing and reports all events already present.
func TestInstallAgentHooksCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	events := claudeHookEvents
	command := "auto hooks fire --agent claude"

	added, existing, created, err := installAgentHooks(path, command, events)
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	if added != len(events) || existing != 0 || !created {
		t.Errorf("first install: added=%d existing=%d created=%v, want added=%d existing=0 created=true",
			added, existing, created, len(events))
	}

	added, existing, created, err = installAgentHooks(path, command, events)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if added != 0 || existing != len(events) || created {
		t.Errorf("second install: added=%d existing=%d created=%v, want added=0 existing=%d created=false",
			added, existing, created, len(events))
	}
}
