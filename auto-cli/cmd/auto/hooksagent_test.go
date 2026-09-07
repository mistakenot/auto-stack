package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The Subagent marker is the second thing the hook does on every tool call of
// every agent on the host, and the first was already load-bearing. These tests
// are the auto-cli half of AC-7: the ordinary agent's path costs nothing new,
// the Subagent's path costs one bounded write and no store, and neither can
// break the agent's turn under any state the filesystem can be left in.
//
// They observe `~/.auto/mail` from outside the module, which AC-11 permits and
// T1's hooksmail_test.go already does: observing a path is not database access,
// and there is no other way to prove the hook did *not* touch one.

// mailDirOf is the mail tree the marker path is scoped to. The assertions below
// are about this subtree only — `hooks.Append` writes under `~/.auto/hooks` on
// every valid payload by design, and scoping the claim to what it is actually
// about is what keeps it true and meaningful.
func mailDirOf(home string) string { return filepath.Join(home, ".auto", "mail") }

func agentsDirOf(home string) string { return filepath.Join(mailDirOf(home), "alpha-agents") }

// markerFiles lists every file under the marker tree. It counts files rather
// than answers: a marker directory is state on a long-lived host, so what
// reaches the disk is the property, not what a reader happens to make of it.
func markerFiles(t *testing.T, home string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(agentsDirOf(home), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", agentsDirOf(home), err)
	}
	return found
}

// subagentPayload is a hook payload from an in-process Subagent: the ordinary
// shape plus the two fields that identify one. The names are copied from real
// payloads rather than invented — `agent_id` is what actually arrives.
func subagentPayload(event, cwd, agentID string) string {
	payload := map[string]any{
		"hook_event_name": event,
		"session_id":      "sess-supervisor",
		"cwd":             cwd,
		"tool_name":       "Read",
		"tool_input":      map[string]any{"file_path": filepath.Join(cwd, "README.md")},
		"agent_id":        agentID,
		"agent_type":      "Explore",
	}
	if cwd == "" {
		delete(payload, "cwd")
		delete(payload, "tool_input")
	}
	return toJSON(payload)
}

// assertAtMostOneObject is D-062-9 as an assertion: two JSON objects on one
// hook's stdout is undefined behaviour in both agents' hook contracts, so
// stdout is either empty or exactly one object — never "mostly one".
func assertAtMostOneObject(t *testing.T, out string) string {
	t.Helper()
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return ""
	}
	lines := []string{}
	for line := range strings.SplitSeq(trimmed, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("the hook wrote %d lines to stdout, want at most one hookSpecificOutput "+
			"object — two objects is undefined behaviour in both agents' hook contracts:\n%s",
			len(lines), out)
	}
	var resp hookResponse
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("stdout is not one JSON object: %v (%q)", err, out)
	}
	return resp.HookSpecificOutput.AdditionalContext
}

// TestFireWithNoAgentIDTouchesNothingUnderMail is AC-7's first half at the
// command surface: an ordinary agent's tool call carries no `agent_id`, and the
// marker path must then perform zero filesystem operations.
//
// The claim is scoped to `~/.auto/mail`, and the hooks log is asserted to exist
// for a reason — without it this test would pass just as well against a hook
// that did nothing at all, which is the version of "costs nothing" nobody wants.
func TestFireWithNoAgentIDTouchesNothingUnderMail(t *testing.T) {
	home := isolateHookEnv(t)
	workspace := initGitRepo(t, "main")

	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		payload := toJSON(map[string]any{
			"hook_event_name": event,
			"session_id":      "sess-ordinary",
			"cwd":             workspace,
			"tool_name":       "Read",
		})
		assertAtMostOneObject(t, runFire(t, "claude", payload))
	}

	if _, err := os.Stat(mailDirOf(home)); !os.IsNotExist(err) {
		t.Errorf("three agent-less hook fires created %s (stat err = %v); the ordinary "+
			"agent's hot path must be bit-for-bit what it was before markers existed",
			mailDirOf(home), err)
	}
	// The hook itself did run. T1's durable append is deliberately a filesystem
	// write on every valid payload, so its presence is what separates "the mail
	// path did nothing" from "nothing happened".
	if _, err := os.Stat(filepath.Join(home, ".auto", "hooks")); err != nil {
		t.Fatalf("the hook wrote no durable log (%v); the assertion above proves nothing "+
			"if the hook never ran", err)
	}
}

// TestFireWithAnAgentIDWritesOneMarkerAndNoStore is AC-7's second half: the
// Subagent's path costs exactly one bounded marker write, opens the store zero
// times, and shells out to nothing.
//
// Zero store opens is observed here as the store file never coming into
// existence, which is the instrument available from outside the module and the
// one T1 already uses — a store open on this path would create it. The counted
// form of the same claim, through the `openStore` package var, lives in
// auto-mail/mail/subagent_test.go where the counter is reachable.
func TestFireWithAnAgentIDWritesOneMarkerAndNoStore(t *testing.T) {
	home := isolateHookEnv(t)
	workspace := initGitRepo(t, "main")

	assertAtMostOneObject(t, runFire(t, "claude",
		subagentPayload("PreToolUse", workspace, "a84a3676a847c5c0b")))
	if got := markerFiles(t, home); len(got) != 1 {
		t.Fatalf("one Subagent hook fire wrote %d marker files, want exactly 1: %v", len(got), got)
	}

	// A refresh is the same file, not a second one: the marker directory is
	// bounded by concurrent Subagents, not by tool calls.
	for range 5 {
		runFire(t, "claude", subagentPayload("PostToolUse", workspace, "a84a3676a847c5c0b"))
	}
	if got := markerFiles(t, home); len(got) != 1 {
		t.Errorf("five refreshes left %d marker files, want 1: %v", len(got), got)
	}

	for _, path := range []string{
		filepath.Join(mailDirOf(home), "alpha-store.db"),
		filepath.Join(mailDirOf(home), "alpha-flags"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("the Subagent path created %s (stat err = %v); recording a marker must "+
				"cost one write and open nothing (G8/AC-7)", path, err)
		}
	}

	// SubagentStop clears it again, so the directory does not grow across a
	// day of Subagents on one host.
	runFire(t, "claude", subagentPayload("SubagentStop", workspace, "a84a3676a847c5c0b"))
	if got := markerFiles(t, home); len(got) != 0 {
		t.Errorf("SubagentStop left %d marker files, want 0: %v", len(got), got)
	}
}

// TestFireSurvivesEveryBrokenMarkerPath is the adversarial set AC-7 names, and
// the reason ObserveHookEvent has no error return.
//
// Each case leaves the marker path in a state it can genuinely reach on a real
// host, then fires a Subagent payload through it. In every one of them the hook
// exits 0, writes no stray bytes, and — the part that makes this more than a
// crash test — still emits T1's nudge when there is mail waiting. A hook that
// survived by going silent would have broken the agent in the way that matters.
//
// Every case also states how many markers it expects, and that number is what
// stops the test passing vacuously: three of these breaks are only breaks if
// the marker write actually fails under them, and a case that quietly succeeded
// would assert nothing at all.
func TestFireSurvivesEveryBrokenMarkerPath(t *testing.T) {
	cases := map[string]struct {
		// seedable is false for a host where the mail store does not exist, so
		// there is no mail and no nudge to expect.
		seedable bool
		// wantMarkers is what must be on disk after the fire.
		wantMarkers int
		breakIt     func(t *testing.T, home, workspace string)
	}{
		"marker directory unwritable": {seedable: true, wantMarkers: 0, breakIt: func(t *testing.T, home, _ string) {
			t.Helper()
			dir := agentsDirOf(home)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			// Applied after creation so the parents stay traversable: the case
			// is one unwritable directory, not an unreachable home.
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		}},
		"marker path occupied by a directory": {seedable: true, wantMarkers: 0, breakIt: func(t *testing.T, home, workspace string) {
			t.Helper()
			// The path is a hash of a hash, so it is discovered rather than
			// computed: one fire creates the marker, and the file it wrote is
			// then replaced by a directory of the same name.
			runFire(t, "claude", subagentPayload("PreToolUse", workspace, "a84a3676a847c5c0b"))
			found := markerFiles(t, home)
			if len(found) != 1 {
				t.Fatalf("setup: want one marker to occupy, found %v", found)
			}
			if err := os.Remove(found[0]); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(found[0], 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		"read-only home": {seedable: true, wantMarkers: 0, breakIt: func(t *testing.T, home, _ string) {
			t.Helper()
			// Read-only the whole way down to the mail directory. Chmodding
			// only $HOME would not be a break at all once mail has been seeded:
			// `~/.auto/mail` already exists by then and is still writable, so
			// the marker write would quietly succeed and the case would prove
			// nothing. Read and execute stay set, so the flag the nudge stats
			// is still reachable — which is the state under test.
			dirs := []string{home, filepath.Join(home, ".auto"), mailDirOf(home)}
			// Innermost first: chmodding $HOME before its children would leave
			// nothing writable to descend into.
			for _, dir := range slices.Backward(dirs) {
				if err := os.Chmod(dir, 0o500); err != nil {
					t.Fatal(err)
				}
			}
			// Restored outermost-first, before the temp dir's own cleanup runs,
			// which would otherwise fail to remove a tree it cannot write to.
			t.Cleanup(func() {
				for _, dir := range dirs {
					_ = os.Chmod(dir, 0o700)
				}
			})
		}},
		// Not a broken path so much as an absent one, and the marker is written
		// anyway: recording which Subagent is acting must not depend on
		// `auto mail init` ever having run, because the hook cannot make it run
		// and a Subagent whose supervisor has not initialised mail still has to
		// reach a refusal rather than a crash.
		"home where auto mail init never ran": {seedable: false, wantMarkers: 1, breakIt: func(*testing.T, string, string) {}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			home := isolateHookEnv(t)
			workspace := initGitRepo(t, "main")

			// Seeded before the break, because seeding writes to the very
			// places the break makes unwritable.
			if tc.seedable {
				seedPendingMail(t, home, workspace, "auto-web/bugs")
			}
			tc.breakIt(t, home, workspace)

			// PostToolUse carries both halves at once: it refreshes the marker
			// and it is the only event the nudge is emitted on.
			out := runFire(t, "claude", subagentPayload("PostToolUse", workspace, "a84a3676a847c5c0b"))
			additional := assertAtMostOneObject(t, out)

			if tc.seedable {
				if !strings.Contains(additional, "auto mail list") {
					t.Errorf("additionalContext = %q, want T1's nudge — a hook that survives "+
						"a broken marker path by going silent has still broken the agent", additional)
				}
			} else if additional != "" {
				t.Errorf("a home with no mail store emitted %q, want nothing", additional)
			}

			if got := markerFiles(t, home); len(got) != tc.wantMarkers {
				t.Errorf("%d marker files after the fire, want %d: %v — a case whose break "+
					"did not actually break the marker write proves nothing",
					len(got), tc.wantMarkers, got)
			}

			// SubagentStop through the same broken state, for the same reasons.
			assertAtMostOneObject(t, runFire(t, "claude",
				subagentPayload("SubagentStop", workspace, "a84a3676a847c5c0b")))
		})
	}
}

// TestFireSurvivesAnAgentIDWithNoCwd: `cwd` is absent from the payload while
// `agent_id` is present.
//
// It is its own case because the two fields are read by different code for
// different purposes — `cwd` is what the binding is derived from — and a
// payload carrying one without the other is the shape most likely to be
// assumed impossible. The hook falls back to its own working directory, which
// for an in-process Subagent is the supervisor's, so the binding is still the
// right one and the nudge still lands.
func TestFireSurvivesAnAgentIDWithNoCwd(t *testing.T) {
	home := isolateHookEnv(t)
	workspace := initGitRepo(t, "main")
	seedPendingMail(t, home, workspace, "auto-web/bugs")
	t.Chdir(workspace)

	context := assertAtMostOneObject(t, runFire(t, "claude",
		subagentPayload("PostToolUse", "", "a84a3676a847c5c0b")))
	if !strings.Contains(context, "auto mail list") {
		t.Errorf("additionalContext = %q, want the nudge — with no cwd in the payload the "+
			"hook falls back to its own, which is the supervisor's", context)
	}
	if got := markerFiles(t, home); len(got) != 1 {
		t.Errorf("a payload with agent_id and no cwd wrote %d marker files, want 1: %v",
			len(got), got)
	}
}

// TestFireEmitsOneObjectWithMailHintAndASubagentPayload: the marker path is a
// third producer sharing the hook's single turn, and D-062-9's rule is that
// stdout carries one object however many producers had something to do.
//
// The marker writes nothing to stdout, so the observable claim is that adding
// it changed neither the count nor the order: mail first, hint second, one
// object — with a Subagent's payload driving all three.
func TestFireEmitsOneObjectWithMailHintAndASubagentPayload(t *testing.T) {
	home := isolateHookEnv(t)
	workspace := initGitRepo(t, "feat/login")
	writeHooksConfig(t, workspace, validHooksConfig)
	seedPendingMail(t, home, workspace, "auto-web/bugs")

	payload := toJSON(map[string]any{
		"hook_event_name": "PostToolUse",
		"session_id":      "sess-supervisor",
		"cwd":             workspace,
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "git push -u origin HEAD"},
		"agent_id":        "a84a3676a847c5c0b",
		"agent_type":      "Explore",
	})
	context := assertAtMostOneObject(t, runFire(t, "claude", payload))

	nudgeAt := strings.Index(context, "auto mail list")
	hintAt := strings.Index(context, "feat/login")
	if nudgeAt < 0 || hintAt < 0 {
		t.Fatalf("additionalContext = %q, want both the nudge and the matching hint", context)
	}
	if nudgeAt > hintAt {
		t.Errorf("additionalContext = %q, want mail first — the ordering is fixed so a "+
			"project's hint rules can never suppress the nudge", context)
	}
	if got := markerFiles(t, home); len(got) != 1 {
		t.Errorf("the same fire wrote %d marker files, want 1: %v", len(got), got)
	}
}
