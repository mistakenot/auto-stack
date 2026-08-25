package sessionoutline

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// RenderText writes the outline as an indented tree for a human reader. It
// carries exactly the same information as the JSON payload — structure, ids,
// counts and breadcrumbs — never a body, unless --expand asked for one.
func RenderText(w io.Writer, o *Outline) {
	renderNode(w, o.OutlineNode, 0)
	if o.Expanded != nil {
		renderExpansion(w, o.Expanded)
	}
}

func renderNode(w io.Writer, n *OutlineNode, indent int) {
	pad := strings.Repeat("  ", indent)

	head := "session " + n.SessionID
	if n.IsSubagent {
		name := n.SubagentName
		if name == "" {
			name = "subagent"
		}
		head = "subagent " + name + " (" + n.SessionID + ")"
		if n.DispatchedAtIndex != nil {
			head += " dispatched at index " + strconv.Itoa(*n.DispatchedAtIndex)
		}
	}
	fmt.Fprintf(w, "%s%s\n", pad, head)
	if n.Intent != "" {
		fmt.Fprintf(w, "%s  intent: %s\n", pad, clip(n.Intent))
	}
	fmt.Fprintf(w, "%s  %d messages · %s\n", pad, n.MsgCount, countsLine(n.Counts))

	if n.Collapsed {
		fmt.Fprintf(w, "%s  collapsed — %s\n", pad, n.Expand)
		return
	}

	for i := range n.Segments {
		renderSegment(w, &n.Segments[i], indent+1)
	}
	for _, c := range n.Children {
		fmt.Fprintln(w)
		renderNode(w, c, indent+1)
	}
}

func renderSegment(w io.Writer, s *Segment, indent int) {
	pad := strings.Repeat("  ", indent)
	fmt.Fprintf(w, "%s[%d-%d] %s (%s)\n", pad, s.IndexRange[0], s.IndexRange[1], s.Label, s.Reason)
	fmt.Fprintf(w, "%s  %s · %s\n", pad, s.ID, countsLine(s.Counts))
	for i := range s.Leaves {
		l := &s.Leaves[i]
		kind := l.Kind
		if l.Tool != "" {
			kind = l.Tool
		}
		flags := ""
		if l.IsError || l.Exit != 0 {
			flags = " ERROR"
		}
		if l.Interrupted {
			flags += " INTERRUPTED"
		}
		fmt.Fprintf(w, "%s    %d %s%s — %s\n", pad, l.Idx, kind, flags, clip(l.Summary))
		if l.Get != "" {
			fmt.Fprintf(w, "%s      %s\n", pad, l.Get)
		}
	}
	if s.Expand != "" {
		fmt.Fprintf(w, "%s  expand: %s\n", pad, s.Expand)
	}
}

func renderExpansion(w io.Writer, e *Expansion) {
	fmt.Fprintf(w, "\nexpanded %s %s\n", e.Kind, e.ID)
	for i := range e.Messages {
		m := &e.Messages[i]
		tag := m.Role
		if m.Tool != "" {
			tag += " " + m.Tool
		}
		fmt.Fprintf(w, "\n<%s index=%d id=%s>\n%s\n</%s>\n", tag, m.MessageIndex, m.ID, m.Content, m.Role)
	}
}

// countsLine renders only the non-zero tallies, so a clean segment stays short.
func countsLine(c Counts) string {
	parts := make([]string, 0, 6)
	for _, p := range []struct {
		label string
		n     int
	}{
		{"bash", c.Bash}, {"file", c.File}, {"tool", c.Tool},
		{"agent", c.Agent}, {"skill", c.Skill}, {"error", c.Error},
	} {
		if p.n > 0 {
			parts = append(parts, p.label+" "+strconv.Itoa(p.n))
		}
	}
	if len(parts) == 0 {
		return "no tool calls"
	}
	return strings.Join(parts, " · ")
}
