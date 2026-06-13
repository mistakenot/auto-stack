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
	"strconv"
	"strings"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	sharedconfig "github.com/mistakenot/auto-shared/config"
	sharedgit "github.com/mistakenot/auto-shared/git"
	"github.com/mistakenot/auto-shared/hooks"
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

// newHooksCmd is the parent for agent hook adapters: `auto hooks fire` (the
// runtime adapter) and `auto hooks install` (wires fire into agent config).
func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Agent hook adapters (Claude Code, Codex)",
	}
	cmd.AddCommand(newHooksFireCmd())
	cmd.AddCommand(newHooksInstallCmd())
	return cmd
}

// newHooksFireCmd implements `auto hooks fire --agent <claude|codex>`. It reads a
// hook payload on stdin, normalizes it into a bus.Event with workspace provenance,
// and best-effort POSTs the event to the running auto-ui server at /api/rpc.
// It ALWAYS exits 0 for any runtime condition (bad payload, UI down) so it cannot
// disrupt the agent.
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

			// Resolve cwd and project for the envelope.
			var payload map[string]any
			if len(bytes.TrimSpace(raw)) > 0 {
				_ = json.Unmarshal(raw, &payload)
			}
			cwd := stringField(payload, "cwd")
			if cwd == "" {
				if wd, err := os.Getwd(); err == nil {
					cwd = wd
				}
			}

			registry := loadRegistryQuietly()

			var project string
			if cwd != "" {
				if p := registry.FindProjectByPath(cwd); p != nil {
					project = p.ID
				}
			}

			// Durable append first — canonical record before the lossy live POST.
			env := hooks.Envelope{
				Agent:      agent,
				CapturedAt: time.Now().UTC().Format(time.RFC3339),
				HostID:     hostIDQuietly(),
				Cwd:        cwd,
				Project:    project,
				Payload:    json.RawMessage(raw),
			}
			if err := hooks.Append(env); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "auto hooks fire: append log: %v\n", err)
			}

			ev := buildBusEvent(agent, raw, registry)
			postBusEvent(uiPort(), ev)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent that fired the hook: claude or codex")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

// mapEventType maps a hook_event_name (and optional tool_name) to a dotted bus
// event type. The default is agent.<lower(event)>; tool events get agent.tool.post.
func mapEventType(hookEvent, tool string) string {
	lower := strings.ToLower(hookEvent)
	switch lower {
	case "posttooluse":
		if tool != "" {
			return "agent.tool.post"
		}
		return "agent.posttooluse"
	case "sessionstart":
		return "agent.session.start"
	case "sessionend", "sessionstop":
		return "agent.session.end"
	case "pretooluse":
		return "agent.tool.pre"
	default:
		if lower == "" {
			return "agent.unknown"
		}
		return "agent." + lower
	}
}

// buildBusEvent normalizes a raw hook payload into a bus.Event with workspace
// provenance. Parsing is lenient: unknown or missing fields are tolerated. Git
// provenance and project resolution are best-effort (omitted on error, never fail).
func buildBusEvent(agent string, raw []byte, registry sharedconfig.ProjectsConfig) bus.Event {
	var payload map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &payload) // tolerate non-JSON / partial payloads
	}

	hookEvent := stringField(payload, "hook_event_name")
	tool := stringField(payload, "tool_name")
	eventType := mapEventType(hookEvent, tool)
	source := "auto/hooks/" + agent

	// Build the ToolPost data payload with normalized + raw fields.
	paths := extractPathRefs(payload)
	tp := bus.ToolPost{
		Tool:  tool,
		Event: hookEvent,
		Paths: paths,
	}
	// Preserve the agent's original tool_input verbatim in Raw.
	if input, ok := payload["tool_input"]; ok {
		if rawInput, err := json.Marshal(input); err == nil {
			tp.Raw = rawInput
		}
	}

	ev, err := bus.NewEvent(eventType, source, tp)
	if err != nil {
		// Marshal failure shouldn't happen for ToolPost, but degrade gracefully.
		ev, _ = bus.NewEvent(eventType, source, nil)
	}

	// Session ID from the payload.
	ev.Session = stringField(payload, "session_id")

	// Resolve cwd from the payload, falling back to the hook process cwd.
	cwd := stringField(payload, "cwd")
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}

	// Best-effort git provenance from the payload cwd.
	if cwd != "" {
		root, branch, commit, err := sharedgit.Provenance(cwd)
		if err == nil && root != "" {
			ev.Worktree = root
			ev.Branch = branch
			ev.Commit = commit

			// Resolve paths relative to the repo root now that we have it.
			paths = resolvePathRefs(payload, cwd, root)
			tp.Paths = paths
			if newData, err := json.Marshal(tp); err == nil {
				ev.Data = newData
			}
		}

		// Normalized remote — never emit the raw origin URL (may contain a PAT).
		rawRemote, _ := sharedgit.OriginRemote(cwd)
		if rawRemote != "" {
			ev.Remote = sharedgit.NormalizeRemoteURL(rawRemote)
		}

		// Resolve project: by remote first (handles worktrees), then by path.
		if ev.Remote != "" {
			if p := registry.FindProjectByRemote(ev.Remote); p != nil {
				ev.Project = p.ID
			}
		}
		if ev.Project == "" {
			root := ev.Worktree
			if root == "" {
				root = cwd
			}
			if p := registry.FindProjectByPath(root); p != nil {
				ev.Project = p.ID
			}
		}
	}

	return ev
}

// extractPathRefs pulls file paths from the payload's tool_input as PathRef
// values. At this stage only Abs may be set (if the tool provides absolute
// paths); Rel is populated later by resolvePathRefs once the repo root is known.
func extractPathRefs(payload map[string]any) []bus.PathRef {
	input, ok := payload["tool_input"].(map[string]any)
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	var refs []bus.PathRef
	for _, key := range []string{"file_path", "notebook_path", "path"} {
		if v, ok := input[key].(string); ok && v != "" {
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			refs = append(refs, bus.PathRef{Abs: v})
		}
	}
	return refs
}

// resolvePathRefs resolves each tool path against the payload cwd and computes
// Rel from the repo root. Claude's file_path is usually absolute; Codex may
// provide relative paths — both are handled.
func resolvePathRefs(payload map[string]any, cwd, root string) []bus.PathRef {
	input, ok := payload["tool_input"].(map[string]any)
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	var refs []bus.PathRef
	for _, key := range []string{"file_path", "notebook_path", "path"} {
		p, ok := input[key].(string)
		if !ok || p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}

		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(cwd, p)
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			rel = ""
		}
		refs = append(refs, bus.PathRef{Rel: rel, Abs: abs})
	}
	return refs
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

// hostIDQuietly returns the host identifier from ~/.auto/host.json, falling
// back to os.Hostname(), then "unknown". It never returns an error — this runs
// in the agent's hot path.
func hostIDQuietly() string {
	path, err := sharedconfig.HostConfigPath()
	if err == nil {
		if cfg, err := sharedconfig.LoadHost(path); err == nil {
			return cfg.HostID
		}
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "unknown"
}

// uiPort resolves the auto-ui port with precedence: AUTO_UI_PORT env >
// ~/.auto/ui/settings.json > built-in default. AUTO_UI_PORT lets an agent
// harness point hooks at an isolated server instance (e.g. one bound to an
// OS-assigned port).
func uiPort() int {
	if v := os.Getenv("AUTO_UI_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
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

// postBusEvent best-effort POSTs the event as a JSON-RPC notification to the
// auto-ui server's /api/rpc endpoint on loopback. All failures (UI down,
// timeout, marshal) are swallowed: the live channel is optional, and the hook
// must not delay or fail the agent.
func postBusEvent(port int, ev bus.Event) {
	body, err := json.Marshal(ev.AsNotification())
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookPostTimeout)
	defer cancel()
	url := fmt.Sprintf("http://127.0.0.1:%d/api/rpc", port)
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
