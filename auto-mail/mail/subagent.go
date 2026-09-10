package mail

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mistakenot/auto-mail/internal/config"
)

// subagentTTL bounds how long a marker is believed after its last refresh.
//
// It is a crash backstop, not the cleanup mechanism: a Subagent's own
// SubagentStop removes its marker, and that event is installed for both agents.
// A TTL short enough to be the primary cleanup would have to be shorter than a
// Subagent's thinking time between tool calls, which is unbounded — it would
// expire live agents. So it is generous, and it costs nothing to be: a stale
// marker can only ever make the *supervisor's* own `#parent` resolve to its own
// address instead of failing, which is the harmless direction (D-063-9).
const subagentTTL = 15 * time.Minute

// markerNow is the clock every part of the marker lifecycle reads — the stamp a
// write leaves and the cutoff a read expires against, so the two can never
// disagree about what "now" was.
//
// It is a package var solely so a test can age a marker by an injected hour
// instead of sleeping for one. A TTL measured in minutes has no test that a
// stopwatch could write: sleeping past it would make the suite take a quarter
// of an hour, and shortening the constant to suit the test would mean the
// number under test was never the number that ships.
var markerNow = func() time.Time { return time.Now().UTC() }

// ActiveAgent is one Subagent the hook has seen acting under a Binding.
//
// SessionID is recorded for diagnostics only. Resolution never keys on it: an
// in-process Subagent's hook events carry its *supervisor's* session_id, so it
// identifies the pair rather than the child, and the Binding already does that
// job better (D-063-4).
type ActiveAgent struct {
	AgentID   string    `json:"agentId"`
	AgentType string    `json:"agentType,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
	At        time.Time `json:"at"`
}

// agentsDir is the per-binding directory this binding's markers live in. The
// directory name is T1's flagName — the same hash of the same pair — because
// the marker and the pending flag answer questions about the same join key,
// and two spellings of one key is how they would eventually disagree.
func agentsDir(home string, b Binding) string {
	return filepath.Join(config.AgentsDirIn(home), flagName(b))
}

// markerTempPrefix marks a marker mid-write. The dot is what makes it
// invisible to ActiveSubagents, and a hex hash can never begin with one, so a
// real marker and a temporary can never be mistaken for each other.
const markerTempPrefix = ".tmp-"

// agentMarkerPath is the one file an ActiveAgent owns.
//
// The id is hashed rather than used verbatim for the reason T1 hashed the
// binding pair: observed ids are filename-safe today (`a84a3676a847c5c0b`), but
// that is an upstream format nobody promised us, and a `/` in one would write
// the marker somewhere else entirely. Hashing also fixes the *shape* of the
// name — 16 hex characters, never a leading dot — which is what lets a
// dot-prefixed temporary be skipped by readers without any other bookkeeping.
func agentMarkerPath(home string, b Binding, agentID string) string {
	sum := sha256.Sum256([]byte(agentID))
	return filepath.Join(agentsDir(home, b), hex.EncodeToString(sum[:8]))
}

// ObserveHookEvent records, from a hook payload, that a Subagent is acting
// under this Binding — or that one has stopped.
//
// This is the hook's one entry point into the Subagent bridge, and it inherits
// HasPending's contract wholesale (G8/D-062-3): no error return, no store open,
// and every failure — an unwritable directory, a home that cannot be resolved,
// a marker path occupied by a directory — read as "nothing recorded". It runs
// on every tool call of every agent on the host, and a caller in that position
// has nothing useful to do with a failure except ignore it.
//
// An empty AgentID is the ordinary agent's case and returns before touching the
// filesystem at all: a supervisor's own tool call must cost exactly what it
// cost before Subagent markers existed, not one syscall more (AC-7).
//
// PreToolUse is the load-bearing write. It fires immediately before the
// Subagent's own `auto mail send`, so the marker is fresh at the moment the
// CLI reads it whatever the child was doing beforehand — which is what makes
// "I hit a wall on my first action" work rather than being an edge case.
func ObserveHookEvent(home string, b Binding, event string, a ActiveAgent) {
	if a.AgentID == "" || !addressable(home, b) {
		return
	}
	switch strings.ToLower(event) {
	case "pretooluse", "posttooluse":
		if a.At.IsZero() {
			a.At = markerNow()
		}
		writeAgentMarker(home, b, a)
	case "subagentstop":
		// Only this agent's own marker, never the directory: a sibling still
		// working under the same Binding must keep its own (D-063-9).
		_ = os.Remove(agentMarkerPath(home, b, a.AgentID))
	}
}

// writeAgentMarker creates or refreshes one marker, atomically: a temporary
// file in the same directory, then a rename over the marker path. Every error
// is swallowed; see ObserveHookEvent for why the hook has no other option.
//
// The atomicity is load-bearing rather than defensive, and it is worth the one
// extra create and rename. A truncating write has a window in which the marker
// is on disk with zero bytes in it, and an unparseable marker reads as *absent*
// — so a refresh of one Subagent's marker can momentarily make it vanish from
// ActiveSubagents while its sibling's stays. That does not degrade to "no
// answer"; it degrades to a *confident wrong* one, because two live Subagents
// briefly look like one and CallerSender then names whichever survived the
// window (D-063-11). A rename has no such window: a reader sees the previous
// marker or the new one, never neither.
func writeAgentMarker(home string, b Binding, a ActiveAgent) {
	encoded, err := json.Marshal(a)
	if err != nil {
		return
	}
	dir := agentsDir(home, b)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	// The temporary lives in the destination directory because rename is only
	// atomic within one filesystem, and it is dot-prefixed because it is
	// briefly visible to a concurrent ActiveSubagents — which skips dotfiles
	// for exactly this reason, so a half-written marker is never a second
	// answer for an agent that already has one.
	tmp, err := os.CreateTemp(dir, markerTempPrefix+"*")
	if err != nil {
		return
	}
	name := tmp.Name()
	written := func() bool {
		if _, err := tmp.Write(encoded); err != nil {
			return false
		}
		// CreateTemp opens 0600; the marker keeps the flag directory's 0644 so
		// one directory does not hold two permission conventions.
		if err := tmp.Chmod(0o644); err != nil {
			return false
		}
		return tmp.Close() == nil
	}()
	if !written {
		_ = tmp.Close()
		_ = os.Remove(name)
		return
	}
	if err := os.Rename(name, agentMarkerPath(home, b, a.AgentID)); err != nil {
		// A failed rename must not leave litter behind: the temporary is
		// invisible to readers but would otherwise never be collected, since
		// nothing expires a file no reader ever parses.
		_ = os.Remove(name)
	}
}

// ActiveSubagents returns the unexpired markers under a Binding, newest first.
//
// It is exported because a marker is otherwise unobservable from outside this
// package (G11) and both the tests and a future `doctor` need to see one. The
// send path goes through CallerSender instead, which asks the narrower question
// this answer is only ever used for.
//
// Reading is one os.ReadDir plus one small read per entry, and it happens on
// `send` — a deliberate action — never in the hook. The number of entries under
// one Binding is bounded by concurrent Subagents, which is single digits.
func ActiveSubagents(home string, b Binding) []ActiveAgent {
	if !addressable(home, b) {
		return nil
	}
	dir := agentsDir(home, b)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	cutoff := markerNow().Add(-subagentTTL)
	out := make([]ActiveAgent, 0, len(entries))
	for _, entry := range entries {
		// Directories are not markers, and a dotfile is a marker mid-write —
		// the temporary writeAgentMarker is about to rename into place. Reading
		// one would count an agent that already has a marker of its own twice.
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var agent ActiveAgent
		if err := json.Unmarshal(raw, &agent); err != nil || agent.AgentID == "" {
			continue
		}
		if agent.At.Before(cutoff) {
			// Opportunistic, and in the safe order: the entry is dropped from
			// the answer first and only then unlinked, so a removal that fails
			// leaves it for the next pass rather than half-forgetting it.
			_ = os.Remove(path)
			continue
		}
		out = append(out, agent)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// CallerSender establishes who the calling process is, from the markers the
// hook left under its own Binding.
//
// Filesystem only, no store, no error return: an unreadable directory reads as
// an ordinary agent, exactly as an unreadable flag reads as "no mail". The
// consequence of a false negative is a refused `#parent` with a remediation
// hint, which is a far better failure than a send that guesses.
//
// What the markers are consulted for is narrower than "who am I". `#parent`
// resolves from the Binding alone, and every concurrent Subagent of one
// supervisor shares that Binding — so the only thing this has to get right is
// the boolean *is a Subagent currently active here*, which racing writers all
// agree on (D-063-4). The name is the part that cannot be known under
// concurrency, and Ambiguous is how that is said out loud rather than guessed
// (D-063-11).
func CallerSender(home string, b Binding) Sender {
	active := ActiveSubagents(home, b)
	switch len(active) {
	case 0:
		return Sender{Kind: SenderAgent}
	case 1:
		return Sender{
			Kind:      SenderSubagent,
			AgentID:   active[0].AgentID,
			AgentType: active[0].AgentType,
		}
	default:
		// Several are live and this process has no agent_id of its own, so the
		// newest marker is a sibling's as often as it is this caller's. Naming
		// one would be a plausible wrong answer, which is the worst kind.
		return Sender{Kind: SenderSubagent, Ambiguous: true}
	}
}
