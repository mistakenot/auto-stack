package sessionhtml

import (
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/mistakenot/auto-search/internal/indexdb"
)

// lightFieldCap bounds tool_input / agent prompt fields under --light. Message
// and tool-output bodies use the index's content_truncated instead; this cap
// only applies to fields that have no pre-truncated variant in the index.
const lightFieldCap = 4000

// childListLimit is a high ceiling on sub-agents claimed per parent — far
// above any real coordinator fan-out, just so ListSessions's default page size
// (50) never silently drops children.
const childListLimit = 10000

var (
	wsRE      = regexp.MustCompile(`\s+`)
	cmdNameRE = regexp.MustCompile(`<command-name>/?([\w-]+)</command-name>`)
	cmdArgsRE = regexp.MustCompile(`<command-args>([^<]*)</command-args>`)
)

// BuildModel recursively builds the work-graph for a session and its
// sub-agents, rooted at rootID. It returns nil only when the root session does
// not exist.
func BuildModel(db *sql.DB, rootID string, opts Options) (*Node, error) {
	return buildNode(db, rootID, opts, 0, "", map[string]bool{})
}

func buildNode(db *sql.DB, sid string, opts Options, depth int, dispatchLabel string, seen map[string]bool) (*Node, error) {
	if seen[sid] {
		return nil, nil // cycle guard
	}
	seen[sid] = true

	sess, err := indexdb.GetSessionByID(db, sid)
	if err != nil {
		// An unknown root surfaces to the caller; an unknown child (orphan
		// dispatch) renders with no child rather than failing the build.
		if depth == 0 {
			return nil, err
		}
		return nil, nil
	}

	msgs, err := indexdb.SessionMessages(db, sid, opts.IncludeThinking)
	if err != nil {
		return nil, err
	}

	// Index tool_result rows by tool_use_id so each tool_use can find its
	// paired result (duration / exit / interrupted / output live there).
	results := make(map[string]indexdb.MessageRow)
	for i := range msgs {
		m := &msgs[i]
		if m.Role == "tool" && m.ToolUseID != "" {
			results[m.ToolUseID] = *m
		}
	}

	// Enumerate children once, full-fidelity (GetSessionByID gives the
	// untruncated FirstUserIntent the prefix match needs), ordered by start
	// time so dispatch-order fallback is chronological.
	kids := loadChildren(db, sid)
	usedKids := make(map[string]bool)
	claimChild := func(prompt string) string {
		p := norm(prompt)
		// Prefer a prompt-prefix match against the child's first user intent.
		for i := range kids {
			k := &kids[i]
			if usedKids[k.SessionID] {
				continue
			}
			fi := norm(k.FirstUserIntent)
			if fi != "" && (strings.HasPrefix(fi, prefix(p, 200)) || strings.HasPrefix(p, prefix(fi, 200))) {
				usedKids[k.SessionID] = true
				return k.SessionID
			}
		}
		// Fallback: next unclaimed child in dispatch order.
		for i := range kids {
			k := &kids[i]
			if !usedKids[k.SessionID] {
				usedKids[k.SessionID] = true
				return k.SessionID
			}
		}
		return ""
	}

	var events []Event
	var counts Counts

	for i := range msgs {
		m := &msgs[i]
		switch m.Role {
		case "thinking":
			body, tr := opts.bodyField(m.Content, m.ContentTruncated)
			events = append(events, Event{
				Kind: "thinking", Idx: m.MessageIndex, MID: m.MessageID,
				Summary: firstLine(m.Content, 100), Body: body, Truncated: tr,
			})
		case "user":
			body, tr := opts.bodyField(m.Content, m.ContentTruncated)
			events = append(events, Event{
				Kind: "user", Idx: m.MessageIndex, MID: m.MessageID,
				Summary: firstLine(m.Content, 140), Body: body, Truncated: tr,
			})
		case "assistant":
			if m.ToolName == "" {
				if strings.TrimSpace(m.Content) == "" {
					continue
				}
				body, tr := opts.bodyField(m.Content, m.ContentTruncated)
				events = append(events, Event{
					Kind: "assistant", Idx: m.MessageIndex, MID: m.MessageID,
					Summary: firstLine(m.Content, 140), Body: body, Truncated: tr,
					OutTokens: m.OutputTokens,
				})
				continue
			}
			ev := buildToolEvent(db, m, results, opts, depth, &counts, seen, claimChild)
			events = append(events, ev)
		}
		// role == "tool": consumed via pairing above; skipped here.
	}

	node := &Node{
		ID:            sess.SessionID,
		Title:         deriveTitle(depth, msgs),
		Intent:        sess.FirstUserIntent,
		SubagentName:  sess.SubagentName,
		DispatchLabel: dispatchLabel,
		IsSubagent:    sess.IsSubagent,
		Workspace:     sess.Workspace,
		GitRemote:     sess.GitRemote,
		Model:         sess.Model,
		FirstMs:       sess.FirstMessageAt,
		LastMs:        sess.LastMessageAt,
		DurationMs:    sess.LastMessageAt - sess.FirstMessageAt,
		TotalTokens:   sess.TotalTokens,
		MsgCount:      len(msgs),
		Counts:        counts,
		Depth:         depth,
		Events:        events,
	}
	return node, nil
}

// buildToolEvent classifies an assistant tool_use row into a tool or agent
// event, pairing it with its result and (for Agent dispatches) recursing into
// the correlated child session.
func buildToolEvent(db *sql.DB, m *indexdb.MessageRow, results map[string]indexdb.MessageRow, opts Options, depth int, counts *Counts, seen map[string]bool, claimChild func(string) string) Event {
	res, hasRes := results[m.ToolUseID]
	var (
		resContent, resTruncated string
		duration                 int64
		isErr, interrupted       bool
		exit                     int
	)
	if hasRes {
		resContent = res.Content
		resTruncated = res.ContentTruncated
		duration = res.DurationMs
		isErr = res.IsError
		interrupted = res.Interrupted
		exit = res.BashExitCode
	}

	d := parseToolInput(m.ToolInput)

	if m.ToolName == "Agent" {
		counts.Agent++
		var childNode *Node
		if childID := claimChild(str(d, "prompt")); childID != "" {
			childNode, _ = buildNode(db, childID, opts, depth+1, str(d, "description"), seen)
		}
		prompt, ptr := opts.clipField(str(d, "prompt"))
		result, rtr := opts.bodyField(resContent, resTruncated)
		summary := str(d, "description")
		if summary == "" {
			summary = firstLine(str(d, "prompt"), 100)
		}
		return Event{
			Kind: "agent", Idx: m.MessageIndex, MID: m.MessageID, Summary: summary,
			SubagentType: str(d, "subagent_type"),
			Prompt:       prompt, PromptTrunc: ptr,
			Result: result, ResultTrunc: rtr,
			Duration: duration, Child: childNode,
		}
	}

	// Classify regular tools for counts.
	switch {
	case m.ToolName == "Bash":
		counts.Bash++
		if exit != 0 || isErr {
			counts.Error++
		}
	case isFileTool(m.ToolName):
		counts.File++
	case m.ToolName == "Skill":
		counts.Skill++
	default:
		counts.Tool++
	}

	input, itr := opts.clipField(m.ToolInput)
	output, otr := opts.bodyField(resContent, resTruncated)
	return Event{
		Kind: "tool", Idx: m.MessageIndex, MID: m.MessageID, Tool: m.ToolName,
		Summary:     toolSummary(m, d),
		Input:       input,
		InputTrunc:  itr,
		Output:      output,
		OutputTrunc: otr,
		Duration:    duration,
		IsError:     isErr,
		Interrupted: interrupted,
		Exit:        exit,
	}
}

// loadChildren returns the full session rows of sid's direct sub-agents,
// ordered by start time. Errors enumerating or loading a child are tolerated
// (a missing child simply isn't claimable).
func loadChildren(db *sql.DB, sid string) []indexdb.SessionRow {
	list, _, err := indexdb.ListSessions(db, &indexdb.ListSessionsOpts{
		ParentSessionID: sid,
		Limit:           childListLimit,
	})
	if err != nil {
		return nil
	}
	rows := make([]indexdb.SessionRow, 0, len(list))
	for i := range list {
		full, err := indexdb.GetSessionByID(db, list[i].SessionID)
		if err != nil {
			continue
		}
		rows = append(rows, *full)
	}
	sortByFirstMessageAt(rows)
	return rows
}

func sortByFirstMessageAt(rows []indexdb.SessionRow) {
	// Insertion sort keeps it allocation-free and stable for the small fan-out
	// sizes seen in practice.
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].FirstMessageAt < rows[j-1].FirstMessageAt; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// deriveTitle picks a human title for the root from the first real
// slash-command invocation (skipping harness no-ops like /clear, /compact).
func deriveTitle(depth int, msgs []indexdb.MessageRow) string {
	if depth != 0 {
		return ""
	}
	var b strings.Builder
	for i := range msgs {
		if msgs[i].Role == "user" {
			b.WriteString(msgs[i].Content)
			b.WriteByte(' ')
		}
	}
	blob := b.String()
	for _, loc := range cmdNameRE.FindAllStringSubmatchIndex(blob, -1) {
		name := blob[loc[2]:loc[3]]
		if name == "clear" || name == "compact" {
			continue
		}
		title := "/" + name
		// Look for command-args shortly after the command-name tag.
		end := loc[1]
		windowEnd := min(end+200, len(blob))
		if am := cmdArgsRE.FindStringSubmatch(blob[end:windowEnd]); am != nil {
			if ca := strings.TrimSpace(am[1]); ca != "" {
				title += " " + ca
			}
		}
		return title
	}
	return ""
}

// bodyField selects the message body: full content by default, the index's
// pre-truncated body under --light. The bool reports whether truncation was
// applied (drives the "…truncated" hint in the viewer).
func (o Options) bodyField(full, truncated string) (string, bool) {
	if o.Light && truncated != "" && truncated != full {
		return truncated, true
	}
	return full, false
}

// clipField bounds a field that has no pre-truncated index variant (tool_input,
// agent prompt). Only trims under --light; the default path embeds full content.
func (o Options) clipField(s string) (string, bool) {
	if o.Light && len(s) > lightFieldCap {
		return s[:lightFieldCap], true
	}
	return s, false
}

func parseToolInput(ti string) map[string]any {
	if ti == "" {
		return nil
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(ti), &d); err != nil {
		return nil
	}
	return d
}

func str(d map[string]any, key string) string {
	if d == nil {
		return ""
	}
	if v, ok := d[key].(string); ok {
		return v
	}
	return ""
}

func isFileTool(t string) bool {
	switch t {
	case "Read", "Write", "Edit", "Glob", "NotebookEdit":
		return true
	}
	return false
}

// toolSummary returns the one-line structural summary for a tool event.
func toolSummary(m *indexdb.MessageRow, d map[string]any) string {
	switch m.ToolName {
	case "Bash":
		cmd := m.BashCommand
		if cmd == "" {
			cmd = str(d, "command")
		}
		return firstLine(cmd, 200)
	case "Read", "Write", "Edit", "Glob", "NotebookEdit":
		if m.ToolFilePath != "" {
			return m.ToolFilePath
		}
		for _, k := range []string{"file_path", "path", "pattern"} {
			if v := str(d, k); v != "" {
				return v
			}
		}
		return ""
	case "Skill":
		if m.SkillName != "" {
			return m.SkillName
		}
		if v := str(d, "command"); v != "" {
			return v
		}
		return str(d, "skill")
	case "TaskCreate", "TaskUpdate":
		for _, k := range []string{"subagent_type", "description", "prompt"} {
			if v := str(d, k); v != "" {
				return firstLine(v, 120)
			}
		}
		return firstLine(jsonString(d), 120)
	case "ToolSearch":
		return firstLine(str(d, "query"), 120)
	case "Grep":
		return firstLine(str(d, "pattern"), 120)
	case "Agent":
		if v := str(d, "description"); v != "" {
			return v
		}
		return firstLine(str(d, "prompt"), 120)
	}
	if s := jsonString(d); s != "" {
		return firstLine(s, 120)
	}
	return firstLine(m.Content, 120)
}

func jsonString(d map[string]any) string {
	if len(d) == 0 {
		return ""
	}
	b, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	return string(b)
}

func norm(s string) string {
	return strings.TrimSpace(wsRE.ReplaceAllString(s, " "))
}

func prefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func firstLine(s string, n int) string {
	s = norm(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
