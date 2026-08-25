package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/mistakenot/auto-shared/bus"
	"gopkg.in/yaml.v3"
)

// HintRule maps a named built-in trigger to a hint template. When the trigger
// matches a PostToolUse payload, the rendered hint is injected into the agent's
// context window as additionalContext.
type HintRule struct {
	Trigger string `yaml:"trigger"`
	Hint    string `yaml:"hint"`
}

// HintsConfig is the project-scoped hint configuration loaded from
// .auto/hooks/hooks.yaml at the repository root.
type HintsConfig struct {
	Hints []HintRule `yaml:"hints"`
}

// TriggerFunc reports whether a parsed hook payload matches a built-in trigger.
// Detection logic lives in Go; users reference triggers by name in hooks.yaml.
type TriggerFunc func(payload map[string]any) bool

// triggerRegistry maps built-in trigger names to their detector. New triggers
// are added as functions and registered here.
var triggerRegistry = map[string]TriggerFunc{
	"pushed-to-pr": pushedToPR,
}

// pushedToPR fires on a PostToolUse Bash command that pushes code or opens/merges
// a PR. tool_name is "Bash" for both Claude and Codex. Failed commands are
// best-effort filtered via is_error when present; Claude's PostToolUse only fires
// on success, and Codex omits is_error, so a failed Codex push is an accepted v1
// false positive (the hint text is conditional, making it low-cost).
func pushedToPR(payload map[string]any) bool {
	if !strings.EqualFold(stringField(payload, "hook_event_name"), "PostToolUse") {
		return false
	}
	if stringField(payload, "tool_name") != "Bash" {
		return false
	}
	if isErrorTrue(payload) {
		return false
	}
	command := bashCommand(payload)
	if command == "" {
		return false
	}
	for _, pattern := range []string{"git push", "gh pr create", "gh pr merge"} {
		if strings.Contains(command, pattern) {
			return true
		}
	}
	return false
}

// bashCommand extracts tool_input.command from a Bash PostToolUse payload.
func bashCommand(payload map[string]any) string {
	input, ok := payload["tool_input"].(map[string]any)
	if !ok {
		return ""
	}
	if c, ok := input["command"].(string); ok {
		return c
	}
	return ""
}

// isErrorTrue reports whether the payload carries a top-level is_error boolean
// set to true. Absent is_error (the common case for Codex) reads as false.
func isErrorTrue(payload map[string]any) bool {
	if v, ok := payload["is_error"].(bool); ok {
		return v
	}
	return false
}

// loadHintsConfig reads .auto/hooks/hooks.yaml from root and returns the parsed
// config, or nil if the file is missing or malformed. It never returns an error:
// this runs in the agent's hot path, and a broken config must degrade silently.
func loadHintsConfig(root string) *HintsConfig {
	if root == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(root, ".auto", "hooks", "hooks.yaml"))
	if err != nil {
		return nil
	}
	var cfg HintsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// buildHintFields assembles the flat template context from the raw payload plus
// resolved provenance carried on the bus event. Missing values are empty strings.
func buildHintFields(agent string, payload map[string]any, ev bus.Event) map[string]string {
	return map[string]string{
		"agent":      agent,
		"branch":     ev.Branch,
		"remote":     ev.Remote,
		"project":    ev.Project,
		"session_id": stringField(payload, "session_id"),
		"tool":       stringField(payload, "tool_name"),
		"event":      stringField(payload, "hook_event_name"),
		"cwd":        stringField(payload, "cwd"),
	}
}

// renderHint renders a hint template against the field map. Unknown fields render
// as empty strings (missingkey=zero) rather than failing or emitting <no value>.
func renderHint(tmpl string, fields map[string]string) (string, error) {
	t, err := template.New("hint").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, fields); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// hookResponse is the stdout envelope both Claude and Codex parse for a
// PostToolUse hook response.
type hookResponse struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// matchHint checks the payload against built-in triggers and returns the
// rendered hint for the first match with a configured hint, or "" when nothing
// matches. Only PostToolUse events can produce a fragment — every other
// installed event (Stop, SessionStart, Codex SubagentStop, …) stays silent so
// we never write unexpected stdout that an agent might reject. All failures are
// swallowed: this runs in the agent's hot path and must never break it.
//
// Matching is split from emission (D-062-9) because the mail nudge is a second
// producer on the same stdout, and two JSON objects on one hook's stdout is
// undefined behaviour in both agents' hook contracts. Every fragment now goes
// through emitAdditionalContext, which writes at most one object.
func matchHint(errOut io.Writer, agent string, payload map[string]any, ev bus.Event, root string) string {
	if payload == nil {
		return ""
	}
	if !strings.EqualFold(stringField(payload, "hook_event_name"), "PostToolUse") {
		return ""
	}
	cfg := loadHintsConfig(root)
	if cfg == nil || len(cfg.Hints) == 0 {
		return ""
	}
	fields := buildHintFields(agent, payload, ev)
	for _, rule := range cfg.Hints {
		detect, ok := triggerRegistry[rule.Trigger]
		if !ok {
			fmt.Fprintf(errOut, "auto hooks fire: unknown trigger %q in hooks.yaml\n", rule.Trigger)
			continue
		}
		if !detect(payload) {
			continue
		}
		rendered, err := renderHint(rule.Hint, fields)
		if err != nil {
			continue
		}
		return rendered // at most one hint per event
	}
	return ""
}

// fragmentSeparator joins the fragments that ride one emission. A blank line
// keeps them legible as separate instructions in the agent's context window.
const fragmentSeparator = "\n\n"

// emitAdditionalContext writes **exactly one** hookSpecificOutput object
// carrying every non-empty fragment, in the order given, or nothing at all when
// every fragment is empty — zero fragments means zero bytes on stdout, exactly
// as before the split (D-062-9).
//
// Scoped to PostToolUse for the same reason each producer is: an agent may
// reject unexpected stdout on an event whose contract has no response shape.
func emitAdditionalContext(out io.Writer, event string, fragments ...string) {
	if !strings.EqualFold(event, "PostToolUse") {
		return
	}
	kept := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		if strings.TrimSpace(fragment) != "" {
			kept = append(kept, fragment)
		}
	}
	if len(kept) == 0 {
		return
	}
	body, err := json.Marshal(hookResponse{HookSpecificOutput: hookSpecificOutput{
		HookEventName:     "PostToolUse",
		AdditionalContext: strings.Join(kept, fragmentSeparator),
	}})
	if err != nil {
		return
	}
	fmt.Fprintln(out, string(body))
}
