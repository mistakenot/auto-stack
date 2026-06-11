package bus

import "encoding/json"

// PathRef carries both the repo-relative and absolute path for a file touched
// by a tool event. Rel is the stable logical identity (used for docs/** matching
// and UI grouping); Abs locates the file on the host.
type PathRef struct {
	Rel string `json:"rel"`
	Abs string `json:"abs"`
}

// ToolPost is the normalized data payload for agent.tool.post events. It
// presents a cross-tool interface (Tool, Event, Paths) alongside the agent's
// original tool fields verbatim in Raw (no fidelity loss).
type ToolPost struct {
	Tool  string          `json:"tool"`
	Event string          `json:"event"`
	Paths []PathRef       `json:"paths"`
	Raw   json.RawMessage `json:"raw,omitempty"`
}

// DocChanged is the data payload for doc.changed events derived by the hub.
type DocChanged struct {
	Project  string `json:"project"`
	Path     string `json:"path"`
	AbsPath  string `json:"abs_path"`
	Worktree string `json:"worktree"`
	Branch   string `json:"branch"`
}

// DecodeData unmarshals ev.Data into T.
func DecodeData[T any](ev Event) (T, error) {
	var v T
	if err := json.Unmarshal(ev.Data, &v); err != nil {
		return v, err
	}
	return v, nil
}
