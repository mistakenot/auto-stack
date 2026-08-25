// Package sessionhtml builds a nested work-graph model of a coding-agent
// session (coordinator + recursively-nested sub-agents) and renders it into a
// single self-contained HTML viewer.
//
// The package splits cleanly into a pure model builder (build.go) and a pure
// renderer (render.go), both unit-testable without a browser. The model JSON
// field names match the embedded template's window.__SESSION__ contract.
package sessionhtml

// Options are the build-time knobs derived from the CLI flags.
type Options struct {
	// IncludeThinking embeds role=thinking messages. Default true; the
	// --exclude-thinking and --light flags turn it off.
	IncludeThinking bool
	// Light shrinks the export: it selects the index's pre-truncated bodies
	// (content_truncated) instead of full content and excludes thinking.
	Light bool
}

// Counts holds per-node event tallies surfaced in the header and sidebar.
type Counts struct {
	Bash  int `json:"bash"`
	File  int `json:"file"`
	Tool  int `json:"tool"`
	Agent int `json:"agent"`
	Skill int `json:"skill"`
	Error int `json:"error"`
}

// Event is one timeline entry. A single struct covers every Kind
// (user|assistant|thinking|tool|agent); irrelevant fields stay zero and are
// omitted from JSON. Agent events carry a nested Child node.
type Event struct {
	Kind    string `json:"kind"`
	Idx     int    `json:"idx"`
	Summary string `json:"summary"`
	MID     string `json:"mid,omitempty"`

	// Ts is the message timestamp in epoch milliseconds. Carried so
	// downstream consumers (the outline segmenter) can cut on wall-clock
	// gaps without re-reading the message rows.
	Ts int64 `json:"ts,omitempty"`

	// Prose bodies (user / assistant / thinking).
	Body      string `json:"body,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	OutTokens int    `json:"out_tokens,omitempty"`

	// Tool events.
	Tool        string `json:"tool,omitempty"`
	Input       string `json:"input,omitempty"`
	InputTrunc  bool   `json:"input_trunc,omitempty"`
	Output      string `json:"output,omitempty"`
	OutputTrunc bool   `json:"output_trunc,omitempty"`
	Duration    int64  `json:"duration,omitempty"`
	IsError     bool   `json:"is_error,omitempty"`
	// ResultMID is the message_id of the paired tool_result row — the row
	// that actually holds the tool output. MID points at the tool_use row,
	// whose content is empty, so full-fidelity expansion of a tool event
	// must go through ResultMID.
	ResultMID   string `json:"result_mid,omitempty"`
	Interrupted bool   `json:"interrupted,omitempty"`
	Exit        int    `json:"exit,omitempty"`

	// Agent dispatch events.
	SubagentType string `json:"subagent_type,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	PromptTrunc  bool   `json:"prompt_trunc,omitempty"`
	Result       string `json:"result,omitempty"`
	ResultTrunc  bool   `json:"result_trunc,omitempty"`
	Child        *Node  `json:"child,omitempty"`
}

// Node is one session in the work-graph tree (the coordinator at depth 0, each
// sub-agent a Child of its dispatching Agent event).
type Node struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Intent        string  `json:"intent"`
	SubagentName  string  `json:"subagent_name"`
	DispatchLabel string  `json:"dispatch_label"`
	IsSubagent    bool    `json:"is_subagent"`
	Workspace     string  `json:"workspace"`
	GitRemote     string  `json:"git_remote"`
	Model         string  `json:"model"`
	FirstMs       int64   `json:"first_ms"`
	LastMs        int64   `json:"last_ms"`
	DurationMs    int64   `json:"duration_ms"`
	TotalTokens   int64   `json:"total_tokens"`
	MsgCount      int     `json:"msg_count"`
	Counts        Counts  `json:"counts"`
	Depth         int     `json:"depth"`
	Events        []Event `json:"events"`
}
