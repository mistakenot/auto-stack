package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

// Envelope is the JSONL line format for hook events.
type Envelope struct {
	Agent      string            `json:"agent"`
	CapturedAt string            `json:"capturedAt"`
	HostID     string            `json:"hostId"`
	Cwd        string            `json:"cwd,omitempty"`
	Project    string            `json:"project,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Payload    json.RawMessage   `json:"payload"`
}

// CaptureEnv returns the NTM and tmux environment variables visible to the
// current process, keyed by their variable name (e.g. NTM_SPAWN_BATCH_ID,
// TMUX_PANE). These identify the pane and spawn batch a hook fired from, which
// lets downstream tooling correlate hook events back to the orchestrator that
// launched the agent.
//
// Capture is entirely optional: any variable that is absent is simply omitted,
// and if none are present the function returns nil so callers can leave the
// envelope's env field empty rather than emit an empty object.
func CaptureEnv() map[string]string {
	return captureEnvFrom(os.Environ())
}

// captureEnvFrom is the testable core of CaptureEnv: it filters a slice of
// "KEY=VALUE" strings down to the NTM_*/TMUX/TMUX_* variables.
func captureEnvFrom(environ []string) map[string]string {
	out := map[string]string{}
	for _, kv := range environ {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		key := kv[:i]
		if strings.HasPrefix(key, "NTM_") || key == "TMUX" || strings.HasPrefix(key, "TMUX_") {
			out[key] = kv[i+1:]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// tmuxFields is the ordered set of tmux format tokens captured to address a
// reply back to the pane that fired a hook. session/window/pane index are what
// `ntm send <session> --pane=<index>` needs; pane_id (e.g. %1) is the stable,
// rename-proof pane handle for `tmux send-keys -t`.
var tmuxFields = []struct {
	key    string // output map key
	format string // tmux #{...} format token
}{
	{"tmux_session", "#{session_name}"},
	{"tmux_window_index", "#{window_index}"},
	{"tmux_pane_index", "#{pane_index}"},
	{"tmux_pane_id", "#{pane_id}"},
}

// tmuxQueryTimeout bounds the tmux query. A hook runs in the agent's critical
// path; the local tmux socket answers in well under a millisecond, but we cap
// it so a wedged tmux server can never stall the hook.
const tmuxQueryTimeout = 200 * time.Millisecond

// tmuxRunner runs `tmux display-message -p -F <format>` for the current pane
// and returns its trimmed stdout. It is a package var so tests can stub the
// tmux server without a real tmux process.
var tmuxRunner = func(format string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxQueryTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-F", format).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// CaptureTmuxTarget resolves the tmux session and pane coordinates of the
// current pane so a future caller can address a message back to the pane that
// fired the hook (e.g. `ntm send <tmux_session> --pane=<tmux_pane_index>`).
//
// It returns nil when not running under tmux (no $TMUX) or when the tmux query
// fails for any reason: capture is best-effort and must never break the hook.
func CaptureTmuxTarget() map[string]string {
	if os.Getenv("TMUX") == "" {
		return nil
	}
	return resolveTmuxTarget(tmuxRunner)
}

// resolveTmuxTarget is the testable core of CaptureTmuxTarget: it asks run for
// every tmux field in a single tab-joined query and maps the results by key.
func resolveTmuxTarget(run func(format string) (string, error)) map[string]string {
	formats := make([]string, len(tmuxFields))
	for i, f := range tmuxFields {
		formats[i] = f.format
	}
	// One display-message call returns every field, tab-separated. tmux index
	// and session-name values do not contain tabs, so the split is unambiguous.
	out, err := run(strings.Join(formats, "\t"))
	if err != nil || out == "" {
		return nil
	}
	parts := strings.Split(out, "\t")
	if len(parts) != len(tmuxFields) {
		return nil
	}
	res := map[string]string{}
	for i, f := range tmuxFields {
		if v := strings.TrimSpace(parts[i]); v != "" {
			res[f.key] = v
		}
	}
	if len(res) == 0 {
		return nil
	}
	return res
}

// CaptureContext returns the combined orchestrator/terminal context for a hook:
// the raw NTM_*/TMUX_* environment variables (CaptureEnv) merged with the tmux
// session and pane coordinates resolved from the tmux server (CaptureTmuxTarget).
// Together these carry enough metadata to deliver a reply back to the pane that
// fired the hook. Returns nil when nothing is captured (not under tmux/ntm).
func CaptureContext() map[string]string {
	out := CaptureEnv()
	target := CaptureTmuxTarget()
	if len(target) == 0 {
		return out
	}
	if out == nil {
		out = make(map[string]string, len(target))
	}
	for k, v := range target {
		out[k] = v
	}
	return out
}

// RawDir returns <AutoDir>/hooks/raw.
func RawDir() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", fmt.Errorf("resolve hooks raw dir: %w", err)
	}
	return filepath.Join(autoDir, "hooks", "raw"), nil
}

// LogPath returns <RawDir>/events-YYYY-MM-DD.jsonl for the given time.
// The caller should pass time.Now().UTC() so the filename day equals the
// UTC CapturedAt day.
func LogPath(t time.Time) (string, error) {
	rawDir, err := RawDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rawDir, t.Format("events-2006-01-02")+".jsonl"), nil
}

// Append marshals env to JSON and appends it as a single line to the
// day-partitioned JSONL log file.
func Append(env Envelope) error {
	rawDir, err := RawDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		return fmt.Errorf("create hooks raw dir: %w", err)
	}

	logPath, err := LogPath(time.Now().UTC())
	if err != nil {
		return err
	}

	line, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal hook envelope: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open hook log %s: %w", logPath, err)
	}
	defer func() { _ = f.Close() }()

	line = append(line, '\n')
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write hook event: %w", err)
	}
	return nil
}

// stringField returns m[key] as a string, or "" if missing or wrong type.
func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}

// ExtractEventName returns payload["hook_event_name"] as a string, or "".
func ExtractEventName(payload map[string]any) string {
	return stringField(payload, "hook_event_name")
}

// ExtractSessionID returns payload["session_id"] as a string, or "".
func ExtractSessionID(payload map[string]any) string {
	return stringField(payload, "session_id")
}

// ExtractTool returns payload["tool_name"] as a string, or "".
func ExtractTool(payload map[string]any) string {
	return stringField(payload, "tool_name")
}

// ExtractPaths extracts file_path, notebook_path, and path from
// payload["tool_input"], returning them sorted and deduped.
// Returns nil if tool_input is missing or not a map.
func ExtractPaths(payload map[string]any) []string {
	if payload == nil {
		return nil
	}
	ti, ok := payload["tool_input"].(map[string]any)
	if !ok {
		return nil
	}

	seen := make(map[string]bool)
	for _, key := range []string{"file_path", "notebook_path", "path"} {
		if v, ok := ti[key].(string); ok && v != "" {
			seen[v] = true
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
