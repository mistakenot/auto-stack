package mail_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

// markerFileFor returns the single marker file under a binding's directory,
// failing if there is not exactly one. Tests that need to corrupt a marker have
// to find it first, and the path is a hash of a hash by design (D-063-9).
func markerFileFor(t *testing.T, home string, b mail.Binding) string {
	t.Helper()
	root := config.AgentsDirIn(home)
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one marker file under %s, found %v", root, found)
	}
	return found[0]
}

// TestMarkerExpiresAtTheTTLWithoutSleeping is AC-9's expiry half.
//
// The TTL that ships is fifteen minutes, so the clock is injected rather than
// waited on: sleeping past a real TTL would make the suite take a quarter of an
// hour, and shortening the constant for the test would leave the shipped number
// untested. Both edges are asserted, because a test that only checks expiry
// passes just as well against a marker that was never believed at all.
func TestMarkerExpiresAtTheTTLWithoutSleeping(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())

	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	restore := mail.SetMarkerClock(func() time.Time { return at })
	t.Cleanup(restore)

	mail.ObserveHookEvent(home, binding, "PreToolUse",
		mail.ActiveAgent{AgentID: "a84a3676a847c5c0b", AgentType: "Explore"})

	// A minute short of the TTL the Subagent is still working. A Subagent's
	// thinking time between tool calls is unbounded, and expiring one that is
	// alive would refuse a legitimate `#parent`.
	at = at.Add(mail.SubagentTTL() - time.Minute)
	if got := mail.ActiveSubagents(home, binding); len(got) != 1 {
		t.Fatalf("a marker one minute short of the TTL = %+v, want it still live", got)
	}
	if got := mail.CallerSender(home, binding); got.Kind != mail.SenderSubagent {
		t.Errorf("CallerSender just inside the TTL = %+v, want a Subagent", got)
	}

	// A minute past it, the crash backstop fires.
	at = at.Add(2 * time.Minute)
	if got := mail.ActiveSubagents(home, binding); len(got) != 0 {
		t.Fatalf("a marker past the TTL = %+v, want it ignored", got)
	}
	if got := mail.CallerSender(home, binding); got.Kind != mail.SenderAgent {
		t.Errorf("CallerSender past the TTL = %+v, want an ordinary agent — a stale marker "+
			"must make `#parent` refuse with a hint, never resolve to a guess", got)
	}

	// Ignored is not enough on its own: a marker that is skipped forever but
	// never unlinked is an unbounded directory on a long-lived host.
	if got := countMarkers(t, home); got != 0 {
		t.Errorf("%d expired marker files survived a read, want 0 — expiry cleans up "+
			"opportunistically rather than accumulating", got)
	}
}

// TestAnUnremovableExpiredMarkerIsStillIgnored is 055's safe order made
// observable: the entry is dropped from the *answer* first and unlinked second,
// so a removal that fails leaves the file for the next pass instead of
// half-forgetting it — and never turns a cleanup failure into a wrong answer.
func TestAnUnremovableExpiredMarkerIsStillIgnored(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())

	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	restore := mail.SetMarkerClock(func() time.Time { return at })
	t.Cleanup(restore)

	mail.ObserveHookEvent(home, binding, "PreToolUse",
		mail.ActiveAgent{AgentID: "a84a3676a847c5c0b", AgentType: "Explore"})
	marker := markerFileFor(t, home, binding)

	// Unlinking is a permission on the *directory*, not on the file, so this is
	// the only way to make the removal fail while the read still succeeds.
	dir := filepath.Dir(marker)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	at = at.Add(mail.SubagentTTL() + time.Minute)
	if got := mail.ActiveSubagents(home, binding); len(got) != 0 {
		t.Errorf("an expired marker that could not be unlinked = %+v, want it ignored — "+
			"the answer must not depend on the cleanup succeeding", got)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the marker was reported gone but is not on disk (%v); a failed removal "+
			"is left for the next pass, never forgotten as though it had worked", err)
	}
}

// TestATruncatedMarkerReadsAsAbsent is AC-9's unparseable half. Every shape a
// half-written or corrupt marker can take reads as "no Subagent here", never as
// an error and never as a marker with empty fields — and a live sibling in the
// same directory is unaffected, so one bad file cannot take the answer down.
func TestATruncatedMarkerReadsAsAbsent(t *testing.T) {
	corruptions := map[string][]byte{
		"empty file":            {},
		"truncated mid-object":  []byte(`{"agentId":"a84a3676a8`),
		"not JSON at all":       []byte("\x00\x01\x02 not json"),
		"JSON but not a marker": []byte(`{"agentId":""}`),
	}

	for name, content := range corruptions {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			binding := mail.BindingFromContext(nil, t.TempDir())

			mail.ObserveHookEvent(home, binding, "PreToolUse",
				mail.ActiveAgent{AgentID: "corrupt-agent", AgentType: "Explore"})
			if err := os.WriteFile(markerFileFor(t, home, binding), content, 0o644); err != nil {
				t.Fatalf("corrupt the marker: %v", err)
			}

			if got := mail.ActiveSubagents(home, binding); len(got) != 0 {
				t.Fatalf("a corrupt marker read as %+v, want absent", got)
			}
			if got := mail.CallerSender(home, binding); got.Kind != mail.SenderAgent {
				t.Errorf("CallerSender over a corrupt marker = %+v, want an ordinary agent", got)
			}

			// A second, healthy Subagent in the same directory is unaffected:
			// the skip is per file, not per directory.
			mail.ObserveHookEvent(home, binding, "PreToolUse",
				mail.ActiveAgent{AgentID: "healthy-agent", AgentType: "Explore"})
			active := mail.ActiveSubagents(home, binding)
			if len(active) != 1 || active[0].AgentID != "healthy-agent" {
				t.Errorf("with one corrupt and one healthy marker, active = %+v, "+
					"want just the healthy one", active)
			}
		})
	}
}

// TestMarkerWritesLeaveNoTemporaryBehind: the marker is written atomically
// through a temporary and a rename (see writeAgentMarker), and the temporary is
// an implementation detail that must never survive as state. Litter here is not
// cosmetic — nothing expires a file no reader ever parses, so a leaked
// temporary would live on the host forever.
func TestMarkerWritesLeaveNoTemporaryBehind(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())
	agent := mail.ActiveAgent{AgentID: "a84a3676a847c5c0b", AgentType: "Explore"}

	for range 20 {
		mail.ObserveHookEvent(home, binding, "PreToolUse", agent)
		mail.ObserveHookEvent(home, binding, "PostToolUse", agent)
	}

	if got := countMarkers(t, home); got != 1 {
		t.Errorf("40 refreshes left %d files, want 1 — a temporary outlived its rename", got)
	}
	// SubagentStop removes the marker by name, so a temporary would also
	// survive the one thing that is supposed to clear the directory.
	mail.ObserveHookEvent(home, binding, "SubagentStop", agent)
	if got := countMarkers(t, home); got != 0 {
		t.Errorf("%d files survived SubagentStop, want 0", got)
	}
}

// TestAMarkerMidWriteIsNeverASecondAnswer: a dot-prefixed file is a marker
// being written, and a reader must skip it.
//
// It matters for attribution rather than tidiness. Counting a temporary
// alongside the marker it is about to replace would make one live Subagent look
// like two, which is exactly the input that flips CallerSender to Ambiguous —
// so a reader that did not skip it would lose the sender's name for no reason
// at all (D-063-11).
func TestAMarkerMidWriteIsNeverASecondAnswer(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())

	mail.ObserveHookEvent(home, binding, "PreToolUse",
		mail.ActiveAgent{AgentID: "a84a3676a847c5c0b", AgentType: "Explore"})
	dir := filepath.Dir(markerFileFor(t, home, binding))

	// A well-formed marker under a dot-prefixed name: the worst case, because
	// it parses. A reader that does not skip by name would believe it.
	planted, err := json.Marshal(mail.ActiveAgent{
		AgentID: "a84a3676a847c5c0b", AgentType: "Explore", At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".tmp-1234567"), planted, 0o644); err != nil {
		t.Fatal(err)
	}

	active := mail.ActiveSubagents(home, binding)
	if len(active) != 1 {
		t.Fatalf("a marker mid-write was counted: active = %+v, want 1", active)
	}
	got := mail.CallerSender(home, binding)
	if got.Ambiguous || got.AgentType != "Explore" {
		t.Errorf("CallerSender = %+v, want the one live Subagent named and no ambiguity — "+
			"a temporary must not make one agent look like two", got)
	}
}

// TestARefreshNeverMakesALiveMarkerVanish is why the write is atomic.
//
// A truncating write has a window in which the marker is on disk with zero
// bytes in it, and an unparseable marker reads as *absent* — so a plain
// os.WriteFile can make a live Subagent disappear mid-refresh. That does not
// degrade to "no answer": with a sibling live it degrades to a confident wrong
// name, which D-063-11 exists to prevent. A rename has no such window.
//
// Phase 4 generalises this to N agents under `-race`; this is the narrow
// property the atomicity decision was made for.
func TestARefreshNeverMakesALiveMarkerVanish(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())
	agent := mail.ActiveAgent{AgentID: "a84a3676a847c5c0b", AgentType: "Explore"}

	mail.ObserveHookEvent(home, binding, "PreToolUse", agent)

	done := make(chan struct{})
	var writer sync.WaitGroup
	writer.Go(func() {
		for {
			select {
			case <-done:
				return
			default:
				mail.ObserveHookEvent(home, binding, "PostToolUse", agent)
			}
		}
	})

	for i := range 500 {
		if got := mail.ActiveSubagents(home, binding); len(got) != 1 {
			close(done)
			writer.Wait()
			t.Fatalf("read %d during a refresh saw %+v, want the one live Subagent — "+
				"the marker vanished mid-write, which is what the rename exists to prevent",
				i, got)
		}
	}
	close(done)
	writer.Wait()
}

// TestTheMarkerPathNeverOpensTheStore is AC-7's counted half, for the path that
// actually does work.
//
// Phase 1 counted the opens an *agent-less* payload performs, which is the
// easy direction — it returns before touching anything. This counts the path
// that writes, reads, expires and resolves, because that is where a store open
// would plausibly be added: resolution is about a Subscription, and reaching
// for the store to find one would be the natural mistake. The counter is the
// only instrument that can say this. A stopwatch cannot: a fast machine passes
// a timing test that opens SQLite anyway.
func TestTheMarkerPathNeverOpensTheStore(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())

	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	restoreClock := mail.SetMarkerClock(func() time.Time { return at })
	t.Cleanup(restoreClock)

	opens, restore := mail.CountStoreOpens()
	t.Cleanup(restore)

	agent := mail.ActiveAgent{AgentID: "a84a3676a847c5c0b", AgentType: "Explore"}
	mail.ObserveHookEvent(home, binding, "PreToolUse", agent)
	mail.ObserveHookEvent(home, binding, "PostToolUse", agent)
	_ = mail.ActiveSubagents(home, binding)
	_ = mail.CallerSender(home, binding)

	// Including the expiry pass, which unlinks — a cleanup is the other place
	// something might reasonably want to consult the store.
	at = at.Add(mail.SubagentTTL() + time.Minute)
	_ = mail.ActiveSubagents(home, binding)
	_ = mail.CallerSender(home, binding)
	mail.ObserveHookEvent(home, binding, "SubagentStop", agent)

	if got := opens(); got != 0 {
		t.Errorf("the marker path opened the store %d times, want 0 — it runs on every "+
			"tool call of every agent on the host (G8/AC-7)", got)
	}
}
