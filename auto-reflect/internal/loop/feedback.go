package loop

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mistakenot/auto-shared/config"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
)

// Feedback outcome enum values.
const (
	OutcomeSuccess   = "success"
	OutcomePartial   = "partial"
	OutcomeFail      = "fail"
	OutcomeAbandoned = "abandoned"
)

var validOutcomes = map[string]struct{}{
	OutcomeSuccess:   {},
	OutcomePartial:   {},
	OutcomeFail:      {},
	OutcomeAbandoned: {},
}

// FeedbackInput is the agent-submitted feedback document. It is the same shape as
// events.FeedbackPayload but is validated before any event is appended.
type FeedbackInput struct {
	Outcome  string                   `json:"outcome"`
	Summary  string                   `json:"summary"`
	Rankings []events.FeedbackRanking `json:"rankings"`
	Gap      *events.FeedbackGap      `json:"gap"`
}

// ParseFeedback decodes a feedback JSON document, rejecting unknown fields so a
// typo'd key is a hard error rather than silently dropped.
func ParseFeedback(raw []byte) (FeedbackInput, error) {
	var in FeedbackInput
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return FeedbackInput{}, &LoopError{Message: fmt.Sprintf("invalid feedback JSON: %v", err)}
	}
	return in, nil
}

// SubmitFeedback validates the payload against the outstanding feedback ids for
// the scope, and only on a fully valid payload appends exactly one feedback
// event. Validation errors are returned as structured errors; nothing is
// written when any error is present.
func (s *Service) SubmitFeedback(in FeedbackInput, sessionOverride string) ([]ValidationError, error) {
	repo, err := gitutil.DetectRepoLenient(s.cwd)
	if err != nil {
		return nil, err
	}

	all, err := events.ReadAll(repo.Root)
	if err != nil {
		return nil, err
	}

	scope := s.resolveScope(sessionOverride)
	outstanding := outstandingForScope(all, scope)

	if errs := validateFeedback(in, outstanding); len(errs) > 0 {
		return errs, nil
	}

	payload := events.FeedbackPayload{
		Outcome:  in.Outcome,
		Summary:  in.Summary,
		Rankings: in.Rankings,
		Gap:      in.Gap,
	}
	if _, err := events.AppendEvent(s.cwd, events.TypeFeedback, payload, events.AppendOptions{SessionID: scope.sessionID}); err != nil {
		return nil, err
	}
	return nil, nil
}

// validateFeedback enforces the payload schema: outcome enum, summary non-empty,
// rankings covering exactly the outstanding ids with a permutation of 1..N and a
// non-empty reason each, and grounded gap (both report and moment non-empty when
// gap is present).
func validateFeedback(in FeedbackInput, outstanding []string) []ValidationError {
	errs := make([]ValidationError, 0)

	if strings.TrimSpace(in.Outcome) == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "outcome", Message: "outcome is required"})
	} else if _, ok := validOutcomes[in.Outcome]; !ok {
		errs = append(errs, ValidationError{Code: "enum", Field: "outcome", Message: "outcome must be one of success, partial, fail, abandoned", Value: in.Outcome})
	}

	if strings.TrimSpace(in.Summary) == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "summary", Message: "summary is required"})
	}

	errs = append(errs, validateRankings(in.Rankings, outstanding)...)

	if in.Gap != nil {
		if strings.TrimSpace(in.Gap.Report) == "" {
			errs = append(errs, ValidationError{Code: "required", Field: "gap.report", Message: "gap.report is required when gap is present"})
		}
		if strings.TrimSpace(in.Gap.Moment) == "" {
			errs = append(errs, ValidationError{Code: "required", Field: "gap.moment", Message: "gap.moment is required when gap is present (what you were doing when you needed it)"})
		}
	}

	return errs
}

// validateRankings checks that the rankings cover EXACTLY the outstanding fb-ids
// (no missing, no extra, no duplicates), the ranks are a permutation of 1..N,
// and each reason is non-empty.
func validateRankings(rankings []events.FeedbackRanking, outstanding []string) []ValidationError {
	errs := make([]ValidationError, 0)

	want := make(map[string]struct{}, len(outstanding))
	for _, id := range outstanding {
		want[id] = struct{}{}
	}

	seen := make(map[string]struct{}, len(rankings))
	ranks := make([]int, 0, len(rankings))
	for i, r := range rankings {
		field := fmt.Sprintf("rankings[%d]", i)
		id := strings.TrimSpace(r.FeedbackID)
		if id == "" {
			errs = append(errs, ValidationError{Code: "required", Field: field + ".feedback_id", Message: "feedback_id is required"})
			continue
		}
		if _, dup := seen[id]; dup {
			errs = append(errs, ValidationError{Code: "duplicate", Field: field + ".feedback_id", Message: "feedback_id appears more than once in rankings", Value: id})
			continue
		}
		seen[id] = struct{}{}
		if _, ok := want[id]; !ok {
			errs = append(errs, ValidationError{Code: "unknown", Field: field + ".feedback_id", Message: "feedback_id is not outstanding for this session: rank only the fb-ids returned by `auto reflect select`", Value: id})
		}
		if strings.TrimSpace(r.Reason) == "" {
			errs = append(errs, ValidationError{Code: "required", Field: field + ".reason", Message: "reason is required for every ranked feedback_id"})
		}
		ranks = append(ranks, r.Rank)
	}

	// Every outstanding id must be covered.
	for _, id := range outstanding {
		if _, ok := seen[id]; !ok {
			errs = append(errs, ValidationError{Code: "missing", Field: "rankings", Message: "every outstanding feedback_id must be ranked", Value: id})
		}
	}

	// Ranks must be a permutation of 1..N where N = number of outstanding ids.
	if !isPermutation(ranks, len(outstanding)) {
		errs = append(errs, ValidationError{Code: "permutation", Field: "rankings.rank", Message: fmt.Sprintf("ranks must be a permutation of 1..%d (one per outstanding feedback_id)", len(outstanding))})
	}

	return errs
}

// isPermutation reports whether ranks is exactly {1, 2, ..., n}.
func isPermutation(ranks []int, n int) bool {
	if len(ranks) != n {
		return false
	}
	seen := make(map[int]struct{}, n)
	for _, r := range ranks {
		if r < 1 || r > n {
			return false
		}
		if _, dup := seen[r]; dup {
			return false
		}
		seen[r] = struct{}{}
	}
	return len(seen) == n
}

// GateResult reports the gate outcome: outstanding feedback ids that still block.
type GateResult struct {
	Outstanding []string
	SessionID   string
}

// Clean reports whether the gate passes (no outstanding feedback ids).
func (g GateResult) Clean() bool { return len(g.Outstanding) == 0 }

// GateCheck computes the outstanding feedback ids for the scope. The gate is
// clean (passes) when no feedback ids minted by selection events remain
// uncovered by feedback events. A session that consumed no rules passes.
func (s *Service) GateCheck(sessionOverride, since string) (GateResult, error) {
	repo, err := gitutil.DetectRepoLenient(s.cwd)
	if err != nil {
		return GateResult{}, err
	}

	all, err := events.ReadAll(repo.Root)
	if err != nil {
		return GateResult{}, err
	}

	scope := s.resolveScope(sessionOverride)
	if since != "" {
		d, err := parseLookback(since)
		if err != nil {
			return GateResult{}, err
		}
		scope.lookback = d
	}

	outstanding := outstandingForScope(all, scope)
	return GateResult{Outstanding: outstanding, SessionID: scope.sessionID}, nil
}

const defaultGateLookback = 24 * time.Hour

// scope captures how outstanding ids are bounded: by an explicit/detected
// session id, otherwise by this worktree's shards within a lookback window.
type scope struct {
	sessionID string
	host      string
	worktree  string // worktree root, for shard discrimination
	lookback  time.Duration
}

// resolveScope determines the gate/feedback scope. An explicit override or a
// detected session id scopes by session. Otherwise the scope falls back to this
// host + this worktree's shards within the lookback window.
func (s *Service) resolveScope(override string) scope {
	sc := scope{lookback: defaultGateLookback}
	sid := strings.TrimSpace(override)
	if sid == "" {
		sid = events.DetectSessionID()
	}
	sc.sessionID = sid

	if repo, err := gitutil.DetectRepoLenient(s.cwd); err == nil {
		sc.worktree = repo.Root
	}
	if _, host, _, err := config.EnsureHost(); err == nil {
		sc.host = host.HostID
	}
	return sc
}

// outstandingForScope returns the feedback ids minted by selection events within
// scope that are not yet covered by a feedback event. When a session id is
// known, scope is that session. Otherwise it falls back to this host + this
// worktree's shards within the lookback window.
func outstandingForScope(all []events.Event, sc scope) []string {
	covered := coveredFeedbackIDs(all)

	cutoff := time.Now().UTC().Add(-sc.lookback)
	outstanding := make([]string, 0)
	for i := range all {
		ev := &all[i]
		if ev.Type != events.TypeSelection {
			continue
		}
		if !selectionInScope(ev, sc, cutoff) {
			continue
		}
		var p events.SelectionPayload
		if decodePayload(ev, &p) != nil {
			continue
		}
		for _, it := range p.Items {
			if _, done := covered[it.FeedbackID]; done {
				continue
			}
			outstanding = append(outstanding, it.FeedbackID)
		}
	}
	sort.Strings(outstanding)
	return outstanding
}

// selectionInScope reports whether a selection event is in scope. With a session
// id, scope is exact session match. Without one, scope is the host plus the
// lookback window (shards are already this-worktree-only because ReadAll walks
// the local worktree's events dir).
func selectionInScope(ev *events.Event, sc scope, cutoff time.Time) bool {
	if sc.sessionID != "" {
		return ev.SessionID == sc.sessionID
	}
	if sc.host != "" && ev.Host != sc.host {
		return false
	}
	ts, err := time.Parse(time.RFC3339, ev.TS)
	if err != nil {
		// Undated selections are conservatively in scope.
		return true
	}
	return !ts.Before(cutoff)
}

// coveredFeedbackIDs returns the set of feedback ids that have at least one
// feedback event covering them.
func coveredFeedbackIDs(all []events.Event) map[string]struct{} {
	covered := make(map[string]struct{})
	for i := range all {
		ev := &all[i]
		if ev.Type != events.TypeFeedback {
			continue
		}
		var p events.FeedbackPayload
		if decodePayload(ev, &p) != nil {
			continue
		}
		for _, r := range p.Rankings {
			covered[r.FeedbackID] = struct{}{}
		}
	}
	return covered
}

// parseLookback parses a --since style duration (5m, 2h, 7d, 1w).
func parseLookback(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultGateLookback, nil
	}
	if len(s) < 2 {
		return 0, &LoopError{Message: fmt.Sprintf("invalid --since %q: use a value like 5m, 2h, 7d, or 1w", s)}
	}
	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil || n < 0 {
		return 0, &LoopError{Message: fmt.Sprintf("invalid --since %q: use a value like 5m, 2h, 7d, or 1w", s)}
	}
	switch unit {
	case 'm':
		return time.Duration(n) * time.Minute, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, &LoopError{Message: fmt.Sprintf("invalid --since %q: use a value like 5m, 2h, 7d, or 1w", s)}
	}
}

// decodePayload unmarshals an event's payload into dst.
func decodePayload(ev *events.Event, dst any) error {
	return json.Unmarshal(ev.Payload, dst)
}
