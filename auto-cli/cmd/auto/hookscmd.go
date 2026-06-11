package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/spf13/cobra"
)

// hookPostTimeout bounds the best-effort notification to the UI. A hook runs in
// the agent's critical path, so it must never hang: if the UI is slow or down,
// we give up quickly and exit 0.
const hookPostTimeout = 150 * time.Millisecond

// defaultUIPort mirrors auto-ui's built-in default; used when ~/.auto/ui/settings.json
// is absent or unreadable.
const defaultUIPort = 8080

// maxHookPayloadBytes bounds how much of stdin we read. Real hook payloads are a
// few KB; this is generous headroom while still guarding against a runaway pipe.
const maxHookPayloadBytes = 1 << 20 // 1 MiB

// HookEvent is the normalized, agent-agnostic event the UI consumes. It is
// intentionally small: the live signal is lossy-but-fast, and the canonical
// record is reconstructed later from transcripts by auto-etl.
type HookEvent struct {
	Agent     string   `json:"agent"`
	Event     string   `json:"event,omitempty"`
	SessionID string   `json:"sessionId,omitempty"`
	Project   string   `json:"project,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
	Tool      string   `json:"tool,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	Timestamp string   `json:"ts"`
}

// newHooksCmd is the parent for agent hook adapters: `auto hooks fire`.
func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Agent hook adapters (Claude Code, Codex)",
	}
	cmd.AddCommand(newHooksFireCmd())
	return cmd
}

// newHooksFireCmd implements `auto hooks fire --agent <claude|codex>`. It reads a
// hook payload on stdin, normalizes it, maps the cwd to a registered project, and
// best-effort POSTs the event to the running auto-ui server. It ALWAYS exits 0
// for any runtime condition (bad payload, UI down) so it cannot disrupt the agent.
func newHooksFireCmd() *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "fire",
		Short: "Normalize a hook payload from stdin and notify the auto-ui server (fire-and-forget)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Invalid --agent is setup-time misconfiguration: fail fast.
			if agent != "claude" && agent != "codex" {
				return fmt.Errorf("--agent must be 'claude' or 'codex', got %q", agent)
			}

			// From here on, never return an error: a hook must not break the agent.
			// Bound the read so a runaway payload can't OOM us (hook payloads are tiny).
			raw, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxHookPayloadBytes))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "auto hooks fire: read stdin: %v\n", err)
				return nil
			}

			registry := loadRegistryQuietly()
			ev := buildHookEvent(agent, raw, registry)
			postHookEvent(uiPort(), ev)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent that fired the hook: claude or codex")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

// buildHookEvent normalizes a raw hook payload into a HookEvent. Parsing is
// lenient: unknown or missing fields are tolerated. The cwd is resolved to a
// registered project id via the registry when possible.
func buildHookEvent(agent string, raw []byte, registry sharedconfig.ProjectsConfig) HookEvent {
	ev := HookEvent{Agent: agent, Timestamp: time.Now().UTC().Format(time.RFC3339)}

	var payload map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &payload) // tolerate non-JSON / partial payloads
	}

	ev.Event = stringField(payload, "hook_event_name")
	ev.SessionID = stringField(payload, "session_id")
	ev.Tool = stringField(payload, "tool_name")
	ev.Cwd = stringField(payload, "cwd")
	if ev.Cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			ev.Cwd = wd
		}
	}
	ev.Paths = extractPaths(payload)

	if ev.Cwd != "" {
		if p := registry.FindProjectByPath(ev.Cwd); p != nil {
			ev.Project = p.ID
		}
	}
	return ev
}

// extractPaths pulls file paths a tool touched out of the payload's tool_input,
// so the UI knows which document changed. Best-effort over the common keys.
func extractPaths(payload map[string]any) []string {
	input, ok := payload["tool_input"].(map[string]any)
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	for _, key := range []string{"file_path", "notebook_path", "path"} {
		if v, ok := input[key].(string); ok && v != "" {
			seen[v] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func stringField(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

// loadRegistryQuietly returns the project registry, or an empty one if it is
// absent or unreadable — never an error (this runs in the agent's hot path).
func loadRegistryQuietly() sharedconfig.ProjectsConfig {
	path, err := sharedconfig.ProjectsConfigPath()
	if err != nil {
		return sharedconfig.ProjectsConfig{}
	}
	if _, err := os.Stat(path); err != nil {
		return sharedconfig.ProjectsConfig{}
	}
	cfg, err := sharedconfig.LoadProjects(path)
	if err != nil {
		return sharedconfig.ProjectsConfig{}
	}
	return cfg
}

// uiPort reads the configured auto-ui port from ~/.auto/ui/settings.json,
// falling back to the built-in default when unavailable.
func uiPort() int {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return defaultUIPort
	}
	var settings struct {
		Port int `json:"port"`
	}
	if err := sharedconfig.DecodeJSONFile(filepath.Join(autoDir, "ui", "settings.json"), &settings); err != nil {
		return defaultUIPort
	}
	if settings.Port <= 0 {
		return defaultUIPort
	}
	return settings.Port
}

// postHookEvent best-effort POSTs the event to the auto-ui server on loopback.
// All failures (UI down, timeout, marshal) are swallowed: the live channel is
// optional, and the hook must not delay or fail the agent.
func postHookEvent(port int, ev HookEvent) {
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookPostTimeout)
	defer cancel()
	url := fmt.Sprintf("http://127.0.0.1:%d/api/hooks", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
