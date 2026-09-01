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

// agentMarkerPath is the one file an ActiveAgent owns.
//
// The id is hashed rather than used verbatim for the reason T1 hashed the
// binding pair: observed ids are filename-safe today (`a84a3676a847c5c0b`), but
// that is an upstream format nobody promised us, and a `/` in one would write
// the marker somewhere else entirely.
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
			a.At = time.Now().UTC()
		}
		writeAgentMarker(home, b, a)
	case "subagentstop":
		// Only this agent's own marker, never the directory: a sibling still
		// working under the same Binding must keep its own (D-063-9).
		_ = os.Remove(agentMarkerPath(home, b, a.AgentID))
	}
}

// writeAgentMarker creates or refreshes one marker. Every error is swallowed;
// see ObserveHookEvent for why the hook has no other option.
func writeAgentMarker(home string, b Binding, a ActiveAgent) {
	encoded, err := json.Marshal(a)
	if err != nil {
		return
	}
	dir := agentsDir(home, b)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	// One write of a few dozen bytes, truncating whatever was there. A torn
	// read is possible in principle and costs nothing in practice: an
	// unparseable marker is skipped by ActiveSubagents exactly as a missing one
	// is, and the next tool call rewrites it.
	_ = os.WriteFile(agentMarkerPath(home, b, a.AgentID), encoded, 0o644)
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
	cutoff := time.Now().UTC().Add(-subagentTTL)
	out := make([]ActiveAgent, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
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
