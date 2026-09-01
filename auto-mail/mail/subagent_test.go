package mail_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-mail/internal/config"
	"github.com/mistakenot/auto-mail/mail"
)

// countMarkers reports how many files exist under the whole marker tree. It
// counts *files*, not answers, because the properties under test here are about
// what reaches the disk — an answer can be right while the disk is wrong.
func countMarkers(t *testing.T, home string) int {
	t.Helper()
	root := config.AgentsDirIn(home)
	n := 0
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			n++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", root, err)
	}
	return n
}

// TestObserveHookEventIgnoresAPayloadWithNoAgent is AC-7's first half, made
// structural: an ordinary agent's tool call carries no agent_id, and the marker
// path must then do nothing at all — not create the directory, not stat a path,
// not open the store.
//
// The store-open count is the same instrument T1 used for the pending flag, and
// for the same reason: a timing assertion cannot say this, because a fast
// machine passes a stopwatch test that opens SQLite anyway.
func TestObserveHookEventIgnoresAPayloadWithNoAgent(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())

	opens, restore := mail.CountStoreOpens()
	t.Cleanup(restore)

	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		mail.ObserveHookEvent(home, binding, event, mail.ActiveAgent{AgentType: "Explore"})
	}

	if _, err := os.Stat(config.AgentsDirIn(home)); !os.IsNotExist(err) {
		t.Errorf("the marker directory exists after three agent-less hook events (stat err = %v); "+
			"an ordinary agent's hot path must be bit-for-bit what it was before markers existed", err)
	}
	if got := opens(); got != 0 {
		t.Errorf("the marker path opened the store %d times, want 0", got)
	}
	if got := mail.CallerSender(home, binding); got.Kind != mail.SenderAgent {
		t.Errorf("CallerSender = %+v, want an ordinary agent", got)
	}
}

// TestSubagentMarkerLifecycle: write, refresh, and clear — and clear only the
// agent that stopped.
//
// The per-agent file is the whole point of D-063-9. A single shared slot would
// let a sibling's SubagentStop erase a working Subagent's marker, which is the
// failure direction that makes a legitimate `#parent` fail intermittently.
func TestSubagentMarkerLifecycle(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())

	first := mail.ActiveAgent{AgentID: "a84a3676a847c5c0b", AgentType: "Explore", SessionID: "parent-session"}
	second := mail.ActiveAgent{AgentID: "ad24f740374961c32", AgentType: "task-063-planner", SessionID: "parent-session"}

	mail.ObserveHookEvent(home, binding, "PreToolUse", first)
	active := mail.ActiveSubagents(home, binding)
	if len(active) != 1 || active[0].AgentID != first.AgentID {
		t.Fatalf("after one PreToolUse, active = %+v, want just %q", active, first.AgentID)
	}
	if active[0].AgentType != "Explore" || active[0].SessionID != "parent-session" {
		t.Errorf("marker lost its fields: %+v", active[0])
	}
	if active[0].At.IsZero() {
		t.Errorf("marker has no timestamp, so the TTL can never expire it: %+v", active[0])
	}

	// A refresh from the same agent is one file, not two.
	mail.ObserveHookEvent(home, binding, "PostToolUse", first)
	if got := countMarkers(t, home); got != 1 {
		t.Errorf("a refresh wrote %d marker files, want 1", got)
	}

	mail.ObserveHookEvent(home, binding, "PreToolUse", second)
	if got := len(mail.ActiveSubagents(home, binding)); got != 2 {
		t.Fatalf("two Subagents under one binding = %d markers, want 2", got)
	}

	// Only the agent that stopped is cleared. The sibling is still working.
	mail.ObserveHookEvent(home, binding, "SubagentStop", second)
	active = mail.ActiveSubagents(home, binding)
	if len(active) != 1 || active[0].AgentID != first.AgentID {
		t.Fatalf("after %q stopped, active = %+v, want just %q still working",
			second.AgentID, active, first.AgentID)
	}

	mail.ObserveHookEvent(home, binding, "SubagentStop", first)
	if got := mail.ActiveSubagents(home, binding); len(got) != 0 {
		t.Errorf("after both stopped, active = %+v, want none", got)
	}
}

// TestSubagentMarkersArePerBinding: the marker answers a question about one
// (manager, target) pair, exactly as the pending flag does. Two agents on one
// host must not see each other's Subagents.
func TestSubagentMarkersArePerBinding(t *testing.T) {
	home := t.TempDir()
	supervisor := mail.BindingFromContext(nil, t.TempDir())
	stranger := mail.BindingFromContext(nil, t.TempDir())

	mail.ObserveHookEvent(home, supervisor, "PreToolUse",
		mail.ActiveAgent{AgentID: "a84a3676a847c5c0b", AgentType: "Explore"})

	if got := mail.CallerSender(home, supervisor); got.Kind != mail.SenderSubagent {
		t.Errorf("the marked binding reads as %+v, want a Subagent", got)
	}
	if got := mail.CallerSender(home, stranger); got.Kind != mail.SenderAgent {
		t.Errorf("an unrelated binding reads as %+v, want an ordinary agent — "+
			"a marker must never leak across bindings", got)
	}
}

// TestCallerSenderWillNotGuessBetweenConcurrentSubagents is D-063-11.
//
// With several Subagents live under one Binding the calling process has no
// agent_id of its own, so it cannot tell which marker is its own. The *kind*
// stays trustworthy — every one of them is a Subagent of the same supervisor,
// which is what `#parent` actually resolves on — but the name does not, and a
// sibling's name confidently reported is worse for a supervisor than no name.
func TestCallerSenderWillNotGuessBetweenConcurrentSubagents(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())

	mail.ObserveHookEvent(home, binding, "PreToolUse",
		mail.ActiveAgent{AgentID: "agent-one", AgentType: "phase1"})
	if got := mail.CallerSender(home, binding); got.Ambiguous || got.AgentType != "phase1" {
		t.Errorf("one live Subagent = %+v, want its name and no ambiguity", got)
	}

	mail.ObserveHookEvent(home, binding, "PreToolUse",
		mail.ActiveAgent{AgentID: "agent-two", AgentType: "phase2"})

	got := mail.CallerSender(home, binding)
	if got.Kind != mail.SenderSubagent {
		t.Errorf("kind = %q under concurrency, want %q — the kind is race-independent",
			got.Kind, mail.SenderSubagent)
	}
	if !got.Ambiguous {
		t.Errorf("Ambiguous = false with two live Subagents: %+v", got)
	}
	if got.AgentType != "" || got.AgentID != "" {
		t.Errorf("a name was reported under concurrency (%+v); it can only be a guess", got)
	}
}

// TestObserveHookEventSwallowsEveryFailure: the hook's contract, inherited from
// HasPending. A marker path that cannot be written, a home that does not exist,
// a marker path occupied by a directory — none of them may panic, return, or
// give a caller anything to fail on, because this runs on every tool call of
// every agent on the host.
func TestObserveHookEventSwallowsEveryFailure(t *testing.T) {
	binding := mail.BindingFromContext(nil, t.TempDir())
	agent := mail.ActiveAgent{AgentID: "a84a3676a847c5c0b", AgentType: "Explore"}

	// A home under a read-only parent: MkdirAll cannot create the tree.
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatalf("chmod %s: %v", readOnly, err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	blocked := filepath.Join(readOnly, "home")

	// A home with the marker directory replaced by a file, so MkdirAll fails
	// for a different reason than permissions.
	occupied := t.TempDir()
	if err := os.MkdirAll(config.MailDirIn(occupied), 0o755); err != nil {
		t.Fatalf("prepare %s: %v", occupied, err)
	}
	if err := os.WriteFile(config.AgentsDirIn(occupied), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("occupy the marker directory: %v", err)
	}

	for _, home := range []string{"", blocked, occupied} {
		for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop", "SessionStart"} {
			mail.ObserveHookEvent(home, binding, event, agent)
		}
		if got := mail.CallerSender(home, binding); got.Kind != mail.SenderAgent {
			t.Errorf("home %q: CallerSender = %+v, want an ordinary agent — "+
				"an unreadable marker directory reads as 'not a Subagent', never as an error",
				home, got)
		}
		if got := mail.ActiveSubagents(home, binding); got != nil {
			t.Errorf("home %q: ActiveSubagents = %+v, want nil", home, got)
		}
	}
}

// TestOnlyToolAndStopEventsTouchTheMarker: the write is scoped to the events
// that carry the fact, so an unrelated installed event cannot create state.
func TestOnlyToolAndStopEventsTouchTheMarker(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())
	agent := mail.ActiveAgent{AgentID: "a84a3676a847c5c0b", AgentType: "Explore"}

	for _, event := range []string{"SessionStart", "SessionEnd", "UserPromptSubmit", ""} {
		mail.ObserveHookEvent(home, binding, event, agent)
	}
	if got := countMarkers(t, home); got != 0 {
		t.Fatalf("%d markers after events that carry no Subagent fact, want 0", got)
	}

	// Case is not load-bearing: codex and claude spell their event names
	// differently, and the bus mapping already lowercases them.
	mail.ObserveHookEvent(home, binding, "pretooluse", agent)
	if got := countMarkers(t, home); got != 1 {
		t.Errorf("%d markers after a lowercased PreToolUse, want 1", got)
	}
}
