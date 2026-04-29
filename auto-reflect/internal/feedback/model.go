package feedback

import "time"

const (
	KindHelpful = "helpful"
	KindHarmful = "harmful"
	KindMissing = "missing"
)

type Subject struct {
	Type            string  `json:"type"`
	File            *string `json:"file,omitempty"`
	StartLine       *int    `json:"start_line,omitempty"`
	EndLine         *int    `json:"end_line,omitempty"`
	StartByte       *int    `json:"start_byte,omitempty"`
	EndByte         *int    `json:"end_byte,omitempty"`
	ContentSnippet  *string `json:"content_snippet,omitempty"`
	SnippetSHA256   *string `json:"snippet_sha256,omitempty"`
	HeadBlobSHA     *string `json:"head_blob_sha,omitempty"`
	ObservedBlobSHA *string `json:"observed_blob_sha,omitempty"`
	CaptureSource   *string `json:"capture_source,omitempty"`
	WorktreeDirty   *bool   `json:"worktree_dirty,omitempty"`
}

type Event struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Comment       string  `json:"comment"`
	Context       *string `json:"context,omitempty"`
	Timestamp     string  `json:"timestamp"`
	GitHash       string  `json:"git_hash"`
	GitTreeSHA    string  `json:"git_tree_sha"`
	GitRemote     string  `json:"git_remote"`
	WorkspaceName string  `json:"workspace_name"`
	WorkspacePath *string `json:"workspace_path,omitempty"`
	SessionID     *string `json:"session_id,omitempty"`
	Agent         *string `json:"agent,omitempty"`
	Subject       Subject `json:"subject"`
}

type AddInput struct {
	Kind    string
	Comment string
	Context string
	File    string
	Start   *int
	End     *int
}

type AddResult struct {
	Path  string `json:"path"`
	Event Event  `json:"event"`
}

type ListInput struct {
	Kind   string
	File   string
	After  *time.Time
	Before *time.Time
	Limit  int
}

type ListResult struct {
	Events []Event `json:"events"`
}
