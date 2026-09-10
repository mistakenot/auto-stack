package lock

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// editTools are the Claude Code tools whose tool_input carries a discrete file
// path the guard can match (D-10, D-12).
var editTools = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"MultiEdit":    true,
	"NotebookEdit": true,
}

// pathKeys are the tool_input fields that name the edited file (mirrors the
// hook adapter's extractPathRefs).
var pathKeys = []string{"file_path", "notebook_path", "path"}

// allow is the silent verdict.
var allow = Decision{}

// Evaluate is the fail-open PreToolUse guard. It denies only on a confirmed
// violation: the payload is a PreToolUse call of an edit tool, the edited path
// falls under a configured Group's glob, and that Group is either unheld or
// held by a different Worker. Every other outcome — non-edit event, no config,
// no match, self-held — and ANY internal error (unreadable config, corrupt
// store, unresolvable identity) is Allow, so the guard can never break the
// agent's turn on its own account (D-10).
func Evaluate(cwd string, payload map[string]any) Decision {
	if !strings.EqualFold(payloadString(payload, "hook_event_name"), "PreToolUse") {
		return allow
	}
	if !editTools[payloadString(payload, "tool_name")] {
		return allow
	}
	paths := editedPaths(payload)
	if len(paths) == 0 {
		return allow
	}

	repo, err := ResolveRepo(cwd)
	if err != nil {
		return allow
	}
	cfg, err := LoadConfig(repo.Root)
	if err != nil || cfg == nil || len(cfg.Groups) == 0 {
		return allow
	}

	type hit struct {
		rel   string
		group Group
	}
	var hits []hit
	seen := map[string]bool{}
	for _, p := range paths {
		rel := relativeToRoot(p, cwd, repo.Root)
		if rel == "" {
			continue
		}
		for _, g := range cfg.MatchGroups(rel) {
			if seen[g.Name] {
				continue
			}
			seen[g.Name] = true
			hits = append(hits, hit{rel: rel, group: g})
		}
	}
	if len(hits) == 0 {
		return allow
	}

	store, err := OpenDefault()
	if err != nil {
		return allow
	}
	locks, err := store.List(repo.Project)
	if err != nil {
		return allow
	}
	held := map[string]Lock{}
	for i := range locks {
		held[locks[i].Group] = locks[i]
	}

	var worker *Worker
	for _, h := range hits {
		l, ok := held[h.group.Name]
		if !ok {
			return Decision{Deny: true, Reason: unlockedReason(h.rel, h.group)}
		}
		if worker == nil {
			w, err := ResolveWorker(cwd, payload, cfg.Identity)
			if err != nil {
				return allow
			}
			worker = &w
		}
		if !worker.Matches(l.Holder) {
			return Decision{Deny: true, Reason: heldReason(l)}
		}
	}
	return allow
}

// unlockedReason is the "no holder yet" block message (Requirements → Block
// message): why the group is serial and the exact take command.
func unlockedReason(rel string, g Group) string {
	return fmt.Sprintf(
		"✗ BLOCKED: %s is under a serial-update lock group %q.\n"+
			"  Why serial: %s\n"+
			"  Take the lock first:   auto lock take %s\n"+
			"  Then retry your edit. Release everything at merge time:  auto lock release",
		rel, g.Name, g.Description, g.Name)
}

// heldReason is the "held by another Worker" block message: who holds it, why,
// since when, and the clear path with its merged-PR caveat.
func heldReason(l Lock) string {
	var who strings.Builder
	who.WriteString(DescribeHolder(l.Holder))
	var details []string
	if l.PR != "" {
		details = append(details, "PR "+l.PR)
	}
	if l.Reason != "" {
		details = append(details, fmt.Sprintf("%q", l.Reason))
	}
	if len(details) > 0 {
		who.WriteString(" (" + strings.Join(details, ", ") + ")")
	}
	pr := "its PR"
	if l.PR != "" {
		pr = "PR " + l.PR
	}
	return fmt.Sprintf(
		"✗ BLOCKED: %q is locked by %s,\n"+
			"  taken %s on host %s.\n"+
			"  Wait for that PR to merge, then:  auto lock clear %s\n"+
			"  (clear refuses unless %s is merged; use --force only if you have confirmed it.)",
		l.Group, who.String(), takenAtDisplay(l.TakenAt), l.Holder.Host, l.Group, pr)
}

// takenAtDisplay renders an RFC 3339 taken_at as "2006-01-02 15:04" for the
// block message, falling back to the raw value.
func takenAtDisplay(ts string) string {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Format("2006-01-02 15:04")
	}
	return ts
}

// editedPaths pulls the edited file paths from tool_input, de-duplicated.
func editedPaths(payload map[string]any) []string {
	input, ok := payload["tool_input"].(map[string]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, key := range pathKeys {
		if v, ok := input[key].(string); ok && v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// relativeToRoot makes p (absolute, or relative to cwd) a slash-separated path
// relative to root for glob matching, or "" when it lies outside root.
func relativeToRoot(p, cwd, root string) string {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return ""
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}
