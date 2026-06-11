package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

// Envelope is the JSONL line format for hook events.
type Envelope struct {
	Agent      string          `json:"agent"`
	CapturedAt string          `json:"capturedAt"`
	HostID     string          `json:"hostId"`
	Cwd        string          `json:"cwd,omitempty"`
	Project    string          `json:"project,omitempty"`
	Payload    json.RawMessage `json:"payload"`
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
