package miner

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	sharedgit "github.com/mistakenot/auto-shared/git"
	sharedmodel "github.com/mistakenot/auto-shared/model"

	"github.com/mistakenot/auto-reflect/internal/etlread"
	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
)

// Version is the current miner version. Sessions terminal at this version are
// excluded from the queue; bumping it causes all sessions to be re-mined.
const Version = 1

// minedState is the coverage fold for one session.
type minedState struct {
	MaxTerminalVersion int
	LastStatus         events.AckStatus
	LastObservations   int
	LastPriorityScore  float64
	LastSignals        events.Signals
	LastMinedAt        time.Time
	AckCount           int
}

// PriorAck is the prior ack marker returned on each work item.
type PriorAck struct {
	Version      int              `json:"version"`
	Status       events.AckStatus `json:"status"`
	Observations int              `json:"observations"`
	TS           string           `json:"ts"`
}

// WorkItem is one ranked queue entry returned by Next.
type WorkItem struct {
	SessionID     string         `json:"session_id"`
	CWD           string         `json:"cwd"`
	LastMessageAt int64          `json:"last_message_at"`
	MessageCount  int            `json:"message_count"`
	PriorityScore float64        `json:"priority_score"`
	Signals       events.Signals `json:"signals"`
	PriorAck      *PriorAck      `json:"prior_ack"`
	Remined       bool           `json:"remined"`
	FetchCmd      string         `json:"fetch_cmd"`
	Subagents     []string       `json:"subagents,omitempty"`
}

// SignalRow is the read-only signal + ack history for one session.
type SignalRow struct {
	SessionID  string         `json:"session_id"`
	Signals    events.Signals `json:"signals"`
	AckHistory []PriorAck     `json:"ack_history"`
}

// NextOpts configures the Next call.
type NextOpts struct {
	Limit            int
	All              bool // widen scope to all workspaces
	IncludeSubagents bool
}

// FoldCoverage folds session_mined events into per-session coverage state.
// A session is terminal at version V only if it has a mined/empty/skipped ack at V.
// "failed" acks are recorded but never make a session terminal (it stays retryable).
func FoldCoverage(evs []events.Event) map[string]minedState {
	out := make(map[string]minedState)
	for i := range evs {
		ev := &evs[i]
		if ev.Type != events.TypeSessionMined {
			continue
		}
		var p events.SessionMinedPayload
		if err := decodePayload(ev, &p); err != nil {
			continue
		}
		s := out[p.SessionID]
		s.AckCount++
		s.LastStatus = p.Status
		s.LastObservations = p.Observations
		s.LastPriorityScore = p.PriorityScore
		s.LastSignals = p.Signals
		if ts, err := time.Parse(time.RFC3339Nano, ev.TS); err == nil {
			s.LastMinedAt = ts
		}
		// Only mined/empty/skipped are terminal; failed is retryable
		if p.Status != events.AckFailed && p.MinerVersion > s.MaxTerminalVersion {
			s.MaxTerminalVersion = p.MinerVersion
		}
		out[p.SessionID] = s
	}
	return out
}

// Next returns ranked, content-free work items for unmined sessions.
func Next(repoRoot, etlRoot string, opts NextOpts) ([]WorkItem, error) {
	sessions, err := etlread.ReadSessions(etlRoot)
	if err != nil {
		return nil, fmt.Errorf("read sessions: %w", err)
	}

	allMsgs, err := etlread.ReadMessageSignals(etlRoot)
	if err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}

	allEvents, err := events.ReadAll(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}

	coverage := FoldCoverage(allEvents)

	// Scope filter
	var scopeRemote string
	var scopeWorkspace string
	if !opts.All {
		repo, err := gitutil.DetectRepoLenient(repoRoot)
		if err == nil {
			scopeRemote = normalizeRemote(repo.Remote)
			if scopeRemote == "" {
				scopeWorkspace = repo.Root
			}
		}
	}

	// Filter to top-level, in-scope, unmined sessions
	var candidates []sharedmodel.AgentSession
	for i := range sessions {
		s := &sessions[i]
		if s.IsSubagent {
			continue
		}
		if !opts.All {
			if scopeRemote != "" {
				if normalizeRemote(s.GitRemote) != scopeRemote {
					continue
				}
			} else if scopeWorkspace != "" {
				if !strings.HasPrefix(s.Workspace, scopeWorkspace) {
					continue
				}
			}
		}
		// Check coverage: exclude if terminal at current version
		if state, ok := coverage[s.ID]; ok && state.MaxTerminalVersion >= Version {
			continue
		}
		candidates = append(candidates, *s)
	}

	// Group messages by session for signal computation
	msgsBySession := groupMessagesBySession(allMsgs)

	// Build work items
	seen := make(map[string]bool)
	var items []WorkItem
	for i := range candidates {
		s := &candidates[i]
		if seen[s.ID] {
			continue // dedupe
		}
		seen[s.ID] = true

		msgs := msgsBySession[s.ID]
		sig := ComputeSignals(msgs)
		score := Score(sig)

		item := WorkItem{
			SessionID:     s.ID,
			CWD:           s.Workspace,
			LastMessageAt: s.LastMessageAt,
			MessageCount:  sig.MessageCount,
			PriorityScore: score,
			Signals:       sig,
			FetchCmd:      fmt.Sprintf("auto search session get %s", s.ID),
		}

		// Prior ack info
		if state, ok := coverage[s.ID]; ok && state.AckCount > 0 {
			item.PriorAck = &PriorAck{
				Version:      state.MaxTerminalVersion,
				Status:       state.LastStatus,
				Observations: state.LastObservations,
				TS:           state.LastMinedAt.Format(time.RFC3339Nano),
			}
			item.Remined = true
		}

		// Subagents
		if opts.IncludeSubagents {
			for j := range sessions {
				sub := &sessions[j]
				if sub.IsSubagent && sub.ParentSessionID == s.ID {
					item.Subagents = append(item.Subagents, sub.ID)
				}
			}
		}

		items = append(items, item)
	}

	// Sort by descending priority score
	sort.Slice(items, func(i, j int) bool {
		return items[i].PriorityScore > items[j].PriorityScore
	})

	if opts.Limit > 0 && len(items) > opts.Limit {
		items = items[:opts.Limit]
	}

	return items, nil
}

// PendingCount returns the number of in-scope top-level sessions not terminal at current version.
// Used by reflect stats to show the input backlog.
func PendingCount(repoRoot, etlRoot string) (int, etlread.SourceState, error) {
	src, err := etlread.ResolveSource(etlRoot)
	if err != nil {
		return 0, src, err
	}
	if src != etlread.SourceOK {
		return 0, src, nil
	}

	sessions, err := etlread.ReadSessions(etlRoot)
	if err != nil {
		return 0, etlread.SourceOK, err
	}

	allEvents, err := events.ReadAll(repoRoot)
	if err != nil {
		return 0, etlread.SourceOK, err
	}
	coverage := FoldCoverage(allEvents)

	// Scope by current repo
	var scopeRemote string
	var scopeWorkspace string
	repo, err := gitutil.DetectRepoLenient(repoRoot)
	if err == nil {
		scopeRemote = normalizeRemote(repo.Remote)
		if scopeRemote == "" {
			scopeWorkspace = repo.Root
		}
	}

	count := 0
	for i := range sessions {
		s := &sessions[i]
		if s.IsSubagent {
			continue
		}
		if scopeRemote != "" {
			if normalizeRemote(s.GitRemote) != scopeRemote {
				continue
			}
		} else if scopeWorkspace != "" {
			if !strings.HasPrefix(s.Workspace, scopeWorkspace) {
				continue
			}
		}
		if state, ok := coverage[s.ID]; ok && state.MaxTerminalVersion >= Version {
			continue
		}
		count++
	}

	return count, etlread.SourceOK, nil
}

// Describe returns signals + full ack history for one session, regardless of ack/subagent state.
// Read-only: no events are written.
func Describe(repoRoot, etlRoot, sessionID string) (SignalRow, error) {
	rows, err := SignalsFor(repoRoot, etlRoot, []string{sessionID})
	if err != nil {
		return SignalRow{}, err
	}
	if len(rows) == 0 {
		return SignalRow{}, fmt.Errorf("session %q not found in ETL data", sessionID)
	}
	return rows[0], nil
}

// SignalsFor returns signal rows for the given session IDs, regardless of ack/subagent state.
// Read-only: no events are written.
func SignalsFor(repoRoot, etlRoot string, ids []string) ([]SignalRow, error) {
	allMsgs, err := etlread.ReadMessageSignals(etlRoot)
	if err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}

	allEvents, err := events.ReadAll(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}

	msgsBySession := groupMessagesBySession(allMsgs)
	ackHistory := foldAckHistory(allEvents)

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	// Also read sessions to verify existence
	sessions, err := etlread.ReadSessions(etlRoot)
	if err != nil {
		return nil, fmt.Errorf("read sessions: %w", err)
	}
	sessionExists := make(map[string]bool, len(sessions))
	for i := range sessions {
		sessionExists[sessions[i].ID] = true
	}

	var rows []SignalRow
	for _, id := range ids {
		if !idSet[id] {
			continue // already processed (dedupe)
		}
		idSet[id] = false

		if !sessionExists[id] {
			continue // session not in ETL data
		}

		msgs := msgsBySession[id]
		sig := ComputeSignals(msgs)

		rows = append(rows, SignalRow{
			SessionID:  id,
			Signals:    sig,
			AckHistory: ackHistory[id],
		})
	}

	return rows, nil
}

func groupMessagesBySession(msgs []etlread.MsgSignalRow) map[string][]etlread.MsgSignalRow {
	out := make(map[string][]etlread.MsgSignalRow)
	for i := range msgs {
		m := &msgs[i]
		out[m.SessionID] = append(out[m.SessionID], *m)
	}
	return out
}

func foldAckHistory(evs []events.Event) map[string][]PriorAck {
	out := make(map[string][]PriorAck)
	for i := range evs {
		ev := &evs[i]
		if ev.Type != events.TypeSessionMined {
			continue
		}
		var p events.SessionMinedPayload
		if err := decodePayload(ev, &p); err != nil {
			continue
		}
		out[p.SessionID] = append(out[p.SessionID], PriorAck{
			Version:      p.MinerVersion,
			Status:       p.Status,
			Observations: p.Observations,
			TS:           ev.TS,
		})
	}
	return out
}

// normalizeRemote produces a stable comparison key from a remote URL. The
// session data (from auto-etl) uses sharedgit.NormalizeRemoteURL which yields
// "https://host/path", while gitutil.DetectRepoLenient strips the scheme to
// "host/path". We normalise via sharedgit then strip the scheme so both
// origins produce the same key.
func normalizeRemote(raw string) string {
	n := sharedgit.NormalizeRemoteURL(raw)
	// Strip scheme prefix for a scheme-agnostic comparison key.
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://"} {
		if strings.HasPrefix(n, prefix) {
			return strings.TrimPrefix(n, prefix)
		}
	}
	return n
}

// decodePayload decodes a JSON event payload.
func decodePayload(ev *events.Event, target any) error {
	return json.Unmarshal(ev.Payload, target)
}
