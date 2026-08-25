// Package sessionoutline builds a navigable, bodies-free outline of a coding
// session: the sub-agent spine (reused from sessionhtml's work-graph model),
// each node's timeline cut into deterministic structural Segments, and
// per-Message leaves addressable for full-fidelity expansion.
//
// It is the cheap rung between `session describe` (a peek) and `session get`
// (the whole transcript): structure, IDs and counts only, with every elided
// region carrying the exact command that recovers it.
package sessionoutline

import (
	"database/sql"
	"fmt"

	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/sessionhtml"
)

// defaultDepth renders the root's own segments plus its immediate sub-agents
// as collapsed one-liners — the cheapest useful view.
const defaultDepth = 1

// Options are the build-time knobs derived from the CLI flags.
type Options struct {
	// Depth bounds how many node levels render expanded. 1 (the default)
	// renders the root's segments plus its immediate sub-agents as collapsed
	// one-liners.
	Depth int
	// Expand names one Segment id (<session_id>#s<n>) or Message id to
	// re-emit at full fidelity. Empty means bodies-free everywhere.
	Expand string
}

// Outline is the command payload: the root OutlineNode (its fields inlined)
// plus, when --expand was used, the expanded region.
type Outline struct {
	*OutlineNode
	Expanded *Expansion `json:"expanded,omitempty"`
}

// OutlineNode is one session in the outline tree — the coordinator at depth 0,
// each sub-agent nested under the node that dispatched it.
type OutlineNode struct {
	SessionID       string `json:"session_id"`
	ParentSessionID string `json:"parent_session_id"`
	IsSubagent      bool   `json:"is_subagent"`
	SubagentName    string `json:"subagent_name,omitempty"`
	// DispatchedAtIndex is the message_index of the Agent event in the parent
	// that spawned this node. Nil on the root.
	DispatchedAtIndex *int   `json:"dispatched_at_index,omitempty"`
	Intent            string `json:"intent,omitempty"`
	Workspace         string `json:"workspace,omitempty"`
	Model             string `json:"model,omitempty"`
	Depth             int    `json:"depth"`
	MsgCount          int    `json:"msg_count"`
	DurationMs        int64  `json:"duration_ms"`
	Counts            Counts `json:"counts"`
	// Collapsed marks a node rendered as a one-liner because --depth stopped
	// short of it; its Segments and Children are omitted.
	Collapsed bool           `json:"collapsed,omitempty"`
	Segments  []Segment      `json:"segments,omitempty"`
	Children  []*OutlineNode `json:"children,omitempty"`
	// Expand is the exact command that renders this node's own structure.
	Expand string `json:"expand,omitempty"`
}

// Leaf is one addressable Message stub inside a Segment. It carries IDs and
// structural metadata only — never a body.
type Leaf struct {
	Kind    string `json:"kind"`
	Idx     int    `json:"idx"`
	Tool    string `json:"tool,omitempty"`
	Summary string `json:"summary,omitempty"`
	// MID is the tool_use / prose Message id; ResultMID the paired
	// tool_result row that actually holds a tool's output.
	MID         string `json:"mid,omitempty"`
	ResultMID   string `json:"result_mid,omitempty"`
	IsError     bool   `json:"is_error,omitempty"`
	Interrupted bool   `json:"interrupted,omitempty"`
	Exit        int    `json:"exit,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
	// Get is the exact command that prints this leaf's full body.
	Get string `json:"get"`
}

// Expansion is the full-fidelity payload returned for --expand.
type Expansion struct {
	// ID is the requested Segment or Message id; Kind is "segment" or
	// "message".
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Messages []ExpandedMessage `json:"messages"`
}

// ExpandedMessage is one Message rendered at full fidelity — the same content
// `auto search message get <id>` prints.
type ExpandedMessage struct {
	ID           string `json:"id"`
	MessageIndex int    `json:"message_index"`
	Role         string `json:"role"`
	Tool         string `json:"tool,omitempty"`
	Content      string `json:"content"`
}

// Build assembles the outline for sessionID. It returns an error when the
// session is not indexed.
func Build(db *sql.DB, sessionID string, opts Options) (*Outline, error) {
	sess, err := indexdb.GetSessionByID(db, sessionID)
	if err != nil {
		return nil, err
	}

	// Light:true selects the index's pre-truncated bodies. The outline never
	// emits a body, but the smaller read keeps the build cheap.
	model, err := sessionhtml.BuildModel(db, sessionID, sessionhtml.Options{Light: true})
	if err != nil {
		return nil, fmt.Errorf("build session model: %w", err)
	}
	if model == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	root := buildNode(model, sess.ParentSessionID, nil, sessionID)
	outline := &Outline{OutlineNode: root}

	// Resolve --expand against the full tree before --depth prunes it, so a
	// region can be expanded without first widening the depth to reach it.
	if opts.Expand != "" {
		expanded, err := expand(db, root, opts.Expand)
		if err != nil {
			return nil, err
		}
		outline.Expanded = expanded
	}

	depth := opts.Depth
	if depth < 1 {
		depth = defaultDepth
	}
	collapse(root, depth, sessionID)

	return outline, nil
}

// collapse prunes every node at or beyond maxDepth to a one-liner carrying the
// command that renders it, mirroring how `session get` prints the command that
// recovers a truncated body.
func collapse(n *OutlineNode, maxDepth int, rootID string) {
	if n.Depth >= maxDepth {
		n.Collapsed = true
		n.Segments = nil
		n.Children = nil
		n.Expand = fmt.Sprintf("auto search session outline %s --depth %d", rootID, n.Depth+1)
		return
	}
	for _, c := range n.Children {
		collapse(c, maxDepth, rootID)
	}
}

// expand resolves an --expand argument to full-fidelity Messages. It accepts
// either a Segment id (<session_id>#s<n>, expanding every Message in that
// segment) or a single Message id.
func expand(db *sql.DB, root *OutlineNode, id string) (*Expansion, error) {
	if seg := findSegment(root, id); seg != nil {
		msgs, err := loadMessages(db, seg.MessageIDs)
		if err != nil {
			return nil, err
		}
		return &Expansion{ID: id, Kind: "segment", Messages: msgs}, nil
	}

	// Not a segment in this outline — try it as a Message id.
	msgs, err := loadMessages(db, []string{id})
	if err != nil {
		return nil, fmt.Errorf("unknown --expand id %q: not a segment in this outline and not an indexed message; "+
			"run: auto search session outline %s to list segment ids", id, root.SessionID)
	}
	return &Expansion{ID: id, Kind: "message", Messages: msgs}, nil
}

// findSegment locates a segment by id anywhere in the tree.
func findSegment(n *OutlineNode, id string) *Segment {
	for i := range n.Segments {
		if n.Segments[i].ID == id {
			return &n.Segments[i]
		}
	}
	for _, c := range n.Children {
		if seg := findSegment(c, id); seg != nil {
			return seg
		}
	}
	return nil
}

// loadMessages hydrates Message ids through the canonical full-fidelity
// reader, so an expanded body is byte-identical to `message get <id>`.
func loadMessages(db *sql.DB, ids []string) ([]ExpandedMessage, error) {
	msgs := make([]ExpandedMessage, 0, len(ids))
	for _, id := range ids {
		m, err := indexdb.GetMessageByID(db, id)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, ExpandedMessage{
			ID:           m.MessageID,
			MessageIndex: m.MessageIndex,
			Role:         m.Role,
			Tool:         m.ToolName,
			Content:      m.Content,
		})
	}
	return msgs, nil
}

// buildNode maps one sessionhtml.Node (and, recursively, the sub-agents nested
// in its Agent events) onto an OutlineNode. rootID is the session the command
// was invoked with; every breadcrumb resolves against it, so a caller can
// always paste one back verbatim.
func buildNode(n *sessionhtml.Node, parentID string, dispatchedAt *int, rootID string) *OutlineNode {
	out := &OutlineNode{
		SessionID:         n.ID,
		ParentSessionID:   parentID,
		IsSubagent:        n.IsSubagent,
		SubagentName:      n.SubagentName,
		DispatchedAtIndex: dispatchedAt,
		Intent:            n.Intent,
		Workspace:         n.Workspace,
		Model:             n.Model,
		Depth:             n.Depth,
		MsgCount:          n.MsgCount,
		DurationMs:        n.DurationMs,
		Counts:            CountEvents(n.Events),
	}

	out.Segments = segment(n.ID, n.Events)
	for i := range out.Segments {
		out.Segments[i].Expand = fmt.Sprintf("auto search session outline %s --expand %s", rootID, out.Segments[i].ID)
	}

	for i := range n.Events {
		ev := &n.Events[i]
		if ev.Child == nil {
			continue
		}
		idx := ev.Idx
		out.Children = append(out.Children, buildNode(ev.Child, n.ID, &idx, rootID))
	}
	return out
}

// leavesFor turns a run of events into bodies-free Leaf stubs. The `get`
// breadcrumb points at whichever Message id actually holds the body:
// result_mid for a tool call, mid for prose.
func leavesFor(events []sessionhtml.Event) []Leaf {
	leaves := make([]Leaf, 0, len(events))
	for i := range events {
		ev := &events[i]
		l := Leaf{
			Kind:        ev.Kind,
			Idx:         ev.Idx,
			Tool:        ev.Tool,
			Summary:     ev.Summary,
			MID:         ev.MID,
			ResultMID:   ev.ResultMID,
			IsError:     ev.IsError,
			Interrupted: ev.Interrupted,
			Exit:        ev.Exit,
			DurationMs:  ev.Duration,
		}
		if id := bodyID(ev); id != "" {
			l.Get = "auto search message get " + id
		}
		leaves = append(leaves, l)
	}
	return leaves
}

// bodyID returns the Message id holding this event's body: the paired
// tool_result row for tool/agent events (the tool_use row's content is empty),
// otherwise the event's own Message id.
func bodyID(ev *sessionhtml.Event) string {
	if ev.ResultMID != "" {
		return ev.ResultMID
	}
	return ev.MID
}
