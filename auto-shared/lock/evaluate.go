package lock

import (
	"errors"
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
// falls under a configured Group's glob, and that Group is either unheld, held
// by a different Worker, or the editing Worker is bare (D-6: a lockable file
// under a checkout where agents cannot be told apart — the one identity
// failure that blocks rather than under-serializes). Every other outcome —
// non-edit event, no config, no match, self-held — and ANY other internal
// error (unreadable config, corrupt store, git failure) is Allow, so the guard
// can never break the agent's turn on its own account (D-10).
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

	// A lockable file is being edited, so identity matters now. A bare Worker
	// is denied before the held/unheld question: it cannot take the lock
	// either, so the unlocked shape's "run take" would only send it in a
	// circle — the D-6 remediation is the useful message.
	worker, err := ResolveWorker(cwd, payload, cfg.Identity)
	if err != nil {
		var bare *BareError
		if errors.As(err, &bare) {
			return Decision{Deny: true, Reason: bareReason(hits[0].rel, hits[0].group, bare)}
		}
		return allow
	}
	for _, h := range hits {
		l, ok := held[h.group.Name]
		if !ok {
			return Decision{Deny: true, Reason: unlockedReason(h.rel, h.group)}
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
// since when, and the recovery path. A worktree holder maps to a PR, so the
// path is the merge-verified clear; an agent or override holder has no PR to
// verify (D-3), so the path is its own release or an explicitly confirmed
// --force.
func heldReason(l Lock) string {
	var who strings.Builder
	who.WriteString(DescribeHolder(l.Holder))
	var details []string
	if l.PR != "" {
		details = append(details, prDisplay(l.PR))
	}
	if l.Reason != "" {
		details = append(details, fmt.Sprintf("%q", l.Reason))
	}
	if len(details) > 0 {
		who.WriteString(" (" + strings.Join(details, ", ") + ")")
	}
	head := fmt.Sprintf(
		"✗ BLOCKED: %q is locked by %s,\n"+
			"  taken %s on host %s.\n",
		l.Group, who.String(), takenAtDisplay(l.TakenAt), l.Holder.Host)
	if l.Holder.Kind == KindWorktree {
		pr := "its PR"
		if l.PR != "" {
			pr = prDisplay(l.PR)
		}
		return head + fmt.Sprintf(
			"  Wait for that PR to merge, then:  auto lock clear %s\n"+
				"  (clear refuses unless %s is merged; use --force only if you have confirmed it.)",
			l.Group, pr)
	}
	return head + fmt.Sprintf(
		"  That worker has no PR to verify: wait for it to run `auto lock release`, or\n"+
			"  once you have confirmed it is done:  auto lock clear %s --force",
		l.Group)
}

// bareReason is the D-6 block message: the file is lockable but this Worker
// cannot be told apart from other agents on the checkout, so it must identify
// itself before it can even take the lock.
func bareReason(rel string, g Group, bare *BareError) string {
	return fmt.Sprintf(
		"✗ BLOCKED: %s is under a serial-update lock group %q, and this Worker cannot be identified:\n"+
			"  %s\n"+
			"  Why serial: %s\n"+
			"  %s Then take the lock:  auto lock take %s",
		rel, g.Name, bare.Detail(), g.Description, bare.Remediation(), g.Name)
}

// prDisplay renders a recorded PR as "PR #42" whether it was stored as "42" or
// "#42".
func prDisplay(pr string) string {
	return "PR #" + strings.TrimPrefix(pr, "#")
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
