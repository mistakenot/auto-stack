package sessionoutline

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mistakenot/auto-search/internal/sessionhtml"
)

// timeGapThresholdMs is the wall-clock silence between two consecutive events
// that reads as a break in the work — a compile, a human stepping away, a
// blocked prompt. Five minutes is long enough that ordinary tool latency never
// trips it and short enough to separate distinct sittings.
const timeGapThresholdMs = 5 * 60 * 1000

// labelWidth bounds a segment label so an outline stays one line per segment.
const labelWidth = 120

// Boundary reasons. Every Segment records why it was cut, so a reader can tell
// a deliberate phase change from an incidental one.
const (
	reasonStart      = "start"             // first segment of a node
	reasonDispatch   = "subagent_dispatch" // an Agent tool_use, always alone
	reasonResume     = "resume"            // the work picking back up after a dispatch
	reasonBashError  = "bash_error"        // a non-zero bash exit begins here
	reasonToolError  = "tool_error"        // a non-Bash tool failure begins here
	reasonErrorClear = "error_cleared"     // the run of failures ends here
	reasonTodoMarker = "todo_marker"       // a todo / bead bookkeeping call
	reasonToolBurst  = "tool_burst"        // the dominant tool kind changed
	reasonTimeGap    = "time_gap"          // a long wall-clock silence
)

// Counts holds per-node and per-segment event tallies.
//
// Error follows the sessionhtml definition (`is_error || bash_exit_code != 0`,
// decision D-2) rather than `session list`'s bash-only error_count, so it
// catches non-Bash tool failures too. Outline error counts can therefore
// exceed the ones `session list` reports.
type Counts struct {
	Bash  int `json:"bash"`
	File  int `json:"file"`
	Tool  int `json:"tool"`
	Agent int `json:"agent"`
	Skill int `json:"skill"`
	Error int `json:"error"`
}

// CountEvents tallies a run of events, mirroring sessionhtml's classification
// so a node's counts always equal the sum of its segments'.
func CountEvents(events []sessionhtml.Event) Counts {
	var c Counts
	for i := range events {
		ev := &events[i]
		switch {
		case ev.Kind == "agent":
			c.Agent++
			continue
		case ev.Kind != "tool":
			continue
		case ev.Tool == "Bash":
			c.Bash++
		case isFileTool(ev.Tool):
			c.File++
		case ev.Tool == "Skill":
			c.Skill++
		default:
			c.Tool++
		}
		if isFailure(ev) {
			c.Error++
		}
	}
	return c
}

func isFileTool(t string) bool {
	switch t {
	case "Read", "Write", "Edit", "Glob", "NotebookEdit":
		return true
	}
	return false
}

// Segment is a contiguous run of a node's timeline collapsed to one line: an
// id, a label, why it was cut, its message_index range, rolled-up counts, and
// the addressable Messages inside it.
type Segment struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Reason     string   `json:"reason"`
	IndexRange [2]int   `json:"index_range"`
	Counts     Counts   `json:"counts"`
	MessageIDs []string `json:"message_ids,omitempty"`
	Leaves     []Leaf   `json:"leaves,omitempty"`
	// Expand is the exact command that re-emits this segment with full bodies.
	Expand string `json:"expand,omitempty"`
}

// segment cuts a node's flat event timeline into Segments at deterministic,
// index-only boundary signals. It is pure: same events in, byte-identical
// segments out, with no index or clock access.
//
// A cut happens at the first signal that fires, in this precedence:
//
//  1. subagent dispatch — an Agent event always stands alone
//  2. error transition — a run of failures begins, or ends
//  3. todo / bead marker — explicit bookkeeping the agent itself wrote
//  4. wall-clock time gap
//  5. tool burst — the dominant tool kind changed
//
// The event after a dispatch always starts a segment too, recorded as
// "resume" unless one of the signals above describes it better. Precedence
// matters only for the recorded reason; the boundary position is the same
// whichever signal claimed it.
func segment(sessionID string, events []sessionhtml.Event) []Segment {
	if len(events) == 0 {
		return nil
	}

	var segs []Segment
	start := 0
	reason := reasonStart
	// lane tracks the dominant tool family, carried across prose events so a
	// chatty assistant between two Read calls does not split the run.
	lane := toolLane(&events[0])
	failing := isFailure(&events[0])

	flush := func(end int) {
		run := events[start:end]
		segs = append(segs, newSegment(sessionID, len(segs), reason, segmentLabel(reason, run), run))
	}

	for i := 1; i < len(events); i++ {
		prev, cur := &events[i-1], &events[i]
		curFailing := isFailure(cur)
		curLane := toolLane(cur)

		cut := ""
		switch {
		case cur.Kind == "agent":
			cut = reasonDispatch
		case curFailing && !failing:
			cut = reasonBashError
			if cur.Exit == 0 {
				cut = reasonToolError
			}
		case !curFailing && failing:
			cut = reasonErrorClear
		case isTodoMarker(cur):
			cut = reasonTodoMarker
		case cur.Ts > 0 && prev.Ts > 0 && cur.Ts-prev.Ts > timeGapThresholdMs:
			cut = reasonTimeGap
		case curLane != "" && lane != "" && curLane != lane:
			cut = reasonToolBurst
		}
		// The work always restarts after a dispatch, but a more specific
		// signal on the resuming event describes it better.
		if cut == "" && prev.Kind == "agent" {
			cut = reasonResume
		}

		if curLane != "" {
			lane = curLane
		}
		failing = curFailing

		if cut != "" {
			flush(i)
			start, reason = i, cut
		}
	}
	flush(len(events))
	return segs
}

// newSegment assembles one Segment from a contiguous run of events.
func newSegment(sessionID string, ordinal int, reason, label string, events []sessionhtml.Event) Segment {
	leaves := leavesFor(events)
	ids := make([]string, 0, len(leaves))
	for i := range events {
		if id := bodyID(&events[i]); id != "" {
			ids = append(ids, id)
		}
	}
	return Segment{
		ID:         fmt.Sprintf("%s#s%d", sessionID, ordinal),
		Label:      label,
		Reason:     reason,
		IndexRange: [2]int{events[0].Idx, events[len(events)-1].Idx},
		Counts:     CountEvents(events),
		MessageIDs: ids,
		Leaves:     leaves,
	}
}

// isFailure applies decision D-2: a tool call counts as failed when the index
// flagged it is_error or its bash exit code was non-zero.
func isFailure(ev *sessionhtml.Event) bool {
	return ev.Kind == "tool" && (ev.IsError || ev.Exit != 0)
}

// toolLane returns the coarse tool family an event belongs to, or "" for prose
// and dispatches (which never move the lane).
func toolLane(ev *sessionhtml.Event) string {
	if ev.Kind != "tool" {
		return ""
	}
	switch {
	case ev.Tool == "Bash":
		return "bash"
	case isFileTool(ev.Tool):
		return "file"
	case ev.Tool == "":
		return "tool"
	default:
		return "tool:" + ev.Tool
	}
}

// isTodoMarker reports whether an event is explicit progress bookkeeping — a
// TodoWrite call, or a beads issue transition run through Bash.
func isTodoMarker(ev *sessionhtml.Event) bool {
	if ev.Kind != "tool" {
		return false
	}
	if ev.Tool == "TodoWrite" {
		return true
	}
	if ev.Tool != "Bash" {
		return false
	}
	cmd := strings.TrimSpace(ev.Summary)
	if !strings.HasPrefix(cmd, "br ") && !strings.HasPrefix(cmd, "bd ") {
		return false
	}
	return strings.Contains(cmd, " update") || strings.Contains(cmd, " close")
}

// segmentLabel derives the one-line summary for a segment from its lead event
// — the one that explains why the segment exists.
func segmentLabel(reason string, events []sessionhtml.Event) string {
	lead := leadEvent(reason, events)

	switch {
	case lead.Kind == "agent":
		name := lead.SubagentType
		if name == "" {
			name = "subagent"
		}
		return clip(strings.TrimSpace(name + " · " + lead.Summary))
	case isFailure(lead):
		tool := lead.Tool
		if tool == "" {
			tool = "tool"
		}
		return clip(tool + " FAIL — " + lead.Summary)
	case isTodoMarker(lead):
		return clip("todo · " + lead.Summary)
	case lead.Kind == "tool":
		n := laneRunLength(toolLane(lead), events)
		if n > 1 {
			return clip(fmt.Sprintf("%s ×%d — %s", lead.Tool, n, lead.Summary))
		}
		return clip(lead.Tool + " — " + lead.Summary)
	case lead.Summary != "":
		return clip(lead.Summary)
	}
	return reason
}

// leadEvent picks the event a segment's label should describe: the signal that
// cut it where there is one, otherwise the first tool call, otherwise the
// first event.
func leadEvent(reason string, events []sessionhtml.Event) *sessionhtml.Event {
	switch reason {
	case reasonDispatch:
		return &events[0]
	case reasonBashError, reasonToolError:
		for i := range events {
			if isFailure(&events[i]) {
				return &events[i]
			}
		}
	case reasonTodoMarker:
		for i := range events {
			if isTodoMarker(&events[i]) {
				return &events[i]
			}
		}
	}
	// Otherwise label the segment after whatever dominates it: the first
	// tool call in a tool-heavy run, the opening message in a prose one.
	tools := 0
	for i := range events {
		if events[i].Kind == "tool" {
			tools++
		}
	}
	if tools*2 > len(events) {
		for i := range events {
			if events[i].Kind == "tool" {
				return &events[i]
			}
		}
	}
	return &events[0]
}

// laneRunLength counts how many events in a segment belong to the given tool
// family — the ×N in a burst label.
func laneRunLength(lane string, events []sessionhtml.Event) int {
	n := 0
	for i := range events {
		if toolLane(&events[i]) == lane {
			n++
		}
	}
	return n
}

// clip bounds a label to labelWidth bytes, cutting back to a rune boundary so
// the result is always valid UTF-8.
func clip(s string) string {
	s = strings.TrimSpace(s)
	n := labelWidth
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
